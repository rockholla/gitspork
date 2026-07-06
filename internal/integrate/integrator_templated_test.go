package integrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/config"
	inputpkg "github.com/rockholla/gitspork/v2/internal/input"
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

// stubRequestInput replaces requestInputFn with a counting stub for the
// duration of a test. Returns a pointer to the call count and the resolved
// values captured per call so tests can assert both invocation count and
// argument shape. Restores the original via t.Cleanup.
func stubRequestInput(t *testing.T, returnValue string) *stubCounter {
	t.Helper()
	orig := requestInputFn
	sc := &stubCounter{returnValue: returnValue}
	requestInputFn = func(opts *inputpkg.RequestInputOptions) (*inputpkg.RequestInputResult, error) {
		sc.calls++
		sc.prompts = append(sc.prompts, opts.Prompt)
		return &inputpkg.RequestInputResult{StringValue: sc.returnValue}, nil
	}
	t.Cleanup(func() { requestInputFn = orig })
	return sc
}

type stubCounter struct {
	calls       int
	prompts     []string
	returnValue string
}

// TestIntegratorTemplated_forceRePrompt covers the four cells of the
// (cached-value present) × (forceRePrompt true|false) matrix on a prompt
// input. The seam swap on requestInputFn is what makes this testable — a
// production regression that stopped honouring forceRePrompt would either
// keep the stub un-called (if the seam were bypassed) or call it in a case
// where cached content should have won.
func TestIntegratorTemplated_forceRePrompt(t *testing.T) {
	setupPromptFixture := func(t *testing.T, seedCache bool, cachedValue string) (string, string) {
		t.Helper()
		upstreamDir := t.TempDir()
		downstreamDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.txt"),
			[]byte(`Hello, {{ index .Inputs "name" }}!`), 0644))
		if seedCache {
			require.NoError(t, saveTemplatedInputs(downstreamDir, map[string]map[string]string{
				"rendered.txt": {"name": cachedValue},
			}))
		}
		return upstreamDir, downstreamDir
	}

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.txt",
		Destination: "rendered.txt",
		Inputs:      []config.GitSporkConfigTemplatedInput{{Name: "name", Prompt: "what is your name?"}},
	}}

	t.Run("no cache, forceRePrompt=false: prompts", func(t *testing.T) {
		// Baseline: without a cached value, the prompt runs regardless of
		// forceRePrompt.
		upstream, downstream := setupPromptFixture(t, false, "")
		stub := stubRequestInput(t, "fresh-value")
		require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstream, downstream, false, sdktypes.NoopLogger()))
		assert.Equal(t, 1, stub.calls, "empty cache must trigger the prompt exactly once")
		got, err := os.ReadFile(filepath.Join(downstream, "rendered.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Hello, fresh-value!", string(got))
	})

	t.Run("cached value, forceRePrompt=false: skips prompt", func(t *testing.T) {
		// The whole point of the cache — a re-integrate must NOT re-prompt
		// the user for values they've already given. A stub that gets called
		// here indicates a broken cache path.
		upstream, downstream := setupPromptFixture(t, true, "cached-value")
		stub := stubRequestInput(t, "SHOULD-NEVER-BE-USED")
		require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstream, downstream, false, sdktypes.NoopLogger()))
		assert.Equal(t, 0, stub.calls,
			"cached value must satisfy the prompt input — a call here indicates a broken cache lookup or accidental re-prompt")
		got, err := os.ReadFile(filepath.Join(downstream, "rendered.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Hello, cached-value!", string(got),
			"rendered content must come from the cache, not the stub")
	})

	t.Run("cached value, forceRePrompt=true: re-prompts and replaces", func(t *testing.T) {
		// The feature under test: users explicitly opt into re-prompting to
		// change previously-captured input values (e.g. renaming a project).
		// The prompt MUST run, and the returned value MUST replace the cache
		// entry for subsequent runs.
		upstream, downstream := setupPromptFixture(t, true, "cached-value")
		stub := stubRequestInput(t, "re-prompted-value")
		require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstream, downstream, true, sdktypes.NoopLogger()))

		assert.Equal(t, 1, stub.calls,
			"forceRePrompt=true must trigger the prompt exactly once even when a cached value exists")
		assert.Equal(t, []string{"what is your name?"}, stub.prompts,
			"the prompt string configured on the input must be passed through")

		got, err := os.ReadFile(filepath.Join(downstream, "rendered.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Hello, re-prompted-value!", string(got),
			"the re-prompted value must win over the cached one for the render")

		// And the new value must be written back to the cache so subsequent
		// runs without forceRePrompt see the new value.
		nextCache, err := loadTemplatedInputs(downstream)
		require.NoError(t, err)
		assert.Equal(t, "re-prompted-value", nextCache["rendered.txt"]["name"],
			"post-forceRePrompt cache must carry the newly-prompted value forward")
	})

	t.Run("no cache, forceRePrompt=true: prompts (same as baseline)", func(t *testing.T) {
		// forceRePrompt=true with no cache is not architecturally different
		// from the baseline — the condition is (cached==empty || forceRePrompt)
		// so either path triggers the prompt. This subtest exists to lock the
		// invariant that the flag isn't accidentally short-circuiting away
		// the prompt for fresh installs.
		upstream, downstream := setupPromptFixture(t, false, "")
		stub := stubRequestInput(t, "value")
		require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstream, downstream, true, sdktypes.NoopLogger()))
		assert.Equal(t, 1, stub.calls)
	})
}

