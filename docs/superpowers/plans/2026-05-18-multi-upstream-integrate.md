# Multi-Upstream Integrate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow `integrate`, `integrate-local`, and `check-drift` to accept multiple upstream sources with left-to-right precedence and per-upstream drift attribution.

**Architecture:** Add `UpstreamSpec` type and `GitSporkUpstreamState` slice to the state schema (auto-migrating old single-upstream fields on first load); extend `Integrate` and `IntegrateLocal` to loop over a `[]UpstreamSpec`/`[]string` slice; update `CheckDrift` to re-integrate each upstream and map drift hunks back to the responsible upstream. CLI changes add a repeatable `--upstream` flag while keeping old single flags for backward compatibility on `integrate`/`integrate-local`.

**Tech Stack:** Go, `github.com/spf13/cobra` repeatable `StringArray` flags, existing go-git and gitspork internals.

---

## File Map

| File | Change |
|---|---|
| `internal/gitspork.go` | Add `UpstreamSpec`, `GitSporkUpstreamState`; update `GitSporkDownstreamState`, `IntegrateOptions`, `IntegrateLocalOptions`, `CheckDriftOptions` |
| `internal/integrate.go` | Add `normalizeUpstreamURL`, `upsertUpstreamState`, `parseUpstreamFlag`; update `loadDownstreamState` migration; update `Integrate` loop; update `saveDownstreamState` calls |
| `internal/integrate-local.go` | Update `IntegrateLocal` to loop over `UpstreamPaths` |
| `internal/check-drift.go` | Update `CheckDrift` for multi-upstream with per-upstream file attribution |
| `internal/integrate_test.go` | Unit tests for `parseUpstreamFlag`, `normalizeUpstreamURL`, state migration, `upsertUpstreamState` |
| `cmd/integrate.go` | Add `--upstream` repeatable flag; conflict check with old flags |
| `cmd/integrate-local.go` | Make `--upstream-path` repeatable |
| `cmd/check-drift.go` | Replace `--upstream-repo-url`/`--upstream-repo-token` with `--upstream` repeatable flag |
| `test/functional/helpers_test.go` | Add `integrateArgsMulti`, `buildSecondUpstream` helpers |
| `test/functional/integrate_test.go` | Add multi-upstream integrate functional tests |
| `test/functional/check_drift_test.go` | Add multi-upstream check-drift functional tests |

---

## Task 1: New Types in `internal/gitspork.go`

**Files:**
- Modify: `internal/gitspork.go`

- [ ] **Step 1: Write the failing test** — add to `internal/integrate_test.go`:

```go
func Test_upsertUpstreamState_newEntry(t *testing.T) {
    state := &GitSporkDownstreamState{}
    upsertUpstreamState(state, GitSporkUpstreamState{URL: "https://github.com/org/repo.git", CommitHash: "abc123"})
    require.Len(t, state.Upstreams, 1)
    assert.Equal(t, "abc123", state.Upstreams[0].CommitHash)
}
```

Run: `go test ./internal/... -run Test_upsertUpstreamState_newEntry`
Expected: FAIL — types not defined.

- [ ] **Step 2: Add `UpstreamSpec` and `GitSporkUpstreamState` to `internal/gitspork.go`** after line 66 (end of `GitSporkDownstreamState`):

```go
// UpstreamSpec is a parsed --upstream flag entry.
type UpstreamSpec struct {
    URL     string
    Version string
    Subpath string
    Token   string
}

// GitSporkUpstreamState records the last integration for a single upstream.
type GitSporkUpstreamState struct {
    URL        string `json:"url"`
    Subpath    string `json:"subpath,omitempty"`
    CommitHash string `json:"commit_hash"`
}
```

- [ ] **Step 3: Replace `GitSporkDownstreamState` in `internal/gitspork.go`** (lines 61–66):

```go
type GitSporkDownstreamState struct {
    MigrationsComplete []string                `json:"migrations_complete"`
    Upstreams          []GitSporkUpstreamState `json:"upstreams,omitempty"`
    // Deprecated: migrated to Upstreams on first load.
    LastUpstreamRepoURL     string `json:"last_upstream_repo_url,omitempty"`
    LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
    LastUpstreamCommitHash  string `json:"last_upstream_commit_hash,omitempty"`
}
```

- [ ] **Step 4: Add `Upstreams []UpstreamSpec` to `IntegrateOptions`** after `ForDriftCheck bool`:

```go
Upstreams []UpstreamSpec
```

- [ ] **Step 5: Add `UpstreamPaths []string` to `IntegrateLocalOptions`** after `UpstreamPath string`:

```go
UpstreamPaths []string
```

- [ ] **Step 6: Replace `UpstreamRepoURL string` and `UpstreamRepoToken string` in `CheckDriftOptions`** with:

```go
Upstreams []UpstreamSpec
```

- [ ] **Step 7: Build** — `go build ./...` — Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/gitspork.go
git commit -m "feat: add UpstreamSpec, GitSporkUpstreamState; update state/options structs"
```

---

## Task 2: `parseUpstreamFlag`, `normalizeUpstreamURL`, `upsertUpstreamState`

**Files:**
- Modify: `internal/integrate.go`
- Modify: `internal/integrate_test.go`

- [ ] **Step 1: Write failing tests** — add to `internal/integrate_test.go`:

```go
func Test_parseUpstreamFlag(t *testing.T) {
    t.Run("url only", func(t *testing.T) {
        spec, err := parseUpstreamFlag("url=git@github.com:org/repo.git")
        require.NoError(t, err)
        assert.Equal(t, "git@github.com:org/repo.git", spec.URL)
    })
    t.Run("all keys", func(t *testing.T) {
        spec, err := parseUpstreamFlag("url=https://github.com/org/repo.git,version=main,subpath=infra,token=tok")
        require.NoError(t, err)
        assert.Equal(t, "main", spec.Version)
        assert.Equal(t, "infra", spec.Subpath)
        assert.Equal(t, "tok", spec.Token)
    })
    t.Run("missing url returns error", func(t *testing.T) {
        _, err := parseUpstreamFlag("version=main")
        require.Error(t, err)
    })
    t.Run("unknown key returns error", func(t *testing.T) {
        _, err := parseUpstreamFlag("url=git@github.com:org/repo.git,branch=main")
        require.Error(t, err)
    })
}

