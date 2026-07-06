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

// setupStructuredMergeFixture prepares an upstream template + optional
// existing downstream file for exercising the templated Merged.Structured
// post-merge branch. Returns upstream+downstream dirs. templateName is the
// path of the template within the upstream dir; destName the path within
// the downstream dir; existingDownstream is content to seed (empty means
// no existing file — the "merge is skipped when dest doesn't exist" case).
func setupStructuredMergeFixture(t *testing.T, templateName, templateBody, destName, existingDownstream string) (string, string) {
	t.Helper()
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, templateName), []byte(templateBody), 0644))
	if existingDownstream != "" {
		dest := filepath.Join(downstreamDir, destName)
		require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0755))
		require.NoError(t, os.WriteFile(dest, []byte(existingDownstream), 0644))
	}
	return upstreamDir, downstreamDir
}

func TestIntegratorTemplated_structuredMerge_preferUpstream_YAML(t *testing.T) {
	// Upstream render "wins" on collision; downstream-only keys survive.
	upstreamTemplate := "shared_key: from-upstream\nupstream_only: value-u\n"
	existingDownstream := "shared_key: from-downstream\ndownstream_only: value-d\n"
	upstreamDir, downstreamDir := setupStructuredMergeFixture(t,
		"template.yaml.go.tmpl", upstreamTemplate,
		"config.yaml", existingDownstream)

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml.go.tmpl",
		Destination: "config.yaml",
		Merged:      &config.GitSporkConfigTemplatedMerged{Structured: config.TemplatedMergeStructuredPreferUpstream},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	merged, err := os.ReadFile(filepath.Join(downstreamDir, "config.yaml"))
	require.NoError(t, err)
	m := readYAMLMap(t, filepath.Join(downstreamDir, "config.yaml"))
	assert.Equal(t, "from-upstream", m["shared_key"], "upstream must win on collision")
	assert.Equal(t, "value-u", m["upstream_only"], "upstream-only key must land in downstream")
	assert.Equal(t, "value-d", m["downstream_only"], "downstream-only key must survive")
	// Sanity: file wasn't left as the raw template render (which would lack downstream_only)
	assert.Contains(t, string(merged), "downstream_only")
}

func TestIntegratorTemplated_structuredMerge_preferDownstream_YAML(t *testing.T) {
	// Downstream "wins" on collision; upstream-only keys land in downstream.
	upstreamTemplate := "shared_key: from-upstream\nupstream_only: value-u\n"
	existingDownstream := "shared_key: from-downstream\ndownstream_only: value-d\n"
	upstreamDir, downstreamDir := setupStructuredMergeFixture(t,
		"template.yaml.go.tmpl", upstreamTemplate,
		"config.yaml", existingDownstream)

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml.go.tmpl",
		Destination: "config.yaml",
		Merged:      &config.GitSporkConfigTemplatedMerged{Structured: config.TemplatedMergeStructuredPreferDownstream},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	m := readYAMLMap(t, filepath.Join(downstreamDir, "config.yaml"))
	assert.Equal(t, "from-downstream", m["shared_key"], "downstream must win on collision")
	assert.Equal(t, "value-d", m["downstream_only"])
	assert.Equal(t, "value-u", m["upstream_only"], "upstream-only key must still land in downstream")
}

func TestIntegratorTemplated_structuredMerge_preferUpstream_JSON(t *testing.T) {
	upstreamTemplate := `{"shared_key":"from-upstream","upstream_only":"value-u"}`
	existingDownstream := `{"shared_key":"from-downstream","downstream_only":"value-d"}`
	upstreamDir, downstreamDir := setupStructuredMergeFixture(t,
		"template.json.go.tmpl", upstreamTemplate,
		"config.json", existingDownstream)

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.json.go.tmpl",
		Destination: "config.json",
		Merged:      &config.GitSporkConfigTemplatedMerged{Structured: config.TemplatedMergeStructuredPreferUpstream},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	m := readJSONMap(t, filepath.Join(downstreamDir, "config.json"))
	assert.Equal(t, "from-upstream", m["shared_key"])
	assert.Equal(t, "value-u", m["upstream_only"])
	assert.Equal(t, "value-d", m["downstream_only"], "downstream-only JSON keys must survive")
}

func TestIntegratorTemplated_structuredMerge_preferDownstream_JSON(t *testing.T) {
	upstreamTemplate := `{"shared_key":"from-upstream","upstream_only":"value-u"}`
	existingDownstream := `{"shared_key":"from-downstream","downstream_only":"value-d"}`
	upstreamDir, downstreamDir := setupStructuredMergeFixture(t,
		"template.json.go.tmpl", upstreamTemplate,
		"config.json", existingDownstream)

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.json.go.tmpl",
		Destination: "config.json",
		Merged:      &config.GitSporkConfigTemplatedMerged{Structured: config.TemplatedMergeStructuredPreferDownstream},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	m := readJSONMap(t, filepath.Join(downstreamDir, "config.json"))
	assert.Equal(t, "from-downstream", m["shared_key"])
	assert.Equal(t, "value-d", m["downstream_only"])
	assert.Equal(t, "value-u", m["upstream_only"])
}

