//go:build functional || functional_docker

package functional

import (
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/stretchr/testify/require"
)

// integrateForDrift runs integrate and commits the downstream, leaving it in a
// clean state ready for check-drift.
func integrateForDrift(t *testing.T, runner Runner, upstreamDir, downstreamDir string) {
	t.Helper()
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "integrate for drift setup failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")
}

func TestCheckDrift_no_drift(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	integrateForDrift(t, runner, upstreamDir, downstreamDir)
	// check-drift re-runs integrate internally and needs input-data.json
	prepDownstreamWithInputData(t, downstreamDir)

	out, code := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
		"--upstream-repo-url", "file://" + upstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "expected no drift (exit 0):\n%s", out)
}

func TestCheckDrift_no_drift_state_url(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// integrate with explicit URL — stores URL in downstream state
	integrateForDrift(t, runner, upstreamDir, downstreamDir)
	// check-drift re-runs integrate internally and needs input-data.json
	prepDownstreamWithInputData(t, downstreamDir)

	// check-drift without --upstream-repo-url; should fall back to state
	out, code := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "expected no drift using state URL (exit 0):\n%s", out)
}

// TestCheckDrift_detached_head verifies check-drift works when the downstream is
// in a detached HEAD state, as CI runners (e.g. Buildkite) leave it after
// checking out a specific commit rather than a branch.
func TestCheckDrift_detached_head(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	integrateForDrift(t, runner, upstreamDir, downstreamDir)
	// check-drift re-runs integrate internally and needs input-data.json
	prepDownstreamWithInputData(t, downstreamDir)

	// Simulate the CI checkout: detach HEAD at the current commit (no branch).
	repo := OpenRepo(t, downstreamDir)
	head, err := repo.Head()
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.Checkout(&gogit.CheckoutOptions{Hash: head.Hash()}))
	detached, err := repo.Head()
	require.NoError(t, err)
	require.False(t, detached.Name().IsBranch(), "test setup should leave a detached HEAD")

	out, code := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
		"--upstream-repo-url", "file://" + upstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "expected detached HEAD handled with no drift (exit 0):\n%s", out)
}

func TestCheckDrift_drift_detected(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	integrateForDrift(t, runner, upstreamDir, downstreamDir)

	// Modify an upstream-owned file to introduce drift, then commit
	WriteFiles(t, downstreamDir, map[string]string{
		"upstream-owned/file.txt": "drifted content\n",
	})
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "introduce drift")

	// check-drift re-runs integrate internally and needs input-data.json
	prepDownstreamWithInputData(t, downstreamDir)

	out, code := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
		"--upstream-repo-url", "file://" + upstreamDir,
		"--verbose",
	}, downstreamDir)
	require.Equal(t, 2, code, "expected drift detected (exit 2):\n%s", out)
	require.Contains(t, out, "upstream-owned/file.txt",
		"expected verbose output to name the drifted file:\n%s", out)
}
