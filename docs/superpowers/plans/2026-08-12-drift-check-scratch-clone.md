# Drift-check scratch clone Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `gitspork.CheckDrift` operate against a temporary scratch clone of the caller's repo instead of the caller's working tree, so no drift-check code path can mutate files in the caller's directory.

**Architecture:** Add two small shell-git-driven helpers (`provisionScratchClone`, `ensureStateFilePresent`) in a new `internal/drift/scratch_clone.go` file, then rewire `CheckDrift` to provision a scratch clone at the caller's HEAD and route every subsequent go-git worktree operation at the scratch clone. The caller's path stays in scope only for the read-only cleanliness gate and state lookup. The restore-checkout defer that currently defends the caller from the mutating flow is deleted — with no caller mutation, there's nothing to restore.

**Tech Stack:** Go 1.26, shell git (via `os/exec`), go-git v6, existing `internal/drift/` and `test/testharness/` packages, `stretchr/testify` for assertions.

**Spec:** `docs/superpowers/specs/2026-08-12-drift-check-scratch-clone-design.md`

---

## File Structure

**New files:**
- `internal/drift/scratch_clone.go` — `provisionScratchClone` and `ensureStateFilePresent` helpers. Single responsibility: producing (and disposing of) a scratch git clone of the caller's repo pinned to the caller's HEAD hash. Depends on `os/exec` (shell git) and `os` (filesystem). No go-git.
- `internal/drift/scratch_clone_test.go` — unit tests for both helpers.

**Modified files:**
- `internal/drift/check_drift.go` — `CheckDrift` gets rewired to use the scratch clone; the restore-checkout defer is removed.
- `internal/drift/check_drift_test.go` — one existing test gets renamed + retargeted to the new "caller untouched" invariant; new invariant test and regression test get added.

**Unchanged:** `internal/integrate/drift_check.go`, `gitspork.go` (public SDK), `internal/cli/check_drift.go`, the `cnd-upgrade-from-foundation` wrapper.

---

## Task 1: Add `provisionScratchClone` helper (TDD)

**Files:**
- Create: `internal/drift/scratch_clone.go`
- Create: `internal/drift/scratch_clone_test.go`

Establish the shell-git-driven scratch-clone helper on its own before wiring it into `CheckDrift`. TDD: happy path first, then detached-HEAD, then cleanup idempotence, then bad-source-path error.

- [ ] **Step 1: Write the failing happy-path test**

Add to `internal/drift/scratch_clone_test.go`:

```go
package drift

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_provisionScratchClone_happyPath(t *testing.T) {
	src := t.TempDir()
	makeBaselineRepo(t, src)

	srcHead := gitRevParseHEAD(t, src)

	scratch, cleanup, err := provisionScratchClone(src)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	// Scratch is a git repo pinned to src's HEAD hash.
	scratchHead := gitRevParseHEAD(t, scratch)
	assert.Equal(t, srcHead, scratchHead, "scratch clone HEAD should match caller HEAD")

	// Tracked files are present with the same content.
	got, err := os.ReadFile(filepath.Join(scratch, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "baseline content", string(got))
}

// gitRevParseHEAD is a small shell-git wrapper used by these tests to read
// HEAD hashes without pulling in go-git.
func gitRevParseHEAD(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-c", "safe.directory=*", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags testharness -run Test_provisionScratchClone -v ./internal/drift/...`
Expected: FAIL — `provisionScratchClone` is undefined.

- [ ] **Step 3: Implement the minimal `provisionScratchClone`**

Create `internal/drift/scratch_clone.go`:

