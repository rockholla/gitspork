# Drift-check scratch clone design

## Motivation

A downstream user of `cnd-upgrade-from-foundation` (which wraps
`gitspork.CheckDrift`) reported that running the upgrade in their local repo
clone silently wiped out gitignored files from their working directory. The
files were things a developer legitimately keeps around locally but doesn't
commit: `.envrc`, editor caches, local virtualenvs, `.env`, and similar.

Nothing in gitspork or the foundation wrapper calls `git clean` or
`os.RemoveAll` on the caller's working tree. The wipe path is subtler and
lives inside `internal/drift/check_drift.go`:

1. `checkCleanWorkingTree` uses shell `git status --porcelain`, which respects
   the user's local `.gitignore`, `core.excludesFile`, and global gitignore.
   A repo with only ignored untracked files reads as clean, and the drift
   check proceeds.
2. `CheckDrift` creates a `_gitspork-check-drift` branch at HEAD, re-integrates
   each upstream against the caller's working tree, then calls
   `diffWorktreeAgainstHEAD`, which does `wt.AddWithOptions(&AddOptions{All:
   true})` followed by a commit and, on exit, a `Force`-checkout back to the
   caller's original HEAD.
3. Steps 1 and 2 use **different** git implementations. The cleanliness gate
   uses shell git; the mutating flow uses go-git. go-git's `.gitignore`
   handling is not equivalent to shell git's — notably, it does not
   consistently honor `core.excludesFile` or the user's global gitignore, and
   its handling of nested `.gitignore` files has known gaps.
4. The consequence: `AddOptions{All: true}` can stage files that shell git
   considers ignored. Those files get written into the temporary drift-check
   commit. The deferred `Force` checkout back to the original HEAD then
   removes them from the working tree, because they are tracked in the
   temp commit but not in HEAD.

The existing "note: this may be running in a container with different global
gitignore rules" text in `checkCleanWorkingTree`'s error message shows the
maintainers already knew the two git implementations could disagree — but the
mitigation stops at the porcelain-status gate and doesn't extend to the
add-all-then-restore sequence downstream.

## Goal

Establish and enforce the invariant:

> `gitspork.CheckDrift` never mutates the caller's working directory.

The property must hold **by construction**, not by careful sequencing of
go-git and shell git operations. Any future change to the drift flow — new
integrator, new go-git version, new file-touching side effect — should
inherit the invariant automatically.

## Non-goals

- Fix go-git's `.gitignore` handling to match shell git's.
- Change the `DriftReport` shape or the public SDK contract.
- Relax the existing `checkCleanWorkingTree` gate. That gate protects a
  different property (the user's local edits to tracked files won't be
  silently overwritten later by `integrate`) and stays as-is.
- Handle concurrent `CheckDrift` invocations against the same caller repo.

## Design

Run all mutating drift-check steps against a scratch clone of the caller's
repo, provisioned via `git clone --local`. The caller's working tree is
observed for cleanliness and then never touched again.

### High-level flow

```
caller repo (clean)
  |
  |-- checkCleanWorkingTree(caller) -> ok
  |-- git clone --local <caller> <scratch>
  |-- git -C <scratch> checkout <caller-HEAD-hash>
  |
  v
scratch clone (identical HEAD, fresh worktree)
  |
  |-- PlainOpen(scratch), create _gitspork-check-drift branch
  |-- for each upstream: IntegrateForDriftCheck against scratch
  |-- diffWorktreeAgainstHEAD: add-all + commit + patch
  |
  v
DriftReport (paths are repo-relative -> identical whether scratch or caller)
  |
  |-- defer os.RemoveAll(scratch)
  v
return report, ErrDriftDetected | nil
```

### Why `git clone --local`

- **Cheap on time and disk.** `--local` hardlinks the object store when the
  source is on the same filesystem, so we don't copy pack files.
- **Self-contained.** The drift-check commit gets written into the scratch's
  own `.git/objects/`, not the caller's. Deleting the scratch dir cleans up
  everything.
- **No cross-repo linkage.** We deliberately don't use `--shared` (which
  wires the scratch to the caller via `objects/info/alternates`). That would
  be cheaper still, but adds a fragile cross-repo dependency and buys nothing
  for a short-lived synchronous operation.
- **Real repo semantics.** The scratch clone is a fully functional git repo,
  so every existing go-git operation in the drift flow works unchanged.

### Checkout by hash

