//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrate_upstreamOnly_excludesMatchingFiles(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".cloud-native-template/shared.txt":    "shared template content\n",
		"cli/.cloud-native-template/local.txt": "cli-local template content\n",
		"upstream-owned/other.txt":             "other upstream content\n",
	}, `upstream_owned:
- '{,**/}.cloud-native-template/**'
- upstream-owned/**
upstream_only:
- cli/**
`)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate exited non-zero:\n%s", out)

	// top-level .cloud-native-template syncs — not matched by upstream_only
	AssertFileContains(t, downstreamDir, ".cloud-native-template/shared.txt", "shared template content")
	// cli/.cloud-native-template excluded by upstream_only: cli/**
	AssertFileAbsent(t, downstreamDir, "cli/.cloud-native-template/local.txt")
	// upstream-owned file outside cli/ syncs normally
	AssertFileContains(t, downstreamDir, "upstream-owned/other.txt", "other upstream content")
	// warning logged for the skipped file
	assert.Contains(t, out, "cli/.cloud-native-template/local.txt")
}