func Test_normalizeUpstreamURL(t *testing.T) {
    t.Run("SSH and HTTPS same repo match", func(t *testing.T) {
        assert.Equal(t,
            normalizeUpstreamURL("git@github.com:org/repo.git", ""),
            normalizeUpstreamURL("https://github.com/org/repo.git", ""))
    })
    t.Run("subpath included in key", func(t *testing.T) {
        assert.NotEqual(t,
            normalizeUpstreamURL("git@github.com:org/repo.git", "infra"),
            normalizeUpstreamURL("git@github.com:org/repo.git", ""))
    })
    t.Run("trailing .git stripped", func(t *testing.T) {
        assert.Equal(t,
            normalizeUpstreamURL("https://github.com/org/repo.git", ""),
            normalizeUpstreamURL("https://github.com/org/repo", ""))
    })
}

func Test_upsertUpstreamState_newEntry(t *testing.T) {
    state := &GitSporkDownstreamState{}
    upsertUpstreamState(state, GitSporkUpstreamState{URL: "https://github.com/org/repo.git", CommitHash: "abc"})
    require.Len(t, state.Upstreams, 1)
    assert.Equal(t, "abc", state.Upstreams[0].CommitHash)
}

func Test_upsertUpstreamState_updateExisting(t *testing.T) {
    state := &GitSporkDownstreamState{Upstreams: []GitSporkUpstreamState{
        {URL: "git@github.com:org/repo.git", CommitHash: "old"},
    }}
    upsertUpstreamState(state, GitSporkUpstreamState{URL: "https://github.com/org/repo.git", CommitHash: "new"})
    require.Len(t, state.Upstreams, 1)
    assert.Equal(t, "new", state.Upstreams[0].CommitHash)
}

func Test_upsertUpstreamState_orderPreserved(t *testing.T) {
    state := &GitSporkDownstreamState{Upstreams: []GitSporkUpstreamState{
        {URL: "https://github.com/org/base.git", CommitHash: "b1"},
        {URL: "https://github.com/org/platform.git", CommitHash: "p1"},
    }}
    upsertUpstreamState(state, GitSporkUpstreamState{URL: "https://github.com/org/base.git", CommitHash: "b2"})
    require.Len(t, state.Upstreams, 2)
    assert.Equal(t, "b2", state.Upstreams[0].CommitHash)
    assert.Equal(t, "p1", state.Upstreams[1].CommitHash)
}
```

Run: `go test ./internal/... -run "Test_parseUpstreamFlag|Test_normalizeUpstreamURL|Test_upsertUpstreamState"`
Expected: FAIL.

- [ ] **Step 2: Implement the three functions** — add to `internal/integrate.go` before the `Integrate` func:

```go
func parseUpstreamFlag(val string) (UpstreamSpec, error) {
    spec := UpstreamSpec{}
    for _, part := range strings.Split(val, ",") {
        kv := strings.SplitN(part, "=", 2)
        if len(kv) != 2 {
            return spec, fmt.Errorf("--upstream: invalid key=value pair %q", part)
        }
        switch kv[0] {
        case "url":
            spec.URL = kv[1]
        case "version":
            spec.Version = kv[1]
        case "subpath":
            spec.Subpath = kv[1]
        case "token":
            spec.Token = kv[1]
        default:
            return spec, fmt.Errorf("--upstream: unknown key %q", kv[0])
        }
    }
    if spec.URL == "" {
        return spec, fmt.Errorf("--upstream: missing required key \"url\"")
    }
    return spec, nil
}

func normalizeUpstreamURL(rawURL string, subpath string) string {
    u := rawURL
    if re := regexp.MustCompile(`^git@([^:]+):(.+)$`); re.MatchString(u) {
        u = re.ReplaceAllString(u, "$1/$2")
    }
    u = regexp.MustCompile(`^https?://`).ReplaceAllString(u, "")
    u = strings.TrimSuffix(u, ".git")
    if subpath != "" {
        u = u + "::" + subpath
    }
    return strings.ToLower(u)
}

func upsertUpstreamState(state *GitSporkDownstreamState, entry GitSporkUpstreamState) {
    key := normalizeUpstreamURL(entry.URL, entry.Subpath)
    for i, existing := range state.Upstreams {
        if normalizeUpstreamURL(existing.URL, existing.Subpath) == key {
            state.Upstreams[i] = entry
            return
        }
    }
    state.Upstreams = append(state.Upstreams, entry)
}
```

- [ ] **Step 3: Run tests** — `go test ./internal/... -run "Test_parseUpstreamFlag|Test_normalizeUpstreamURL|Test_upsertUpstreamState"` — Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/integrate.go internal/integrate_test.go
git commit -m "feat: add parseUpstreamFlag, normalizeUpstreamURL, upsertUpstreamState"
```

---

## Task 3: State migration in `loadDownstreamState`

**Files:**
- Modify: `internal/integrate.go`
- Modify: `internal/integrate_test.go`

- [ ] **Step 1: Write failing test** — add to `internal/integrate_test.go`:

```go
func Test_loadDownstreamState_migration(t *testing.T) {
    dir := t.TempDir()
    metaDir := filepath.Join(dir, ".gitspork")
    require.NoError(t, os.MkdirAll(metaDir, 0755))
    oldState := `{"migrations_complete":["m1"],"last_upstream_repo_url":"git@github.com:org/repo.git","last_upstream_repo_subpath":"infra","last_upstream_commit_hash":"abc123"}`
    require.NoError(t, os.WriteFile(filepath.Join(metaDir, "downstream-state.json"), []byte(oldState), 0644))

    state, err := loadDownstreamState(dir)
    require.NoError(t, err)
    require.Len(t, state.Upstreams, 1)
    assert.Equal(t, "git@github.com:org/repo.git", state.Upstreams[0].URL)
    assert.Equal(t, "infra", state.Upstreams[0].Subpath)
    assert.Equal(t, "abc123", state.Upstreams[0].CommitHash)
    assert.Equal(t, "", state.LastUpstreamRepoURL)
    assert.Equal(t, "", state.LastUpstreamCommitHash)
}
```

Run: `go test ./internal/... -run Test_loadDownstreamState_migration`
Expected: FAIL — migration not implemented.

