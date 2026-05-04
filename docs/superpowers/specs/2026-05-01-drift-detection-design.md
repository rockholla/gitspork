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

A new helper `resolveUpstreamURL(storedURL string, overrideURL string, token string) string` is called inside `cloneUpstreamForIntegrate`, which is the single clone site shared by both `integrate` and `check-drift`.

Resolution order:
1. If `overrideURL` is non-empty, return it immediately.
2. If `SSH_AUTH_SOCK` is set and no token provided, and stored URL is HTTPS (`^https://`) → rewrite to SSH (`git@host:org/repo`).
3. If `SSH_AUTH_SOCK` is absent and a token is provided, and stored URL is SSH (`^git@`) → rewrite to HTTPS (`https://host/org/repo`).
4. Otherwise return the stored/provided URL unchanged.

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
3. **Clean working tree check** — run `git status --porcelain` in the downstream path; if any output, return error asking user to commit or stash first.
4. **Copy downstream to temp dir** — `os.MkdirTemp` + recursive file copy, excluding the `.git` directory.
5. **`git init` in temp dir** — `git add -A` + initial commit to establish a baseline for diffing.
6. **Run `Integrate`** against the temp dir using stored URL/subpath/hash (with URL rewriting via `cloneUpstreamForIntegrate`) and the provided or stored token.
7. **Run `git diff`** in the temp dir; capture output.
8. **Report:**
   - No diff → print "no drift detected", exit 0.
   - Diff found → print summary (count of changed files); if `--verbose`, print full colorized diff; exit 1.
9. **Cleanup** — `defer os.RemoveAll(tempDir)`.

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
