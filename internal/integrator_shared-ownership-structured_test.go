package internal

// Verifies the structured-data merge behavior of the consolidated
// IntegratorSharedOwnershipStructured: collision precedence in both directions,
// and that keys unique to either side survive the merge (the prefer-upstream
// regression — previously the raw upstream was written, dropping downstream-only
// keys).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeJSON(t *testing.T, path string, data map[string]any) {
	t.Helper()
	b, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0644))
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func TestIntegratorSharedOwnershipStructured_Merge(t *testing.T) {
	cases := []struct {
		name           string
		preferUpstream bool
		wantShared     string // expected value of the colliding "shared" key
	}{
		{name: "prefer upstream wins collision", preferUpstream: true, wantShared: "up"},
		{name: "prefer downstream wins collision", preferUpstream: false, wantShared: "down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstreamDir := t.TempDir()
			downstreamDir := t.TempDir()
			writeJSON(t, filepath.Join(upstreamDir, "config.json"), map[string]any{
				"shared":       "up",
				"upstreamOnly": "u",
			})
			writeJSON(t, filepath.Join(downstreamDir, "config.json"), map[string]any{
				"shared":         "down",
				"downstreamOnly": "d",
			})

			integrator := &IntegratorSharedOwnershipStructured{PreferUpstream: tc.preferUpstream}
			require.NoError(t, integrator.Integrate([]string{"config.json"}, upstreamDir, downstreamDir, NewLogger()))

			got := readJSON(t, filepath.Join(downstreamDir, "config.json"))
			assert.Equal(t, tc.wantShared, got["shared"], "collision precedence")
			// keys unique to either side must always survive the union merge,
			// regardless of preference (the prefer-upstream data-loss regression).
			assert.Equal(t, "u", got["upstreamOnly"], "upstream-only key must survive")
			assert.Equal(t, "d", got["downstreamOnly"], "downstream-only key must survive")
		})
	}
}
