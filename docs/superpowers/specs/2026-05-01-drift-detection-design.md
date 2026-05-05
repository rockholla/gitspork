# Drift Detection Design

**Date:** 2026-05-01
**Issue:** [#28 Drift Detection Assistance](https://github.com/rockholla/gitspork/issues/28)
**Branch:** feat/drift-detection

## Summary

`gitspork integrate` is already a natural drift detector: running it at the exact upstream commit hash used in the last integration, against the current downstream state, produces a diff that represents drift. This feature makes that capability first-class by (a) recording the necessary upstream metadata in downstream state after each integration, and (b) providing a `gitspork check-drift` command that automates the full workflow safely.

---

## Section 1: State Tracking Changes

`GitSporkDownstreamState` in `internal/gitspork.go` gains three new fields:

```go
LastUpstreamRepoURL     string `json:"last_upstream_repo_url,omitempty"`
LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
LastUpstreamCommitHash  string `json:"last_upstream_commit_hash,omitempty"`
```

After a successful `Integrate` (remote) call, the resolved commit hash is read from the cloned repo (returned by `git.PlainClone`) and written to `.gitspork/downstream-state.json` alongside the URL and subpath used.

`integrate-local` does not update these fields — there is no remote URL or resolvable commit hash in that flow.

---

## Section 2: URL Rewriting

A new helper `resolveUpstreamURL(url string, token string) string` is called inside `cloneUpstreamForIntegrate`, which is the single clone site shared by both `integrate` and `check-drift`. The function takes two parameters: the URL to potentially rewrite, and the token (may be empty).

The caller (`CheckDrift`) is responsible for selecting which URL to use before calling `resolveUpstreamURL` — it checks whether `opts.UpstreamRepoURL` is non-empty (override) and falls back to `state.LastUpstreamRepoURL` if not. `resolveUpstreamURL` itself only handles SSH↔HTTPS rewriting; it has no override logic.

Resolution order:
1. If `SSH_AUTH_SOCK` is set and no token provided, and URL is HTTPS (`^https://`) → rewrite to SSH (`git@host:org/repo`).
2. If `SSH_AUTH_SOCK` is absent and a token is provided, and URL is SSH (`^git@`) → rewrite to HTTPS (`https://host/org/repo`).
3. Otherwise return the URL unchanged.

The rewrite handles the standard GitHub/GitLab/Bitbucket URL shape. Clone errors surface normally if the rewrite produces an unusable URL — no errors are swallowed.

This applies universally to `integrate` as well, so passing an SSH URL to `--upstream-repo-url` in an HTTPS-only environment (or vice versa) is handled automatically.

---

## Section 3: `check-drift` Internals

New function `CheckDrift(opts *CheckDriftOptions) error` in `internal/check-drift.go`.

```go
type CheckDriftOptions struct {
    Logger             *Logger
    DownstreamRepoPath string
    UpstreamRepoURL    string  // override; empty means use stored state
    UpstreamRepoToken  string
    Verbose            bool
}
```

Execution steps:

1. **Resolve downstream path** — default to CWD if empty, same pattern as `Integrate`.
2. **Load state** — call `loadDownstreamState`; if `LastUpstreamCommitHash` is empty, return a clear error: `"no previous integration found in downstream state — run 'gitspork integrate' first"`.
3. **Clean working tree check** — use go-git `wt.StatusWithOptions` to verify the downstream is clean; if not, return an error.
4. **Detached HEAD guard** — verify the downstream is on a branch (not detached HEAD).
5. **Create drift-check branch** — create or reset `_gitspork-check-drift` branch at current HEAD, check it out.
6. **Run `Integrate`** in-place against the drift-check branch using stored URL/subpath/hash (with `ForDriftCheck: true`) and the provided or stored token.
7. **Diff worktree against HEAD** using go-git — stage all changes in the drift-check branch, commit them, and compute the patch between HEAD and that new commit.
8. **Report:**
   - No diff → print "no drift detected", return `nil`.
   - Diff found → print summary (count of changed files); if `--verbose`, encode and print the patch; return `ErrDriftDetected` (a typed sentinel: `var ErrDriftDetected = errors.New("drift detected")`).
9. **CLI maps sentinel to exit code** — `cmd/check-drift.go` checks `errors.Is(err, internal.ErrDriftDetected)` and calls `os.Exit(1)`. The `CheckDrift` function itself does not call `os.Exit`.
10. **Cleanup** — a `defer` restores the original branch and deletes the drift-check branch.

---

## Section 4: CLI Surface

New `cmd/check-drift.go` wired into the root command.

```
gitspork check-drift [flags]

Flags:
  --downstream-repo-path   path to the downstream repo (default: CWD)
  --upstream-repo-url      override the upstream URL stored in state
  --upstream-repo-token    token for HTTPS auth
  --verbose                print full git diff output when drift is detected
```

**Exit codes:**
- `0` — no drift detected
- `1` — drift detected
- non-zero (other) — failure (bad state, unclean working tree, clone error, etc.)

---

## What Is Not In Scope

- `integrate-local` drift detection (no remote URL/hash to store or compare against)
- Any changes to existing `integrate` or `integrate-local` behavior beyond state recording and URL rewriting
