package drift

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/integrate"
	"github.com/rockholla/gitspork/v2/internal/logutil"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/rockholla/gitspork/v2/test/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDrift(t *testing.T) {
	t.Run("returns error when no previous integration in state", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test-downstream")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		_, err = CheckDrift(&sdktypes.CheckDriftOptions{
			Logger:             logutil.New(),
			DownstreamRepoPath: dir,
		})
		assert.ErrorContains(t, err, "no previous integration found")
	})

	t.Run("returns error when working tree is dirty", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test-downstream")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		makeBaselineRepo(t, dir)
		err = os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirty"), 0644)
		require.NoError(t, err)

		state := &sdktypes.DownstreamState{
			LastUpstreamRepoURL:     "https://github.com/rockholla/gitspork.git",
			LastUpstreamRepoSubpath: "docs/examples/simple/upstream",
			LastUpstreamCommitHash:  "abc123",
		}
		require.NoError(t, integrate.SaveDownstreamState(dir, state))

		_, err = CheckDrift(&sdktypes.CheckDriftOptions{
			Logger:             logutil.New(),
			DownstreamRepoPath: dir,
		})
		assert.ErrorContains(t, err, "working tree is not clean")
	})

	// Note: the "no upstream URL" test case was removed as part of multi-upstream
	// refactoring (Task 1). URL validation will be added back in Task 6.
}

func Test_checkCleanWorkingTree(t *testing.T) {
	t.Run("clean repo passes", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		makeBaselineRepo(t, dir)
		assert.NoError(t, checkCleanWorkingTree(dir))
	})

	t.Run("untracked file fails", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		makeBaselineRepo(t, dir)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0644))
		err = checkCleanWorkingTree(dir)
		assert.ErrorContains(t, err, "working tree is not clean")
		assert.ErrorContains(t, err, "untracked.txt")
	})

	t.Run("modified tracked file fails", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		makeBaselineRepo(t, dir)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("modified"), 0644))
		err = checkCleanWorkingTree(dir)
		assert.ErrorContains(t, err, "working tree is not clean")
		assert.ErrorContains(t, err, "file.txt")
	})
}

func Test_diffWorktreeAgainstHEAD(t *testing.T) {
	t.Run("returns nil patch when nothing changed", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		makeBaselineRepo(t, dir)
		repo, err := gogit.PlainOpen(dir)
		require.NoError(t, err)
		wt, err := repo.Worktree()
		require.NoError(t, err)

		patch, err := diffWorktreeAgainstHEAD(repo, wt)
		assert.NoError(t, err)
		assert.Nil(t, patch)
	})

	t.Run("returns patch when file is modified", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		makeBaselineRepo(t, dir)
		repo, err := gogit.PlainOpen(dir)
		require.NoError(t, err)
		wt, err := repo.Worktree()
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("modified content"), 0644))

		patch, err := diffWorktreeAgainstHEAD(repo, wt)
		assert.NoError(t, err)
		require.NotNil(t, patch)
		assert.Equal(t, 1, len(patch.Stats()))
		assert.Equal(t, "file.txt", patch.Stats()[0].Name)
	})

	t.Run("returns patch when new file is added", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		makeBaselineRepo(t, dir)
		repo, err := gogit.PlainOpen(dir)
		require.NoError(t, err)
		wt, err := repo.Worktree()
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new file"), 0644))

		patch, err := diffWorktreeAgainstHEAD(repo, wt)
		assert.NoError(t, err)
		require.NotNil(t, patch)
		assert.Equal(t, 1, len(patch.Stats()))
		assert.Equal(t, "new.txt", patch.Stats()[0].Name)
	})
}

// makeBaselineRepo initialises a git repo with one committed file and returns the Worktree.
func makeBaselineRepo(t *testing.T, dir string) *gogit.Worktree {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("master")),
	)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("baseline content"), 0644))
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: config.GitSpork, Email: config.GitSpork + "@localhost", When: time.Now()}
	_, err = wt.Commit("baseline", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
	return wt
}