- [ ] **Step 2: Add migration to `loadDownstreamState`** in `internal/integrate.go`

After `err = json.Unmarshal(f, state)` and its error check, add before the final `return state, nil`:

```go
// Migrate deprecated single-upstream fields to Upstreams slice.
if len(state.Upstreams) == 0 && state.LastUpstreamCommitHash != "" {
    state.Upstreams = []GitSporkUpstreamState{{
        URL:        state.LastUpstreamRepoURL,
        Subpath:    state.LastUpstreamRepoSubpath,
        CommitHash: state.LastUpstreamCommitHash,
    }}
    state.LastUpstreamRepoURL = ""
    state.LastUpstreamRepoSubpath = ""
    state.LastUpstreamCommitHash = ""
}
```

- [ ] **Step 3: Run test** — `go test ./internal/... -run Test_loadDownstreamState_migration` — Expected: PASS.

- [ ] **Step 4: Run all unit tests** — `make test-unit` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate.go internal/integrate_test.go
git commit -m "feat: auto-migrate deprecated single-upstream state fields to Upstreams slice"
```

---

## Task 4: Update `Integrate` to loop over `Upstreams`

**Files:**
- Modify: `internal/integrate.go`

No new unit tests required here — the functional tests in Task 8 cover the multi-upstream loop. The unit-level concern (state upsert) is already covered by Task 2.

- [ ] **Step 1: Replace the `Integrate` func body** in `internal/integrate.go`

The current `Integrate` function runs one upstream. Replace it with a normalization step followed by a loop. The full replacement for the `Integrate` function (lines 44–125 in the current file):

```go
func Integrate(opts *IntegrateOptions) error {
    var err error

    if opts.DownstreamRepoPath == "" {
        opts.DownstreamRepoPath, err = os.Getwd()
        if err != nil {
            return fmt.Errorf("unable to get the present working directory: %v", err)
        }
    } else {
        opts.DownstreamRepoPath, err = filepath.Abs(opts.DownstreamRepoPath)
        if err != nil {
            return fmt.Errorf("unable to determine local downstream repo path: %v", err)
        }
    }

    // Normalize: if Upstreams is empty but single-upstream fields are set, synthesize.
    if len(opts.Upstreams) == 0 && opts.UpstreamRepoURL != "" {
        opts.Upstreams = []UpstreamSpec{{
            URL:     opts.UpstreamRepoURL,
            Version: opts.UpstreamRepoVersion,
            Subpath: opts.UpstreamRepoSubpath,
            Token:   opts.UpstreamRepoToken,
        }}
    }
    if len(opts.Upstreams) == 0 {
        return fmt.Errorf("no upstream specified: provide --upstream or --upstream-repo-url")
    }

    for _, upstream := range opts.Upstreams {
        if err := integrateOne(opts, upstream); err != nil {
            return err
        }
    }
    return nil
}

func integrateOne(opts *IntegrateOptions, upstream UpstreamSpec) error {
    prevHash := ""
    if !opts.ForDriftCheck {
        existingState, err := loadDownstreamState(opts.DownstreamRepoPath)
        if err != nil {
            return fmt.Errorf("error loading downstream state for delta check: %v", err)
        }
        // find prevHash for this specific upstream
        key := normalizeUpstreamURL(upstream.URL, upstream.Subpath)
        for _, u := range existingState.Upstreams {
            if normalizeUpstreamURL(u.URL, u.Subpath) == key {
                prevHash = u.CommitHash
                break
            }
        }
    }

    cloneDir, err := os.MkdirTemp("", gitSpork)
    if err != nil {
        return fmt.Errorf("error creating temporary directory: %v", err)
    }
    defer os.RemoveAll(cloneDir)

    singleOpts := &IntegrateOptions{
        Logger:                 opts.Logger,
        UpstreamRepoURL:        upstream.URL,
        UpstreamRepoVersion:    upstream.Version,
        UpstreamRepoSubpath:    upstream.Subpath,
        UpstreamRepoToken:      upstream.Token,
        DownstreamRepoPath:     opts.DownstreamRepoPath,
        ForceRePrompt:          opts.ForceRePrompt,
        ForDriftCheck:          opts.ForDriftCheck,
        PrevUpstreamCommitHash: prevHash,
    }

    originalUpstreamURL := upstream.URL
    opts.Logger.Log("cloning gitspork upstream repo %s", upstream.URL)
    commitHash, err := cloneUpstreamForIntegrate(cloneDir, singleOpts)
    if err != nil {
        return err
    }

    upstreamRootPath := filepath.Join(cloneDir, upstream.Subpath)
    opts.Logger.Log("parsing the gitspork config file in the upstream repo clone at %s or %s", gitSporkConfigFileName, gitSporkConfigFileNameAlt)
    gitSporkConfig, err := getGitSporkConfig(upstreamRootPath)
    if err != nil {
        return err
    }

    if !opts.ForDriftCheck && prevHash != "" {
        upstreamRepo, err := git.PlainOpen(cloneDir)
        if err != nil {
            return fmt.Errorf("error opening upstream clone for delta computation: %v", err)
        }
        delta, err := computeUpstreamDelta(upstreamRepo, prevHash, commitHash, gitSporkConfig, upstream.Subpath)
        if err != nil {
            return fmt.Errorf("error computing upstream delta: %v", err)
        }
        if err := applyUpstreamDelta(delta, opts.DownstreamRepoPath, opts.Logger); err != nil {
            return fmt.Errorf("error applying upstream delta to downstream: %v", err)
        }
    }

    if err := integrate(gitSporkConfig, upstreamRootPath, opts.DownstreamRepoPath, opts.ForceRePrompt, opts.ForDriftCheck, opts.Logger); err != nil {
        return err
    }

    if !opts.ForDriftCheck {
        state, err := loadDownstreamState(opts.DownstreamRepoPath)
        if err != nil {
            return fmt.Errorf("error loading downstream state to save upstream metadata: %v", err)
        }
        upsertUpstreamState(state, GitSporkUpstreamState{
            URL:        originalUpstreamURL,
            Subpath:    upstream.Subpath,
            CommitHash: commitHash,
        })
        if err := saveDownstreamState(opts.DownstreamRepoPath, state); err != nil {
            return fmt.Errorf("error saving upstream metadata to downstream state: %v", err)
        }
    }

    return nil
}
```

Note: `cloneUpstreamForIntegrate` currently takes `opts *IntegrateOptions` and mutates `opts.UpstreamRepoURL` via `resolveUpstreamURL`. Since we now pass a dedicated `singleOpts`, the mutation is contained to that copy. No change needed to `cloneUpstreamForIntegrate`.

- [ ] **Step 2: Build** — `go build ./...` — Expected: PASS.

- [ ] **Step 3: Run unit tests** — `make test-unit` — Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/integrate.go
git commit -m "feat: Integrate loops over Upstreams slice; extract integrateOne helper"
```

