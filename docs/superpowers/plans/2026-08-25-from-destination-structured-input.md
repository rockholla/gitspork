# `from_destination_structured` Templated Input Source — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `from_destination_structured` input source to `GitSporkConfigTemplated` that reads a scalar value from a dot-delimited path within the current template's already-rendered destination file (YAML or JSON), falling through to the remaining configured sources (`json_data_path`, `prompt`, `previous_input`) when the file or path is unavailable.

**Architecture:** A new `resolveStructuredPath(filePath, dotPath string) (string, bool, error)` helper in `internal/integrate/structured_path.go` handles format detection, parsing (reusing existing `parseJSON`/`parseYAML`), and `node`-tree traversal. The `integrator_templated.go` input loop gains a pre-check block at the top: if `from_destination_structured` is defined and resolves, the value is used and the loop `continue`s, skipping the `json_data_path`/`prompt`/`previous_input` chain. If it doesn't resolve, the chain runs unchanged. A typo in the config YAML tag is also fixed.

**Tech Stack:** Go stdlib (`os`, `filepath`, `strings`, `fmt`), existing `node`/`parseJSON`/`parseYAML` infrastructure in `internal/integrate`, `github.com/stretchr/testify`.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/config/config.go` | Modify | Fix YAML tag typo; add `from_destination_structured` schema example |
| `internal/integrate/structured_path.go` | Create | `resolveStructuredPath` helper |
| `internal/integrate/structured_path_test.go` | Create | Unit tests for `resolveStructuredPath` |
| `internal/integrate/integrator_templated.go` | Modify | New `from_destination_structured` input branch |
| `internal/integrate/integrator_templated_test.go` | Modify | Integration tests for the new input branch |

---

## Task 1: Fix config typo and add schema example

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Fix the YAML tag typo**

In `internal/config/config.go` at the `FromDestinationStructured` field on `GitSporkConfigTemplatedInput`, change the YAML tag from `from_destionation_structured` to `from_destination_structured`:

```go
FromDestinationStructured *GitSporkConfigTemplatedInputDestinationStructured `yaml:"from_destination_structured,omitempty" comment:"(optional) pull value from a dot-delimited path in the already-rendered destination file (must be JSON or YAML); falls through to 'prompt' when file or path is unavailable"`
```

Note: also add `omitempty` so the field is omitted when nil during YAML marshalling (consistent with other optional pointer fields like `PreviousInput`).

- [ ] **Step 2: Add schema example entry**

In `GetGitSporkConfigSchema()`, the `Templated` slice has one example entry. Add a fourth `GitSporkConfigTemplatedInput` to its `Inputs` slice showing `from_destination_structured` + `prompt` together:

```go
Templated: []GitSporkConfigTemplated{
    {
        Template:    "meta.txt.go.tmpl",
        Destination: "meta.txt",
        Merged: &GitSporkConfigTemplatedMerged{
            Structured: TemplatedMergeStructuredPreferDownstream,
        },
        Inputs: []GitSporkConfigTemplatedInput{
            {
                Name:   "input_one",
                Prompt: "What is the value of input_one?",
            },
            {
                Name:         "input_two",
                JSONDataPath: "./.json/data.json",
            },
            {
                Name: "input_three",
                PreviousInput: &GitSporkConfigTemplatedInputPrevious{
                    Template: "meta.txt.go.tmpl",
                    Name:     "input_one",
                },
            },
            {
                Name:   "input_four",
                Prompt: "What is the value of input_four?",
                FromDestinationStructured: &GitSporkConfigTemplatedInputDestinationStructured{
                    Path: "some.nested.key",
                },
            },
        },
    },
},
```

- [ ] **Step 3: Verify unit tests still pass**

```bash
make test-unit
```

Expected: all tests pass; no compile errors.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "fix(config): correct from_destination_structured yaml tag typo; add schema example"
```

---

## Task 2: Write failing unit tests for `resolveStructuredPath`

**Files:**
- Create: `internal/integrate/structured_path_test.go`

- [ ] **Step 1: Create the test file**

```go
package integrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStructuredPath_yamlScalar(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("name: alice\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "name")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "alice", v)
}

func TestResolveStructuredPath_ymlExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
		[]byte("name: bob\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yml"), "name")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "bob", v)
}

func TestResolveStructuredPath_jsonScalar(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"name":"charlie"}`), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.json"), "name")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "charlie", v)
}