// TestIntegratorTemplated_previousInput_happyPath verifies the cross-template
// value flow: template B pulls an input value that template A populated
// earlier in the same Integrate call. This is the documented previous_input
// contract (integrator_templated.go:98-112) with no direct end-to-end proof
// prior to this test.
func TestIntegratorTemplated_previousInput_happyPath(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()

	// Template A resolves an input via json_data_path; template B echoes A's
	// captured value via previous_input.
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "a.txt.go.tmpl"),
		[]byte(`A: {{ index .Inputs "shared" }}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "b.txt.go.tmpl"),
		[]byte(`B: {{ index .Inputs "borrowed" }}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "inputs.json"),
		[]byte(`{"shared":"from-A"}`), 0644))

	instructions := []config.GitSporkConfigTemplated{
		{
			Template:    "a.txt.go.tmpl",
			Destination: "a.txt",
			Inputs:      []config.GitSporkConfigTemplatedInput{{Name: "shared", JSONDataPath: "inputs.json"}},
		},
		{
			Template:    "b.txt.go.tmpl",
			Destination: "b.txt",
			Inputs: []config.GitSporkConfigTemplatedInput{{
				Name: "borrowed",
				PreviousInput: &config.GitSporkConfigTemplatedInputPrevious{
					Template: "a.txt.go.tmpl",
					Name:     "shared",
				},
			}},
		},
	}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	aOut, err := os.ReadFile(filepath.Join(downstreamDir, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "A: from-A", string(aOut))

	bOut, err := os.ReadFile(filepath.Join(downstreamDir, "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "B: from-A", string(bOut),
		"template B should see the value template A captured earlier in the run")
}

// TestIntegratorTemplated_previousInput_templateNotFound: template B references
// a template that was never defined in this run. previous_input reads from
// capturedInputValues, which is only populated for templates iterated so far,
// so a nonexistent name (or a template defined AFTER the reference) triggers
// this branch.
func TestIntegratorTemplated_previousInput_templateNotFound(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "b.txt.go.tmpl"),
		[]byte(`B: {{ index .Inputs "borrowed" }}`), 0644))

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "b.txt.go.tmpl",
		Destination: "b.txt",
		Inputs: []config.GitSporkConfigTemplatedInput{{
			Name: "borrowed",
			PreviousInput: &config.GitSporkConfigTemplatedInputPrevious{
				Template: "nonexistent.txt.go.tmpl",
				Name:     "shared",
			},
		}},
	}}
	err := (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "previous template not found: nonexistent.txt.go.tmpl")
	assert.Contains(t, err.Error(), "b.txt.go.tmpl",
		"error should identify which template's config triggered the failure")
}