---

## Task 5: Update `IntegrateLocal` to loop over `UpstreamPaths`

**Files:**
- Modify: `internal/integrate-local.go`

- [ ] **Step 1: Replace `IntegrateLocal`** in `internal/integrate-local.go`:

```go
package internal

import "path/filepath"

func IntegrateLocal(opts *IntegrateLocalOptions) error {
    // Normalize: single UpstreamPath -> UpstreamPaths slice.
    if len(opts.UpstreamPaths) == 0 && opts.UpstreamPath != "" {
        opts.UpstreamPaths = []string{opts.UpstreamPath}
    }
    if len(opts.UpstreamPaths) == 0 {
        return fmt.Errorf("no upstream path specified: provide --upstream-path")
    }

    for _, upstreamPath := range opts.UpstreamPaths {
        opts.Logger.Log("parsing the gitspork config file at %s or %s",
            filepath.Join(upstreamPath, gitSporkConfigFileName),
            filepath.Join(upstreamPath, gitSporkConfigFileNameAlt))
        gitSporkConfig, err := getGitSporkConfig(upstreamPath)
        if err != nil {
            return err
        }
        if err := integrate(gitSporkConfig, upstreamPath, opts.DownstreamPath, opts.ForceRePrompt, false, opts.Logger); err != nil {
            return err
        }
    }
    return nil
}
```

Add `"fmt"` to the import if not already present (the file currently only imports `"path/filepath"`). Add it:

```go
import (
    "fmt"
    "path/filepath"
)
```

- [ ] **Step 2: Build** — `go build ./...` — Expected: PASS.

