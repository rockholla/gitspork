package drift

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/integrate"
	"github.com/rockholla/gitspork/v2/internal/logutil"
	"github.com/rockholla/gitspork/v2/internal/testharness"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
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

func TestCheckDrift_cleansUpDriftCheckBranch(t *testing.T) {
	// Whatever the outcome of CheckDrift, the transient _gitspork-check-drift
	// branch must not linger in the downstream repo — otherwise subsequent
	// invocations start from an unclean state and users see stray refs.
	upstreamDir, _ := testharness.MinimalUpstream(t)
	downstreamDir := testharness.EmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)

	t.Run("no drift path", func(t *testing.T) {
		_, err := CheckDrift(&sdktypes.CheckDriftOptions{
			Logger:             logutil.New(),
			DownstreamRepoPath: downstreamDir,
		})
		require.NoError(t, err)
		assertDriftCheckBranchAbsent(t, downstreamDir)
	})

	t.Run("drift detected path", func(t *testing.T) {
		testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", "drifted\n")
		_, err := CheckDrift(&sdktypes.CheckDriftOptions{
			Logger:             logutil.New(),
			DownstreamRepoPath: downstreamDir,
		})
		require.ErrorIs(t, err, sdktypes.ErrDriftDetected)
		assertDriftCheckBranchAbsent(t, downstreamDir)
	})
}

func assertDriftCheckBranchAbsent(t *testing.T, downstreamDir string) {
	t.Helper()
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	_, err = repo.Reference(plumbing.NewBranchReferenceName(driftCheckBranch), false)
	assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound,
		"transient drift-check branch %q must be cleaned up when CheckDrift returns", driftCheckBranch)
}

func TestCheckDrift_restoresWorktreeOnMidLoopFailure(t *testing.T) {
	// When re-integration of one upstream mutates worktree files and a *later*
	// upstream fails to integrate, CheckDrift returns an error mid-loop and the
	// deferred restore fires while the worktree still has uncommitted mutations.
	// The restore must succeed and put the worktree back to the caller's original
	// committed content — not leave the upstream-canonical mutations in place.
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
		"restore must overwrite mid-loop worktree mutations left by the earlier upstream, even though those changes are unstaged")
}

func TestCheckDrift_restoresWorktreeContentAfterDrift(t *testing.T) {
	// After CheckDrift returns, the downstream worktree files must match the
	// caller's original HEAD content — CheckDrift must not leave the drifted
	// upstream-canonical content in place of the user's committed content.
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
		"worktree should be restored to the caller's original committed content, not left with the upstream-canonical content used during drift detection")
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
