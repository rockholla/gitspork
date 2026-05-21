//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		"--upstream", "url=file://" + upstreamDir,
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
		"--upstream", "url=file://" + upstreamDir,
		"--verbose",
	}, downstreamDir)
	require.Equal(t, 2, code, "expected drift detected (exit 2):\n%s", out)
	require.Contains(t, out, "upstream-owned/file.txt",
		"expected verbose output to name the drifted file:\n%s", out)
}

func TestCheckDrift_multi_upstream_no_drift(t *testing.T) {
	if isDockerBuild {
		t.Skip("multi-upstream path rewriting not supported in DockerRunner")
	}
	upstreamDir1 := buildSimpleUpstream(t)
	upstreamDir2 := buildSecondUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir1, downstreamDir)

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
	if isDockerBuild {
		t.Skip("multi-upstream path rewriting not supported in DockerRunner")
	}
	upstreamDir1 := buildSimpleUpstream(t)
	upstreamDir2 := buildSecondUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir1, downstreamDir)

	out, code := runner.Run(t, integrateArgsMulti(upstreamDir1, upstreamDir2, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "multi integrate failed:\n%s", out)

	// Modify the upstream-owned file (last written by upstreamDir2) to introduce drift.
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

func TestCheckDrift_upstream_override_explicit_url(t *testing.T) {
	// --upstream override with explicit url= matches the state entry and finds no drift.
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	integrateForDrift(t, runner, upstreamDir, downstreamDir)
	prepDownstreamWithInputData(t, downstreamDir)

	out, code := runner.Run(t, []string{
		"check-drift",
		"--upstream", "url=file://" + upstreamDir,
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "expected no drift with explicit --upstream:\n%s", out)
}

func TestCheckDrift_uses_stored_commit_not_head(t *testing.T) {
	// Regression test: check-drift must re-integrate at the stored commit hash,
	// not at HEAD. After integration, a new upstream commit changes an
	// upstream-owned file. check-drift should still report no drift because
	// it uses the commit that was actually integrated, not the new HEAD.
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// Integrate and commit the downstream at the current upstream HEAD.
	integrateForDrift(t, runner, upstreamDir, downstreamDir)
	// check-drift re-runs integrate internally and needs input-data.json.
	prepDownstreamWithInputData(t, downstreamDir)

	// Add a new commit to the upstream that changes an upstream-owned file.
	// check-drift must NOT use this new commit — it must use the stored one.
	WriteFiles(t, upstreamDir, map[string]string{
		"upstream-owned/file.txt": "new upstream content after integration\n",
	})
	CommitAll(t, OpenRepo(t, upstreamDir), upstreamDir, "upstream advances past integrated commit")

	out, code := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
		"--upstream", "url=file://" + upstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code,
		"check-drift should report no drift because it must use the stored commit, not the new upstream HEAD:\n%s", out)
}