func TestCheckDrift_returns_report_no_drift(t *testing.T) {
	upstreamDir, _ := testharness.MinimalUpstream(t)
	downstreamDir := testharness.EmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)

	report, err := CheckDrift(&sdktypes.CheckDriftOptions{
		Logger:             logutil.New(),
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.HasDrift)
	assert.Empty(t, report.Files)
}

func TestCheckDrift_returns_report_with_drifted_file_and_attribution(t *testing.T) {
	upstreamDir, _ := testharness.MinimalUpstream(t)
	downstreamDir := testharness.EmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)
	testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", "drifted\n")

	report, err := CheckDrift(&sdktypes.CheckDriftOptions{
		Logger:             logutil.New(),
		DownstreamRepoPath: downstreamDir,
	})
	require.ErrorIs(t, err, sdktypes.ErrDriftDetected)
	require.NotNil(t, report)
	assert.True(t, report.HasDrift)
	require.Len(t, report.Files, 1)
	assert.Equal(t, "upstream-owned/file.txt", report.Files[0].Path)
	assert.Equal(t, "file://"+upstreamDir, report.Files[0].AttributedURL)
}

// testIntegrateAndCommitBaseline integrates upstreamDir into downstreamDir and
// commits the resulting downstream state so the working tree is clean and
// CheckDrift can operate. Returns the post-integrate commit hash.
func testIntegrateAndCommitBaseline(t *testing.T, upstreamDir, downstreamDir string) plumbing.Hash {
	t.Helper()
	_, err := integrate.Integrate(&sdktypes.IntegrateOptions{
		Logger:             logutil.New(),
		Upstreams:          []sdktypes.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	return testharness.CommitAllWithMessage(t, repo, "post-integrate baseline")
}

// testWriteAndCommitInDownstream writes content to a file inside downstreamDir
// and commits, simulating a downstream-side edit that check-drift should detect.
func testWriteAndCommitInDownstream(t *testing.T, downstreamDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(downstreamDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	testharness.CommitAllWithMessage(t, repo, "drift edit: "+relPath)
}

func TestCheckDrift_leavesCallerUntouchedOnMidLoopFailure(t *testing.T) {
	// When re-integration of one upstream succeeds and a *later* upstream fails
	// to integrate, CheckDrift returns an error mid-loop. The caller's worktree
	// must contain the caller's original committed content — not the
	// upstream-canonical content that the earlier upstream wrote into the
	// scratch clone. Since CheckDrift runs against a scratch clone, this holds
	// by construction: the caller's directory is never written to at all.
	upstreamA, _ := testharness.MinimalUpstream(t)
	downstreamDir := testharness.EmptyDownstream(t)

	// Baseline integrate of A, then commit a drifted value so the file is not
	// what upstream A would produce, so IntegrateForDriftCheck will mutate it.
	testIntegrateAndCommitBaseline(t, upstreamA, downstreamDir)
	const driftedContent = "drifted-committed\n"
	testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", driftedContent)

	// Swap in a second state entry that points at a bogus URL so
	// IntegrateForDriftCheck for that entry fails (well after upstream A has
	// mutated the worktree).
	state, err := integrate.LoadDownstreamState(downstreamDir)
	require.NoError(t, err)
	require.Len(t, state.Upstreams, 1, "baseline integrate should record exactly one upstream")
	state.Upstreams = append(state.Upstreams, sdktypes.UpstreamState{
		URL:        "file:///nonexistent-gitspork-drift-restore-test-" + t.Name(),
		CommitHash: state.Upstreams[0].CommitHash,
	})
	require.NoError(t, integrate.SaveDownstreamState(downstreamDir, state))
	// SaveDownstreamState edits .gitspork/downstream-state.json — commit it so
	// the working tree is clean when CheckDrift runs.
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	testharness.CommitAllWithMessage(t, repo, "add bogus state entry")

	_, err = CheckDrift(&sdktypes.CheckDriftOptions{
		Logger:             logutil.New(),
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err, "CheckDrift must fail when a later upstream cannot be integrated")

	got, readErr := os.ReadFile(filepath.Join(downstreamDir, "upstream-owned/file.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, driftedContent, string(got),
		"caller worktree must retain committed content across a mid-loop CheckDrift failure (scratch clone isolates all mutations)")
}

func TestCheckDrift_leavesCallerContentUntouchedAfterDrift(t *testing.T) {
	// After CheckDrift returns, the downstream worktree files match the
	// caller's original committed content because CheckDrift runs against a
	// scratch clone — the caller's directory is never written to.
	upstreamDir, _ := testharness.MinimalUpstream(t)
	downstreamDir := testharness.EmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)

	driftPath := filepath.Join(downstreamDir, "upstream-owned/file.txt")
	driftedContent := "drifted-committed\n"
	testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", driftedContent)

	_, err := CheckDrift(&sdktypes.CheckDriftOptions{
		Logger:             logutil.New(),
		DownstreamRepoPath: downstreamDir,
	})
	require.ErrorIs(t, err, sdktypes.ErrDriftDetected)

	got, readErr := os.ReadFile(driftPath)
	require.NoError(t, readErr)
	assert.Equal(t, driftedContent, string(got),
		"caller worktree must match caller's committed content after CheckDrift (scratch clone isolates the upstream-canonical content used during drift detection)")
}

func TestCheckDrift_report_files_include_unified_diff(t *testing.T) {
	upstreamDir, _ := testharness.MinimalUpstream(t)
	downstreamDir := testharness.EmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)
	testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", "drifted\n")

	report, err := CheckDrift(&sdktypes.CheckDriftOptions{
		Logger:             logutil.New(),
		DownstreamRepoPath: downstreamDir,
	})
	require.ErrorIs(t, err, sdktypes.ErrDriftDetected)
	require.Len(t, report.Files, 1)
	diff := report.Files[0].Diff
	assert.Contains(t, diff, "upstream-owned/file.txt",
		"expected the unified diff to reference the path, got:\n%s", diff)
	assert.Contains(t, diff, "-upstream content", "expected removed-line marker for old content")
	assert.Contains(t, diff, "+drifted", "expected added-line marker for new content")
}

func TestCheckDrift_leavesCallerWorkingTreeByteIdentical(t *testing.T) {
	// Invariant: CheckDrift must not delete or alter any file that existed in
	// the caller's working tree before the call, regardless of whether drift is
	// detected. This test snapshots every non-.git file before and after
	// CheckDrift and asserts byte-equality. (It does not catch transient
	// creates — files that CheckDrift creates and deletes within the call —
	// which is out of scope for the reported bug class of silent deletion.)

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

func TestCheckDrift_selfIntegrationBlockedByOriginURL(t *testing.T) {
	// Unit-level regression for the bug where the self-integration URL guard
	// was bypassed because the guard reads the target repo's origin remote —
	// after the scratch-clone refactor, the scratch's origin points at the
	// caller's local path (not the caller's actual origin URL), so the guard
	// never matched. Fix: CheckDrift runs EnsureNotSelfIntegration against
	// the caller before provisioning the scratch.
	upstreamDir, _ := testharness.MinimalUpstream(t)
	downstreamDir := testharness.EmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)

	// After baseline integrate: attach origin pointing at the upstream. Now
	// the caller "identifies as the same repo" per the URL guard.
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"file://" + upstreamDir},
	})
	require.NoError(t, err)
	// The CreateRemote edit doesn't touch the worktree, so no re-commit needed.

	_, err = CheckDrift(&sdktypes.CheckDriftOptions{
		Logger:             logutil.New(),
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, sdktypes.ErrSelfIntegration,
		"CheckDrift must catch by-origin self-integration against the caller repo, before provisioning scratch clone")
}

// snapshotWorktree wraps listWorktreeFiles (the production walker used by
// CheckDrift for file attribution) so the invariant test measures the caller's
// worktree the same way production code does. If listWorktreeFiles ever
// changes (symlink handling, hash algorithm, path normalization), the
// invariant test stays consistent by construction rather than drifting
// silently.
func snapshotWorktree(t *testing.T, dir string) map[string]string {
	t.Helper()
	m, err := listWorktreeFiles(dir)
	require.NoError(t, err)
	return m
}
