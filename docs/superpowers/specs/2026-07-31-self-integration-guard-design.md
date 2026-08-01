# Self-Integration Guard Design

**Date:** 2026-07-31
**Motivation:** `integrate`, `integrate-local`, and `check-drift` have no safety check preventing them from operating when the upstream and the downstream are the same repo. Running `gitspork integrate` from inside a clone of the very upstream it's supposed to pull from, or pointing `integrate-local` at a directory that resolves to the downstream itself, silently proceeds and can clobber the repo it's meant to serve.
**Branch:** `feat/self-integration-guard`

## Summary

Add a single guard function, `EnsureNotSelfIntegration`, in `internal/integrate/self_guard.go`. It's called at the top of every SDK-level integration entrypoint — `integrateOneInternal` (which serves both public `Integrate` and `check-drift` via `IntegrateForDriftCheck`) and the per-upstream loop in `IntegrateLocal`. The guard runs two independent checks; either match fails fast with a hard error. There is no bypass flag.

- **Path check** (when a local upstream path is provided): resolve absolute paths + symlinks on both sides; if the upstream path sits at or inside the downstream repo tree, or vice versa, error.
- **URL check** (when an upstream URL is provided): open the downstream as a git repo; if it has an `origin` remote whose normalized URL matches the normalized upstream URL, error. Downstream not a git repo, or has no `origin`, silently passes. Only `origin` is checked — other remote names (e.g. `upstream`) are legitimate developer conveniences and must not trip the guard.

Failures return a sentinel error `sdktypes.ErrSelfIntegration` (wrapped with `%w`) so callers can `errors.Is` on it. The CLI intercepts the sentinel in each subcommand's `RunE` and exits with code **3** (1 = generic error, 2 = drift detected, 3 = self-integration blocked).

---

## Section 1: Placement & call sites

### New file

`internal/integrate/self_guard.go` — kept out of `integrate.go` because it's a self-contained concern with its own tests. Companion test file `internal/integrate/self_guard_test.go` (no build tag, runs under `make test-unit`).

### Exported entrypoint

```go
// EnsureNotSelfIntegration errors when the upstream identity matches the
// downstream repo. Called at the top of every integration path so both the
// CLI and any SDK consumer honor the guard.
//
// upstreamURL: the upstream repo URL (empty for pure local-path integrations).
// upstreamLocalPath: absolute or relative local path to the upstream when
//     invoked via IntegrateLocal (empty otherwise).
// downstreamRepoPath: the downstream repo path (already absolutized by callers).
func EnsureNotSelfIntegration(downstreamRepoPath, upstreamURL, upstreamLocalPath string) error
```

### Call sites

1. `internal/integrate/integrate.go` — top of `integrateOneInternal`, immediately after `upstream.Subpath = config.NormalizeUpstreamPath(upstream.Subpath)`. Passes `upstream.URL` as `upstreamURL` and `""` as `upstreamLocalPath`. Covers **both** public `Integrate` (via `integrateOne` → `integrateOneInternal`) **and** `check-drift` (via `IntegrateForDriftCheck` → `integrateOneInternal`).
2. `internal/integrate/integrate_local.go` — top of the per-upstream loop in `IntegrateLocal`, before `getGitSporkConfig`. Passes `""` as `upstreamURL` and the current `upstreamPath` as `upstreamLocalPath`.

Placing the guard at the SDK boundary rather than in the CLI ensures any consumer of `github.com/rockholla/gitspork/v2` (including future integrations) picks it up automatically.

---

## Section 2: Matching logic

Two independent checks; either one triggering fails fast. Path check runs first (cheap, stat-only) then URL check (opens the git repo).

### Path check

Runs whenever `upstreamLocalPath != ""`.

1. `filepath.Abs` both `upstreamLocalPath` and `downstreamRepoPath`. Bubble any error.
2. Resolve symlinks on both via `filepath.EvalSymlinks` (best-effort). If either path doesn't exist yet, fall back to the plain abs path for that side — this avoids false negatives from a not-yet-created directory.
3. If the two resolved paths are equal → self-integration.
4. Otherwise, compute `rel, err := filepath.Rel(downstreamAbs, upstreamAbs)`. If `err == nil` and `rel` is not `"."`, does not equal `".."`, and does not start with `"../"` (or the OS separator equivalent), the upstream sits inside the downstream tree → self-integration.
5. Symmetric check: swap the arguments and repeat step 4 to catch the reverse (downstream inside upstream).
6. On match, return an error naming both paths (see Section 3).

### URL check

Runs whenever `upstreamURL != ""`.

1. `gogit.PlainOpen(downstreamRepoPath)`. If it returns `git.ErrRepositoryNotExists`, skip the URL check silently — there's nothing to compare against. Any other error bubbles up.
2. `repo.Remote("origin")`. If it returns `git.ErrRemoteNotFound`, skip silently. Only `origin` is checked — other remote names (e.g. `upstream`) are legitimate developer conveniences for manual integration testing and must not trip the guard.
3. Iterate `origin.Config().URLs` — remotes can carry multiple URLs (fetch vs. push).
4. For each remote URL, compare `NormalizeUpstreamURL(remoteURL, "")` to `NormalizeUpstreamURL(upstreamURL, "")`. Subpath is intentionally ignored — same URL with a different subpath still targets the same physical repo.
5. On match, return an error naming both the upstream URL and the matching origin URL (see Section 3).