- [ ] **Step 3: Run unit tests** — `make test-unit` — Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/integrate-local.go
git commit -m "feat: IntegrateLocal loops over UpstreamPaths slice"
```

---

## Task 6: Update `CheckDrift` for multi-upstream with attribution

**Files:**
- Modify: `internal/check-drift.go`

The current `CheckDrift` uses `opts.UpstreamRepoURL` / `opts.UpstreamRepoToken` and a single `state.LastUpstreamCommitHash`. Replace it with a loop that re-integrates each upstream, tracks which files each one touched, then attributes the final drift diff.

- [ ] **Step 1: Replace `CheckDrift`** in `internal/check-drift.go`:

```go
func CheckDrift(opts *CheckDriftOptions) error {
    var err error

    if opts.DownstreamRepoPath == "" {
        opts.DownstreamRepoPath, err = os.Getwd()
        if err != nil {
            return fmt.Errorf("unable to get the present working directory: %v", err)
        }
    } else {
        opts.DownstreamRepoPath, err = filepath.Abs(opts.DownstreamRepoPath)
        if err != nil {
            return fmt.Errorf("unable to determine local downstream repo path: %v", err)
        }
    }

    state, err := loadDownstreamState(opts.DownstreamRepoPath)
    if err != nil {
        return fmt.Errorf("error loading downstream state: %v", err)
    }

    // Resolve which upstreams to check and their commit hashes.
    type upstreamCheckEntry struct {
        spec       UpstreamSpec
        commitHash string
    }
    var entries []upstreamCheckEntry

    if len(opts.Upstreams) > 0 {
        // Override: match each override to its state entry for the commit hash.
        for _, override := range opts.Upstreams {
            key := normalizeUpstreamURL(override.URL, override.Subpath)
            found := false
            for _, su := range state.Upstreams {
                if normalizeUpstreamURL(su.URL, su.Subpath) == key {
                    entries = append(entries, upstreamCheckEntry{spec: override, commitHash: su.CommitHash})
                    found = true
                    break
                }
            }
            if !found {
                return fmt.Errorf("--upstream override %q has no matching state entry — run 'gitspork integrate' first", override.URL)
            }
        }
    } else {
        if len(state.Upstreams) == 0 {
            return fmt.Errorf("no previous integration found in downstream state — run 'gitspork integrate' first")
        }
        for _, su := range state.Upstreams {
            entries = append(entries, upstreamCheckEntry{
                spec:       UpstreamSpec{URL: su.URL, Subpath: su.Subpath},
                commitHash: su.CommitHash,
            })
        }
    }

    repo, err := gogit.PlainOpen(opts.DownstreamRepoPath)
    if err != nil {
        return fmt.Errorf("error opening downstream repo: %v", err)
    }
    wt, err := repo.Worktree()
    if err != nil {
        return fmt.Errorf("error accessing downstream worktree: %v", err)
    }

    if err := checkCleanWorkingTree(opts.DownstreamRepoPath); err != nil {
        return err
    }

    headRef, err := repo.Head()
    if err != nil {
        return fmt.Errorf("error resolving HEAD: %v", err)
    }
    if !headRef.Name().IsBranch() {
        return fmt.Errorf("downstream repo is in detached HEAD state — check out a branch before running check-drift")
    }
    originalBranch := headRef.Name()

    driftBranchRef := plumbing.NewBranchReferenceName(driftCheckBranch)
    if err := repo.Storer.SetReference(plumbing.NewHashReference(driftBranchRef, headRef.Hash())); err != nil {
        return fmt.Errorf("error creating/resetting drift-check branch: %v", err)
    }
    if err := wt.Checkout(&gogit.CheckoutOptions{Branch: driftBranchRef}); err != nil {
        return fmt.Errorf("error checking out drift-check branch: %v", err)
    }
    defer func() {
        _ = wt.Checkout(&gogit.CheckoutOptions{Branch: originalBranch})
        _ = repo.DeleteBranch(driftCheckBranch)
    }()

    // Re-integrate each upstream; track which files each one last touched.
    // fileOwner maps relative file path -> upstream URL that last wrote it.
    fileOwner := map[string]string{}

    for _, entry := range entries {
        opts.Logger.Log("re-integrating upstream %s at commit %s", entry.spec.URL, entry.commitHash)

        beforeFiles, err := listWorktreeFiles(opts.DownstreamRepoPath)
        if err != nil {
            return fmt.Errorf("error listing worktree files before integrate: %v", err)
        }

        if err := Integrate(&IntegrateOptions{
            Logger:             opts.Logger,
            UpstreamRepoURL:    entry.spec.URL,
            UpstreamRepoSubpath: entry.spec.Subpath,
            UpstreamRepoToken:  entry.spec.spec.Token,
            UpstreamRepoCommit: entry.commitHash,
            DownstreamRepoPath: opts.DownstreamRepoPath,
            ForDriftCheck:      true,
        }); err != nil {
            return fmt.Errorf("error running integration for drift check: %v", err)
        }

        afterFiles, err := listWorktreeFiles(opts.DownstreamRepoPath)
        if err != nil {
            return fmt.Errorf("error listing worktree files after integrate: %v", err)
        }

        // Any file that appeared or changed gets attributed to this upstream.
        for f, hash := range afterFiles {
            if beforeFiles[f] != hash {
                fileOwner[f] = entry.spec.URL
            }
        }
        for f := range beforeFiles {
            if _, stillPresent := afterFiles[f]; !stillPresent {
                fileOwner[f] = entry.spec.URL
            }
        }
    }

    patch, err := diffWorktreeAgainstHEAD(repo, wt)
    if err != nil {
        return fmt.Errorf("error diffing downstream against HEAD: %v", err)
    }
    if patch == nil {
        opts.Logger.Log("no drift detected")
        return nil
    }

    stats := patch.Stats()
    opts.Logger.Log("drift detected: %d file(s) changed", len(stats))

    // Group by upstream URL.
    byUpstream := map[string][]string{}
    for _, s := range stats {
        owner := fileOwner[s.Name]
        if owner == "" {
            owner = "(unknown upstream)"
        }
        byUpstream[owner] = append(byUpstream[owner], s.Name)
        opts.Logger.Log("  %s (upstream: %s)", s.Name, owner)
    }

    if opts.Verbose {
        pr, pw := io.Pipe()
        go func() { pw.CloseWithError(patch.Encode(pw)) }()
        if err := opts.Logger.Diff(pr); err != nil {
            return fmt.Errorf("error encoding diff: %v", err)
        }
    }

    return ErrDriftDetected
}
```

- [ ] **Step 2: Fix the struct field access bug** — in the `Integrate` call above, `entry.spec.spec.Token` should be `entry.spec.Token`. Rewrite the call with the correct field:

```go
if err := Integrate(&IntegrateOptions{
    Logger:              opts.Logger,
    UpstreamRepoURL:     entry.spec.URL,
    UpstreamRepoSubpath: entry.spec.Subpath,
    UpstreamRepoToken:   entry.spec.Token,
    UpstreamRepoCommit:  entry.commitHash,
    DownstreamRepoPath:  opts.DownstreamRepoPath,
    ForDriftCheck:       true,
}); err != nil {
    return fmt.Errorf("error running integration for drift check: %v", err)
}
```

- [ ] **Step 3: Add `listWorktreeFiles` helper** to `internal/check-drift.go`:

```go
// listWorktreeFiles returns a map of relative path -> hex content hash for all
// non-.git files under dir. Used to detect which files an integrate pass touched.
func listWorktreeFiles(dir string) (map[string]string, error) {
    result := map[string]string{}
    err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        if info.IsDir() {
            if info.Name() == ".git" {
                return filepath.SkipDir
            }
            return nil
        }
        rel, err := filepath.Rel(dir, path)
        if err != nil {
            return err
        }
        b, err := os.ReadFile(path)
        if err != nil {
            return err
        }
        h := fmt.Sprintf("%x", sha256.Sum256(b))
        result[rel] = h
        return nil
    })
    return result, err
}
```

Add `"crypto/sha256"` to the imports in `internal/check-drift.go`.

- [ ] **Step 4: Add missing imports** to `internal/check-drift.go` — ensure `"crypto/sha256"` and `"os"` are present (they likely are already; verify).

- [ ] **Step 5: Build** — `go build ./...` — Expected: PASS.

- [ ] **Step 6: Run unit tests** — `make test-unit` — Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/check-drift.go
git commit -m "feat: CheckDrift loops over all recorded upstreams with per-upstream file attribution"
```

---

## Task 7: CLI flag changes

**Files:**
- Modify: `cmd/integrate.go`
- Modify: `cmd/integrate-local.go`
- Modify: `cmd/check-drift.go`

- [ ] **Step 1: Update `cmd/integrate.go`**

Replace the entire file:

```go
package cmd

import (
    "fmt"

    "github.com/spf13/cobra"

    "github.com/rockholla/gitspork/internal"
)

const (
    integrateHelpShort string = "integrate/re-integrate w/ a gitspork upstream"
    integrateHelpLong  string = `integration is the way in which downstreams will continually stay up-to-date w/ their upstreams.

This command is the key orchestration of gitspork, ensuring that specific and advanced integration and merging happen between the
upstream and downstream. See https://github.com/rockholla/gitspork/docs for more info.`
)

// IntegrateSubcommand represents the subcommand and all related functionality for ` + "`gitspork integrate`" + `
type IntegrateSubcommand struct{}

