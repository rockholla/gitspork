# Self-Integration Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Block `integrate`, `integrate-local`, and `check-drift` from operating when the upstream identifies the same repo as the downstream. Hard error, no bypass, dedicated CLI exit code 3.

**Architecture:** A single guard function `EnsureNotSelfIntegration` lives in `internal/integrate/self_guard.go` and runs two independent checks (path equality; upstream URL vs. downstream `origin` remote). It is called at the top of `integrateOneInternal` (covers both public `Integrate` and `check-drift`) and at the top of the per-upstream loop in `IntegrateLocal`. Failures wrap the new sentinel `sdktypes.ErrSelfIntegration` (re-exported as `gitspork.ErrSelfIntegration`). The three CLI subcommands intercept the sentinel and `os.Exit(3)`.

**Tech Stack:** Go 1.26; existing `github.com/go-git/go-git/v6`; existing testify; cobra; no new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-31-self-integration-guard-design.md`

**Branch:** `feat/self-integration-guard`

---

## Task 1: Add `ErrSelfIntegration` sentinel to sdktypes

**Files:**
- Modify: `internal/sdktypes/errors.go`

- [ ] **Step 1: Add the sentinel**

Edit `internal/sdktypes/errors.go`, appending after the existing sentinels:

```go
// ErrSelfIntegration is returned when integrate / integrate-local / check-drift
// detects that the upstream identifies the same repo as the downstream. SDK
// consumers can check via errors.Is(err, gitspork.ErrSelfIntegration).
var ErrSelfIntegration = errors.New("upstream and downstream identify the same repo")
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/sdktypes/errors.go
git commit -m "feat(sdk): add ErrSelfIntegration sentinel"
```

---

## Task 2: Re-export `ErrSelfIntegration` from root gitspork package

**Files:**
- Modify: `gitspork.go`

- [ ] **Step 1: Locate the existing `ErrDriftDetected` re-export**

Run: `grep -n "ErrDriftDetected\|ErrGitBinaryMissing" gitspork.go`
Expected: lines showing the existing `var ErrDriftDetected = sdktypes.ErrDriftDetected` (and any similar sentinel aliases).

- [ ] **Step 2: Add a matching re-export**

Below the existing sentinel re-exports (the exact location follows the pattern already present), add:

```go
// ErrSelfIntegration is returned by Integrate, IntegrateLocal, and CheckDrift
// when the upstream identifies the same repo as the downstream. Callers can
// distinguish this from other errors via errors.Is(err, ErrSelfIntegration).
var ErrSelfIntegration = sdktypes.ErrSelfIntegration
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add gitspork.go
git commit -m "feat(sdk): re-export ErrSelfIntegration from root package"
```

---

## Task 3: Path check in `self_guard.go` — TDD

**Files:**
- Create: `internal/integrate/self_guard.go`
- Create: `internal/integrate/self_guard_test.go`

- [ ] **Step 1: Write the failing tests (path check only)**

Create `internal/integrate/self_guard_test.go`:

```go
package integrate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureNotSelfIntegration_pathCheck(t *testing.T) {
	t.Run("empty upstreamLocalPath skips path check", func(t *testing.T) {
		downstream := t.TempDir()
		err := EnsureNotSelfIntegration(downstream, "", "")
		assert.NoError(t, err)
	})

	t.Run("upstream path == downstream path errors", func(t *testing.T) {
		downstream := t.TempDir()
		err := EnsureNotSelfIntegration(downstream, "", downstream)
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
		assert.Contains(t, err.Error(), "self-integration blocked")
		assert.Contains(t, err.Error(), downstream)
	})

	t.Run("upstream inside downstream errors", func(t *testing.T) {
		downstream := t.TempDir()
		upstream := filepath.Join(downstream, "templates")
		require.NoError(t, os.MkdirAll(upstream, 0755))
		err := EnsureNotSelfIntegration(downstream, "", upstream)
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})

	t.Run("downstream inside upstream errors", func(t *testing.T) {
		upstream := t.TempDir()
		downstream := filepath.Join(upstream, "some-child")
		require.NoError(t, os.MkdirAll(downstream, 0755))
		err := EnsureNotSelfIntegration(downstream, "", upstream)
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})

	t.Run("unrelated absolute paths pass", func(t *testing.T) {
		downstream := t.TempDir()
		upstream := t.TempDir()
		err := EnsureNotSelfIntegration(downstream, "", upstream)
		assert.NoError(t, err)
	})

	t.Run("upstream path that doesn't exist yet — unrelated, passes", func(t *testing.T) {
		downstream := t.TempDir()
		// Sibling of downstream that isn't created on disk.
		upstream := filepath.Join(filepath.Dir(downstream), "not-yet-created")
		err := EnsureNotSelfIntegration(downstream, "", upstream)
		assert.NoError(t, err)
	})

	t.Run("symlink pointing into downstream triggers error after EvalSymlinks", func(t *testing.T) {
		downstream := t.TempDir()
		realDir := filepath.Join(downstream, "real")
		require.NoError(t, os.MkdirAll(realDir, 0755))
		symlinkParent := t.TempDir()
		symlink := filepath.Join(symlinkParent, "link-into-downstream")
		require.NoError(t, os.Symlink(realDir, symlink))
		err := EnsureNotSelfIntegration(downstream, "", symlink)
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/integrate/ -run TestEnsureNotSelfIntegration_pathCheck -v`
Expected: FAIL with `undefined: EnsureNotSelfIntegration`.

- [ ] **Step 3: Implement path check only**

Create `internal/integrate/self_guard.go`:

```go
package integrate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// EnsureNotSelfIntegration errors when the upstream identity matches the
// downstream repo. Called at the top of every integration path (both SDK and
// CLI consumers go through this).
//
// upstreamURL: the upstream repo URL (empty for pure local-path integrations).
// upstreamLocalPath: absolute or relative local path to the upstream when
// invoked via IntegrateLocal (empty otherwise).
// downstreamRepoPath: the downstream repo path (already absolutized by callers).
//
// Returns nil when no self-integration is detected. Any returned error wraps
// sdktypes.ErrSelfIntegration.
func EnsureNotSelfIntegration(downstreamRepoPath, upstreamURL, upstreamLocalPath string) error {
	if err := selfGuardPathCheck(downstreamRepoPath, upstreamLocalPath); err != nil {
		return err
	}
	// URL check is added in a later task.
	return nil
}

func selfGuardPathCheck(downstreamRepoPath, upstreamLocalPath string) error {
	if upstreamLocalPath == "" {
		return nil
	}

	downAbs, err := filepath.Abs(downstreamRepoPath)
	if err != nil {
		return fmt.Errorf("resolving downstream path: %w", err)
	}
	upAbs, err := filepath.Abs(upstreamLocalPath)
	if err != nil {
		return fmt.Errorf("resolving upstream local path: %w", err)
	}

	downResolved := resolveSymlinksBestEffort(downAbs)
	upResolved := resolveSymlinksBestEffort(upAbs)

	if downResolved == upResolved {
		return newSelfIntegrationPathError(upResolved, downResolved)
	}
	if pathIsInside(downResolved, upResolved) || pathIsInside(upResolved, downResolved) {
		return newSelfIntegrationPathError(upResolved, downResolved)
	}
	return nil
}

// resolveSymlinksBestEffort applies filepath.EvalSymlinks when the path exists;
// falls back to the plain absolute path when it does not. This preserves the
// caller's intent for not-yet-created directories (e.g. a downstream we're
// about to write into) without producing false negatives.
func resolveSymlinksBestEffort(abs string) string {
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// pathIsInside reports whether child sits at or inside parent (but is not
// equal to parent). Uses filepath.Rel to handle OS separators uniformly.
func pathIsInside(parent, child string) bool {
	if parent == child {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func newSelfIntegrationPathError(upstream, downstream string) error {
	return fmt.Errorf(
		"self-integration blocked: upstream local path resolves inside the downstream repo (upstream=%s, downstream=%s) — cannot integrate a repo against itself: %w",
		upstream, downstream, sdktypes.ErrSelfIntegration,
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/integrate/ -run TestEnsureNotSelfIntegration_pathCheck -v`
Expected: all subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/self_guard.go internal/integrate/self_guard_test.go
git commit -m "feat(integrate): add self-guard path check"
```

---

## Task 4: URL check in `self_guard.go` — TDD

**Files:**
- Modify: `internal/integrate/self_guard.go`
- Modify: `internal/integrate/self_guard_test.go`

- [ ] **Step 1: Write the failing tests (URL check)**

Append to `internal/integrate/self_guard_test.go`:

```go
func TestEnsureNotSelfIntegration_urlCheck(t *testing.T) {
	t.Run("empty upstreamURL skips URL check", func(t *testing.T) {
		downstream := t.TempDir()
		err := EnsureNotSelfIntegration(downstream, "", "")
		assert.NoError(t, err)
	})

	t.Run("downstream not a git repo — URL check skipped", func(t *testing.T) {
		downstream := t.TempDir() // no git init
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		assert.NoError(t, err)
	})

	t.Run("downstream has no origin — URL check skipped", func(t *testing.T) {
		downstream := initGitRepo(t) // helper defined below; no remotes added
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		assert.NoError(t, err)
	})

	t.Run("origin exactly matches upstream URL", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemote(t, downstream, "origin", "git@github.com:acme/foo.git")
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
		assert.Contains(t, err.Error(), "self-integration blocked")
		assert.Contains(t, err.Error(), "origin remote")
	})

	t.Run("origin SSH matches upstream HTTPS same repo", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemote(t, downstream, "origin", "git@github.com:acme/foo.git")
		err := EnsureNotSelfIntegration(downstream, "https://github.com/acme/foo", "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})

	t.Run("origin differs from upstream — no error", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemote(t, downstream, "origin", "git@github.com:acme/downstream.git")
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/upstream.git", "")
		assert.NoError(t, err)
	})

	t.Run("non-origin remote matches — no error (origin-only policy)", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemote(t, downstream, "origin", "git@github.com:acme/downstream.git")
		addRemote(t, downstream, "upstream", "git@github.com:acme/foo.git")
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		assert.NoError(t, err)
	})

	t.Run("origin has multiple URLs, one matches", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemoteMultiURL(t, downstream, "origin", []string{
			"git@github.com:acme/downstream.git",
			"https://github.com/acme/foo",
		})
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})
}
```

At the bottom of the same test file, add the helpers:

```go
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)
	return dir
}

func addRemote(t *testing.T, dir, name, url string) {
	t.Helper()
	addRemoteMultiURL(t, dir, name, []string{url})
}

func addRemoteMultiURL(t *testing.T, dir, name string, urls []string) {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{Name: name, URLs: urls})
	require.NoError(t, err)
}
```

Add the imports at the top of the test file:

```go
gogit "github.com/go-git/go-git/v6"
gitconfig "github.com/go-git/go-git/v6/config"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/integrate/ -run TestEnsureNotSelfIntegration_urlCheck -v`
Expected: subtests that expect errors FAIL (the URL check isn't implemented yet); the "skipped" subtests may pass by default.

- [ ] **Step 3: Implement URL check**

Edit `internal/integrate/self_guard.go`:

Add imports at the top:

```go
gogit "github.com/go-git/go-git/v6"
```

Update `EnsureNotSelfIntegration` to call the URL check:

```go
func EnsureNotSelfIntegration(downstreamRepoPath, upstreamURL, upstreamLocalPath string) error {
	if err := selfGuardPathCheck(downstreamRepoPath, upstreamLocalPath); err != nil {
		return err
	}
	if err := selfGuardURLCheck(downstreamRepoPath, upstreamURL); err != nil {
		return err
	}
	return nil
}
```

Add the URL check function to the same file:

```go
func selfGuardURLCheck(downstreamRepoPath, upstreamURL string) error {
	if upstreamURL == "" {
		return nil
	}

	repo, err := gogit.PlainOpen(downstreamRepoPath)
	if err != nil {
		if err == gogit.ErrRepositoryNotExists {
			return nil // no git repo, nothing to compare
		}
		return fmt.Errorf("opening downstream repo for self-integration check: %w", err)
	}

	origin, err := repo.Remote("origin")
	if err != nil {
		if err == gogit.ErrRemoteNotFound {
			return nil // no origin, nothing to compare
		}
		return fmt.Errorf("reading origin remote: %w", err)
	}

	targetKey := NormalizeUpstreamURL(upstreamURL, "")
	for _, remoteURL := range origin.Config().URLs {
		if NormalizeUpstreamURL(remoteURL, "") == targetKey {
			return newSelfIntegrationURLError(upstreamURL, remoteURL)
		}
	}
	return nil
}

func newSelfIntegrationURLError(upstreamURL, matchedOriginURL string) error {
	return fmt.Errorf(
		"self-integration blocked: upstream URL matches the downstream's origin remote (upstream=%s, origin=%s) — cannot integrate a repo against itself: %w",
		upstreamURL, matchedOriginURL, sdktypes.ErrSelfIntegration,
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/integrate/ -run TestEnsureNotSelfIntegration -v`
Expected: all subtests (path AND URL) PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/self_guard.go internal/integrate/self_guard_test.go
git commit -m "feat(integrate): add self-guard URL check"
```

---

## Task 5: Wire guard into `integrateOneInternal`

**Files:**
- Modify: `internal/integrate/integrate.go`
- Modify: `internal/integrate/integrate_test.go` (add wiring test)

- [ ] **Step 1: Write the failing integration test**

Append to `internal/integrate/integrate_test.go`:

```go
func Test_integrateOneInternal_blocksSelfIntegrationByURL(t *testing.T) {
	downstream := t.TempDir()
	repo, err := gogit.PlainInit(downstream, false)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"git@github.com:acme/foo.git"},
	})
	require.NoError(t, err)

	req := &internalRequest{
		DownstreamRepoPath: downstream,
	}
	_, err = integrateOneInternal(req, sdktypes.UpstreamSpec{URL: "https://github.com/acme/foo"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
}
```

Add the required imports to the test file if not already present:

```go
"errors"
gogit "github.com/go-git/go-git/v6"
gitconfig "github.com/go-git/go-git/v6/config"
"github.com/rockholla/gitspork/v2/internal/sdktypes"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrate/ -run Test_integrateOneInternal_blocksSelfIntegrationByURL -v`
Expected: FAIL — `integrateOneInternal` currently tries to clone the upstream and fails on network/auth instead of returning `ErrSelfIntegration`.

- [ ] **Step 3: Insert guard call at the top of `integrateOneInternal`**

Open `internal/integrate/integrate.go`. Find the `integrateOneInternal` function (currently around line 176). Immediately after the line:

```go
upstream.Subpath = config.NormalizeUpstreamPath(upstream.Subpath)
```

insert:

```go
if err := EnsureNotSelfIntegration(req.DownstreamRepoPath, upstream.URL, ""); err != nil {
	return sdktypes.IntegratedUpstream{}, err
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/integrate/ -v`
Expected: the new test PASSES; all existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/integrate.go internal/integrate/integrate_test.go
git commit -m "feat(integrate): guard integrateOneInternal against self-integration"
```

---

## Task 6: Wire guard into `IntegrateLocal`

**Files:**
- Modify: `internal/integrate/integrate_local.go`
- Modify: `internal/integrate/integrate_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/integrate/integrate_test.go`:

```go
func TestIntegrateLocal_blocksSelfIntegrationByPath(t *testing.T) {
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)
	// Give it a valid .gitspork.yml so we'd otherwise progress past parsing.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitspork.yml"), []byte("{}\n"), 0644))

	_, err = IntegrateLocal(&sdktypes.IntegrateLocalOptions{
		UpstreamPaths:  []string{dir},
		DownstreamPath: dir,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
}
```

Ensure `os` and `path/filepath` are imported in `integrate_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrate/ -run TestIntegrateLocal_blocksSelfIntegrationByPath -v`
Expected: FAIL — either the integration proceeds past the guard, or a downstream failure (like a config-parsing error) occurs before the guard, but not `ErrSelfIntegration`.

- [ ] **Step 3: Insert guard call at the top of the loop**

Open `internal/integrate/integrate_local.go`. Find the `for _, upstreamPath := range opts.UpstreamPaths {` loop. Immediately at the top of the loop body (before the existing `opts.Logger.Log(...)` call), insert:

```go
if err := EnsureNotSelfIntegration(opts.DownstreamPath, "", upstreamPath); err != nil {
	return result, err
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/integrate/ -v`
Expected: the new test PASSES; existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/integrate_local.go internal/integrate/integrate_test.go
git commit -m "feat(integrate): guard IntegrateLocal against self-integration"
```

---

## Task 7: CLI exit code 3 — intercept in `integrate` subcommand

**Files:**
- Modify: `internal/cli/integrate.go`

- [ ] **Step 1: Add the intercept**

Open `internal/cli/integrate.go`. Locate the `RunE` closure. Find the block:

```go
if _, err := integrate.Integrate(opts); err != nil {
	return err
}
return nil
```

Replace with:

```go
if _, err := integrate.Integrate(opts); err != nil {
	if errors.Is(err, sdktypes.ErrSelfIntegration) {
		logger.Log("%v", err)
		os.Exit(3)
	}
	return err
}
return nil
```

Add the required imports to the top of the file:

```go
"errors"
"os"
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/integrate.go
git commit -m "feat(cli): exit 3 on self-integration for integrate"
```

---

## Task 8: CLI exit code 3 — intercept in `integrate-local` subcommand

**Files:**
- Modify: `internal/cli/integrate_local.go`

- [ ] **Step 1: Add the intercept**

Open `internal/cli/integrate_local.go`. Locate the `RunE` closure. Find the block:

```go
if _, err := integrate.IntegrateLocal(&sdktypes.IntegrateLocalOptions{
	Logger:         logger,
	UpstreamPaths:  upstreamPaths,
	DownstreamPath: downstreamPath,
	ForceRePrompt:  forceRePrompt,
}); err != nil {
	return err
}
return nil
```

Replace with:

```go
if _, err := integrate.IntegrateLocal(&sdktypes.IntegrateLocalOptions{
	Logger:         logger,
	UpstreamPaths:  upstreamPaths,
	DownstreamPath: downstreamPath,
	ForceRePrompt:  forceRePrompt,
}); err != nil {
	if errors.Is(err, sdktypes.ErrSelfIntegration) {
		logger.Log("%v", err)
		os.Exit(3)
	}
	return err
}
return nil
```

Add the required imports:

```go
"errors"
"os"
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/integrate_local.go
git commit -m "feat(cli): exit 3 on self-integration for integrate-local"
```

---

## Task 9: CLI exit code 3 — intercept in `check-drift` subcommand

**Files:**
- Modify: `internal/cli/check_drift.go`

- [ ] **Step 1: Add the intercept**

Open `internal/cli/check_drift.go`. Locate:

```go
report, err := drift.CheckDrift(opts)
if err != nil && !errors.Is(err, sdktypes.ErrDriftDetected) {
	return err
}
```

Change to:

```go
report, err := drift.CheckDrift(opts)
if err != nil && !errors.Is(err, sdktypes.ErrDriftDetected) {
	if errors.Is(err, sdktypes.ErrSelfIntegration) {
		logger.Log("%v", err)
		os.Exit(3)
	}
	return err
}
```

`errors` and `os` are already imported in this file — verify with `grep -n "^import\|\"errors\"\|\"os\"" internal/cli/check_drift.go`.

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/check_drift.go
git commit -m "feat(cli): exit 3 on self-integration for check-drift"
```

---

## Task 10: Functional test — `integrate` blocked by matching origin

**Files:**
- Create: `test/functional/self_integration_test.go`

- [ ] **Step 1: Write the failing test**

Create `test/functional/self_integration_test.go`:

```go
//go:build functional || functional_docker

package functional

import (
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/stretchr/testify/require"
)

// TestIntegrate_selfIntegrationBlocked_byOrigin verifies that when the
// downstream has its origin remote pointing at the upstream URL, `gitspork
// integrate` refuses to proceed and exits with code 3.
func TestIntegrate_selfIntegrationBlocked_byOrigin(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	upstreamURL := "file://" + upstreamDir
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)

	// Point downstream's origin at the upstream.
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{upstreamURL},
	})
	require.NoError(t, err)

	runner := resolveRunner(t, upstreamDir, downstreamDir)
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 3, code, "expected exit code 3 on self-integration; got %d\nout:\n%s", code, out)
	require.True(t, strings.Contains(out, "self-integration blocked"),
		"expected self-integration message in output:\n%s", out)
}
```

- [ ] **Step 2: Run test**

Run: `make test-functional TESTFLAGS="-run TestIntegrate_selfIntegrationBlocked_byOrigin -v"`

(If your local `make test-functional` target doesn't pass through `TESTFLAGS`, use the equivalent direct command:
`go test -tags "functional,testharness" -run TestIntegrate_selfIntegrationBlocked_byOrigin -v ./test/functional/`)

Expected: PASS (guard is already implemented; this is a red-black test that would only fail if wiring drifts).

- [ ] **Step 3: Commit**

```bash
git add test/functional/self_integration_test.go
git commit -m "test(functional): self-integration blocks integrate with matching origin"
```

---

## Task 11: Functional test — `integrate-local` blocked by same path

**Files:**
- Modify: `test/functional/self_integration_test.go`

- [ ] **Step 1: Add the test**

Append to `test/functional/self_integration_test.go`:

```go
// TestIntegrateLocal_selfIntegrationBlocked_samePath verifies that
// `gitspork integrate-local --upstream-path X --downstream-path X` refuses
// to proceed and exits with code 3.
func TestIntegrateLocal_selfIntegrationBlocked_samePath(t *testing.T) {
	// Use the simple upstream as both upstream and downstream: the upstream
	// directory is a valid gitspork upstream, so the guard is the only thing
	// that should stop integration.
	dir := buildSimpleUpstream(t)
	runner := resolveRunner(t, dir, dir)

	out, code := runner.Run(t, []string{
		"integrate-local",
		"--upstream-path", dir,
		"--downstream-path", dir,
	}, dir)
	require.Equal(t, 3, code, "expected exit code 3 on self-integration; got %d\nout:\n%s", code, out)
	require.True(t, strings.Contains(out, "self-integration blocked"),
		"expected self-integration message in output:\n%s", out)
}
```

- [ ] **Step 2: Run test**

Run: `go test -tags "functional,testharness" -run TestIntegrateLocal_selfIntegrationBlocked_samePath -v ./test/functional/`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/functional/self_integration_test.go
git commit -m "test(functional): self-integration blocks integrate-local with same path"
```

---

## Task 12: Functional test — `check-drift` blocked by matching origin

**Files:**
- Modify: `test/functional/self_integration_test.go`

- [ ] **Step 1: Add the test**

Append to `test/functional/self_integration_test.go`:

```go
// TestCheckDrift_selfIntegrationBlocked_byOrigin verifies that check-drift
// refuses to re-integrate when the stored state's upstream URL matches the
// downstream's origin remote, exiting with code 3.
//
// Setup: run integrate once with the origin NOT set (so it succeeds and
// writes state), then add origin pointing at the upstream URL, then run
// check-drift.
func TestCheckDrift_selfIntegrationBlocked_byOrigin(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	upstreamURL := "file://" + upstreamDir
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)

	runner := resolveRunner(t, upstreamDir, downstreamDir)
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "initial integrate failed:\n%s", out)

	// Commit downstream state so check-drift's clean-tree precondition passes.
	// Matches the pattern used at test/functional/check_drift_test.go:20.
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// Now add the matching origin remote after the integrate.
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{upstreamURL},
	})
	require.NoError(t, err)

	out, code = runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 3, code, "expected exit code 3 on self-integration; got %d\nout:\n%s", code, out)
	require.True(t, strings.Contains(out, "self-integration blocked"),
		"expected self-integration message in output:\n%s", out)
}
```

- [ ] **Step 2: Run test**

Run: `go test -tags "functional,testharness" -run TestCheckDrift_selfIntegrationBlocked_byOrigin -v ./test/functional/`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/functional/self_integration_test.go
git commit -m "test(functional): self-integration blocks check-drift with matching origin"
```

---

## Task 13: SDK sentinel re-export test

**Files:**
- Modify: `test/sdk/sdk_test.go` (append) OR create `test/sdk/self_integration_test.go`

- [ ] **Step 1: Add the test**

Create `test/sdk/self_integration_test.go`:

```go
//go:build sdk

package sdk_test

import (
	"errors"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/rockholla/gitspork/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrate_selfIntegration_returnsSentinel verifies that
// gitspork.Integrate returns an error that unwraps to gitspork.ErrSelfIntegration
// when the downstream's origin remote matches the upstream URL. Guards the
// sentinel re-export from the internal sdktypes package.
func TestIntegrate_selfIntegration_returnsSentinel(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	upstreamURL := "file://" + upstreamDir
	downstreamDir := emptyDownstream(t)

	// Give the downstream an origin remote pointing at the upstream URL.
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{upstreamURL},
	})
	require.NoError(t, err)

	_, err = gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: upstreamURL, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, gitspork.ErrSelfIntegration),
		"expected err to unwrap to gitspork.ErrSelfIntegration; got: %v", err)
}
```

Notes on the helpers used:
- `minimalUpstream(t)` (test/sdk/helpers_test.go:21) returns `(dir, hash)`; we ignore the hash.
- `emptyDownstream(t)` (test/sdk/helpers_test.go:43) delegates to `testharness.EmptyDownstream`, which does a `gogit.PlainInit` — so `PlainOpen` + `CreateRemote` works directly without an extra init.

- [ ] **Step 2: Run test**

Run: `make test-sdk` (or `go test -tags "sdk,testharness" -run TestIntegrate_selfIntegration_returnsSentinel -v ./test/sdk/`)

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/sdk/self_integration_test.go
git commit -m "test(sdk): ErrSelfIntegration is re-exported from root package"
```

---

## Task 14: Documentation — exit codes reference

**Files:**
- Modify: `docs/README.md`

`docs/README.md` currently has no "Exit codes" section (verified — `grep -c "exit code" docs/README.md` returns 0). Add one.

- [ ] **Step 1: Add an Exit codes section**

Append this section to the end of `docs/README.md`:

```markdown
## Exit codes

- `0` — success.
- `1` — generic failure (any error not covered by a dedicated code).
- `2` — drift detected (returned by `check-drift` when the downstream has diverged from the recorded upstream state).
- `3` — self-integration blocked (returned by `integrate`, `integrate-local`, and `check-drift` when the upstream and downstream identify the same repo).
```

- [ ] **Step 2: Commit**

```bash
git add docs/README.md
git commit -m "docs: document CLI exit codes including new self-integration code 3"
```

---

## Task 15: Full test suite verification

- [ ] **Step 1: Run unit tests**

Run: `make test-unit`
Expected: all tests pass.

- [ ] **Step 2: Run functional tests**

Run: `make test-functional`
Expected: all tests pass.

- [ ] **Step 3: Run SDK tests**

Run: `make test-sdk`
Expected: all tests pass.

- [ ] **Step 4: Run examples tests**

Run: `make test-examples`
Expected: all tests pass (should be unaffected — the docs/examples upstream repos are distinct dirs from the downstream test harness so the guard won't trip).

- [ ] **Step 5: Push branch and open PR**

```bash
git push -u origin feat/self-integration-guard
gh pr create --title "feat: self-integration guard for integrate / integrate-local / check-drift" --body "$(cat <<'EOF'
## Summary
- Blocks integrate / integrate-local / check-drift from operating when the upstream identifies the same repo as the downstream.
- Two independent checks: path equality (local integrations) and upstream URL vs downstream `origin` remote (URL-based integrations). Only `origin` is checked so developer-added `upstream` convenience remotes don't trip the guard.
- New sentinel `gitspork.ErrSelfIntegration` (re-exported from `internal/sdktypes`).
- New CLI exit code `3` for self-integration; documented alongside existing codes (1 generic, 2 drift).

Spec: `docs/superpowers/specs/2026-07-31-self-integration-guard-design.md`
Plan: `docs/superpowers/plans/2026-07-31-self-integration-guard.md`

## Test plan
- [ ] `make test-unit` covers `EnsureNotSelfIntegration` (path + URL branches) and the wire-up at `integrateOneInternal` / `IntegrateLocal`.
- [ ] `make test-functional` covers exit code 3 end-to-end for all three subcommands.
- [ ] `make test-sdk` covers the sentinel re-export.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: PR URL printed.