```go
package drift

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// provisionScratchClone clones callerRepoPath into a temporary directory and
// checks it out at the caller's exact HEAD hash. Returns the scratch path plus
// a cleanup func the caller must defer.
//
// The scratch clone is a fully functional git repo with its own object store,
// so any writes gitspork's drift-check flow performs (temporary branch,
// staging, commit) land in the scratch and are removed when cleanup runs.
// This keeps the caller's working tree strictly untouched by CheckDrift.
func provisionScratchClone(callerRepoPath string) (string, func(), error) {
	scratchPath, err := os.MkdirTemp("", "gitspork-drift-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("error creating scratch temp dir: %v", err)
	}
	cleanup := func() { _ = os.RemoveAll(scratchPath) }

	callerHead, err := shellGitOutput(callerRepoPath, "rev-parse", "HEAD")
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("error resolving caller HEAD hash: %v", err)
	}

	// --local hardlinks the object store (cheap on time and disk). --no-checkout
	// skips the initial checkout so we can pin explicitly to the caller's HEAD
	// hash below — necessary when the caller is on a detached HEAD (e.g. CI).
	if _, err := shellGitOutput("", "clone", "--local", "--no-checkout", callerRepoPath, scratchPath); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("error cloning caller repo to scratch: %v", err)
	}
	if _, err := shellGitOutput(scratchPath, "-c", "advice.detachedHead=false", "checkout", callerHead); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("error checking out caller HEAD in scratch: %v", err)
	}

	return scratchPath, cleanup, nil
}

// shellGitOutput runs `git <args...>` (with -c safe.directory=* prepended) and
// returns trimmed stdout, wrapping stderr into the error on failure so callers
// see the real git message.
//
// dir="" runs from the current working directory (used for `clone` where -C is
// meaningless).
func shellGitOutput(dir string, args ...string) (string, error) {
	full := []string{"-c", "safe.directory=*"}
	if dir != "" {
		full = append(full, "-C", dir)
	}
	full = append(full, args...)

	cmd := exec.Command("git", full...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
```

- [ ] **Step 4: Run happy-path test to verify it passes**

Run: `go test -tags testharness -run Test_provisionScratchClone_happyPath -v ./internal/drift/...`
Expected: PASS.

- [ ] **Step 5: Add the detached-HEAD test**

Add to `internal/drift/scratch_clone_test.go`:

```go
func Test_provisionScratchClone_detachedHEADSource(t *testing.T) {
	src := t.TempDir()
	makeBaselineRepo(t, src)
	srcHead := gitRevParseHEAD(t, src)

	// Detach source HEAD by checking out the commit hash directly.
	require.NoError(t, exec.Command("git", "-c", "safe.directory=*", "-C", src, "-c", "advice.detachedHead=false", "checkout", srcHead).Run())

	scratch, cleanup, err := provisionScratchClone(src)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	assert.Equal(t, srcHead, gitRevParseHEAD(t, scratch), "scratch must land on the caller's exact HEAD hash even when source is detached")
}
```

- [ ] **Step 6: Run detached-HEAD test to verify it passes**

Run: `go test -tags testharness -run Test_provisionScratchClone_detachedHEADSource -v ./internal/drift/...`
Expected: PASS.

- [ ] **Step 7: Add the cleanup-idempotence test**

Add to `internal/drift/scratch_clone_test.go`:

```go
func Test_provisionScratchClone_cleanupIsIdempotent(t *testing.T) {
	src := t.TempDir()
	makeBaselineRepo(t, src)

	scratch, cleanup, err := provisionScratchClone(src)
	require.NoError(t, err)

	cleanup()
	// Calling cleanup a second time must not panic and must not return the dir.
	assert.NotPanics(t, cleanup)

	_, statErr := os.Stat(scratch)
	assert.True(t, os.IsNotExist(statErr), "scratch dir should be gone after cleanup")
}
```

- [ ] **Step 8: Run cleanup test to verify it passes**

Run: `go test -tags testharness -run Test_provisionScratchClone_cleanupIsIdempotent -v ./internal/drift/...`
Expected: PASS (`os.RemoveAll` is idempotent on missing paths, and calling it twice is safe).

- [ ] **Step 9: Add the bad-source test**

Add to `internal/drift/scratch_clone_test.go`:

```go
func Test_provisionScratchClone_failsOnNonRepo(t *testing.T) {
	src := t.TempDir() // no git init

	_, cleanup, err := provisionScratchClone(src)
	if cleanup != nil {
		defer cleanup()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving caller HEAD hash",
		"error should identify the rev-parse step that failed")
}
```

- [ ] **Step 10: Run bad-source test to verify it passes**

Run: `go test -tags testharness -run Test_provisionScratchClone_failsOnNonRepo -v ./internal/drift/...`
Expected: PASS.

