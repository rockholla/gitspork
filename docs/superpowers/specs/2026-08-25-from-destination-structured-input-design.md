# Design: `from_destination_structured` Templated Input Source

**Date:** 2026-08-25  
**Status:** Approved

## Problem

Templated inputs currently have three sources: `prompt` (interactive), `json_data_path` (external data file), and `previous_input` (cross-template reference). None of these let you read a value that the downstream has already set inside a previously-rendered destination file. This matters when the destination is structured data (YAML/JSON) and the downstream maintainer has customised a field — on re-integration, the upstream has no non-prompt way to pick up that value and feed it back into the template.

## Solution

Add a fourth input source: `from_destination_structured`. It reads the current template's destination file (if it already exists in the downstream), navigates to a dot-delimited key path within it, and uses the resolved scalar as the input value. When the file or path is unavailable (or `forceRePrompt=true`), it falls through to the remaining configured sources (`json_data_path`, `prompt`, `previous_input`) in the normal order.

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

`FromDestinationStructured` is **not** a one-of exclusive with other sources — it can (and typically should) be set alongside `prompt`, `json_data_path`, or `previous_input`. `from_destination_structured` runs as a pre-check; when it resolves, the value wins immediately; when it doesn't, the remaining sources run in their normal order.

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

`from_destination_structured` is implemented as a **pre-check** at the top of the input loop — before the `if/else if` source chain — not as a branch within it. Resolution order:

1. **`from_destination_structured` pre-check** ← new (if defined and resolves → `continue`, skipping everything below)
2. `json_data_path`
3. `prompt`
4. `previous_input`
5. error: "requires at least one of 'prompt', 'json_data_path', or 'previous_input' to be defined"

**Logic:**

```
if input.FromDestinationStructured != nil && !forceRePrompt:
    fullDestPath = filepath.Join(downstreamPath, templatedInstruction.Destination)
    value, found, err = resolveStructuredPath(fullDestPath, input.FromDestinationStructured.Path)
    if err → return error
    if found:
        set templateData.Inputs[input.Name] = value
        set capturedInputValues[template][input.Name] = value
        continue   // skip the source chain entirely

// not resolved (file/path absent, null value) or forceRePrompt=true:
// fall through to json_data_path / prompt / previous_input as normal
```

**`forceRePrompt` interaction:** when `forceRePrompt=true`, the pre-check is skipped entirely and the full source chain runs — consistent with how `forceRePrompt` forces re-capture of all inputs.

**Unresolved with no other source:** if `from_destination_structured` is the only configured source and it doesn't resolve, the input hits the generic `else` error ("requires at least one of …"). The error is technically correct — no valid source was configured for this run.

**Cache interaction:** values resolved via `from_destination_structured` flow into `capturedInputValues` and `nextCache` exactly like other sources, so they are available to `previous_input` references in later templates and survive in the cache for that run.

## Testing

### `internal/integrate/structured_path_test.go` (unit, 13 tests)

- YAML file, dot-delimited path resolves to scalar → correct value returned
- `.yml` extension handled same as `.yaml`
- JSON file, same
- Nested path (`a.b.c`) resolves correctly
- Path not found → `("", false, nil)`
- File not found → `("", false, nil)`
- Malformed file (parse error) → error returned
- Non-scalar at path (mapping or sequence) → `("", false, nil)`
- Unsupported file extension → error returned
- Explicit `null` value in YAML → `("", false, nil)` (not `"<nil>"`)
- Explicit `null` value in JSON → same

### `internal/integrate/integrator_templated_test.go` (integration, 6 tests)

- Happy path: destination YAML exists with path, value used, prompt not called
- Happy path: destination JSON, same
- `forceRePrompt=true`: structured read skipped, prompt fires
- Destination file missing: prompt fallback fires
- Path not found in file: prompt fallback fires
- Value flows into `capturedInputValues` and is accessible to a `previous_input` reference in a later template
