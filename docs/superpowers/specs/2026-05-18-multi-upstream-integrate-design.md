# Multi-Upstream Integration Design

## Goal

Allow `gitspork integrate` and `gitspork integrate-local` to accept multiple upstream sources in a single invocation, with explicit left-to-right precedence. Update `check-drift` to re-integrate all recorded upstreams and report aggregated, per-upstream-attributed drift.

## Architecture

The change touches four layers: CLI flags, `IntegrateOptions`/`CheckDriftOptions` structs, the `Integrate`/`CheckDrift` implementations, and the downstream state schema. All changes are backward compatible — existing single-upstream downstreams continue working without any manual migration.

## Tech Stack

Go, `github.com/spf13/cobra` (repeatable flags), existing go-git and gitspork internals.

---

## Section 1: New `UpstreamSpec` Type

A new shared type in `internal/gitspork.go`:

```go
type UpstreamSpec struct {
    URL     string
    Version string
    Subpath string
    Token   string
}
```

For `integrate-local`, upstreams are represented as plain `[]string` paths (no version/token concept).

---

## Section 2: CLI Interface

### `gitspork integrate`

New repeatable `--upstream` flag accepting comma-separated `key=value` pairs:

```
gitspork integrate \
  --upstream "url=git@github.com:org/base.git,version=main" \
  --upstream "url=git@github.com:org/platform.git,version=v1.2.0,subpath=infra,token=ghp_..."
```

Valid keys: `url`, `version`, `subpath`, `token`. All except `url` are optional.

Existing single-value flags (`--upstream-repo-url`, `--upstream-repo-version`, `--upstream-repo-subpath`, `--upstream-repo-token`) are retained for backward compatibility. If both old flags and `--upstream` are provided in the same invocation, the command returns an error.

Precedence: left-to-right. Later `--upstream` entries win when the same file is touched by multiple upstreams.

### `gitspork integrate-local`

`--upstream-path` becomes repeatable. Multiple values are accepted in order:

```
gitspork integrate-local \
  --upstream-path /path/to/base \
  --upstream-path /path/to/platform
```

The existing single `--upstream-path` flag continues to work as a single-entry shorthand.

### `gitspork check-drift`

Old `--upstream-repo-url` single-override flag is removed. New repeatable `--upstream` flag (same syntax as `integrate`) overrides the full upstream list stored in state. When not provided, state is used. When provided, the override list replaces state entirely for that run (state is not modified).

---

## Section 3: State Schema

`GitSporkDownstreamState` replaces the three single-upstream fields with a slice:

```go
type GitSporkUpstreamState struct {
    URL        string `json:"url"`
    Subpath    string `json:"subpath,omitempty"`
    CommitHash string `json:"commit_hash"`
}

type GitSporkDownstreamState struct {
    MigrationsComplete []string                 `json:"migrations_complete"`
    Upstreams          []GitSporkUpstreamState  `json:"upstreams,omitempty"`

    // Deprecated: migrated to Upstreams on first load
    LastUpstreamRepoURL     string `json:"last_upstream_repo_url,omitempty"`
    LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
    LastUpstreamCommitHash  string `json:"last_upstream_commit_hash,omitempty"`
}
```

**Migration:** `loadDownstreamState` checks if `Upstreams` is empty and the deprecated fields are non-empty. If so, it synthesizes a single-entry `Upstreams` slice from the deprecated fields. The deprecated fields are cleared and the migrated state is written back on the next `saveDownstreamState` call (which happens at the end of every successful `integrate` run).

**Upsert key:** entries in `Upstreams` are matched by normalized URL + subpath. URL normalization strips protocol prefix (`https://`, `git@`), converts `:` to `/` (SSH form), and strips trailing `.git`. This ensures `git@github.com:org/repo.git` and `https://github.com/org/repo.git` match the same state entry.

**Order:** `Upstreams` preserves integration order. New entries are appended; existing entries (matched by normalized URL + subpath) are updated in place.

---

## Section 4: `Integrate` Implementation

`IntegrateOptions` gains:

```go
Upstreams []UpstreamSpec  // populated from --upstream flags
```

