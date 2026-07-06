//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckDrift_gitignoredDownstreamFile_doesNotDisrupt exercises the
// everyday case: a downstream repo has a .gitignore covering private local
// files (.env, build artefacts, editor tmp files). Those files must NOT
// disrupt check-drift — the clean-working-tree gate should pass because
// git treats them as ignored (not "untracked" for status --porcelain), and
// no drift should be reported because the ignored files aren't part of what
// gitspork manages.
func TestCheckDrift_gitignoredDownstreamFile_doesNotDisrupt(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// Baseline integrate — establishes state.
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "baseline integrate failed:\n%s", out)

	// Commit downstream tree including a .gitignore that covers a private
	// .env file, plus the .env file itself. Post-commit, the .env file is
	// tracked by git as ignored (matches the .gitignore rule) and status
	// --porcelain treats it as absent.
	WriteFiles(t, downstreamDir, map[string]string{
		".gitignore": ".env\nbuild/\n*.tmp\n",
		".env":       "SECRET_KEY=downstream-only-private-value\n",
		"build/artifact.bin": "downstream build output\n",
		"editor.tmp": "swap file\n",
	})
	// input-data.json is required for the templated step during the drift-check's
	// internal re-integrate.
	prepDownstreamWithInputData(t, downstreamDir)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline with .gitignore + private files")

	// check-drift must succeed cleanly. The .env / build/ / *.tmp files are
	// ignored by git and must not:
	//   - trip the "working tree not clean" gate (checkCleanWorkingTree uses
	//     git status --porcelain which respects .gitignore),
	//   - or produce spurious drift entries.
	out, code = runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	assert.Equal(t, 0, code, "check-drift should exit 0 despite gitignored downstream files:\n%s", out)
}

// TestCheckDrift_gitignoredThenIntegrated_stillReportsDrift: user's
// .gitignore matches a path that they subsequently integrate against
// (upstream_owned). The file lands via os.WriteFile (not via git-add), so
// on-disk it exists. But git ignores it — it won't be tracked when the
// downstream commits.
//
// The tests below pin the OBSERVABLE behavior so a future user reading the
// suite understands what happens today: an upstream-owned file that
// happens to match the downstream's .gitignore silently doesn't get
// tracked, so drift on it goes unreported. This is a known interaction
// worth documenting via tests; users who want to track upstream-owned
// files must not gitignore them.
func TestCheckDrift_gitignoredMatchingUpstreamOwned_documentedBehavior(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)

	// Downstream gitignores the exact path an upstream_owned file will
	// land at.
	WriteFiles(t, downstreamDir, map[string]string{
		".gitignore": "upstream-owned/file.txt\n",
	})
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "add .gitignore covering upstream-owned target")
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "integrate should succeed even when upstream files land in gitignored paths:\n%s", out)

	// The file exists on disk...
	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")

	// Commit the post-integrate baseline so the working tree is clean.
	// The gitignored upstream-owned/file.txt does NOT get committed (git
	// respects the .gitignore), but every other integrated file does.
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// check-drift proceeds cleanly. The gitignored file's content is
	// invisible to git-diff, so drift on it goes unreported by design.
	prepDownstreamWithInputData(t, downstreamDir)
	out, code = runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	assert.Equal(t, 0, code,
		"check-drift with gitignored upstream target should exit 0 — file changes to gitignored paths are invisible to git, so drift-check does not report on them. This is the documented interaction; downstreams tracking upstream_owned files must not gitignore them:\n%s", out)
}
