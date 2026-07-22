//go:build functional || functional_docker

package functional

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrate_cache_populatesAndHitsWithinTTL locks the primary cache
// contract: first integrate populates the cache; a second integrate within
// TTL performs NO network operation.
func TestIntegrate_cache_populatesAndHitsWithinTTL(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR; not plumbed through DockerRunner")
	}

	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// First integrate — populates.
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "first integrate exited non-zero:\n%s", out)
	assert.Contains(t, out, "populating upstream cache",
		"first integrate must populate the cache")

	// Commit downstream so the tree is clean for the second integrate.
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-first-integrate")
	prepDownstreamWithInputData(t, downstreamDir)

	// Second integrate within default TTL — cache hit.
	out, code = runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "second integrate exited non-zero:\n%s", out)
	assert.Contains(t, out, "upstream cache hit",
		"second integrate within TTL must hit the cache")
	assert.NotContains(t, out, "populating upstream cache",
		"second integrate must NOT re-populate")
	assert.NotContains(t, out, "refreshing upstream cache",
		"second integrate must NOT refresh (cache is fresh)")
}

func TestIntegrate_cache_staleTTL_refreshes(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR")
	}
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// First integrate populates.
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-first-integrate")
	prepDownstreamWithInputData(t, downstreamDir)

	// Second integrate with --cache-ttl 1ns: cache is instantly stale, refresh.
	args := append(integrateArgs(upstreamDir, downstreamDir), "--cache-ttl", "1ns")
	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "second integrate with tiny TTL failed:\n%s", out)
	assert.Contains(t, out, "refreshing upstream cache",
		"tiny TTL must trigger a refresh")
}

func TestIntegrate_cache_noCache_bypassesEntirely(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR")
	}
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	args := append(integrateArgs(upstreamDir, downstreamDir), "--no-cache")
	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "integrate --no-cache failed:\n%s", out)

	// None of the three cache log lines appear.
	assert.NotContains(t, out, "populating upstream cache")
	assert.NotContains(t, out, "refreshing upstream cache")
	assert.NotContains(t, out, "upstream cache hit")

	// And the cache dir is empty.
	entries, err := os.ReadDir(cacheDir)
	if err == nil {
		assert.Empty(t, entries, "no-cache run must leave the cache dir empty")
	}
}

// Keep the strings import active once the cross-process test is added below.
var _ = strings.Contains