- [ ] **Step 11: Run `go vet` on the drift package**

Run: `go vet -tags testharness ./internal/drift/...`
Expected: no output (clean).

- [ ] **Step 12: Run the whole drift package test suite**

Run: `go test -tags testharness -v ./internal/drift/...`
Expected: PASS.

- [ ] **Step 13: Commit**

```bash
git add internal/drift/scratch_clone.go internal/drift/scratch_clone_test.go
git commit -m "feat(drift): add provisionScratchClone helper"
```

---

## Task 2: Add `ensureStateFilePresent` helper (TDD)

**Files:**
- Modify: `internal/drift/scratch_clone.go`
- Modify: `internal/drift/scratch_clone_test.go`

Edge case: caller ran integrate but has not committed `.gitspork/downstream-state.json`, and it is `.gitignore`d out of a clean tree. The scratch clone would then be missing the state file needed by `LoadDownstreamState`. Belt-and-suspenders: copy it across.

- [ ] **Step 1: Write the failing test**

Add to `internal/drift/scratch_clone_test.go`:

```go
func Test_ensureStateFilePresent_copiesWhenMissingInScratch(t *testing.T) {
	src := t.TempDir()
	scratch := t.TempDir()

	// Simulate the edge case: state file exists in caller but scratch was
	// provisioned without it (git clone of a repo where the file is ignored
	// and never committed).
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".gitspork"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".gitspork/downstream-state.json"), []byte(`{"upstreams":[]}`), 0644))

	require.NoError(t, ensureStateFilePresent(src, scratch))

	got, err := os.ReadFile(filepath.Join(scratch, ".gitspork/downstream-state.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"upstreams":[]}`, string(got))
}