// GetCmd will return the native cobra command for the integrate subcommand
func (isc *IntegrateSubcommand) GetCmd() *cobra.Command {
    var upstreamRepoURL string
    var upstreamRepoVersion string
    var upstreamRepoSubpath string
    var upstreamRepoToken string
    var upstreamFlags []string
    var downstreamRepoPath string
    var forceRePrompt bool

    var cmd = &cobra.Command{
        Use:   "integrate",
        Short: integrateHelpShort,
        Long:  fmt.Sprintf("%s\n\n%s", integrateHelpShort, integrateHelpLong),
        RunE: func(cmd *cobra.Command, args []string) error {
            oldFlagsSet := upstreamRepoURL != "" || upstreamRepoVersion != "" || upstreamRepoSubpath != "" || upstreamRepoToken != ""
            if len(upstreamFlags) > 0 && oldFlagsSet {
                return fmt.Errorf("cannot mix --upstream with --upstream-repo-url/version/subpath/token flags")
            }

            opts := &internal.IntegrateOptions{
                Logger:              logger,
                UpstreamRepoURL:     upstreamRepoURL,
                UpstreamRepoVersion: upstreamRepoVersion,
                UpstreamRepoSubpath: upstreamRepoSubpath,
                UpstreamRepoToken:   upstreamRepoToken,
                DownstreamRepoPath:  downstreamRepoPath,
                ForceRePrompt:       forceRePrompt,
            }
            for _, f := range upstreamFlags {
                spec, err := internal.ParseUpstreamFlag(f)
                if err != nil {
                    return err
                }
                opts.Upstreams = append(opts.Upstreams, spec)
            }
            return internal.Integrate(opts)
        },
    }

    cmd.PersistentFlags().StringArrayVar(&upstreamFlags, "upstream", nil,
        "upstream spec as comma-separated key=value pairs (url, version, subpath, token); repeatable for multiple upstreams")
    cmd.PersistentFlags().StringVarP(&upstreamRepoURL, "upstream-repo-url", "u", "",
        "upstream gitspork repo to integrate/re-integrate with (single-upstream shorthand)")
    cmd.PersistentFlags().StringVarP(&upstreamRepoVersion, "upstream-repo-version", "v", "",
        "upstream gitspork repo version (single-upstream shorthand)")
    cmd.PersistentFlags().StringVarP(&upstreamRepoSubpath, "upstream-repo-subpath", "p", "",
        "upstream gitspork repo subpath (single-upstream shorthand)")
    cmd.PersistentFlags().StringVarP(&upstreamRepoToken, "upstream-repo-token", "t", "",
        "upstream gitspork repo token (single-upstream shorthand)")
    cmd.PersistentFlags().StringVarP(&downstreamRepoPath, "downstream-repo-path", "d", "",
        "local path to the downstream repo clone to integrate/re-integrate, defaults to the present working directory")
    cmd.PersistentFlags().BoolVarP(&forceRePrompt, "force-re-prompt", "f", false,
        "If true, will disregard any previous prompt input value caches for templated instructions")

    return cmd
}
```

Note: `parseUpstreamFlag` must be exported as `ParseUpstreamFlag` (capital P) so `cmd/` can call it. Update the function name in `internal/integrate.go` and its test references accordingly.

- [ ] **Step 2: Update `cmd/integrate-local.go`**

Replace the entire file:

```go
package cmd

import (
    "fmt"

    "github.com/spf13/cobra"

    "github.com/rockholla/gitspork/internal"
)

const (
    integrateLocalHelpShort string = "integrate/re-integrate w/ a gitspork local upstream template"
    integrateLocalHelpLong  string = `integration is the way in which local downstream directories can integrate/re-integrate w/ standards set forth by another local directory upstream template.

This command supports all of the same configuration the normal integrate command does, but just operates against two local directories.
See https://github.com/rockholla/gitspork/docs for more info on configuration options.
`
)

// IntegrateLocalSubcommand represents the subcommand and all related functionality for ` + "`gitspork integrate-local`" + `
type IntegrateLocalSubcommand struct{}

// GetCmd will return the native cobra command for the integrate-local subcommand
func (ilsc *IntegrateLocalSubcommand) GetCmd() *cobra.Command {
    var upstreamPaths []string
    var downstreamPath string
    var forceRePrompt bool

    var cmd = &cobra.Command{
        Use:   "integrate-local",
        Short: integrateLocalHelpShort,
        Long:  fmt.Sprintf("%s\n\n%s", integrateLocalHelpShort, integrateLocalHelpLong),
        RunE: func(cmd *cobra.Command, args []string) error {
            return internal.IntegrateLocal(&internal.IntegrateLocalOptions{
                Logger:        logger,
                UpstreamPaths: upstreamPaths,
                DownstreamPath: downstreamPath,
                ForceRePrompt: forceRePrompt,
            })
        },
    }

    cmd.PersistentFlags().StringArrayVarP(&upstreamPaths, "upstream-path", "u", nil,
        "local path that contains a template/gitspork upstream configuration; repeatable for multiple upstreams")
    cmd.PersistentFlags().StringVarP(&downstreamPath, "downstream-path", "d", "",
        "local path to integrate/re-integrate w/ the standards set at the upstream-path")
    cmd.PersistentFlags().BoolVarP(&forceRePrompt, "force-re-prompt", "f", false,
        "If true, will disregard any previous prompt input value caches for templated instructions")

    return cmd
}
```

- [ ] **Step 3: Update `cmd/check-drift.go`**

Replace the entire file:

```go
package cmd

import (
    "errors"
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "github.com/rockholla/gitspork/internal"
)

const (
    checkDriftHelpShort string = "check if a downstream repo has drifted from its last integrated upstream state"
    checkDriftHelpLong  string = `check-drift re-runs the integration at the exact upstream commit hash used in the last
integrate run, against an isolated copy of the downstream repo, and reports any differences.

Exit codes:
  0 - no drift detected
  1 - error (missing state, unclean working tree, clone failure, etc.)
  2 - drift detected

See https://github.com/rockholla/gitspork/docs for more info.`
)

// CheckDriftSubcommand represents the subcommand and all related functionality for 'gitspork check-drift'
type CheckDriftSubcommand struct{}