The existing single-value fields (`UpstreamRepoURL`, `UpstreamRepoVersion`, etc.) remain for internal use. Before the integrate loop begins, the command layer normalizes: if `Upstreams` is empty and `UpstreamRepoURL` is set, a single-entry `Upstreams` is synthesized.

`Integrate` loops through `Upstreams` in order. For each entry it calls the existing single-upstream integrate logic (clone, parse config, run integrators, run migrations). State is upserted after each upstream completes successfully. If any upstream fails, the loop stops and returns the error — state reflects however far it got, so re-running is safe and idempotent.

`IntegrateLocal` follows the same pattern: `IntegrateLocalOptions` gains `UpstreamPaths []string`. Single `UpstreamPath` field is retained for internal use; the command layer synthesizes `UpstreamPaths` from whichever was provided. `IntegrateLocal` does not write to downstream state (unchanged from current behavior).

---

## Section 5: `CheckDrift` Implementation

`CheckDriftOptions` gains:

```go
Upstreams []UpstreamSpec  // populated from --upstream flags; if empty, loaded from state
```

The existing `UpstreamRepoURL` and `UpstreamRepoToken` single-override fields are removed.

**Flow:**

1. Resolve upstream list: use `opts.Upstreams` if provided, otherwise load from `state.Upstreams`. Error if neither yields any entries.
2. If using overrides, match each override to its state entry by normalized URL + subpath to retrieve the recorded `commit_hash`. If an override entry has no matching state entry, error with a clear message.
3. Create the isolated temp downstream copy (existing mechanism).
4. For each upstream in order, re-integrate at the recorded commit hash into the temp copy (`ForDriftCheck: true`). Track which files are written/modified during each upstream's pass by diffing the worktree before and after each integrate call.
5. After all upstreams have been re-integrated, run a single `diffWorktreeAgainstHEAD` on the temp copy.
6. Map each changed file in the final diff back to the upstream that last touched it (using the per-pass tracking from step 4).
7. Report: grouped by upstream, showing URL and commit hash, then the files/hunks attributed to that upstream. Verbose mode (`--verbose`) prints full diff hunks per group.

Exit codes remain: `0` no drift, `1` error, `2` drift detected.

---

## Section 6: Error Handling

- Providing both `--upstream` and old single flags together: immediate error before any work begins.
- `--upstream` flag with missing `url` key: immediate parse error.
- `check-drift` `--upstream` override entry with no matching state entry (no recorded commit hash): error with message naming the unmatched upstream. This does not apply to `integrate`, where an `--upstream` entry with no prior state record is the normal first-time integration case.
- Any upstream failing mid-loop in `integrate`: stop, return error, state reflects completed upstreams.
- `check-drift` with no upstreams in state and no `--upstream` flags: error directing user to run `integrate` first.

---

## Section 7: Testing

**Unit tests:**
- `parseUpstreamFlag` — all key/value combinations, missing url, unknown keys
- URL normalization — SSH↔HTTPS equivalence, trailing `.git`, subpath matching
- `loadDownstreamState` migration — deprecated fields → `Upstreams` slice
- `upsertUpstreamState` — new entry appended, existing entry updated in place, order preserved

**Functional tests:**
- `integrate` with two upstreams: assert later upstream files overwrite earlier
- `integrate` backward compat: old single flags still work
- `check-drift` multi-upstream: no drift, drift in first upstream only, drift in second upstream only, drift in both
- `check-drift` with `--upstream` override: URL protocol switch (SSH→HTTPS) matches correct state entry
- `integrate-local` with two upstream paths: assert precedence

---

## Backward Compatibility

| Scenario | Behavior |
|---|---|
| Existing downstream with old state fields | Auto-migrated to `Upstreams` on first `integrate` or `check-drift` run |
| `integrate` with single `--upstream-repo-url` | Converted to single-entry `Upstreams` internally, identical behavior |
| `check-drift` with old `--upstream-repo-url` flag | Flag removed; users must use `--upstream "url=..."` instead — **breaking change on this flag only** |
| `integrate-local` with single `--upstream-path` | Converted to single-entry `UpstreamPaths` internally, identical behavior |