func Test_ensureStateFilePresent_noopWhenAlreadyInScratch(t *testing.T) {
	src := t.TempDir()
	scratch := t.TempDir()

	// State present in both (normal case: git clone brought it over).
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".gitspork"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".gitspork/downstream-state.json"), []byte(`{"upstreams":["src"]}`), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(scratch, ".gitspork"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(scratch, ".gitspork/downstream-state.json"), []byte(`{"upstreams":["scratch"]}`), 0644))

	require.NoError(t, ensureStateFilePresent(src, scratch))

	// The scratch's existing file wins — this helper never overwrites.
	got, err := os.ReadFile(filepath.Join(scratch, ".gitspork/downstream-state.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"upstreams":["scratch"]}`, string(got))
}

func Test_ensureStateFilePresent_noopWhenAbsentInBoth(t *testing.T) {
	src := t.TempDir()
	scratch := t.TempDir()

	// Nothing to copy: no state file anywhere. The downstream state loader
	// will produce its own not-found error later; this helper stays quiet.
	require.NoError(t, ensureStateFilePresent(src, scratch))

	_, err := os.Stat(filepath.Join(scratch, ".gitspork/downstream-state.json"))
	assert.True(t, os.IsNotExist(err))
}
```

Add `"path/filepath"` back to `scratch_clone_test.go`'s imports if not already present (it's used by these tests).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags testharness -run Test_ensureStateFilePresent -v ./internal/drift/...`
Expected: FAIL — `ensureStateFilePresent` is undefined.

- [ ] **Step 3: Implement `ensureStateFilePresent`**

Add to `internal/drift/scratch_clone.go` (and add `"path/filepath"` to the import block — Task 1's implementation didn't need it, but this helper does):

```go
// ensureStateFilePresent copies .gitspork/downstream-state.json from callerPath
// into scratchPath when it exists in caller but not scratch. Handles the edge
// case where the caller has integrated but not committed the state file (and
// possibly ignored it), so `git clone --local` did not bring it over. When the
// file already exists in scratch, this is a no-op; the caller version is never
// preferred over what git cloned.
func ensureStateFilePresent(callerPath, scratchPath string) error {
	const rel = ".gitspork/downstream-state.json"
	if _, err := os.Stat(filepath.Join(scratchPath, rel)); err == nil {
		return nil // scratch already has it — the normal path
	}
	src := filepath.Join(callerPath, rel)
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // caller doesn't have it either — LoadDownstreamState will surface the right error
		}
		return fmt.Errorf("error reading caller state file %s: %v", src, err)
	}
	dst := filepath.Join(scratchPath, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("error creating scratch state dir: %v", err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("error writing scratch state file: %v", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags testharness -run Test_ensureStateFilePresent -v ./internal/drift/...`
Expected: PASS on all three.

- [ ] **Step 5: Run the whole drift package test suite**

Run: `go test -tags testharness -v ./internal/drift/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/drift/scratch_clone.go internal/drift/scratch_clone_test.go
git commit -m "feat(drift): add ensureStateFilePresent helper for uncommitted state edge case"
```

---

## Task 3: Add the "caller untouched" invariant test

**Files:**
- Modify: `internal/drift/check_drift_test.go`

Write the test that expresses the invariant we're establishing: `CheckDrift` must not modify any file in the caller's working tree. This test may pass against the current implementation for the specific test scenarios below (the ignored-file bug requires specific gitignore-disagreement conditions to trigger), but locks the invariant in against future regressions.

- [ ] **Step 1: Write the failing (or passing-but-load-bearing) test**

Add to `internal/drift/check_drift_test.go`:

```go
func TestCheckDrift_leavesCallerWorkingTreeByteIdentical(t *testing.T) {
	// Invariant: CheckDrift must not modify any file in the caller's working
	// tree, regardless of whether drift is detected. This test snapshots every
	// non-.git file before and after CheckDrift and asserts byte-equality.

	t.Run("no drift path", func(t *testing.T) {
		upstreamDir, _ := testharness.MinimalUpstream(t)
		downstreamDir := testharness.EmptyDownstream(t)
		testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)

		before := snapshotWorktree(t, downstreamDir)

		_, err := CheckDrift(&sdktypes.CheckDriftOptions{
			Logger:             logutil.New(),
			DownstreamRepoPath: downstreamDir,
		})
		require.NoError(t, err)

		after := snapshotWorktree(t, downstreamDir)
		assert.Equal(t, before, after, "CheckDrift must leave the caller worktree byte-identical")
	})

	t.Run("drift detected path", func(t *testing.T) {
		upstreamDir, _ := testharness.MinimalUpstream(t)
		downstreamDir := testharness.EmptyDownstream(t)
		testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)
		testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", "drifted\n")

		before := snapshotWorktree(t, downstreamDir)

		_, err := CheckDrift(&sdktypes.CheckDriftOptions{
			Logger:             logutil.New(),
			DownstreamRepoPath: downstreamDir,
		})
		require.ErrorIs(t, err, sdktypes.ErrDriftDetected)

		after := snapshotWorktree(t, downstreamDir)
		assert.Equal(t, before, after, "CheckDrift must leave the caller worktree byte-identical even when drift is detected")
	})
}

// snapshotWorktree returns a map of repo-relative path -> sha256 hex for every
// non-.git file under dir. Used by the caller-untouched invariant test.
func snapshotWorktree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
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
		out[rel] = fmt.Sprintf("%x", sha256.Sum256(b))
		return nil
	})
	require.NoError(t, err)
	return out
}
```

Add `"crypto/sha256"` and `"fmt"` to the imports at the top of the file if not already present.

- [ ] **Step 2: Run the test to see current behavior**

Run: `go test -tags testharness -run TestCheckDrift_leavesCallerWorkingTreeByteIdentical -v ./internal/drift/...`
Expected: Either PASS (current code happens to be byte-preserving in these scenarios) or FAIL. Either outcome is acceptable — this test's role is to lock in the invariant against the refactor and future regressions.

- [ ] **Step 3: Commit**

```bash
git add internal/drift/check_drift_test.go
git commit -m "test(drift): assert CheckDrift leaves caller worktree byte-identical"
```

---

## Task 4: Refactor `CheckDrift` to run against the scratch clone

**Files:**
- Modify: `internal/drift/check_drift.go`

Rewire `CheckDrift` so all mutating operations target the scratch clone. Delete the restore-checkout defer that only existed to defend the caller.

- [ ] **Step 1: Replace the mutating-flow section of `CheckDrift`**

In `internal/drift/check_drift.go`, keep the input-resolution and cleanliness sections through `checkCleanWorkingTree` unchanged.

Immediately after `checkCleanWorkingTree(opts.DownstreamRepoPath)` returns nil, insert:

```go
	opts.Logger.Log("provisioning scratch clone of %s for drift-check", opts.DownstreamRepoPath)
	scratchPath, cleanup, err := provisionScratchClone(opts.DownstreamRepoPath)
	if err != nil {
		return report, fmt.Errorf("error provisioning scratch clone for drift-check: %w", err)
	}
	defer cleanup()
	if err := ensureStateFilePresent(opts.DownstreamRepoPath, scratchPath); err != nil {
		return report, fmt.Errorf("error preparing scratch clone: %w", err)
	}
	opts.Logger.Log("running drift-check against scratch clone at %s", scratchPath)
```

Then replace the rest of the function so every subsequent go-git / worktree operation uses `scratchPath` instead of `opts.DownstreamRepoPath`. The full replacement (from just after `checkCleanWorkingTree`'s call site through the end of the function body) reads:

```go
	opts.Logger.Log("provisioning scratch clone of %s for drift-check", opts.DownstreamRepoPath)
	scratchPath, cleanup, err := provisionScratchClone(opts.DownstreamRepoPath)
	if err != nil {
		return report, fmt.Errorf("error provisioning scratch clone for drift-check: %w", err)
	}
	defer cleanup()
	if err := ensureStateFilePresent(opts.DownstreamRepoPath, scratchPath); err != nil {
		return report, fmt.Errorf("error preparing scratch clone: %w", err)
	}
	opts.Logger.Log("running drift-check against scratch clone at %s", scratchPath)

	repo, err := gogit.PlainOpen(scratchPath)
	if err != nil {
		return report, fmt.Errorf("error opening scratch clone: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return report, fmt.Errorf("error accessing scratch clone worktree: %v", err)
	}

	headRef, err := repo.Head()
	if err != nil {
		return report, fmt.Errorf("error resolving scratch HEAD: %v", err)
	}

	driftBranchRef := plumbing.NewBranchReferenceName(driftCheckBranch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(driftBranchRef, headRef.Hash())); err != nil {
		return report, fmt.Errorf("error creating drift-check branch in scratch: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: driftBranchRef}); err != nil {
		return report, fmt.Errorf("error checking out drift-check branch in scratch: %v", err)
	}

	// Re-integrate each upstream against the scratch clone; track which files
	// each one last touched. fileOwner maps relative file path -> upstream URL
	// that last wrote it.
	fileOwner := map[string]string{}

	for _, entry := range entries {
		opts.Logger.Log("re-integrating upstream %s at commit %s", entry.spec.URL, entry.commitHash)

		beforeFiles, err := listWorktreeFiles(scratchPath)
		if err != nil {
			return report, fmt.Errorf("error listing worktree files before integrate: %v", err)
		}

		if err := integrate.IntegrateForDriftCheck(&integrate.DriftCheckRequest{
			Logger:             opts.Logger,
			DownstreamRepoPath: scratchPath,
			UpstreamURL:        entry.spec.URL,
			UpstreamSubpath:    entry.spec.Subpath,
			UpstreamToken:      entry.spec.Token,
			UpstreamCommit:     entry.commitHash,
			CacheTTL:           opts.CacheTTL,
			NoCache:            opts.NoCache,
			Progress:           opts.Progress,
		}); err != nil {
			return report, fmt.Errorf("error running integration for drift check: %w", err)
		}

		afterFiles, err := listWorktreeFiles(scratchPath)
		if err != nil {
			return report, fmt.Errorf("error listing worktree files after integrate: %v", err)
		}

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
		return report, fmt.Errorf("error diffing scratch against HEAD: %v", err)
	}
	if patch == nil {
		return report, nil
	}

	report.HasDrift = true
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		var name string
		switch {
		case to != nil:
			name = to.Path()
		case from != nil:
			name = from.Path()
		default:
			continue
		}
		diffText, err := encodeFilePatch(fp)
		if err != nil {
			return report, fmt.Errorf("error encoding per-file diff for %s: %v", name, err)
		}
		report.Files = append(report.Files, sdktypes.DriftedFile{
			Path:          name,
			AttributedURL: fileOwner[name], // empty string means unattributed
			Diff:          diffText,
			ColorizedDiff: logutil.ColorizeUnifiedDiff(diffText),
		})
	}

	return report, sdktypes.ErrDriftDetected
}
```

Delete from the previous version of the function (superseded by the block above):

- The `restore := &gogit.CheckoutOptions{Hash: headRef.Hash(), Force: true}` block.
- The `if headRef.Name().IsBranch() { restore = &gogit.CheckoutOptions{Branch: headRef.Name(), Force: true} }` block.
- The `defer func() { _ = wt.Checkout(restore); _ = repo.Storer.RemoveReference(driftBranchRef) }()` block.
- The multi-line comment that documents why `Force: true` is required on the restore.

Also delete the old copies of the drift branch creation, upstream re-integration loop, `diffWorktreeAgainstHEAD` call, and report-population code — they are all covered by the block above.

- [ ] **Step 2: Verify the file compiles**

Run: `go build -tags testharness ./internal/drift/...`
Expected: no output (clean build).

If the build complains about unused `restore` or leftover code, remove those blocks — they are dead weight now.

- [ ] **Step 3: Run the invariant test — this is the key checkpoint**

Run: `go test -tags testharness -run TestCheckDrift_leavesCallerWorkingTreeByteIdentical -v ./internal/drift/...`
Expected: PASS on both sub-tests. The invariant now holds by construction.

- [ ] **Step 4: Run the full drift package suite**

Run: `go test -tags testharness -v ./internal/drift/...`
Expected: PASS. Some tests may report cosmetic differences in narration (new "provisioning scratch clone" / "running drift-check against scratch" INFO lines), but no assertions should fail.

If `TestCheckDrift_cleansUpDriftCheckBranch` passes: good — the drift-check branch was never created in the caller's `.git/`, so `assertDriftCheckBranchAbsent` still holds (trivially now, because we never touched the caller's ref store).

If `TestCheckDrift_restoresWorktreeOnMidLoopFailure` and `TestCheckDrift_restoresWorktreeContentAfterDrift` pass: good — the caller's committed content is on disk because we never touched it. The tests' assertions still hold; only their names/comments will be updated in Task 5.

- [ ] **Step 5: Run every other test tier that touches drift**

Run: `go test -tags testharness -v ./...`
Expected: PASS. Nothing outside `internal/drift/` should be affected — the public SDK contract is unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/drift/check_drift.go
git commit -m "refactor(drift): run CheckDrift against a scratch clone instead of the caller's worktree

CheckDrift previously mutated the caller's working tree (drift branch,
add-all + commit, force-restore) and relied on go-git's gitignore
handling matching shell git's. When it did not (global gitignore,
core.excludesFile), files ignored by shell git could be staged into the
temporary drift-check commit and then removed from the caller's worktree
by the force-restore.