`git clone --local` puts the scratch on whatever branch HEAD refers to. When
the caller is on a detached HEAD (typical for CI), we want to reproduce
exactly that commit in the scratch clone regardless of branch names. Approach:

```
git clone --local --no-checkout <caller> <scratch>
git -c safe.directory=* -C <scratch> checkout <caller-HEAD-hash>
```

`--no-checkout` skips the initial checkout so we can pin explicitly to the
hash. `-c safe.directory=*` matches what the existing shell-git call sites
use — CI environments frequently need it.

### State file handling

`.gitspork/downstream-state.json` is normally committed, so `git clone`
brings it over. Edge case: the caller ran integrate but hasn't committed the
state file yet, and it's `.gitignore`d out. Cleanliness gate wouldn't catch
this because the file is ignored.

After provisioning the scratch clone, verify that `.gitspork/downstream-state.json`
exists in the scratch. If it doesn't but exists in the caller, copy it
across. This is a small belt-and-suspenders guard, not a behavioral commitment
— we may tighten this to an error later.

## Components

Scoped entirely to `internal/drift/`.

### `internal/drift/scratch_clone.go` (new)

```go
// provisionScratchClone clones callerRepoPath into a temporary directory
// pinned to caller HEAD's exact hash. Returns the scratch path plus a
// cleanup func that removes the scratch dir.
func provisionScratchClone(callerRepoPath string) (scratchPath string, cleanup func(), err error)
```

Implementation:

- `os.MkdirTemp("", "gitspork-drift-*")` for the temp root.
- Resolve caller HEAD hash via `git -c safe.directory=* -C <caller> rev-parse HEAD`.
- `git -c safe.directory=* clone --local --no-checkout <caller> <scratch>`.
- `git -c safe.directory=* -C <scratch> checkout <hash>`.
- Cleanup: `os.RemoveAll(scratchPath)`, idempotent.
- Wrap subprocess stderr into the returned errors so failures surface useful
  detail.

```go
// ensureStateFilePresent copies .gitspork/downstream-state.json from caller
// into scratch when it exists in caller but not in scratch (edge case where
// the caller has not committed the state file yet).
func ensureStateFilePresent(callerRepoPath, scratchPath string) error
```

### `internal/drift/check_drift.go` (edit)

Before the current worktree-mutating code, provision the scratch clone and
defer its cleanup:

```go
scratchPath, cleanup, err := provisionScratchClone(opts.DownstreamRepoPath)
if err != nil {
    return report, fmt.Errorf("error provisioning scratch clone for drift-check: %w", err)
}
defer cleanup()

if err := ensureStateFilePresent(opts.DownstreamRepoPath, scratchPath); err != nil {
    return report, fmt.Errorf("error preparing scratch clone: %w", err)
}
```

Replace every mutating use of `opts.DownstreamRepoPath` with `scratchPath`:

- `gogit.PlainOpen(scratchPath)`
- The `DriftCheckRequest.DownstreamRepoPath` set to `scratchPath` inside the
  upstream loop.
- Both `listWorktreeFiles(scratchPath)` calls.

Keep `opts.DownstreamRepoPath` for the two read-only uses that remain:

- `LoadDownstreamState(opts.DownstreamRepoPath)`.
- `checkCleanWorkingTree(opts.DownstreamRepoPath)`.

Delete the following, which existed only to defend the caller's working
tree from the mutating flow:

- The `restore := &gogit.CheckoutOptions{...}` construction.
- The `defer func() { _ = wt.Checkout(restore); _ = repo.Storer.RemoveReference(driftBranchRef) }()`
  block.
- The comments narrating why `Force: true` is required for `restore`.

In the scratch clone we deliberately do not care what state the drift branch
is in when the directory is removed.

### Unchanged

- `internal/integrate/drift_check.go` (`IntegrateForDriftCheck`) — accepts
  a downstream path; we now pass it a scratch path. No code changes.
- Public SDK surface in `gitspork.go` — `CheckDriftOptions`, `DriftReport`,
  `ErrDriftDetected`.
- `internal/cli/check_drift.go` — the CLI wrapper.
- The `cnd-upgrade-from-foundation` wrapper — no change needed.

## Data flow and error handling

### Failure disposition