// TestIntegratorTemplated_structuredMerge_skippedWhenDestinationAbsent guards
// the pre-merge Stat check at integrator_templated.go:135. When Merged.Structured
// is set but the destination file doesn't yet exist, the merge path must be
// skipped — otherwise a fresh downstream would try to merge against a
// non-existent file and either error or produce garbage. Instead the template
// render is written straight to the destination unchanged.
func TestIntegratorTemplated_structuredMerge_skippedWhenDestinationAbsent(t *testing.T) {
	upstreamTemplate := "shared_key: from-upstream\nupstream_only: value-u\n"
	upstreamDir, downstreamDir := setupStructuredMergeFixture(t,
		"template.yaml.go.tmpl", upstreamTemplate,
		"config.yaml", "") // no existing downstream file

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml.go.tmpl",
		Destination: "config.yaml",
		Merged:      &config.GitSporkConfigTemplatedMerged{Structured: config.TemplatedMergeStructuredPreferUpstream},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	got, err := os.ReadFile(filepath.Join(downstreamDir, "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, upstreamTemplate, string(got),
		"first-run render with no existing dest must skip the structured merge and write the template output verbatim")
}

// TestIntegratorTemplated_structuredMerge_invalidMode surfaces a clear
// error rather than a silent no-op or panic when Merged.Structured is set
// to an unrecognized value AND the destination already exists.
func TestIntegratorTemplated_structuredMerge_invalidMode(t *testing.T) {
	upstreamDir, downstreamDir := setupStructuredMergeFixture(t,
		"template.yaml.go.tmpl", "k: v\n",
		"config.yaml", "existing: keep\n") // dest must exist so the guard runs

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml.go.tmpl",
		Destination: "config.yaml",
		Merged:      &config.GitSporkConfigTemplatedMerged{Structured: "prefer-neither"},
	}}
	err := (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid templated merged.structured value")
	assert.Contains(t, err.Error(), "prefer-neither")
	assert.Contains(t, err.Error(), config.TemplatedMergeStructuredPreferUpstream)
	assert.Contains(t, err.Error(), config.TemplatedMergeStructuredPreferDownstream)
}

// TestIntegratorTemplated_structuredMerge_noTmpDirLeak asserts the fix from
// PR #66 (defer scoping) holds: rendering many templated instructions in one
// Integrate call must not leave temp directories behind at the end of the run.
func TestIntegratorTemplated_structuredMerge_noTmpDirLeak(t *testing.T) {
	// Build an upstream with three templates + matching existing downstream files
	// so all three trigger the tmpDir path.
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()

	instructions := []config.GitSporkConfigTemplated{}
	for i, name := range []string{"a", "b", "c"} {
		templateFile := name + ".yaml.go.tmpl"
		destFile := name + ".yaml"
		_ = i
		require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, templateFile),
			[]byte("key_"+name+": upstream\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, destFile),
			[]byte("key_"+name+": downstream\ndownstream_only_"+name+": v\n"), 0644))
		instructions = append(instructions, config.GitSporkConfigTemplated{
			Template:    templateFile,
			Destination: destFile,
			Merged:      &config.GitSporkConfigTemplatedMerged{Structured: config.TemplatedMergeStructuredPreferUpstream},
		})
	}

	// Snapshot the OS temp root's entries before the run so we can spot leaks.
	tmpRoot := os.TempDir()
	before, err := os.ReadDir(tmpRoot)
	require.NoError(t, err)
	beforeCount := countGitsporkPrefixed(before)

	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	after, err := os.ReadDir(tmpRoot)
	require.NoError(t, err)
	afterCount := countGitsporkPrefixed(after)

	assert.Equal(t, beforeCount, afterCount,
		"structured-merge tmpDir defer must scope per iteration; leftover gitspork-prefixed temp dirs indicate a leak (before=%d, after=%d)", beforeCount, afterCount)

	// Merged output sanity — pick one destination and verify the merge shape
	// is what prefer-upstream should produce.
	m := readYAMLMap(t, filepath.Join(downstreamDir, "a.yaml"))
	assert.Equal(t, "upstream", m["key_a"])
	assert.Equal(t, "v", m["downstream_only_a"])
}

func countGitsporkPrefixed(entries []os.DirEntry) int {
	n := 0
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) >= len("gitspork") && e.Name()[:len("gitspork")] == "gitspork" {
			n++
		}
	}
	return n
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
