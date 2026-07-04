//go:build functional || functional_docker

package functional

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrateLocal_multi_upstream_precedence verifies that when multiple
// --upstream-path values are given, files written by later upstreams overwrite
// files written by earlier ones (left-to-right precedence per spec §2, §4).
func TestIntegrateLocal_multi_upstream_precedence(t *testing.T) {
	if isDockerBuild {
		t.Skip("multi-upstream path rewriting not supported in DockerRunner")
	}
	upstreamDir1 := buildSimpleUpstream(t)
	upstreamDir2 := buildSecondUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir1, downstreamDir)

	out, code := runner.Run(t, []string{
		"integrate-local",
		"--upstream-path", upstreamDir1,
		"--upstream-path", upstreamDir2,
		"--downstream-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate-local multi failed:\n%s", out)

	// upstreamDir2 writes upstream-owned/file.txt last, so its content wins.
	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "second upstream content")
	// upstreamDir1 owns upstream-owned.mk exclusively; it should still be present.
	AssertFileContains(t, downstreamDir, "upstream-owned.mk", "upstream mk content")
}

// TestIntegrateLocal_does_not_write_state verifies the spec §4 invariant that
// IntegrateLocal does not record upstreams in the downstream state file. When
// the integrated config has no migrations, no state file is created at all.
func TestIntegrateLocal_does_not_write_state(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, []string{
		"integrate-local",
		"--upstream-path", upstreamDir,
		"--downstream-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate-local failed:\n%s", out)

	statePath := filepath.Join(downstreamDir, ".gitspork", "downstream-state.json")
	_, err := os.Stat(statePath)
	assert.True(t, os.IsNotExist(err),
		"expected no downstream-state.json after integrate-local (spec §4 invariant), got err: %v", err)
}