| Failure point | Caller working tree | Behavior |
|---|---|---|
| `checkCleanWorkingTree` fails | untouched | Existing error, unchanged |
| `git clone --local` fails | untouched | `error provisioning scratch clone for drift-check: <stderr>` |
| Scratch `git checkout <hash>` fails | untouched | Same wrap; cleanup runs via defer |
| `PlainOpen(scratch)` fails | untouched | Wrapped error; cleanup runs |
| `IntegrateForDriftCheck` fails | untouched | Existing error propagation; caller safe |
| `diffWorktreeAgainstHEAD` fails | untouched | Existing behavior |
| `os.RemoveAll(scratch)` fails | untouched | Log-and-swallow (matches existing tempdir cleanup pattern) |

The invariant "caller working tree untouched after `checkCleanWorkingTree`
passes" holds under all failure modes, including panics. Panics may leak the
scratch dir (until the OS reclaims `/tmp`), but the caller's repo is safe.

### Progress narration

New `Log` calls routed through `opts.Logger`:

- `"provisioning scratch clone of %s"` before clone.
- `"running drift-check against scratch clone at %s"` after clone.

Cleanup is silent — matches the existing tempdir-cleanup convention.

## Testing

### Regression: reported bug

`internal/drift/check_drift_test.go` — new test that reproduces the report:

- Set up a downstream repo with a normal upstream_owned integration recorded
  in state.
- Populate the caller's working tree with files that shell git ignores but
  go-git wouldn't confidently ignore: `.envrc`, `.env`, `node_modules/foo`,
  plus a file matched by a global gitignore pattern (hermetic via
  `GIT_CONFIG_GLOBAL` pointing at a tempfile).
- Run `CheckDrift`.
- Assert: every one of those untracked files is still present in the caller's
  dir afterward with byte-identical contents.
- Assert: the returned `DriftReport` matches expected drift semantics
  (independent of the untracked-file property).

### Invariant: never mutates the caller

Second test that expresses the property directly:

- Snapshot the SHA-256 of every file in the caller's working tree (excluding
  `.git/`) before `CheckDrift`.
- Run `CheckDrift` (both drift-present and drift-absent scenarios).
- Snapshot again. Assert set equality and byte equality for every file.

This test locks in the invariant even if the specific ignored-file bug ever
re-surfaces via a different mechanism.

### `scratch_clone_test.go` (new)

`provisionScratchClone`:

- Returns a directory containing the caller's HEAD tracked files at the
  expected commit hash.
- `cleanup` is idempotent (calling twice does not panic).
- Fails cleanly when the source path is not a git repo.
- Handles a detached-HEAD source (CI-style checkout by hash).

### Existing tests

Audit and update any assertion in `check_drift_test.go` / related tests that
reasons about internal side effects on the caller repo — most notably any
assertion about the `_gitspork-check-drift` branch existing then being
cleaned up. That branch now lives in the scratch clone and dies with it.
Public behavior (returned report, sentinel error) is unchanged, so
public-facing tests should not need edits.

### Explicitly out of scope

- Concurrent `CheckDrift` runs against the same caller repo.
- Performance / large-repo timing. `--local` is fast enough that a dedicated
  perf test is not earning its keep.

## Migration and rollout

- No API changes. Downstream consumers (including
  `cnd-upgrade-from-foundation`) get the fix by upgrading their gitspork
  dependency and re-cutting.
- No state migration. The `.gitspork/downstream-state.json` schema and
  contents are unchanged.
- No feature flag. The scratch-clone flow is strictly safer than the current
  in-place flow, and there is no scenario in which a caller would want the
  old behavior.

## Rejected alternatives

- **Fix the go-git/shell-git ignore disagreement in-place.** Replace the
  go-git `AddOptions{All: true}` + `Force` checkout with shell git
  equivalents. Rejected: patches one symptom but leaves gitspork mutating
  the caller's working tree, so any future path that walks a similar
  sequence can reintroduce the same class of bug.
- **Detect + refuse.** Compute `git ls-files --others` and compare against
  go-git's view; refuse when they disagree. Rejected: doesn't fix anything,
  only refuses more often; leaves a landmine for other flows; users get
  "gitspork won't run in my clone" errors with no local remedy.
- **`--shared` clone.** Skips the object copy via `objects/info/alternates`.
  Rejected: adds cross-repo linkage that buys nothing for a synchronous,
  short-lived operation.
- **Fix inside the foundation wrapper only.** Rejected during brainstorming:
  every gitspork caller benefits from the invariant, and duplicating the
  scratch-clone logic in each wrapper is worse than fixing it once at the
  source.
