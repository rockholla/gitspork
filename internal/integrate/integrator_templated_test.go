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

func setupTemplatedFixture(t *testing.T, upstreamTemplateContent, downstreamJSONInputContent string) (string, string) {
	t.Helper()
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.txt"), []byte(upstreamTemplateContent), 0644))
	if downstreamJSONInputContent != "" {
		require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "inputs.json"), []byte(downstreamJSONInputContent), 0644))
	}
	return upstreamDir, downstreamDir
}

func TestIntegratorTemplated_writesConsolidatedCacheAndGitattributes(t *testing.T) {
	upstreamDir, downstreamDir := setupTemplatedFixture(t,
		`Hello, {{ index .Inputs "name" }}!`,
		`{"name":"world"}`,
	)
	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.txt",
		Destination: "rendered.txt",
		Inputs: []config.GitSporkConfigTemplatedInput{
			{Name: "name", JSONDataPath: "inputs.json"},
		},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	t.Run("consolidated cache is written", func(t *testing.T) {
		cache, err := loadTemplatedInputs(downstreamDir)
		require.NoError(t, err)
		assert.Equal(t, "world", cache["rendered.txt"]["name"])
	})
	t.Run("no per-destination legacy file is written", func(t *testing.T) {
		_, err := os.Stat(filepath.Join(downstreamDir, ".gitspork", "rendered.txt.json"))
		assert.True(t, os.IsNotExist(err))
	})
	t.Run(".gitattributes marks cache as generated", func(t *testing.T) {
		attrs, err := os.ReadFile(filepath.Join(downstreamDir, ".gitattributes"))
		require.NoError(t, err)
		assert.Contains(t, string(attrs), gitsporkAttrPattern)
		assert.Contains(t, string(attrs), gitsporkAttrMarker)
	})
	t.Run("template rendered as expected", func(t *testing.T) {
		rendered, err := os.ReadFile(filepath.Join(downstreamDir, "rendered.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Hello, world!", string(rendered))
	})
}

func TestIntegratorTemplated_migratesLegacyCacheOnRun(t *testing.T) {
	upstreamDir, downstreamDir := setupTemplatedFixture(t,
		`Hello, {{ index .Inputs "name" }}!`,
		"", // no inputs.json — we'll seed the legacy cache instead
	)
	// seed a legacy per-destination cache file (the old on-disk format)
	require.NoError(t, os.MkdirAll(filepath.Join(downstreamDir, ".gitspork"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(downstreamDir, ".gitspork", "rendered.txt.json"),
		[]byte(`{"inputs":{"name":"legacy-value"}}`),
		0644,
	))

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.txt",
		Destination: "rendered.txt",
		Inputs: []config.GitSporkConfigTemplatedInput{
			// prompt input, but the migrated value should already satisfy it (forceRePrompt=false),
			// so the prompt path won't try to read stdin
			{Name: "name", Prompt: "enter name"},
		},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	t.Run("legacy file removed", func(t *testing.T) {
		_, err := os.Stat(filepath.Join(downstreamDir, ".gitspork", "rendered.txt.json"))
		assert.True(t, os.IsNotExist(err))
	})
	t.Run("value migrated into consolidated cache", func(t *testing.T) {
		cache, err := loadTemplatedInputs(downstreamDir)
		require.NoError(t, err)
		assert.Equal(t, "legacy-value", cache["rendered.txt"]["name"])
	})
	t.Run("render uses the migrated value", func(t *testing.T) {
		rendered, err := os.ReadFile(filepath.Join(downstreamDir, "rendered.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Hello, legacy-value!", string(rendered))
	})
}

func TestIntegratorTemplated_prunesStaleDestinationsFromCache(t *testing.T) {
	upstreamDir, downstreamDir := setupTemplatedFixture(t,
		`Hello, {{ index .Inputs "name" }}!`,
		`{"name":"world"}`,
	)
	// seed the consolidated cache with a destination that ISN'T in this run's instructions
	require.NoError(t, saveTemplatedInputs(downstreamDir, map[string]map[string]string{
		"stale.txt":    {"old": "value"},
		"rendered.txt": {"name": "previous-run-value"},
	}))

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.txt",
		Destination: "rendered.txt",
		Inputs: []config.GitSporkConfigTemplatedInput{
			{Name: "name", JSONDataPath: "inputs.json"},
		},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	cache, err := loadTemplatedInputs(downstreamDir)
	require.NoError(t, err)
	assert.NotContains(t, cache, "stale.txt", "destinations no longer configured must be pruned")
	assert.Contains(t, cache, "rendered.txt", "currently-configured destinations must survive")
	assert.Equal(t, "world", cache["rendered.txt"]["name"], "current-run inputs should overwrite prior values")
}

func TestIntegratorTemplated_noopWithEmptyInstructionsAndNoCache(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()

	require.NoError(t, (&IntegratorTemplated{}).Integrate(nil, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	_, err := os.Stat(filepath.Join(downstreamDir, ".gitspork"))
	assert.True(t, os.IsNotExist(err), "must not create .gitspork/ when there's nothing templated to cache")
	_, err = os.Stat(filepath.Join(downstreamDir, ".gitattributes"))
	assert.True(t, os.IsNotExist(err), "must not create .gitattributes on a downstream with no templated integration")
}

func TestIntegratorTemplated_repeatRunProducesByteIdenticalCache(t *testing.T) {
	upstreamDir, downstreamDir := setupTemplatedFixture(t,
		`Hello, {{ index .Inputs "name" }}!`,
		`{"name":"world"}`,
	)
	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.txt",
		Destination: "rendered.txt",
		Inputs: []config.GitSporkConfigTemplatedInput{
			{Name: "name", JSONDataPath: "inputs.json"},
		},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))
	firstBytes, err := os.ReadFile(filepath.Join(downstreamDir, ".gitspork", templatedInputsCacheFileName))
	require.NoError(t, err)

	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))
	secondBytes, err := os.ReadFile(filepath.Join(downstreamDir, ".gitspork", templatedInputsCacheFileName))
	require.NoError(t, err)

	assert.Equal(t, firstBytes, secondBytes, "repeated integrate with unchanged inputs must produce byte-identical cache (no git churn)")
}
