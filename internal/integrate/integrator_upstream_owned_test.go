package integrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegratorUpstreamOwned_upstreamOnly_skipsMatchingFiles(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	for rel, content := range map[string]string{
		"cli/.cloud-native-template/local.txt": "cli local\n",
		".cloud-native-template/shared.txt":    "shared\n",
	} {
		full := filepath.Join(upstreamDir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}

	entries := []config.OwnedEntry{{Pattern: "{,**/}.cloud-native-template/**"}}
	integrator := &IntegratorUpstreamOwned{UpstreamOnly: []string{"cli/**"}}
	require.NoError(t, integrator.Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	// excluded by upstream_only: cli/**
	_, err := os.Stat(filepath.Join(downstreamDir, "cli/.cloud-native-template/local.txt"))
	assert.True(t, os.IsNotExist(err), "cli/.cloud-native-template/local.txt should be absent from downstream")

	// not excluded — syncs normally
	got, err := os.ReadFile(filepath.Join(downstreamDir, ".cloud-native-template/shared.txt"))
	require.NoError(t, err)
	assert.Equal(t, "shared\n", string(got))
}
