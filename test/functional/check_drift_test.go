//go:build functional || functional_docker

package functional

import (
	"os"
	"testing"

	gogit "github.com/go-git/go-git/v6"
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
		"--upstream", "url=file://" + upstreamDir,
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
	// upstream-owned/file.txt is written by both upstreams; the last writer
	// (upstream2) is the correct attribution.
	assert.Contains(t, out, "upstream: file://"+upstreamDir2,
		"expected drift attributed to upstream2 (%s) as the last writer:\n%s", upstreamDir2, out)
	assert.NotContains(t, out, "upstream: file://"+upstreamDir1,
		"upstream1 (%s) should not be credited for a file upstream2 wrote last:\n%s", upstreamDir1, out)
}

// TestCheckDrift_multi_upstream_drift_in_first_only exercises attribution when
// the drifted file is owned exclusively by the first upstream. upstream-owned.mk
// is written only by buildSimpleUpstream (upstream1); buildSecondUpstream's
// glob "upstream-owned/**" does not match it.
func TestCheckDrift_multi_upstream_drift_in_first_only(t *testing.T) {
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

	// Modify a file owned only by upstream1.
	WriteFiles(t, downstreamDir, map[string]string{
		"upstream-owned.mk": "drifted mk\n",
	})
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "introduce first-upstream drift")
	prepDownstreamWithInputData(t, downstreamDir)

	out, code = runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 2, code, "expected drift exit 2:\n%s", out)
	assert.Contains(t, out, "upstream-owned.mk", "expected the drifted filename in output:\n%s", out)
	assert.Contains(t, out, "upstream: file://"+upstreamDir1,
		"expected drift attributed to upstream1 (%s):\n%s", upstreamDir1, out)
	assert.NotContains(t, out, "upstream: file://"+upstreamDir2,
		"upstream2 (%s) should not appear in attribution for a file it did not touch:\n%s", upstreamDir2, out)
}

// TestCheckDrift_multi_upstream_drift_in_both exercises per-file attribution
// when drift exists in files owned by different upstreams: upstream-owned.mk
// is owned only by upstream1, upstream-owned/file.txt is written last by
// upstream2. Both files must appear in the report attributed to their
// respective owners.
func TestCheckDrift_multi_upstream_drift_in_both(t *testing.T) {
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

	WriteFiles(t, downstreamDir, map[string]string{
		"upstream-owned.mk":       "drifted mk\n",
		"upstream-owned/file.txt": "drifted file\n",
	})
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "introduce drift in both upstreams' files")
	prepDownstreamWithInputData(t, downstreamDir)

	out, code = runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 2, code, "expected drift exit 2:\n%s", out)
	// Both files reported.
	assert.Contains(t, out, "upstream-owned.mk", "expected upstream1's drifted file:\n%s", out)
	assert.Contains(t, out, "upstream-owned/file.txt", "expected upstream2's drifted file:\n%s", out)
	// Both upstreams named in attribution.
	assert.Contains(t, out, "upstream: file://"+upstreamDir1,
		"expected upstream1 (%s) attribution:\n%s", upstreamDir1, out)
	assert.Contains(t, out, "upstream: file://"+upstreamDir2,
		"expected upstream2 (%s) attribution:\n%s", upstreamDir2, out)
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

// TestCheckDrift_upstream_override_url_normalized verifies end-to-end that
// URL normalization is applied when matching --upstream overrides against
// state entries. The state records the upstream URL without a trailing
// ".git"; the override is passed with ".git" appended. `normalizeUpstreamURL`
// strips the suffix so the two must match. A same-directory symlink lets
// the override URL clone-resolve to the same repo.
func TestCheckDrift_upstream_override_url_normalized(t *testing.T) {
	if isDockerBuild {
		t.Skip("symlink-based normalization test relies on host-side paths not mapped into the container")
	}
	upstreamDir := buildSimpleUpstream(t)
	upstreamAlias := upstreamDir + ".git"
	require.NoError(t, os.Symlink(upstreamDir, upstreamAlias))

	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// Integrate using the un-suffixed URL; state will record this form.
	integrateForDrift(t, runner, upstreamDir, downstreamDir)
	prepDownstreamWithInputData(t, downstreamDir)

	// Override with the ".git"-suffixed alias — normalization must match to
	// the state entry recorded above.
	out, code := runner.Run(t, []string{
		"check-drift",
		"--upstream", "url=file://" + upstreamAlias,
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "expected no drift when override URL matches by normalization:\n%s", out)
}

// TestCheckDrift_no_state_no_override_errors verifies check-drift on a
// downstream that was never integrated (no state file) and given no
// --upstream override exits non-zero with a message pointing at integrate.
func TestCheckDrift_no_state_no_override_errors(t *testing.T) {
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, "", downstreamDir)

	out, code := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.NotEqual(t, 0, code, "expected non-zero exit when no state and no override:\n%s", out)
	assert.Contains(t, out, "gitspork integrate",
		"expected message to direct user to run gitspork integrate first:\n%s", out)
}

// TestCheckDrift_override_no_matching_state_errors verifies check-drift with
// an --upstream override whose URL matches nothing in state exits non-zero
// with a message naming the unmatched URL.
func TestCheckDrift_override_no_matching_state_errors(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// Integrate so state exists with upstreamDir's URL.
	integrateForDrift(t, runner, upstreamDir, downstreamDir)
	prepDownstreamWithInputData(t, downstreamDir)

	// Override with a URL that was never integrated.
	bogusURL := "file:///tmp/gitspork-never-integrated-" + t.Name()
	out, code := runner.Run(t, []string{
		"check-drift",
		"--upstream", "url=" + bogusURL,
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.NotEqual(t, 0, code, "expected non-zero exit for unmatched override:\n%s", out)
	assert.Contains(t, out, bogusURL,
		"expected error to name the unmatched upstream URL %q:\n%s", bogusURL, out)
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
