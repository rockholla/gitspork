# Design: `from_destination_structured` Templated Input Source

**Date:** 2026-08-25  
**Status:** Approved

## Problem

Templated inputs currently have three sources: `prompt` (interactive), `json_data_path` (external data file), and `previous_input` (cross-template reference). None of these let you read a value that the downstream has already set inside a previously-rendered destination file. This matters when the destination is structured data (YAML/JSON) and the downstream maintainer has customised a field — on re-integration, the upstream has no non-prompt way to pick up that value and feed it back into the template.

## Solution

Add a fourth input source: `from_destination_structured`. It reads the current template's destination file (if it already exists in the downstream), navigates to a dot-delimited key path within it, and uses the resolved scalar as the input value. When the file or path is unavailable it falls through to `prompt`.

## Config changes

### Typo fix

The YAML tag on `GitSporkConfigTemplatedInput.FromDestinationStructured` has a typo in the user's initial sketch (`from_destionation_structured`). Fix to `from_destination_structured`.

### Struct definition (already sketched, corrected)

```go
// GitSporkConfigTemplatedInputDestinationStructured allows re-using an
// already-rendered destination template's value as an input. The destination
// file must be structured data (JSON or YAML).
type GitSporkConfigTemplatedInputDestinationStructured struct {
    Path string `yaml:"path" comment:"dot-delimited key path into the destination file, e.g. 'metadata.owner'; resolves to a scalar string value"`
}
```

`FromDestinationStructured` is **not** a one-of exclusive with `Prompt` — both can (and typically should) be set together, with `from_destination_structured` winning when the file/path resolves and `prompt` serving as the first-run fallback.

### Schema example

`GetGitSporkConfigSchema()` in `internal/config/config.go` gains a new `GitSporkConfigTemplatedInput` entry in the example `Templated` slice demonstrating `from_destination_structured` + `prompt` together.

### Validation

No new parse-time validation. Misconfiguration (no prompt fallback when file/path is unavailable) surfaces as a runtime error during integration, consistent with existing input error handling.

## New helper: `internal/integrate/structured_path.go`

```go
func resolveStructuredPath(filePath, dotPath string) (string, bool, error)
```

**Format detection:** by file extension — `.json` → JSON, `.yml`/`.yaml` → YAML. Any other extension returns an error.

**Parsing:** reuses the existing `parseJSON` / `parseYAML` functions (both return `*node`); no new parsing code.

**Traversal:** splits `dotPath` on `.` and walks the `node` tree via `orderedMap.Get` at each segment. Array indexing is out of scope — scalar keys only for now.

**Return contract:**

| Condition | Returns |
|---|---|
| Path resolves to a scalar | `(fmt.Sprint(scalar), true, nil)` |
| File not found | `("", false, nil)` |
| Path not found in file | `("", false, nil)` |
| Node at path is a mapping or sequence | `("", false, nil)` |
| File found but parse fails | `("", false, err)` |
| Unsupported file extension | `("", false, err)` |

Non-scalar at path is treated as "not found" (not an error) because silently falling through to prompt is more recoverable than crashing — the upstream author likely has a config path mistake and the downstream can still function.

## Input loop changes: `internal/integrate/integrator_templated.go`

A new `else if input.FromDestinationStructured != nil` branch is inserted **before** the final `else` error branch. Resolution order within the chain:

1. `json_data_path`
2. `prompt`
3. `previous_input`
4. **`from_destination_structured`** ← new
5. error: "requires at least one of …" (updated to name the new option)

**Logic:**

```
fullDestPath = filepath.Join(downstreamPath, templatedInstruction.Destination)

if !forceRePrompt:
    value, found, err = resolveStructuredPath(fullDestPath, input.FromDestinationStructured.Path)
    if err → return error
    if found:
        set templateData.Inputs[input.Name] = value
        set capturedInputValues[template][input.Name] = value
        continue

// not found, or forceRePrompt=true — fall through to prompt
if input.Prompt == "":
    return error: from_destination_structured path %q not resolved in %s and no prompt fallback configured
// else: run the existing prompt branch — check cache (templateData.Inputs[input.Name] == "" || forceRePrompt),
//       then call requestInputFn if needed
```

**`forceRePrompt` interaction:** when `forceRePrompt=true`, the structured read is skipped entirely and the prompt path runs, consistent with how `forceRePrompt` forces re-capture of all prompt-sourced inputs.

**Cache interaction:** values resolved via `from_destination_structured` flow into `capturedInputValues` and `nextCache` exactly like other sources, so they are available to `previous_input` references in later templates and survive in the cache for that run.

## Testing

### `internal/integrate/structured_path_test.go` (unit)

- YAML file, dot-delimited path resolves to scalar → correct value returned
- JSON file, same
- Nested path (`a.b.c`) resolves correctly
- Path not found → `("", false, nil)`
- File not found → `("", false, nil)`
- Malformed file (parse error) → error returned
- Non-scalar at path (mapping or sequence) → `("", false, nil)`
- Unsupported file extension → error returned

### `internal/integrate/integrator_templated_test.go` (integration)

- Happy path: destination YAML exists with path, value used, prompt not called
- Happy path: destination JSON, same
- `forceRePrompt=true`: structured read skipped, prompt fires
- Destination file missing: prompt fallback fires
- Path not found in file: prompt fallback fires
- No prompt fallback + file missing → clear error naming the path and destination
- No prompt fallback + path missing → clear error naming the path and destination
- Value flows into `capturedInputValues` and is accessible to a `previous_input` reference in a later template
