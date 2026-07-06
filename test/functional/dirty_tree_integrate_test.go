//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrate_dirtyTree_proceeds locks the divergence from check-drift:
// gitspork integrate does NOT gate on a clean working tree (unlike
// check-drift, which calls checkCleanWorkingTree at the top). integrate
// treats the downstream as the writable target and lays down what the
// config says to lay down, regardless of what uncommitted content was
// already there.
//
// The subtests below spell out the observable per-category behaviour so
// future readers can rely on this document — and so a regression that
// starts gating integrate on a dirty tree (or that changes the per-category
// overwrite behaviour) surfaces immediately.
func TestIntegrate_dirtyTree_proceeds(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// Baseline: integrate + commit. Post-commit the tree is clean; we'll
	// dirty it deliberately for the assertions below.
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "baseline integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// Dirty the downstream in three ways simultaneously:
	//   1. Edit an upstream_owned file. This edit will be overwritten by
	//      the next integrate — upstream_owned means upstream is the source
	//      of truth on every integrate.
	//   2. Edit a downstream_owned file (already seeded). This edit must
	//      SURVIVE the next integrate — the "downstream owns it thereafter"
	//      invariant applies to a dirty edit just as it does to a committed
	//      one.
	//   3. Add an unrelated new file. gitspork doesn't touch paths it
	//      doesn't manage, so this must survive.
	WriteFiles(t, downstreamDir, map[string]string{
		"upstream-owned/file.txt": "uncommitted downstream edit — WILL BE OVERWRITTEN\n",
		"downstream-owned.md":     "uncommitted downstream edit — must survive\n",
		"my-unrelated-notes.md":   "downstream engineer's notes\n",
	})
	// Re-prep input data — integrate needs it for the templated step.
	prepDownstreamWithInputData(t, downstreamDir)

	// integrate proceeds against the dirty tree with exit 0. This is the
	// key divergence from check-drift (which refuses a dirty tree).
	out, code = runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code,
		"integrate MUST proceed against a dirty downstream tree (unlike check-drift, which gates on clean). exit=%d output:\n%s", code, out)

	t.Run("uncommitted upstream_owned edit is overwritten", func(t *testing.T) {
		got := ReadFile(t, downstreamDir, "upstream-owned/file.txt")
		assert.Equal(t, "upstream content\n", got,
			"upstream_owned is source-of-truth per integrate — uncommitted downstream edits are silently overwritten (documented behaviour; run check-drift first if you want to detect and preserve pending changes)")
	})

	t.Run("uncommitted downstream_owned edit survives", func(t *testing.T) {
		got := ReadFile(t, downstreamDir, "downstream-owned.md")
		assert.Equal(t, "uncommitted downstream edit — must survive\n", got,
			"the downstream_owned 'seed once, downstream owns thereafter' invariant applies to uncommitted edits too — the file exists, so integrate skips it")
	})

	t.Run("uncommitted unrelated file survives", func(t *testing.T) {
		// gitspork must not touch paths outside its configured ownership.
		AssertFileContains(t, downstreamDir, "my-unrelated-notes.md", "downstream engineer's notes")
	})
}
