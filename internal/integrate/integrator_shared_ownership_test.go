package integrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupStructuredPair(t *testing.T, filename, upstreamContent, downstreamContent string) (string, string) {
	t.Helper()
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, filename), []byte(upstreamContent), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, filename), []byte(downstreamContent), 0644))
	return upstreamDir, downstreamDir
}

func readYAMLMap(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	m := map[string]any{}
	require.NoError(t, yaml.Unmarshal(b, &m))
	return m
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	m := map[string]any{}
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func readOrderedYAMLKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	n, err := parseYAML(b)
	require.NoError(t, err)
	return n.mapping.Keys()
}

func readOrderedJSONKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	n, err := parseJSON(b)
	require.NoError(t, err)
	return n.mapping.Keys()
}

func TestIntegratorSharedOwnershipStructuredPreferUpstream_YAML(t *testing.T) {
	upstreamYAML := "shared_key: from-upstream\nupstream_only: value-u\n"
	downstreamYAML := "shared_key: from-downstream\ndownstream_only: value-d\n"
	upstreamDir, downstreamDir := setupStructuredPair(t, "config.yaml", upstreamYAML, downstreamYAML)

	integrator := &IntegratorSharedOwnershipStructuredPreferUpstream{}
	require.NoError(t, integrator.Integrate([]string{"config.yaml"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	result := readYAMLMap(t, filepath.Join(downstreamDir, "config.yaml"))

	t.Run("upstream wins on collision", func(t *testing.T) {
		assert.Equal(t, "from-upstream", result["shared_key"])
	})
	t.Run("upstream-only keys land in downstream", func(t *testing.T) {
		assert.Equal(t, "value-u", result["upstream_only"])
	})
	t.Run("downstream-only keys survive", func(t *testing.T) {
		assert.Equal(t, "value-d", result["downstream_only"],
			"downstream-only keys should be preserved in a prefer-upstream merge (shared ownership means downstream can add keys of its own)")
	})
	t.Run("preserves upstream key order first, downstream-only appended", func(t *testing.T) {
		assert.Equal(t, []string{"shared_key", "upstream_only", "downstream_only"}, readOrderedYAMLKeys(t, filepath.Join(downstreamDir, "config.yaml")))
	})
}

func TestIntegratorSharedOwnershipStructuredPreferUpstream_JSON(t *testing.T) {
	upstreamJSON := `{"shared_key":"from-upstream","upstream_only":"value-u"}`
	downstreamJSON := `{"shared_key":"from-downstream","downstream_only":"value-d"}`
	upstreamDir, downstreamDir := setupStructuredPair(t, "config.json", upstreamJSON, downstreamJSON)

	integrator := &IntegratorSharedOwnershipStructuredPreferUpstream{}
	require.NoError(t, integrator.Integrate([]string{"config.json"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	result := readJSONMap(t, filepath.Join(downstreamDir, "config.json"))

	t.Run("upstream wins on collision", func(t *testing.T) {
		assert.Equal(t, "from-upstream", result["shared_key"])
	})
	t.Run("upstream-only keys land in downstream", func(t *testing.T) {
		assert.Equal(t, "value-u", result["upstream_only"])
	})
	t.Run("downstream-only keys survive", func(t *testing.T) {
		assert.Equal(t, "value-d", result["downstream_only"],
			"downstream-only keys should be preserved in a prefer-upstream merge (shared ownership means downstream can add keys of its own)")
	})
	t.Run("preserves upstream key order first, downstream-only appended", func(t *testing.T) {
		assert.Equal(t, []string{"shared_key", "upstream_only", "downstream_only"}, readOrderedJSONKeys(t, filepath.Join(downstreamDir, "config.json")))
	})
}

func TestIntegratorSharedOwnershipStructuredPreferDownstream_YAML(t *testing.T) {
	upstreamYAML := "shared_key: from-upstream\nupstream_only: value-u\n"
	downstreamYAML := "shared_key: from-downstream\ndownstream_only: value-d\n"
	upstreamDir, downstreamDir := setupStructuredPair(t, "config.yaml", upstreamYAML, downstreamYAML)

	integrator := &IntegratorSharedOwnershipStructuredPreferDownstream{}
	require.NoError(t, integrator.Integrate([]string{"config.yaml"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	result := readYAMLMap(t, filepath.Join(downstreamDir, "config.yaml"))

	t.Run("downstream wins on collision", func(t *testing.T) {
		assert.Equal(t, "from-downstream", result["shared_key"])
	})
	t.Run("downstream-only keys survive", func(t *testing.T) {
		assert.Equal(t, "value-d", result["downstream_only"])
	})
	t.Run("upstream-only keys land in downstream", func(t *testing.T) {
		assert.Equal(t, "value-u", result["upstream_only"])
	})
	t.Run("preserves downstream key order first, upstream-only appended", func(t *testing.T) {
		assert.Equal(t, []string{"shared_key", "downstream_only", "upstream_only"}, readOrderedYAMLKeys(t, filepath.Join(downstreamDir, "config.yaml")))
	})
}

func TestIntegratorSharedOwnershipStructuredPreferDownstream_JSON(t *testing.T) {
	upstreamJSON := `{"shared_key":"from-upstream","upstream_only":"value-u"}`
	downstreamJSON := `{"shared_key":"from-downstream","downstream_only":"value-d"}`
	upstreamDir, downstreamDir := setupStructuredPair(t, "config.json", upstreamJSON, downstreamJSON)

	integrator := &IntegratorSharedOwnershipStructuredPreferDownstream{}
	require.NoError(t, integrator.Integrate([]string{"config.json"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	result := readJSONMap(t, filepath.Join(downstreamDir, "config.json"))

	t.Run("downstream wins on collision", func(t *testing.T) {
		assert.Equal(t, "from-downstream", result["shared_key"])
	})
	t.Run("downstream-only keys survive", func(t *testing.T) {
		assert.Equal(t, "value-d", result["downstream_only"])
	})
	t.Run("upstream-only keys land in downstream", func(t *testing.T) {
		assert.Equal(t, "value-u", result["upstream_only"])
	})
	t.Run("preserves downstream key order first, upstream-only appended", func(t *testing.T) {
		assert.Equal(t, []string{"shared_key", "downstream_only", "upstream_only"}, readOrderedJSONKeys(t, filepath.Join(downstreamDir, "config.json")))
	})
}

func TestIntegratorSharedOwnershipMerged(t *testing.T) {
	beginMarker := "# ::gitspork::begin-upstream-owned-block"
	endMarker := "# ::gitspork::end-upstream-owned-block"

	t.Run("upstream block content replaces downstream block content", func(t *testing.T) {
		upstream := beginMarker + "\nupstream owned line A\nupstream owned line B\n" + endMarker + "\n"
		downstream := beginMarker + "\nstale downstream block content\n" + endMarker + "\n"
		upstreamDir, downstreamDir := setupStructuredPair(t, "Makefile", upstream, downstream)

		integrator := &IntegratorSharedOwnershipMerged{}
		require.NoError(t, integrator.Integrate([]string{"Makefile"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

		got, err := os.ReadFile(filepath.Join(downstreamDir, "Makefile"))
		require.NoError(t, err)
		assert.Contains(t, string(got), "upstream owned line A")
		assert.Contains(t, string(got), "upstream owned line B")
		assert.NotContains(t, string(got), "stale downstream block content")
	})

	t.Run("downstream lines outside upstream blocks are preserved", func(t *testing.T) {
		upstream := beginMarker + "\nupstream owned\n" + endMarker + "\n"
		downstream := "downstream-only line above\n" +
			beginMarker + "\nstale\n" + endMarker + "\n" +
			"downstream-only line below\n"
		upstreamDir, downstreamDir := setupStructuredPair(t, "Makefile", upstream, downstream)

		integrator := &IntegratorSharedOwnershipMerged{}
		require.NoError(t, integrator.Integrate([]string{"Makefile"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

		got, err := os.ReadFile(filepath.Join(downstreamDir, "Makefile"))
		require.NoError(t, err)
		assert.Contains(t, string(got), "downstream-only line above")
		assert.Contains(t, string(got), "downstream-only line below")
		assert.Contains(t, string(got), "upstream owned")
		assert.NotContains(t, string(got), "stale")

		aboveIdx := strings.Index(string(got), "downstream-only line above")
		upstreamIdx := strings.Index(string(got), "upstream owned")
		belowIdx := strings.Index(string(got), "downstream-only line below")
		assert.True(t, aboveIdx < upstreamIdx && upstreamIdx < belowIdx,
			"downstream lines should retain their original relative position around the upstream block")
	})

	t.Run("upstream block appended when missing in downstream", func(t *testing.T) {
		upstream := beginMarker + "\nfresh upstream content\n" + endMarker + "\n"
		downstream := "downstream only content\nno markers here\n"
		upstreamDir, downstreamDir := setupStructuredPair(t, "Makefile", upstream, downstream)

		integrator := &IntegratorSharedOwnershipMerged{}
		require.NoError(t, integrator.Integrate([]string{"Makefile"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

		got, err := os.ReadFile(filepath.Join(downstreamDir, "Makefile"))
		require.NoError(t, err)
		assert.Contains(t, string(got), "downstream only content")
		assert.Contains(t, string(got), "fresh upstream content")

		downstreamIdx := strings.Index(string(got), "downstream only content")
		upstreamIdx := strings.Index(string(got), "fresh upstream content")
		assert.True(t, downstreamIdx < upstreamIdx,
			"an upstream block not present in downstream should be appended after existing downstream content")
	})

	t.Run("downstream has more begin-markers than upstream: no panic, orphan block preserved", func(t *testing.T) {
		// Upstream provides zero upstream-owned blocks (e.g. block was removed by upstream);
		// downstream still carries a marker pair from a previous integration. This must not
		// panic and the orphaned downstream block should be preserved verbatim so the user
		// can reconcile — it has effectively transitioned to downstream ownership.
		upstream := "regular upstream content\n"
		downstream := "downstream head\n" +
			beginMarker + "\norphan block content\n" + endMarker + "\n" +
			"downstream tail\n"
		upstreamDir, downstreamDir := setupStructuredPair(t, "Makefile", upstream, downstream)

		integrator := &IntegratorSharedOwnershipMerged{}
		require.NotPanics(t, func() {
			require.NoError(t, integrator.Integrate([]string{"Makefile"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))
		})

		got, err := os.ReadFile(filepath.Join(downstreamDir, "Makefile"))
		require.NoError(t, err)
		assert.Contains(t, string(got), "downstream head")
		assert.Contains(t, string(got), "downstream tail")
		assert.Contains(t, string(got), "orphan block content",
			"content inside an unmatched downstream begin/end marker pair should be preserved (now downstream-owned)")
		assert.Contains(t, string(got), beginMarker,
			"the unmatched begin-marker itself should be preserved so the user can reconcile explicitly")
	})

	t.Run("handles lines larger than the stdlib bufio.Scanner default (64 KiB)", func(t *testing.T) {
		// Real-world files under this integrator include Makefiles and lock
		// files that occasionally carry very long lines (minified content,
		// generated code). The default bufio.Scanner buffer capped at 64 KiB
		// per line would return bufio.ErrTooLong; bumped to a per-file limit
		// large enough for reasonable content.
		const longLineLen = 200 * 1024 // 200 KiB > default 64 KiB
		longLine := strings.Repeat("x", longLineLen)
		upstream := beginMarker + "\n" + longLine + "\n" + endMarker + "\n"
		downstream := beginMarker + "\nstale short line\n" + endMarker + "\n"
		upstreamDir, downstreamDir := setupStructuredPair(t, "generated.txt", upstream, downstream)

		integrator := &IntegratorSharedOwnershipMerged{}
		require.NoError(t, integrator.Integrate([]string{"generated.txt"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

		got, err := os.ReadFile(filepath.Join(downstreamDir, "generated.txt"))
		require.NoError(t, err)
		assert.Contains(t, string(got), longLine, "long upstream-owned line should be preserved intact")
		assert.NotContains(t, string(got), "stale short line", "downstream block content should be replaced by upstream content")
	})

	t.Run("downstream has second unmatched begin-marker after a matched one", func(t *testing.T) {
		// Mixed case: first begin-marker in downstream matches the single upstream block;
		// second begin-marker in downstream has nothing to match. Must not panic.
		upstream := beginMarker + "\nupstream block\n" + endMarker + "\n"
		downstream := beginMarker + "\nstale first block\n" + endMarker + "\n" +
			"middle downstream line\n" +
			beginMarker + "\norphan second block\n" + endMarker + "\n"
		upstreamDir, downstreamDir := setupStructuredPair(t, "Makefile", upstream, downstream)

		integrator := &IntegratorSharedOwnershipMerged{}
		require.NotPanics(t, func() {
			require.NoError(t, integrator.Integrate([]string{"Makefile"}, upstreamDir, downstreamDir, sdktypes.NoopLogger()))
		})

		got, err := os.ReadFile(filepath.Join(downstreamDir, "Makefile"))
		require.NoError(t, err)
		assert.Contains(t, string(got), "upstream block")
		assert.NotContains(t, string(got), "stale first block")
		assert.Contains(t, string(got), "middle downstream line")
		assert.Contains(t, string(got), "orphan second block")
	})
}