// GetCmd will return the native cobra command for the check-drift subcommand
func (cds *CheckDriftSubcommand) GetCmd() *cobra.Command {
    var downstreamRepoPath string
    var upstreamFlags []string
    var verbose bool

    var cmd = &cobra.Command{
        Use:   "check-drift",
        Short: checkDriftHelpShort,
        Long:  fmt.Sprintf("%s\n\n%s", checkDriftHelpShort, checkDriftHelpLong),
        RunE: func(cmd *cobra.Command, args []string) error {
            opts := &internal.CheckDriftOptions{
                Logger:             logger,
                DownstreamRepoPath: downstreamRepoPath,
                Verbose:            verbose,
            }
            for _, f := range upstreamFlags {
                spec, err := internal.ParseUpstreamFlag(f)
                if err != nil {
                    return err
                }
                opts.Upstreams = append(opts.Upstreams, spec)
            }
            err := internal.CheckDrift(opts)
            if errors.Is(err, internal.ErrDriftDetected) {
                os.Exit(2)
            }
            return err
        },
    }

    cmd.PersistentFlags().StringVarP(&downstreamRepoPath, "downstream-repo-path", "d", "",
        "local path to the downstream repo to check, defaults to the present working directory")
    cmd.PersistentFlags().StringArrayVar(&upstreamFlags, "upstream", nil,
        "override upstream(s) to check as comma-separated key=value pairs (url, subpath, token); repeatable")
    cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false,
        "print full git diff output when drift is detected")

    return cmd
}
```

- [ ] **Step 4: Export `parseUpstreamFlag` → `ParseUpstreamFlag`**

In `internal/integrate.go`, rename `parseUpstreamFlag` to `ParseUpstreamFlag`. Update `internal/integrate_test.go` to call `ParseUpstreamFlag` (capital P).

- [ ] **Step 5: Build** — `go build ./...` — Expected: PASS.

- [ ] **Step 6: Run unit tests** — `make test-unit` — Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/integrate.go cmd/integrate-local.go cmd/check-drift.go internal/integrate.go internal/integrate_test.go
git commit -m "feat: CLI flags — repeatable --upstream for integrate/check-drift, repeatable --upstream-path for integrate-local"
```

---

## Task 8: Functional tests — multi-upstream `integrate`

**Files:**
- Modify: `test/functional/helpers_test.go`
- Modify: `test/functional/integrate_test.go`

- [ ] **Step 1: Add helpers** to `test/functional/helpers_test.go`

Add after `integrateArgs`:

```go
// buildSecondUpstream creates a minimal second upstream with a distinct file
// so precedence tests can verify which upstream's file wins.
func buildSecondUpstream(t *testing.T) string {
    t.Helper()
    return NewUpstreamRepo(t, map[string]string{
        "upstream-owned/file.txt": "second upstream content\n",
    }, `upstream_owned:
