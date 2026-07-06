//go:build functional || functional_docker

package functional

import (
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalForcePushGitsporkYML is a stripped-down upstream config used only by
// the force-push tests. Keeping it minimal (upstream_owned only, no
// templated / shared-ownership) means the replacement upstream tree needs
// exactly one file — the whole point of these tests is the force-push
// mechanic, not the range of ownership types.
const minimalForcePushGitsporkYML = `upstream_owned:
- upstream-owned/**
`

// integrateArgsMinimal builds integrate args for a minimal upstream. Distinct
// from integrateArgs (which uses simpleGitsporkYML and requires input-data.json).
func integrateArgsMinimal(upstreamDir, downstreamDir string) []string {
	return []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}
}

// wipeAndReinitUpstream tears down and rebuilds an upstream repo IN PLACE
// (same host directory, same URL) but with a completely different commit
// history. Simulates an upstream force-push that garbage-collects the
// original integrated commit — from the downstream's perspective, the
// commit hash it stored is no longer reachable in the upstream, and a
// fresh clone won't fetch it.
func wipeAndReinitUpstream(t *testing.T, upstreamDir string, replacementFiles map[string]string, gitsporkYML string) {
	t.Helper()
	// Remove everything under upstreamDir (including .git so the object graph
	// is truly gone), then rebuild.
	entries, err := os.ReadDir(upstreamDir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NoError(t, os.RemoveAll(filepath.Join(upstreamDir, e.Name())))
	}
	// NewUpstreamRepo can't be re-used because it calls t.TempDir(). Recreate
	// the git repo + files + commit in place using the same primitives.
	repo, err := gogit.PlainInit(upstreamDir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	files := make(map[string]string, len(replacementFiles)+1)
	for k, v := range replacementFiles {
		files[k] = v
	}
	if gitsporkYML != "" {
		files[".gitspork.yml"] = gitsporkYML
	}
	WriteFiles(t, upstreamDir, files)
	CommitAll(t, repo, upstreamDir, "force-pushed history — original commit is now gone")
}

// TestCheckDrift_upstreamForcePushedPastStoredCommit locks the contract that
// check-drift surfaces a clear error when the upstream has force-pushed past
// the commit the downstream last integrated against. Silent "no drift"
// would be wrong here: the stored commit no longer exists, so any drift
// assessment against it is meaningless.
func TestCheckDrift_upstreamForcePushedPastStoredCommit(t *testing.T) {
	if isDockerBuild {
		// DockerRunner rewrites host paths to /upstream. That works fine — but
		// wipeAndReinitUpstream re-writes the same host directory in place,
		// and while the container mount is live the docker daemon may cache
		// object files differently across the two runs. Native runner covers
		// the contract cleanly; docker path adds no new signal.
		t.Skip("wipe-and-reinit-in-place fixture is native-runner-scoped; contract is not container-specific")
	}
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"upstream-owned/file.txt": "initial content\n",
	}, minimalForcePushGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// Baseline: integrate at the current upstream HEAD; state records that
	// commit hash. Commit the downstream so the tree is clean.
	out, code := runner.Run(t, integrateArgsMinimal(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "baseline integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// Force-push the upstream to a completely different history. The commit
	// hash stored in the downstream's state no longer exists.
	wipeAndReinitUpstream(t, upstreamDir,
		map[string]string{"upstream-owned/file.txt": "totally new history — original commit is gone\n"},
		minimalForcePushGitsporkYML)

	driftOut, driftCode := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
		"--upstream", "url=file://" + upstreamDir,
	}, downstreamDir)
	require.NotEqual(t, 0, driftCode,
		"check-drift MUST exit non-zero when the stored commit is unreachable in the upstream — silent success would be a lie:\n%s", driftOut)
	// The error message should mention the unreachable commit hash so users
	// can act on it. That commit is the one in state; we don't have easy
	// access to it here, but assert on the general shape.
	assert.Contains(t, driftOut, "commit",
		"error output should reference the missing commit so users can diagnose")
}

// TestIntegrate_upstreamForcePushed_deltaSkippedSilently locks the sibling
// contract from PR #65: when re-integrating after an upstream force-push,
// the delta propagation step must silently no-op (because ErrObjectNotFound
// on the prev commit is the "prev no longer reachable" signal — not a
// corruption error to surface). Integrate itself must succeed, because the
// new HEAD is a valid commit to integrate at — the missing prev only
// affects delta computation.
func TestIntegrate_upstreamForcePushed_deltaSkippedSilently(t *testing.T) {
	if isDockerBuild {
		t.Skip("wipe-and-reinit-in-place fixture is native-runner-scoped; contract is not container-specific")
	}
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"upstream-owned/file.txt": "initial content\n",
	}, minimalForcePushGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// First integrate — records commit A in state.
	firstOut, code := runner.Run(t, integrateArgsMinimal(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", firstOut)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-first-integrate baseline")

	// Force-push the upstream to different history. Commit A no longer exists.
	wipeAndReinitUpstream(t, upstreamDir,
		map[string]string{"upstream-owned/file.txt": "post-force-push content\n"},
		minimalForcePushGitsporkYML)

	// Re-integrate. Must succeed even though the delta computation can't
	// resolve the prev hash. PR #65 classifies ErrObjectNotFound as "silent
	// skip" while surfacing other errors — a regression there would produce
	// either a false failure here (over-surfacing) or a corruption-swallowing
	// silent no-op elsewhere.
	secondOut, code := runner.Run(t, integrateArgsMinimal(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code,
		"re-integrate after upstream force-push must succeed — the missing prev commit only affects delta propagation, not the current integrate:\n%s", secondOut)

	// The downstream should reflect the new upstream content post-force-push.
	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "post-force-push content")
}