func TestResolveStructuredPath_nestedDotPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("user:\n  profile:\n    handle: dexter\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "user.profile.handle")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "dexter", v)
}

func TestResolveStructuredPath_pathNotFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("name: alice\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "missing.key")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_fileNotFound(t *testing.T) {
	v, found, err := resolveStructuredPath("/nonexistent/dir/config.yaml", "name")
	require.NoError(t, err) // missing file is not an error — it's "not found"
	assert.False(t, found)
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_nonScalarAtPath_mapping(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("user:\n  name: alice\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "user")
	require.NoError(t, err)
	assert.False(t, found, "a mapping node at the path must be treated as not-found, not an error")
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_nonScalarAtPath_sequence(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("tags:\n  - a\n  - b\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "tags")
	require.NoError(t, err)
	assert.False(t, found, "a sequence node at the path must be treated as not-found, not an error")
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_malformedYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("key: [unclosed_bracket\n"), 0644))
	_, _, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing destination file")
}

func TestResolveStructuredPath_malformedJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"key": "unterminated`), 0644))
	_, _, err := resolveStructuredPath(filepath.Join(dir, "config.json"), "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing destination file")
}

func TestResolveStructuredPath_unsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte(`name = "alice"`+"\n"), 0644))
	_, _, err := resolveStructuredPath(filepath.Join(dir, "config.toml"), "name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file extension")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/integrate/ -run TestResolveStructuredPath -v 2>&1 | head -20
```

Expected: compile error — `undefined: resolveStructuredPath`.

- [ ] **Step 3: Commit the test file**

```bash
git add internal/integrate/structured_path_test.go
git commit -m "test(integrate): failing unit tests for resolveStructuredPath"
```

---

## Task 3: Implement `resolveStructuredPath`

**Files:**
- Create: `internal/integrate/structured_path.go`

- [ ] **Step 1: Create the implementation**

```go
package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveStructuredPath reads filePath (JSON or YAML, detected by extension),
// navigates to the dot-delimited dotPath within it, and returns the scalar
// value as a string. Returns ("", false, nil) when the file is absent, the
// path is not found, or the node at the path is not a scalar. Returns
// ("", false, err) when the file exists but cannot be parsed.
func resolveStructuredPath(filePath, dotPath string) (string, bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	var parseFn func([]byte) (*node, error)
	switch ext {
	case ".json":
		parseFn = parseJSON
	case ".yml", ".yaml":
		parseFn = parseYAML
	default:
		return "", false, fmt.Errorf("unsupported file extension %q for from_destination_structured (must be .json, .yml, or .yaml)", ext)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("error reading destination file %s: %v", filePath, err)
	}

	root, err := parseFn(data)
	if err != nil {
		return "", false, fmt.Errorf("error parsing destination file %s: %v", filePath, err)
	}

	current := root
	for _, seg := range strings.Split(dotPath, ".") {
		if current.kind != nodeMapping {
			return "", false, nil
		}
		child, ok := current.mapping.Get(seg)
		if !ok {
			return "", false, nil
		}
		current = child
	}

	if current.kind != nodeScalar {
		return "", false, nil
	}
	return fmt.Sprint(current.scalar), true, nil
}
```

- [ ] **Step 2: Run the unit tests**

```bash
go test ./internal/integrate/ -run TestResolveStructuredPath -v
```

Expected: all 11 `TestResolveStructuredPath_*` tests pass.

- [ ] **Step 3: Run the full unit suite to check for regressions**

```bash
make test-unit
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/integrate/structured_path.go
git commit -m "feat(integrate): add resolveStructuredPath helper for from_destination_structured inputs"
```

---

## Task 4: Write failing integration tests for the new input branch

**Files:**
- Modify: `internal/integrate/integrator_templated_test.go`

- [ ] **Step 1: Add the integration tests at the end of the file**

```go
// TestIntegratorTemplated_fromDestinationStructured_yamlHappyPath: when the
// destination YAML file already exists and the configured path resolves to a
// scalar, the structured read wins and prompt is never called.
func TestIntegratorTemplated_fromDestinationStructured_yamlHappyPath(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.yaml"),
		[]byte("service: {{ index .Inputs \"service_name\" }}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "config.yaml"),
		[]byte("service: my-service\n"), 0644))

	stub := stubRequestInput(t, "SHOULD-NOT-BE-CALLED")
	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml",
		Destination: "config.yaml",
		Inputs: []config.GitSporkConfigTemplatedInput{{
			Name:   "service_name",
			Prompt: "Service name?",
			FromDestinationStructured: &config.GitSporkConfigTemplatedInputDestinationStructured{
				Path: "service",
			},
		}},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))
	assert.Equal(t, 0, stub.calls, "prompt must not fire when structured read succeeds")
	got, err := os.ReadFile(filepath.Join(downstreamDir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "my-service", "rendered output must use the value read from the destination file")
}

