package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDrift(t *testing.T) {
	t.Run("returns error when no previous integration in state", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test-downstream")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		_, err = CheckDrift(&CheckDriftOptions{
			Logger:             NewLogger(),
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

		state := &GitSporkDownstreamState{
			LastUpstreamRepoURL:     "https://github.com/rockholla/gitspork.git",
			LastUpstreamRepoSubpath: "docs/examples/simple/upstream",
			LastUpstreamCommitHash:  "abc123",
		}
		require.NoError(t, saveDownstreamState(dir, state))

		_, err = CheckDrift(&CheckDriftOptions{
			Logger:             NewLogger(),
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
	sig := &object.Signature{Name: gitSpork, Email: gitSpork + "@localhost", When: time.Now()}
	_, err = wt.Commit("baseline", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
	return wt
}

func TestCheckDrift_returns_report_no_drift(t *testing.T) {
	upstreamDir, _ := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)

	report, err := CheckDrift(&CheckDriftOptions{
		Logger:             NewLogger(),
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.HasDrift)
	assert.Empty(t, report.Files)
}

func TestCheckDrift_returns_report_with_drifted_file_and_attribution(t *testing.T) {
	upstreamDir, _ := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)
	testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", "drifted\n")

	report, err := CheckDrift(&CheckDriftOptions{
		Logger:             NewLogger(),
		DownstreamRepoPath: downstreamDir,
	})
	require.ErrorIs(t, err, ErrDriftDetected)
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
	_, err := Integrate(&IntegrateOptions{
		Logger:             NewLogger(),
		Upstreams:          []UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	return testCommitAll(t, repo, "post-integrate baseline")
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
	testCommitAll(t, repo, "drift edit: "+relPath)
}

func TestCheckDrift_report_files_include_unified_diff(t *testing.T) {
	upstreamDir, _ := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)
	testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", "drifted\n")

	report, err := CheckDrift(&CheckDriftOptions{
		Logger:             NewLogger(),
		DownstreamRepoPath: downstreamDir,
	})
	require.ErrorIs(t, err, ErrDriftDetected)
	require.Len(t, report.Files, 1)
	diff := report.Files[0].Diff
	assert.Contains(t, diff, "upstream-owned/file.txt",
		"expected the unified diff to reference the path, got:\n%s", diff)
	assert.Contains(t, diff, "-upstream content", "expected removed-line marker for old content")
	assert.Contains(t, diff, "+drifted", "expected added-line marker for new content")
}