// TestIntegratorTemplated_previousInput_forwardReferenceFails: previous_input
// only looks at templates PROCESSED SO FAR — a forward reference (A references
// B but A comes first) must fail with the same "previous template not found"
// error rather than magically resolving. Documents the intended ordering.
func TestIntegratorTemplated_previousInput_forwardReferenceFails(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "a.txt.go.tmpl"),
		[]byte(`A: {{ index .Inputs "borrowed" }}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "b.txt.go.tmpl"),
		[]byte(`B: {{ index .Inputs "shared" }}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "inputs.json"),
		[]byte(`{"shared":"from-B"}`), 0644))

	// A tries to pull from B — but A is listed first, so B hasn't been
	// processed yet when A runs.
	instructions := []config.GitSporkConfigTemplated{
		{
			Template:    "a.txt.go.tmpl",
			Destination: "a.txt",
			Inputs: []config.GitSporkConfigTemplatedInput{{
				Name: "borrowed",
				PreviousInput: &config.GitSporkConfigTemplatedInputPrevious{
					Template: "b.txt.go.tmpl",
					Name:     "shared",
				},
			}},
		},
		{
			Template:    "b.txt.go.tmpl",
			Destination: "b.txt",
			Inputs:      []config.GitSporkConfigTemplatedInput{{Name: "shared", JSONDataPath: "inputs.json"}},
		},
	}
	err := (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "previous template not found: b.txt.go.tmpl",
		"previous_input must not resolve forward references — the earlier template can't see values from a template defined later")
}

// TestIntegratorTemplated_previousInput_inputNameNotFound: template B correctly
// references template A (which was processed earlier), but the input name it
// asks for was never captured under A.
func TestIntegratorTemplated_previousInput_inputNameNotFound(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "a.txt.go.tmpl"),
		[]byte(`A: {{ index .Inputs "known_key" }}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "b.txt.go.tmpl"),
		[]byte(`B: {{ index .Inputs "borrowed" }}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "inputs.json"),
		[]byte(`{"known_key":"available"}`), 0644))

	instructions := []config.GitSporkConfigTemplated{
		{
			Template:    "a.txt.go.tmpl",
			Destination: "a.txt",
			Inputs:      []config.GitSporkConfigTemplatedInput{{Name: "known_key", JSONDataPath: "inputs.json"}},
		},
		{
			Template:    "b.txt.go.tmpl",
			Destination: "b.txt",
			Inputs: []config.GitSporkConfigTemplatedInput{{
				Name: "borrowed",
				PreviousInput: &config.GitSporkConfigTemplatedInputPrevious{
					Template: "a.txt.go.tmpl",
					Name:     "missing_key", // never captured under A
				},
			}},
		},
	}
	err := (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "previous input name missing_key not found in template a.txt.go.tmpl")
	assert.Contains(t, err.Error(), "b.txt.go.tmpl",
		"error should identify which template's config triggered the failure")
}

// TestIntegratorTemplated_previousInput_jsonParseError_bailsBeforeNextTemplate
// pins the current behavior: a json_data_path parse failure in template A
// aborts the whole Integrate call rather than letting subsequent templates
// see partial data. This together with the maps.Copy ordering (PR #66) means
// capturedInputValues cannot leak partial JSON data into a previous_input
// chain — the check-then-copy order and the immediate return together
// enforce the invariant.
func TestIntegratorTemplated_previousInput_jsonParseError_bailsBeforeNextTemplate(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "a.txt.go.tmpl"),
		[]byte(`A: {{ index .Inputs "k" }}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "b.txt.go.tmpl"),
		[]byte(`B: {{ index .Inputs "borrowed" }}`), 0644))
	// Malformed JSON — parse fails.
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "inputs.json"),
		[]byte(`{"k": "unterminated`), 0644))

	instructions := []config.GitSporkConfigTemplated{
		{
			Template:    "a.txt.go.tmpl",
			Destination: "a.txt",
			Inputs:      []config.GitSporkConfigTemplatedInput{{Name: "k", JSONDataPath: "inputs.json"}},
		},
		{
			Template:    "b.txt.go.tmpl",
			Destination: "b.txt",
			Inputs: []config.GitSporkConfigTemplatedInput{{
				Name: "borrowed",
				PreviousInput: &config.GitSporkConfigTemplatedInputPrevious{
					Template: "a.txt.go.tmpl",
					Name:     "k",
				},
			}},
		},
	}
	err := (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing json_data_path file",
		"the parse error, not the previous_input error, should surface (A fails before B runs)")

	// Template B must NOT have been processed — neither destination file should exist.
	_, aErr := os.Stat(filepath.Join(downstreamDir, "a.txt"))
	assert.True(t, os.IsNotExist(aErr), "template A should not have been rendered when its input parse failed")
	_, bErr := os.Stat(filepath.Join(downstreamDir, "b.txt"))
	assert.True(t, os.IsNotExist(bErr), "template B must not run when A's input capture failed")
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