Provision a temporary scratch clone via git clone --local, pinned to the
caller's exact HEAD hash, and run the mutating flow against the scratch.
The caller's directory is observed only for the cleanliness gate and
state lookup. Deleting the scratch dir cleans everything up; no restore
dance needed."
```

---

## Task 5: Update tests with stale "restore"-oriented names and comments

**Files:**
- Modify: `internal/drift/check_drift_test.go`

Two existing tests (`TestCheckDrift_restoresWorktreeOnMidLoopFailure` and `TestCheckDrift_restoresWorktreeContentAfterDrift`) express the property "the caller's worktree ends up right." After the refactor, the mechanism is "we never touched it" rather than "we mutated then restored it." Rename and re-comment so the tests document the new invariant they now defend.

- [ ] **Step 1: Rename and re-comment `TestCheckDrift_restoresWorktreeOnMidLoopFailure`**

In `internal/drift/check_drift_test.go`, replace:

```go
func TestCheckDrift_restoresWorktreeOnMidLoopFailure(t *testing.T) {
	// When re-integration of one upstream mutates worktree files and a *later*
	// upstream fails to integrate, CheckDrift returns an error mid-loop and the
	// deferred restore fires while the worktree still has uncommitted mutations.
	// The restore must succeed and put the worktree back to the caller's original
	// committed content — not leave the upstream-canonical mutations in place.
```

with:

```go
func TestCheckDrift_leavesCallerUntouchedOnMidLoopFailure(t *testing.T) {
	// When re-integration of one upstream succeeds and a *later* upstream fails
	// to integrate, CheckDrift returns an error mid-loop. The caller's worktree
	// must contain the caller's original committed content — not the
	// upstream-canonical content that the earlier upstream wrote into the
	// scratch clone. Since CheckDrift runs against a scratch clone, this holds
	// by construction: the caller's directory is never written to at all.
```

Only rename the function and update the comment — the test body stays as-is; its assertion (final content == `driftedContent`) is still the right check.

At the end of the test body, update the trailing `assert.Equal` message from:

```go
		"restore must overwrite mid-loop worktree mutations left by the earlier upstream, even though those changes are unstaged")
```

to:

```go
		"caller worktree must retain committed content across a mid-loop CheckDrift failure (scratch clone isolates all mutations)")