### Deliberately omitted

- **Git identity fingerprinting** (comparing root-commit hashes, e.g.). Rejected — heavier, slows the happy path, and the guard is a safety net, not a proof.
- **Non-origin remote comparison.** Rejected — false-positive-prone; a developer adding the upstream as a convenience remote named `upstream` for manual testing is common and legitimate.
- **Bypass flag / env var.** Rejected — there is no legitimate use case for integrating a repo against itself.

Consequence of these choices: a fresh downstream with no `origin` remote silently passes the URL check. That's acceptable — the guard's job is to catch the obvious self-integration cases, not to make it impossible.

---

## Section 3: Error surface & user-visible behavior

### Sentinel error

Add to `internal/sdktypes/errors.go`, co-located with existing `ErrDriftDetected`:

```go
// ErrSelfIntegration is returned when integrate / integrate-local / check-drift
// detects that the upstream identifies the same repo as the downstream.
var ErrSelfIntegration = errors.New("upstream and downstream identify the same repo")
```

Re-export from `gitspork.go` alongside `ErrDriftDetected` so SDK consumers can `errors.Is(err, gitspork.ErrSelfIntegration)`.

### Error messages

`EnsureNotSelfIntegration` wraps the sentinel with `%w` and a message specific to the check that triggered:

- **Path match:**
  ```
  self-integration blocked: upstream local path resolves inside the downstream repo (upstream=<abs>, downstream=<abs>) — cannot integrate a repo against itself
  ```
- **URL match:**
  ```
  self-integration blocked: upstream URL matches the downstream's origin remote (upstream=<url>, origin=<matched-url>) — cannot integrate a repo against itself
  ```

Each message states the trigger, names the specific colliding values, and ends with the reason. No "how to bypass" hint — there is no bypass.

### CLI exit code

- **Exit code 3** for `ErrSelfIntegration`, consistent across `integrate`, `integrate-local`, and `check-drift`.
- 1 = generic error (existing default via cobra `RunE`), 2 = drift detected (existing special case in `check-drift`), 3 = new.
- In each of `internal/cli/integrate.go`, `internal/cli/integrate_local.go`, and `internal/cli/check_drift.go`, add an intercept right after the SDK call, matching the existing `check-drift` pattern:

```go
if err != nil {
    if errors.Is(err, sdktypes.ErrSelfIntegration) {
        logger.Log("%v", err)
        os.Exit(3)
    }
    return err
}
```

Exit codes are not documented in `--help` output. Instead, add a short "Exit codes" reference to the CLI-level docs (`docs/` or the relevant reference doc) covering all three of 1/2/3.

### Propagation from check-drift

`CheckDrift` sets up a temporary drift-check branch before calling `IntegrateForDriftCheck`, which calls `integrateOneInternal`, where the guard runs. When the guard returns, the error bubbles up through the existing `defer` in `CheckDrift` that restores HEAD and removes the drift branch — no additional cleanup is needed.

---

## Section 4: Testing

### Unit tests — `internal/integrate/self_guard_test.go` (no build tag; `make test-unit`)

Table-driven around `EnsureNotSelfIntegration`.

**Path check cases:**
- upstream path == downstream path → error
- upstream path is a subdirectory of downstream (e.g. `<downstream>/templates`) → error
- downstream is a subdirectory of upstream → error
- unrelated absolute paths → no error
- upstream path doesn't exist yet (falls back to plain abs path) → no error when unrelated
- symlink resolution: temp symlink pointing into the downstream tree, upstream path uses the symlink → error after `EvalSymlinks`
- `upstreamLocalPath == ""` → path check skipped

**URL check cases** — build a real temp git repo per subtest with `git.PlainInit` + `repo.CreateRemote`:
- downstream not a git repo → URL check skipped, no error
- downstream has no origin remote → skipped, no error
- origin URL matches upstream URL exactly → error
- origin URL is SSH form, upstream URL is HTTPS form of the same repo → error (exercises `NormalizeUpstreamURL`)
- origin URL and upstream URL differ → no error
- downstream has an `upstream` remote matching, but `origin` doesn't → no error (validates the origin-only decision)
- origin has multiple URLs, one matches → error

**Error identity:**
- One case per branch asserting `errors.Is(err, sdktypes.ErrSelfIntegration)` and that the message contains the offending value (path pair or URL pair).

### Functional tests — `test/functional/` (build tag `functional`)

One scenario per command, using the compiled binary and a synthetic upstream/downstream. Proves the guard fires end-to-end through the CLI + exit code.

- `integrate` scenario: downstream is a git repo whose `origin` points at the upstream URL → assert exit code 3 and message on stderr.
- `integrate-local` scenario: upstream path == downstream path → assert exit code 3.
- `check-drift` scenario: stored state URL matches the downstream's origin remote → assert exit code 3.

### SDK sentinel re-export test — `test/sdk/` (build tag `sdk`)

One focused test: call `gitspork.Integrate` with a downstream whose `origin` matches the upstream URL, assert `errors.Is(err, gitspork.ErrSelfIntegration)`. Validates that the sentinel is exported and preserved across the `sdktypes` → root package boundary.

### Skipped test tiers

- `test/functional_docker/` — exercises the same binary paths; no additional coverage for a pure Go path/config guard.
- `test/examples/` — no example docs change.