// TestIntegratorTemplated_fromDestinationStructured_jsonHappyPath: same as
// YAML happy path but with a JSON destination file.
func TestIntegratorTemplated_fromDestinationStructured_jsonHappyPath(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.json"),
		[]byte(`{"service":"{{ index .Inputs "service_name" }}"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "config.json"),
		[]byte(`{"service":"json-service"}`), 0644))

	stub := stubRequestInput(t, "SHOULD-NOT-BE-CALLED")
	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.json",
		Destination: "config.json",
		Inputs: []config.GitSporkConfigTemplatedInput{{
			Name:   "service_name",
			Prompt: "Service name?",
			FromDestinationStructured: &config.GitSporkConfigTemplatedInputDestinationStructured{
				Path: "service",
			},
		}},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))
	assert.Equal(t, 0, stub.calls, "prompt must not fire when JSON structured read succeeds")
}

// TestIntegratorTemplated_fromDestinationStructured_forceRePromptSkipsRead:
// forceRePrompt=true must bypass the structured read and fire the prompt even
// when the destination file exists with the value at the path.
func TestIntegratorTemplated_fromDestinationStructured_forceRePromptSkipsRead(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.yaml"),
		[]byte("service: {{ index .Inputs \"service_name\" }}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "config.yaml"),
		[]byte("service: old-service\n"), 0644))

	stub := stubRequestInput(t, "new-service")
	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml",
		Destination: "config.yaml",
		Inputs: []config.GitSporkConfigTemplatedInput{{
			Name:   "service_name",
			Prompt: "Service name?",
			FromDestinationStructured: &config.GitSporkConfigTemplatedInputDestinationStructured{
				Path: "service",
			},
		}},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, true, sdktypes.NoopLogger()))
	assert.Equal(t, 1, stub.calls, "forceRePrompt must skip the structured read and fire the prompt exactly once")
	got, err := os.ReadFile(filepath.Join(downstreamDir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "new-service", "render must use the re-prompted value, not the old destination value")
}

// TestIntegratorTemplated_fromDestinationStructured_missingFilePromptFallback:
// when the destination file doesn't yet exist (first run), the structured read
// returns not-found and the prompt fires as the fallback.
func TestIntegratorTemplated_fromDestinationStructured_missingFilePromptFallback(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.yaml"),
		[]byte("service: {{ index .Inputs \"service_name\" }}\n"), 0644))
	// No destination file — simulates a first-run downstream.

	stub := stubRequestInput(t, "prompted-service")
	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml",
		Destination: "config.yaml",
		Inputs: []config.GitSporkConfigTemplatedInput{{
			Name:   "service_name",
			Prompt: "Service name?",
			FromDestinationStructured: &config.GitSporkConfigTemplatedInputDestinationStructured{
				Path: "service",
			},
		}},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))
	assert.Equal(t, 1, stub.calls, "prompt must fire when destination file is absent")
	got, err := os.ReadFile(filepath.Join(downstreamDir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "prompted-service")
}

// TestIntegratorTemplated_fromDestinationStructured_pathNotFoundPromptFallback:
// when the destination file exists but the configured path is absent from it,
// the structured read returns not-found and the prompt fires.
func TestIntegratorTemplated_fromDestinationStructured_pathNotFoundPromptFallback(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.yaml"),
		[]byte("service: {{ index .Inputs \"service_name\" }}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "config.yaml"),
		[]byte("other_key: unrelated\n"), 0644))

	stub := stubRequestInput(t, "prompted-service")
	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml",
		Destination: "config.yaml",
		Inputs: []config.GitSporkConfigTemplatedInput{{
			Name:   "service_name",
			Prompt: "Service name?",
			FromDestinationStructured: &config.GitSporkConfigTemplatedInputDestinationStructured{
				Path: "service",
			},
		}},
	}}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))
	assert.Equal(t, 1, stub.calls, "prompt must fire when path is absent from destination file")
}

// TestIntegratorTemplated_fromDestinationStructured_noPromptFallback_fileMissing:
// when no prompt fallback is configured and the destination file is absent,
// the error must name the missing path and mention the lack of a fallback.
func TestIntegratorTemplated_fromDestinationStructured_noPromptFallback_fileMissing(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.yaml"),
		[]byte("service: {{ index .Inputs \"service_name\" }}\n"), 0644))

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml",
		Destination: "config.yaml",
		Inputs: []config.GitSporkConfigTemplatedInput{{
			Name: "service_name",
			// No Prompt — no fallback configured.
			FromDestinationStructured: &config.GitSporkConfigTemplatedInputDestinationStructured{
				Path: "service",
			},
		}},
	}}
	err := (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service", "error must name the configured path")
	assert.Contains(t, err.Error(), "no prompt fallback", "error must tell the user why it can't proceed")
}

// TestIntegratorTemplated_fromDestinationStructured_noPromptFallback_pathMissing:
// same as above but the destination file exists — it's the path that's absent.
func TestIntegratorTemplated_fromDestinationStructured_noPromptFallback_pathMissing(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "template.yaml"),
		[]byte("service: {{ index .Inputs \"service_name\" }}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "config.yaml"),
		[]byte("other: value\n"), 0644))

	instructions := []config.GitSporkConfigTemplated{{
		Template:    "template.yaml",
		Destination: "config.yaml",
		Inputs: []config.GitSporkConfigTemplatedInput{{
			Name: "service_name",
			FromDestinationStructured: &config.GitSporkConfigTemplatedInputDestinationStructured{
				Path: "service",
			},
		}},
	}}
	err := (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service")
	assert.Contains(t, err.Error(), "no prompt fallback")
}

// TestIntegratorTemplated_fromDestinationStructured_valueAvailableToPreviousInput:
// a value resolved via from_destination_structured must land in capturedInputValues
// so that a subsequent template can reference it via previous_input.
func TestIntegratorTemplated_fromDestinationStructured_valueAvailableToPreviousInput(t *testing.T) {
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "a.yaml"),
		[]byte("service: {{ index .Inputs \"service_name\" }}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "b.yaml"),
		[]byte("also: {{ index .Inputs \"borrowed\" }}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(downstreamDir, "config.yaml"),
		[]byte("service: shared-service\n"), 0644))

	instructions := []config.GitSporkConfigTemplated{
		{
			Template:    "a.yaml",
			Destination: "config.yaml",
			Inputs: []config.GitSporkConfigTemplatedInput{{
				Name:   "service_name",
				Prompt: "Service name?",
				FromDestinationStructured: &config.GitSporkConfigTemplatedInputDestinationStructured{
					Path: "service",
				},
			}},
		},
		{
			Template:    "b.yaml",
			Destination: "b.yaml",
			Inputs: []config.GitSporkConfigTemplatedInput{{
				Name: "borrowed",
				PreviousInput: &config.GitSporkConfigTemplatedInputPrevious{
					Template: "a.yaml",
					Name:     "service_name",
				},
			}},
		},
	}
	require.NoError(t, (&IntegratorTemplated{}).Integrate(instructions, upstreamDir, downstreamDir, false, sdktypes.NoopLogger()))

	bOut, err := os.ReadFile(filepath.Join(downstreamDir, "b.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(bOut), "shared-service",
		"from_destination_structured value must be accessible to previous_input in a later template")
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

```bash
go test ./internal/integrate/ -run TestIntegratorTemplated_fromDestinationStructured -v 2>&1 | head -40
```

Expected: compile error or test failures — the `FromDestinationStructured` field exists in config but the input branch isn't implemented yet, so inputs with only that field set will hit the "requires at least one of" error path.

- [ ] **Step 3: Commit the tests**

```bash
git add internal/integrate/integrator_templated_test.go
git commit -m "test(integrate): failing integration tests for from_destination_structured input branch"
```

---

## Task 5: Implement the `from_destination_structured` input branch

**Files:**
- Modify: `internal/integrate/integrator_templated.go`

- [ ] **Step 1: Add the pre-check block to the input-gathering loop**

Before the existing `if input.JSONDataPath != ""` chain, insert a pre-check block. When `from_destination_structured` is defined and resolves, set the value and `continue` to the next input, skipping the chain. If it doesn't resolve (file/path absent, null, or `forceRePrompt=true`), fall through to the chain unchanged:

```go
// from_destination_structured pre-check: runs before the source chain.
// On hit, value wins and we skip to the next input. On miss, the chain below runs.
if input.FromDestinationStructured != nil && !forceRePrompt {
    fullDestPath := filepath.Join(downstreamPath, templatedInstruction.Destination)
    value, found, err := resolveStructuredPath(fullDestPath, input.FromDestinationStructured.Path)
    if err != nil {
        return fmt.Errorf("error resolving from_destination_structured path %q in %s: %v",
            input.FromDestinationStructured.Path, fullDestPath, err)
    }
    if found {
        templateData.Inputs[input.Name] = value
        capturedInputValues[templatedInstruction.Template][input.Name] = value
        continue
    }
}
if input.JSONDataPath != "" {
    jsonDataPath := filepath.Join(downstreamPath, input.JSONDataPath)
    jsonData, err := os.ReadFile(jsonDataPath)
    if err != nil {
        return fmt.Errorf("error reading json_data_path at %s: %v", jsonDataPath, err)
    }
    if err := json.Unmarshal(jsonData, &templateData.Inputs); err != nil {
        return fmt.Errorf("error parsing json_data_path file %s into inputs: %v", jsonDataPath, err)
    }
    maps.Copy(capturedInputValues[templatedInstruction.Template], templateData.Inputs)
} else if input.Prompt != "" {
    if templateData.Inputs[input.Name] == "" || forceRePrompt {
        requestInputOpts := &inputpkg.RequestInputOptions{
            Type:   inputpkg.SingleValue,
            Prompt: input.Prompt,
        }
        requestInputResult, err := requestInputFn(requestInputOpts)
        if err != nil {
            return fmt.Errorf("error setting up prompt input: %v", err)
        }
        templateData.Inputs[input.Name] = requestInputResult.StringValue
        capturedInputValues[templatedInstruction.Template][input.Name] = requestInputResult.StringValue
    }
} else if input.PreviousInput != nil {
    var previousInputErr error
    if _, ok := capturedInputValues[input.PreviousInput.Template]; ok {
        if value, ok := capturedInputValues[input.PreviousInput.Template][input.PreviousInput.Name]; ok {
            templateData.Inputs[input.Name] = value
            capturedInputValues[templatedInstruction.Template][input.Name] = value
        } else {
            previousInputErr = fmt.Errorf("previous input name %s not found in template %s", input.PreviousInput.Name, input.PreviousInput.Template)
        }
    } else {
        previousInputErr = fmt.Errorf("previous template not found: %s", input.PreviousInput.Template)
    }
    if previousInputErr != nil {
        return fmt.Errorf("error in previous_input configuration under template %s: %v", templatedInstruction.Template, previousInputErr)
    }
} else {
    return fmt.Errorf("templated definition %s requires at least one of 'prompt', 'json_data_path', or 'previous_input' to be defined", input.Name)
}
```

- [ ] **Step 2: Run the new integration tests**

```bash
go test ./internal/integrate/ -run TestIntegratorTemplated_fromDestinationStructured -v
```

Expected: all 6 `TestIntegratorTemplated_fromDestinationStructured_*` tests pass.

- [ ] **Step 3: Run the full unit suite**

```bash
make test-unit
```

Expected: all tests pass, no regressions.

- [ ] **Step 4: Commit**

```bash
git add internal/integrate/integrator_templated.go
git commit -m "feat(integrate): implement from_destination_structured templated input source"
```

---

## Task 6: Final verification

- [ ] **Step 1: Run the complete test suite**

```bash
make test-unit && make test-functional && make test-examples && make test-sdk
```

Expected: all suites pass.

- [ ] **Step 2: Verify schema output includes the new example**

```bash
go run ./cmd/gitspork schema 2>&1 | grep -A5 "from_destination_structured"
```

Expected: the new `input_four` entry is visible in the schema output with both `prompt` and `from_destination_structured` fields shown.
