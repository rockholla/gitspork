//go:build examples

package examples

import (
	"encoding/json"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/rockholla/gitspork/v2/test/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultiUpstreamExample covers the multi-upstream integration pattern at
// the examples tier. All four other worked examples use a single upstream;
// this scenario locks in the "platform base + language overlay" layering
// pattern real teams use to layer standards.
//
// The example lives at docs/examples/multi-upstream/ with two sibling
// upstream directories: platform-base/ (shared platform defaults) and
// language-overlay/ (language-specific additions that override selected
// base values).
func TestMultiUpstreamExample(t *testing.T) {
	baseUpstream := initExampleRepoAt(t, "multi-upstream", "platform-base")
	overlayUpstream := initExampleRepoAt(t, "multi-upstream", "language-overlay")
	downstreamDir := testharness.NewDownstreamRepo(t)

	// Integrate BOTH upstreams in one gitspork invocation, base first then
	// overlay. That order is what gives us last-writer-wins overlay
	// precedence on any shared_ownership.structured collision.
	out, code := runGitspork(t, []string{
		"integrate",
		"--upstream", "url=file://" + baseUpstream + ",version=main",
		"--upstream", "url=file://" + overlayUpstream + ",version=main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "multi-upstream integrate failed:\n%s", out)

	t.Run("non-overlapping upstream_owned files from BOTH upstreams land", func(t *testing.T) {
		// From platform-base (upstream #1)
		testharness.AssertFileContains(t, downstreamDir, "ci/build.yml", "name: build")
		testharness.AssertFileContains(t, downstreamDir, "ci/lint.yml", "name: lint")
		// From language-overlay (upstream #2)
		testharness.AssertFileContains(t, downstreamDir, "ci/language-check.yml", "name: language-check")
	})

	t.Run("structured shared file merges with overlay winning on collisions", func(t *testing.T) {
		got := testharness.ReadFile(t, downstreamDir, "app-config.yaml")
		m := map[string]any{}
		require.NoError(t, yaml.Unmarshal([]byte(got), &m))

		// Overlay overrides base on the collision.
		assert.Equal(t, "debug", m["log_level"],
			"overlay's log_level=debug must win over base's log_level=info because overlay integrates SECOND (last-writer-wins for shared_ownership.structured under prefer_upstream)")

		// Overlay-only key lands.
		assert.EqualValues(t, 5, m["retry_count"], "overlay-added key must survive")

		// Base-only key survives.
		assert.EqualValues(t, 30, m["timeout_seconds"], "base-only key must survive across the overlay merge")

		// Nested map: both sides contribute — tags.owner from base,
		// tags.language from overlay.
		tags, ok := m["tags"].(map[string]any)
		require.True(t, ok, "tags must be a nested mapping")
		assert.Equal(t, "platform", tags["owner"], "base's tags.owner must survive")
		assert.Equal(t, "go", tags["language"], "overlay's tags.language must land")
	})

	t.Run("state records BOTH upstreams in integration order", func(t *testing.T) {
		raw := testharness.ReadFile(t, downstreamDir, ".gitspork/downstream-state.json")
		var state struct {
			Upstreams []struct {
				URL        string `json:"url"`
				CommitHash string `json:"commit_hash"`
			} `json:"upstreams"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &state))
		require.Len(t, state.Upstreams, 2,
			"multi-upstream integrate must record every integrated upstream in state, in order")
		assert.Equal(t, "file://"+baseUpstream, state.Upstreams[0].URL,
			"base upstream must land at slot 0 (integrated first)")
		assert.Equal(t, "file://"+overlayUpstream, state.Upstreams[1].URL,
			"overlay upstream must land at slot 1 (integrated second)")
		assert.NotEmpty(t, state.Upstreams[0].CommitHash)
		assert.NotEmpty(t, state.Upstreams[1].CommitHash)
	})

	t.Run("check-drift succeeds with no drift after multi-upstream integrate", func(t *testing.T) {
		// Commit baseline so the working tree is clean.
		testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "post-multi-upstream integrate baseline")

		out, code := runGitspork(t, []string{
			"check-drift",
			"--downstream-repo-path", downstreamDir,
		}, downstreamDir)
		assert.Equal(t, 0, code, "check-drift should report no drift immediately after integrate:\n%s", out)
	})
}