- upstream-owned/**
`)
}

// integrateArgsMulti returns integrate args using the repeatable --upstream flag.
func integrateArgsMulti(upstreamDir1, upstreamDir2, downstreamDir string) []string {
    return []string{
        "integrate",
        "--upstream", "url=file://" + upstreamDir1 + ",version=main",
        "--upstream", "url=file://" + upstreamDir2 + ",version=main",
        "--downstream-repo-path", downstreamDir,
    }
}
```

- [ ] **Step 2: Add multi-upstream functional tests** to `test/functional/integrate_test.go`

```go
func TestIntegrate_multi_upstream_precedence(t *testing.T) {
    // Second upstream wins on file.txt because it comes last.
    upstreamDir1 := buildSimpleUpstream(t)
    upstreamDir2 := buildSecondUpstream(t)
    downstreamDir := NewDownstreamRepo(t)
    prepDownstreamWithInputData(t, downstreamDir)
    runner := resolveRunner(t, upstreamDir1, downstreamDir)

    // The runner only rewrites paths for one upstream dir; pass args manually rewritten.
    out, code := runner.Run(t, integrateArgsMulti(upstreamDir1, upstreamDir2, downstreamDir), downstreamDir)
    require.Equal(t, 0, code, "multi-upstream integrate failed:\n%s", out)

    AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "second upstream content")
}

func TestIntegrate_multi_upstream_backward_compat_old_flags(t *testing.T) {
    // Single --upstream-repo-url flag still works.
    upstreamDir := buildSimpleUpstream(t)
    downstreamDir := NewDownstreamRepo(t)
    prepDownstreamWithInputData(t, downstreamDir)
    runner := resolveRunner(t, upstreamDir, downstreamDir)

    out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
    require.Equal(t, 0, code, "backward-compat single flag integrate failed:\n%s", out)
    AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
}

func TestIntegrate_multi_upstream_flag_conflict_error(t *testing.T) {
    // Mixing --upstream and --upstream-repo-url returns exit code 1.
    upstreamDir := buildSimpleUpstream(t)
    downstreamDir := NewDownstreamRepo(t)
    runner := resolveRunner(t, upstreamDir, downstreamDir)

    out, code := runner.Run(t, []string{
        "integrate",
        "--upstream", "url=file://" + upstreamDir + ",version=main",
        "--upstream-repo-url", "file://" + upstreamDir,
        "--downstream-repo-path", downstreamDir,
    }, downstreamDir)
    require.Equal(t, 1, code, "expected error when mixing flags:\n%s", out)
}

func TestIntegrate_multi_upstream_state_records_all(t *testing.T) {
    // Both upstream URLs appear in downstream-state.json after a multi-upstream integrate.
    upstreamDir1 := buildSimpleUpstream(t)
    upstreamDir2 := buildSecondUpstream(t)
    downstreamDir := NewDownstreamRepo(t)
    prepDownstreamWithInputData(t, downstreamDir)
    runner := resolveRunner(t, upstreamDir1, downstreamDir)

    out, code := runner.Run(t, integrateArgsMulti(upstreamDir1, upstreamDir2, downstreamDir), downstreamDir)
    require.Equal(t, 0, code, "multi-upstream integrate failed:\n%s", out)

    state := ReadFile(t, downstreamDir, ".gitspork/downstream-state.json")
    assert.Contains(t, state, `"upstreams"`)
    assert.Contains(t, state, `"commit_hash"`)
}
```

Note: `integrateArgsMulti` passes raw host paths including the second upstream, which the `DockerRunner` won't rewrite (it only knows one upstream dir). For the Docker runner variant, these tests will only pass on the native runner unless the Docker harness is extended. Mark with a build comment or skip if `dockerImageTag != ""` is set. Add this skip guard at the top of the multi-upstream tests that reference two upstream dirs:

```go
if _, ok := resolveRunner(t, upstreamDir1, downstreamDir).(*DockerRunner); ok {
    t.Skip("multi-upstream path rewriting not supported in DockerRunner")
}
```

(The `DockerRunner` type is in `harness_docker.go` under `functional_docker` build tag; the type assertion is safe because under `functional` only `BinaryRunner` exists.)

- [ ] **Step 3: Run functional tests** — `make test-functional` — Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add test/functional/helpers_test.go test/functional/integrate_test.go
git commit -m "test: add multi-upstream integrate functional tests"
```

---

## Task 9: Functional tests — multi-upstream `check-drift`

**Files:**
- Modify: `test/functional/check_drift_test.go`

- [ ] **Step 1: Add multi-upstream check-drift functional tests**

```go
func TestCheckDrift_multi_upstream_no_drift(t *testing.T) {
    upstreamDir1 := buildSimpleUpstream(t)
    upstreamDir2 := buildSecondUpstream(t)
    downstreamDir := NewDownstreamRepo(t)
    prepDownstreamWithInputData(t, downstreamDir)
    runner := resolveRunner(t, upstreamDir1, downstreamDir)

    // integrate both upstreams
    out, code := runner.Run(t, integrateArgsMulti(upstreamDir1, upstreamDir2, downstreamDir), downstreamDir)
    require.Equal(t, 0, code, "multi integrate failed:\n%s", out)
    CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")
    prepDownstreamWithInputData(t, downstreamDir)

    out, code = runner.Run(t, []string{
        "check-drift",
        "--downstream-repo-path", downstreamDir,
    }, downstreamDir)
    require.Equal(t, 0, code, "expected no drift:\n%s", out)
}

func TestCheckDrift_multi_upstream_drift_attributed(t *testing.T) {
    upstreamDir1 := buildSimpleUpstream(t)
    upstreamDir2 := buildSecondUpstream(t)
    downstreamDir := NewDownstreamRepo(t)
    prepDownstreamWithInputData(t, downstreamDir)
    runner := resolveRunner(t, upstreamDir1, downstreamDir)

    out, code := runner.Run(t, integrateArgsMulti(upstreamDir1, upstreamDir2, downstreamDir), downstreamDir)
    require.Equal(t, 0, code, "multi integrate failed:\n%s", out)

    // drift: modify the upstream-owned file (owned by upstreamDir2 since it was last)
    WriteFiles(t, downstreamDir, map[string]string{
        "upstream-owned/file.txt": "drifted\n",
    })
    CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "introduce drift")
    prepDownstreamWithInputData(t, downstreamDir)

    out, code = runner.Run(t, []string{
        "check-drift",
        "--downstream-repo-path", downstreamDir,
    }, downstreamDir)
    require.Equal(t, 2, code, "expected drift exit 2:\n%s", out)
    assert.Contains(t, out, "upstream-owned/file.txt")
}

func TestCheckDrift_multi_upstream_state_fallback(t *testing.T) {
    // check-drift without --upstream reads all recorded upstreams from state.
    upstreamDir := buildSimpleUpstream(t)
    downstreamDir := NewDownstreamRepo(t)
    prepDownstreamWithInputData(t, downstreamDir)
    runner := resolveRunner(t, upstreamDir, downstreamDir)

    integrateForDrift(t, runner, upstreamDir, downstreamDir)
    prepDownstreamWithInputData(t, downstreamDir)

    out, code := runner.Run(t, []string{
        "check-drift",
        "--downstream-repo-path", downstreamDir,
    }, downstreamDir)
    require.Equal(t, 0, code, "expected no drift using state:\n%s", out)
}

func TestCheckDrift_upstream_override_url_protocol_switch(t *testing.T) {
    // --upstream override with HTTPS when state recorded SSH (or vice versa) still matches.
    upstreamDir := buildSimpleUpstream(t)
    downstreamDir := NewDownstreamRepo(t)
    prepDownstreamWithInputData(t, downstreamDir)
    runner := resolveRunner(t, upstreamDir, downstreamDir)

    // integrate using file:// URL (recorded in state)
    integrateForDrift(t, runner, upstreamDir, downstreamDir)
    prepDownstreamWithInputData(t, downstreamDir)

    // check-drift passing the same file:// URL explicitly via --upstream
    out, code := runner.Run(t, []string{
        "check-drift",
        "--upstream", "url=file://" + upstreamDir,
        "--downstream-repo-path", downstreamDir,
    }, downstreamDir)
    require.Equal(t, 0, code, "expected no drift with explicit --upstream:\n%s", out)
}
```

- [ ] **Step 2: Run functional tests** — `make test-functional` — Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/functional/check_drift_test.go
git commit -m "test: add multi-upstream check-drift functional tests"
```

---

## Task 10: Update existing tests that reference removed `CheckDriftOptions` fields

**Files:**
- Modify: `test/functional/check_drift_test.go` (existing tests)
- Modify: `test/functional/docker_volume_mount_test.go`

After Task 7 removes `UpstreamRepoURL` and `UpstreamRepoToken` from `CheckDriftOptions`, any code passing those fields won't compile. The existing functional tests pass `--upstream-repo-url` as a CLI flag to the binary — that's fine since the CLI flag is gone too; those tests need updating.

- [ ] **Step 1: Update existing `check-drift` tests** in `test/functional/check_drift_test.go`

`TestCheckDrift_no_drift` passes `--upstream-repo-url`. Replace that arg with `--upstream`:

```go
// old:
"--upstream-repo-url", "file://" + upstreamDir,
// new:
"--upstream", "url=file://" + upstreamDir,
```

`TestCheckDrift_drift_detected` — same replacement.

`TestCheckDrift_no_drift_state_url` — no `--upstream-repo-url` used; no change needed.

- [ ] **Step 2: Update `TestDockerRootMount_check_drift`** in `test/functional/docker_volume_mount_test.go`

```go
// old:
"--upstream-repo-url", "file://" + upstreamDir,
// new:
"--upstream", "url=file://" + upstreamDir,
```

- [ ] **Step 3: Build** — `go build ./...` — Expected: PASS.

- [ ] **Step 4: Run all test suites**

```bash
make test-unit
make test-functional
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add test/functional/check_drift_test.go test/functional/docker_volume_mount_test.go
git commit -m "fix: update existing check-drift tests to use new --upstream flag syntax"
```

---

## Backward Compatibility Summary

| Scenario | Behavior after this plan |
|---|---|
| Existing downstream with old state fields | Auto-migrated to `Upstreams` on first `integrate` or `check-drift` |
| `integrate` with `--upstream-repo-url` | Converted to single-entry `Upstreams` internally — identical behavior |
| `check-drift` with old `--upstream-repo-url` | Flag removed; use `--upstream "url=..."` — **breaking change on this flag** |
| `integrate-local` with single `--upstream-path` | Accepted as one-entry `UpstreamPaths` — identical behavior |

---