```

- [ ] **Step 2: Rename and re-comment `TestCheckDrift_restoresWorktreeContentAfterDrift`**

Replace:

```go
func TestCheckDrift_restoresWorktreeContentAfterDrift(t *testing.T) {
	// After CheckDrift returns, the downstream worktree files must match the
	// caller's original HEAD content — CheckDrift must not leave the drifted
	// upstream-canonical content in place of the user's committed content.
```

with:

```go
func TestCheckDrift_leavesCallerContentUntouchedAfterDrift(t *testing.T) {
	// After CheckDrift returns, the downstream worktree files match the
	// caller's original committed content because CheckDrift runs against a
	// scratch clone — the caller's directory is never written to.
```

Update the trailing `assert.Equal` message from:

```go
		"worktree should be restored to the caller's original committed content, not left with the upstream-canonical content used during drift detection")
```

to:

```go
		"caller worktree must match caller's committed content after CheckDrift (scratch clone isolates the upstream-canonical content used during drift detection)")
```

- [ ] **Step 3: Run the drift test suite**

Run: `go test -tags testharness -v ./internal/drift/...`
Expected: PASS. The renames don't change any assertions.

- [ ] **Step 4: Commit**

```bash
git add internal/drift/check_drift_test.go
git commit -m "test(drift): retarget restore tests to the caller-untouched invariant"
```

---

## Task 6: Add the gitignored-files regression test

**Files:**
- Modify: `internal/drift/check_drift_test.go`

Reproduce the reported scenario in a hermetic form: files present in the caller's worktree that shell git ignores via `core.excludesFile` (global gitignore). Assert they survive `CheckDrift`. This test locks in the specific class of files the report was about.

- [ ] **Step 1: Write the failing (or passing-but-load-bearing) test**

Add to `internal/drift/check_drift_test.go`:

```go
func TestCheckDrift_preservesGloballyIgnoredFiles(t *testing.T) {
	// The reported bug: files ignored by the user's global gitignore
	// (core.excludesFile) were silently deleted from the caller's worktree
	// during CheckDrift. Root cause was go-git's gitignore handling not
	// matching shell git's. The scratch-clone flow makes the whole class of
	// caller-worktree mutations impossible, so this regression is locked in
	// by construction.

	upstreamDir, _ := testharness.MinimalUpstream(t)
	downstreamDir := testharness.EmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)

	// Point core.excludesFile at a hermetic gitignore that covers .envrc and
	// .direnv/. Anything shell git would treat as ignored via this file must
	// not be touched by CheckDrift.
	globalIgnore := filepath.Join(t.TempDir(), "global-gitignore")
	require.NoError(t, os.WriteFile(globalIgnore, []byte(".envrc\n.direnv/\n"), 0644))

	require.NoError(t, exec.Command(
		"git", "-c", "safe.directory=*", "-C", downstreamDir,
		"config", "--local", "core.excludesFile", globalIgnore,
	).Run())

	// Populate the caller's worktree with globally-ignored files. checkCleanWorkingTree
	// must still see the tree as clean because these files are ignored.
	envrcPath := filepath.Join(downstreamDir, ".envrc")
	direnvPath := filepath.Join(downstreamDir, ".direnv", "cache.dat")
	require.NoError(t, os.WriteFile(envrcPath, []byte("export FOO=bar\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Dir(direnvPath), 0755))
	require.NoError(t, os.WriteFile(direnvPath, []byte("opaque-cache-blob"), 0644))

	_, err := CheckDrift(&sdktypes.CheckDriftOptions{
		Logger:             logutil.New(),
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err, "CheckDrift should succeed against a clean tree with only globally-ignored untracked files")

	envrcGot, envrcErr := os.ReadFile(envrcPath)
	require.NoError(t, envrcErr, "globally-ignored .envrc must still exist after CheckDrift")
	assert.Equal(t, "export FOO=bar\n", string(envrcGot))

	direnvGot, direnvErr := os.ReadFile(direnvPath)
	require.NoError(t, direnvErr, "globally-ignored .direnv/cache.dat must still exist after CheckDrift")
	assert.Equal(t, "opaque-cache-blob", string(direnvGot))
}
```

Add `"os/exec"` to the imports of `check_drift_test.go` if not already present.

- [ ] **Step 2: Run the test**

Run: `go test -tags testharness -run TestCheckDrift_preservesGloballyIgnoredFiles -v ./internal/drift/...`
Expected: PASS. After the Task 4 refactor, `CheckDrift` operates entirely on the scratch clone; the caller's `.envrc` and `.direnv/cache.dat` are never in a code path that could touch them.

- [ ] **Step 3: Run the full drift suite one more time**

Run: `go test -tags testharness -v ./internal/drift/...`
Expected: PASS.

- [ ] **Step 4: Run every test tier as a final smoke check**

Run:

```bash
make test-unit
```

Expected: PASS.

Optionally, if the environment supports it, also:

```bash
make test-functional
```

Expected: PASS. Functional tests exercise the real binary; this refactor doesn't change the SDK contract, so they should all still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/drift/check_drift_test.go
git commit -m "test(drift): regression for globally-ignored files during CheckDrift"
```

---

## Self-review checklist (executed after writing this plan)

**Spec coverage**

- Motivation / mechanism analysis → captured in commit message on Task 4.
- Goal ("`CheckDrift` never mutates the caller") → Task 3 (invariant test) + Task 4 (refactor).
- Non-goals (don't fix go-git; don't change public API; keep `checkCleanWorkingTree`) → respected in Task 4 (only mutating uses of `opts.DownstreamRepoPath` switch to `scratchPath`; cleanliness gate + state lookup stay on the caller).
- `git clone --local` design → Task 1 Step 3.
- `--no-checkout` + explicit hash checkout → Task 1 Step 3, verified by Task 1 Step 5 (detached HEAD test).
- State file edge case → Task 2.
- Component breakdown (`scratch_clone.go` + `check_drift.go` edits + deletions) → Tasks 1, 2, 4.
- Failure disposition table → covered by tests in Task 1 (bad-source path, cleanup idempotence) plus the existing SDK error propagation paths in Task 4.
- Progress narration → Task 4 Step 1 adds both INFO lines.
- Regression test (globally-ignored files) → Task 6.
- Invariant test (byte-identity) → Task 3.
- Existing-tests audit → Task 4 Step 4 + Task 5.
- Migration/rollout (no API change, no state migration, no feature flag) → nothing to do; behavior of the SDK contract is unchanged, and the spec's rollout section says so.

**Placeholder scan**

- No TBD / TODO / "fill in details" lines. Every step has either exact code or an exact command with expected output.

**Type consistency**

- `provisionScratchClone` signature is `(callerRepoPath string) (string, func(), error)` in Task 1 Step 3 and used with matching destructuring in Task 4 Step 1.
- `ensureStateFilePresent` signature is `(callerPath, scratchPath string) error` in Task 2 Step 3 and called with matching argument order in Task 4 Step 1.
- `driftCheckBranch` constant is unchanged and referenced as-is.
- All go-git types (`gogit.CheckoutOptions`, `plumbing.NewBranchReferenceName`, `wt.Checkout`) are used identically to the pre-refactor code.

Plan verified.
