# upstream_owned / downstream_owned Rename Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an `upstream_owned` *or* `downstream_owned` entry be either a plain glob (unchanged) or a `{from, to}` map that renames a file as it syncs from upstream to downstream.

**Architecture:** Introduce one shared, ownership-neutral `OwnedEntry` type (used by both flat ownership lists) with custom goccy YAML (un)marshalers so each YAML list stays a heterogeneous scalar-or-map list. Both integrators, delta propagation, and `mv`/`rm` consume entries via helper methods (`SourcePattern`, `IsRename`, `ResolveDest`). The reflection-based schema renderer is corrected with a post-processing pass that collapses plain entries back to scalars in both blocks.

**Tech Stack:** Go 1.26, `github.com/goccy/go-yaml` (custom `BytesMarshaler`/`BytesUnmarshaler`), `github.com/gobwas/glob`, `github.com/rockholla/go-lib/marshal`, cobra, testify.

---

## Background (read before starting)

The design spec is `docs/superpowers/specs/2026-06-03-upstream-owned-rename-syntax-design.md`. A de-risking spike already proved the marshaling approach; its tests live in `internal/upstream_owned_marshal_test.go` using **temporary fixture types** (`marshalTestEntry`/`marshalTestConfig`) and a **test-local** `collapsePlainUpstreamOwned`. This plan replaces those fixtures with the real `OwnedEntry` type, renames the test file to `internal/owned_entry_test.go`, and promotes the collapse helper to production code as `collapsePlainOwnedEntries` (now covering both the `upstream_owned:` and `downstream_owned:` blocks).

Key facts about the existing code:
- `internal/gitspork.go:26-27` — `UpstreamOwned []string` and `DownstreamOwned []string`. Custom marshalers will only be honored by goccy, not by `marshal.YAMLWithComments` (reflection-based, see `GetGitSporkConfigSchema` at `internal/gitspork.go:155`, render call at `:202`).
- `globNonWildcardPrefix(pattern string) string` already exists in `internal/upstream-mv-rm.go:11` and returns the portion of a glob before the first `*`/`?`/`[` (e.g. `configs/**` → `configs`, `a.txt` → `a.txt`).
- `downstream_owned` files are owned by the downstream: `IntegratorDownstreamOwned.Integrate` (`internal/integrator_downstream-owned.go`) seeds a file from upstream **only if the downstream destination does not already exist**. `downstream_owned` is deliberately **excluded** from delta propagation (`buildManagedGlobs` in `internal/upstream-delta.go:100` does not reference it), so rename support there needs **no** delta changes. The existing delta test "downstream_owned file deleted does not appear in delta" (`internal/upstream-delta_test.go:62`) asserts this and must keep passing.
- Consumers that must change when the field types flip: `internal/integrate.go:178` (upstream) and `:184` (downstream), `internal/integrator_upstream-owned.go`, `internal/integrator_downstream-owned.go`, `internal/upstream-delta.go:102`, `internal/upstream-mv-rm.go:57-58,128-129`, the schema example at `internal/gitspork.go:157-158`, and unit-test fixtures in `internal/upstream-delta_test.go` and `internal/upstream-mv-rm_test.go`.

Run unit tests with `make test-unit` (`go vet ./... && go test ./...`). Functional tests need a build tag: `make test-functional`.

---

## File Structure

- **Create** `internal/owned-entry.go` — the `OwnedEntry` type, its marshalers, `SourcePattern`/`IsRename`/`ResolveDest`, and `collapsePlainOwnedEntries`. One file, one responsibility: the entry abstraction shared by both ownership lists.
- **Rename + rewrite** `internal/upstream_owned_marshal_test.go` → `internal/owned_entry_test.go` — drop fixture types, point tests at the real `OwnedEntry` and production `collapsePlainOwnedEntries`.
- **Modify** `internal/gitspork.go` — flip both field types; update field comments; update schema example (both blocks, each with a rename); wire `collapsePlainOwnedEntries` into `GetGitSporkConfigSchema`.
- **Modify** `internal/integrator_upstream-owned.go` — per-entry integration using `ResolveDest`.
- **Modify** `internal/integrator_downstream-owned.go` — per-entry seeding using `ResolveDest`, preserving one-time-seed semantics (skip when the destination already exists).
- **Modify** `internal/upstream-mv-rm.go` — entry-aware rewrite/filter matching the source side, applied to both `UpstreamOwned` and `DownstreamOwned`.
- **Modify** `internal/upstream-delta.go` — managed *matchers* that map upstream source paths to downstream destinations (`upstream_owned` + `shared_ownership.*` only).
- **Modify** `internal/upstream-delta_test.go`, `internal/upstream-mv-rm_test.go` — fixture migration + new rename cases.
- **Create** `test/functional/rename_test.go` — end-to-end integrate + delta scenarios for renames (upstream and downstream).
- **Modify** `docs/README.md` — document the rename form for both lists.

---

## Task 1: `OwnedEntry` type, helpers, and schema-collapse helper

**Files:**
- Create: `internal/owned-entry.go`
- Rename + rewrite: `internal/upstream_owned_marshal_test.go` → `internal/owned_entry_test.go`

- [ ] **Step 1: Create the entry type and helpers**

Create `internal/owned-entry.go`:

```go
package internal

import (
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// OwnedEntry is a single entry in an ownership list (upstream_owned or
// downstream_owned). It is either a plain glob pattern (Pattern, from a YAML
// scalar) or a rename (From/To, from a {from, to} YAML map). The forms are
// mutually exclusive. The type is ownership-neutral: it describes a path/rename,
// not a policy — the difference between the two lists lives in their integrators.
//
// The yaml/comment struct tags are consumed ONLY by the reflection-based
// marshal.YAMLWithComments schema renderer. goccy uses the custom
// UnmarshalYAML/MarshalYAML below, which ignore tags.
type OwnedEntry struct {
	Pattern string `yaml:"pattern,omitempty" comment:"a single glob file pattern"`
	From    string `yaml:"from,omitempty" comment:"(rename) upstream source glob/path"`
	To      string `yaml:"to,omitempty" comment:"(rename) downstream destination glob/path"`
}

// IsRename reports whether the entry renames a file (From/To form).
func (e OwnedEntry) IsRename() bool { return e.From != "" }

// SourcePattern returns the glob matched against the upstream tree.
func (e OwnedEntry) SourcePattern() string {
	if e.IsRename() {
		return e.From
	}
	return e.Pattern
}

// ResolveDest returns the downstream destination path for an upstream file that
// matched this entry's SourcePattern. Plain entries map to the same path; rename
// entries swap the source pattern's non-wildcard prefix for the destination's,
// preserving the remainder (prefix substitution).
func (e OwnedEntry) ResolveDest(matchedFile string) string {
	if !e.IsRename() {
		return matchedFile
	}
	srcPrefix := globNonWildcardPrefix(e.From)
	dstPrefix := globNonWildcardPrefix(e.To)
	return dstPrefix + strings.TrimPrefix(matchedFile, srcPrefix)
}

// UnmarshalYAML accepts either a scalar (plain pattern) or a {from, to} map.
func (e *OwnedEntry) UnmarshalYAML(b []byte) error {
	var s string
	if err := yaml.Unmarshal(b, &s); err == nil {
		e.Pattern = s
		return nil
	}
	var m struct {
		From string `yaml:"from"`
		To   string `yaml:"to"`
	}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return err
	}
	e.From, e.To = m.From, m.To
	return nil
}

// MarshalYAML emits a scalar for plain entries and a {from, to} map for renames.
func (e OwnedEntry) MarshalYAML() ([]byte, error) {
	if e.IsRename() {
		return yaml.Marshal(yaml.MapSlice{
			{Key: "from", Value: e.From},
			{Key: "to", Value: e.To},
		})
	}
	return yaml.Marshal(e.Pattern)
}

// collapsePlainOwnedEntries rewrites reflection-rendered `- pattern: "X"` lines
// within the upstream_owned: and downstream_owned: blocks of schema output back
// to bare scalars `- "X"`, leaving {from, to} rename entries and other sections
// untouched. Needed because marshal.YAMLWithComments is reflection-based and
// ignores OwnedEntry.MarshalYAML. The block-start check below lists both keys and
// runs before the block-end check, so each owned block is handled independently.
var ownedEntryPatternLineRE = regexp.MustCompile(`^(\s*)- pattern: (".*?"|\S+)(\s*#.*)?$`)

func collapsePlainOwnedEntries(schema string) string {
	lines := strings.Split(schema, "\n")
	inBlock := false
	for i, line := range lines {
		if strings.HasPrefix(line, "upstream_owned:") || strings.HasPrefix(line, "downstream_owned:") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		// A list item or its continuation stays in the block; a non-indented,
		// non-list, non-blank line is a new top-level key and ends the block.
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "" {
			inBlock = false
			continue
		}
		if m := ownedEntryPatternLineRE.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + "- " + m[2]
		}
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 2: Remove the spike test file and create the real-type test file**

Remove the old spike file and create the renamed one:

```bash
git rm internal/upstream_owned_marshal_test.go
```

Create `internal/owned_entry_test.go`:

```go
package internal

// Verifies the scalar-or-map marshaling of OwnedEntry across goccy (the config
// read/write path) and the schema post-processing that corrects the
// reflection-based marshal.YAMLWithComments output.

import (
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/rockholla/go-lib/marshal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// test-local container so we can exercise list unmarshaling with the real type.
type ownedEntryYAML struct {
	UpstreamOwned   []OwnedEntry `yaml:"upstream_owned" comment:"plain pattern or {from,to} rename"`
	DownstreamOwned []OwnedEntry `yaml:"downstream_owned" comment:"plain pattern or {from,to} rename"`
}

func TestOwnedEntry_UnmarshalUnion(t *testing.T) {
	src := `upstream_owned:
  - src/**
  - from: .markdownlint-downstream.jsonc
    to: .markdownlint.jsonc
  - configs/**
`
	var cfg ownedEntryYAML
	require.NoError(t, yaml.Unmarshal([]byte(src), &cfg))
	require.Len(t, cfg.UpstreamOwned, 3)
	assert.Equal(t, "src/**", cfg.UpstreamOwned[0].Pattern)
	assert.False(t, cfg.UpstreamOwned[0].IsRename())
	assert.Equal(t, ".markdownlint-downstream.jsonc", cfg.UpstreamOwned[1].From)
	assert.Equal(t, ".markdownlint.jsonc", cfg.UpstreamOwned[1].To)
	assert.True(t, cfg.UpstreamOwned[1].IsRename())
	assert.Equal(t, "configs/**", cfg.UpstreamOwned[2].Pattern)
}

func TestOwnedEntry_MarshalRoundTrip(t *testing.T) {
	cfg := ownedEntryYAML{UpstreamOwned: []OwnedEntry{
		{Pattern: "src/**"},
		{From: "a.txt", To: "b.txt"},
	}}
	out, err := yaml.Marshal(&cfg)
	require.NoError(t, err)
	assert.Contains(t, string(out), "- src/**")
	assert.Contains(t, string(out), "from: a.txt")
	assert.Contains(t, string(out), "to: b.txt")

	var back ownedEntryYAML
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, "src/**", back.UpstreamOwned[0].Pattern)
	assert.Equal(t, "a.txt", back.UpstreamOwned[1].From)
	assert.Equal(t, "b.txt", back.UpstreamOwned[1].To)
}

func TestOwnedEntry_SourcePattern(t *testing.T) {
	assert.Equal(t, "src/**", OwnedEntry{Pattern: "src/**"}.SourcePattern())
	assert.Equal(t, "a.txt", OwnedEntry{From: "a.txt", To: "b.txt"}.SourcePattern())
}

func TestOwnedEntry_ResolveDest(t *testing.T) {
	// plain entry: identity
	assert.Equal(t, "x/y.txt", OwnedEntry{Pattern: "x/**"}.ResolveDest("x/y.txt"))
	// exact rename
	assert.Equal(t, "b.txt", OwnedEntry{From: "a.txt", To: "b.txt"}.ResolveDest("a.txt"))
	// glob rename: prefix substitution
	e := OwnedEntry{From: "configs/**", To: ".configs/**"}
	assert.Equal(t, ".configs/app/db.yml", e.ResolveDest("configs/app/db.yml"))
	assert.Equal(t, ".configs/x/y/z.txt", e.ResolveDest("configs/x/y/z.txt"))
}

func TestCollapsePlainOwnedEntries_bothBlocks(t *testing.T) {
	cfg := &ownedEntryYAML{
		UpstreamOwned: []OwnedEntry{
			{Pattern: "upstream-owned.txt"},
			{From: ".markdownlint-downstream.jsonc", To: ".markdownlint.jsonc"},
		},
		DownstreamOwned: []OwnedEntry{
			{Pattern: "downstream-owned.md"},
			{From: "seed-from.md", To: "seed-to.md"},
		},
	}
	raw, err := marshal.YAMLWithComments(cfg, 0)
	require.NoError(t, err)
	out := collapsePlainOwnedEntries(raw)
	// plain entries in both blocks collapse to scalars
	assert.Contains(t, out, `- "upstream-owned.txt"`)
	assert.Contains(t, out, `- "downstream-owned.md"`)
	assert.NotContains(t, out, "- pattern:")
	// rename entries in both blocks are preserved
	assert.Contains(t, out, `from: ".markdownlint-downstream.jsonc"`)
	assert.Contains(t, out, `to: ".markdownlint.jsonc"`)
	assert.Contains(t, out, `from: "seed-from.md"`)
	assert.Contains(t, out, `to: "seed-to.md"`)
}

func TestGitSporkConfig_RenameRoundTripPreservesComments(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.gitspork.yml"
	src := `upstream_owned:
# keep me
- src/**
- from: a.txt
  to: b.txt
`
	require.NoError(t, os.WriteFile(path, []byte(src), 0644))
	cfg, err := ParseGitSporkConfig(path)
	require.NoError(t, err)
	require.Len(t, cfg.UpstreamOwned, 2)
	assert.Equal(t, "src/**", cfg.UpstreamOwned[0].Pattern)
	assert.Equal(t, "a.txt", cfg.UpstreamOwned[1].From)

	require.NoError(t, WriteGitSporkConfig(path, cfg))
	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(out), "keep me")
	assert.Contains(t, string(out), "- src/**")
	assert.Contains(t, string(out), "from: a.txt")
}
```

Note: `TestGitSporkConfig_RenameRoundTripPreservesComments` depends on `GitSporkConfig.UpstreamOwned` already being `[]OwnedEntry`. It will not compile until Task 2 Step 1 flips the field. **In this task, comment it out** (or expect a build failure on it) and verify the other tests pass via the package build in Step 3; un-comment it at Task 2 Step 10. To keep Task 1 green and committable, **omit `TestGitSporkConfig_RenameRoundTripPreservesComments` from the file in this task** and add it in Task 2 Step 9 instead. Also omit the `"os"` import in this task (add it in Task 2 Step 9).

- [ ] **Step 3: Run the tests, expect PASS**

Run: `go test ./internal -run 'TestOwnedEntry|TestCollapsePlainOwnedEntries' -v`
Expected: all PASS. (The config fields are still `[]string`; `OwnedEntry` stands alone, so the package still builds.)

- [ ] **Step 4: Verify the package builds and vets**

Run: `go vet ./internal/`
Expected: exit 0, no output.

- [ ] **Step 5: Commit**

```bash
git add internal/owned-entry.go internal/owned_entry_test.go
git commit -m "feat: add OwnedEntry type with scalar-or-map YAML support"
```

---

## Task 2: Flip both ownership lists to `[]OwnedEntry`; integrate, mv/rm, schema

This task changes `GitSporkConfig.UpstreamOwned` and `.DownstreamOwned` to `[]OwnedEntry`, which breaks compilation across the package until every consumer is updated. Do all steps before running the suite.

**Files:**
- Modify: `internal/gitspork.go` (field types @ lines 26-27, comments, schema example @ lines 157-158, `GetGitSporkConfigSchema` render @ ~line 202)
- Modify: `internal/integrator_upstream-owned.go`
- Modify: `internal/integrator_downstream-owned.go`
- Modify: `internal/upstream-mv-rm.go` (`ComputeUpstreamMvFromConfig`, `ComputeUpstreamRmFromConfig`)
- Modify: `internal/upstream-delta.go` (`buildManagedGlobs` @ line 100)
- Modify: `internal/upstream-mv-rm_test.go`, `internal/upstream-delta_test.go` (fixtures)
- Test: add cases to `internal/upstream-mv-rm_test.go`; add the config round-trip test to `internal/owned_entry_test.go`

- [ ] **Step 1: Flip both field types and update their comments**

In `internal/gitspork.go`, change lines 26-27 from:

```go
	UpstreamOwned   []string                      `yaml:"upstream_owned" comment:"file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the upstream gitspork repo"`
	DownstreamOwned []string                      `yaml:"downstream_owned" comment:"file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the downstream repo once it's been initially integrated"`
```

to:

```go
	UpstreamOwned   []OwnedEntry                  `yaml:"upstream_owned" comment:"file patterns (https://github.com/gobwas/glob) fully owned by the upstream; an entry may instead be a {from, to} map to rename a file as it syncs to the downstream"`
	DownstreamOwned []OwnedEntry                  `yaml:"downstream_owned" comment:"file patterns (https://github.com/gobwas/glob) fully owned by the downstream once initially integrated; an entry may instead be a {from, to} map to seed a file at a different downstream path"`
```

- [ ] **Step 2: Update the schema example to include renames in both blocks**

In `internal/gitspork.go` `GetGitSporkConfigSchema`, change the `gitSporkExampleConfig` fields (lines 157-158) from:

```go
		UpstreamOwned:   []string{"upstream-owned.txt"},
		DownstreamOwned: []string{"downstream-owned.md"},
```

to:

```go
		UpstreamOwned: []OwnedEntry{
			{Pattern: "upstream-owned.txt"},
			{From: "upstream-owned-renamed-from.txt", To: "downstream-renamed-to.txt"},
		},
		DownstreamOwned: []OwnedEntry{
			{Pattern: "downstream-owned.md"},
			{From: "downstream-owned-seed-from.md", To: "downstream-owned-seed-to.md"},
		},
```

- [ ] **Step 3: Wire the collapse post-processing into the schema renderer**

In `internal/gitspork.go` `GetGitSporkConfigSchema`, change:

```go
	renderedMain, err := marshal.YAMLWithComments(gitSporkExampleConfig, 0)
	if err != nil {
		return "", "", err
	}
```

to:

```go
	renderedMain, err := marshal.YAMLWithComments(gitSporkExampleConfig, 0)
	if err != nil {
		return "", "", err
	}
	renderedMain = collapsePlainOwnedEntries(renderedMain)
```

- [ ] **Step 4: Make the upstream-owned integrator per-entry and rename-aware**

Replace the body of `internal/integrator_upstream-owned.go` with:

```go
package internal

import (
	"fmt"
	"path/filepath"
)

// IntegratorUpstreamOwned will process a list of files to be managed as owned by the upstream gitspork repo
type IntegratorUpstreamOwned struct{}

// Integrate copies each upstream-owned file to the downstream, applying rename
// entries' destination resolution.
func (i *IntegratorUpstreamOwned) Integrate(entries []OwnedEntry, upstreamPath string, downstreamPath string, logger *Logger) error {
	for _, entry := range entries {
		integrateFiles, err := getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
		if err != nil {
			return fmt.Errorf("error determining the list of files to integrate in %s from %q: %v", upstreamPath, entry.SourcePattern(), err)
		}
		for _, integrateFile := range integrateFiles {
			dest := entry.ResolveDest(integrateFile)
			if dest == integrateFile {
				logger.Log("➡️ copying/overwriting %s to downstream", integrateFile)
			} else {
				logger.Log("➡️ copying/overwriting %s to downstream as %s", integrateFile, dest)
			}
			if err := syncFile(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, dest)); err != nil {
				return err
			}
		}
	}
	return nil
}
```

(The call site `internal/integrate.go:178` already passes `gitSporkConfig.UpstreamOwned`; no change needed there once the signature accepts `[]OwnedEntry`.)

- [ ] **Step 5: Make the downstream-owned integrator per-entry and rename-aware (preserve one-time-seed)**

Replace the body of `internal/integrator_downstream-owned.go` with:

```go
package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

// IntegratorDownstreamOwned will process a list of files to be managed as owned by the downstream gitspork repo, just initially bootstrapped by the upstream
type IntegratorDownstreamOwned struct{}

// Integrate seeds each downstream-owned file from the upstream a single time,
// applying rename entries' destination resolution. A file is only copied when
// its downstream destination does not already exist — the downstream owns it
// thereafter.
func (i *IntegratorDownstreamOwned) Integrate(entries []OwnedEntry, upstreamPath string, downstreamPath string, logger *Logger) error {
	for _, entry := range entries {
		integrateFiles, err := getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
		if err != nil {
			return fmt.Errorf("error determining the list of files to integrate in %s from %q: %v", upstreamPath, entry.SourcePattern(), err)
		}
		for _, integrateFile := range integrateFiles {
			dest := entry.ResolveDest(integrateFile)
			destination := filepath.Join(downstreamPath, dest)
			if _, err := os.Stat(destination); os.IsNotExist(err) {
				if dest == integrateFile {
					logger.Log("➡️ copying %s one time to downstream", integrateFile)
				} else {
					logger.Log("➡️ copying %s one time to downstream as %s", integrateFile, dest)
				}
				if err := syncFile(filepath.Join(upstreamPath, integrateFile), destination); err != nil {
					return err
				}
			} else {
				logger.Log("🔒 downstream-owned file %s exists, not doing anything", dest)
			}
		}
	}
	return nil
}
```

(The call site `internal/integrate.go:184` already passes `gitSporkConfig.DownstreamOwned`; no change needed there once the signature accepts `[]OwnedEntry`.)

- [ ] **Step 6: Fix the delta managed-glob builder to compile (behavior-preserving)**

In `internal/upstream-delta.go` `buildManagedGlobs` (line ~100), change:

```go
	var patterns []string
	patterns = append(patterns, config.UpstreamOwned...)
	patterns = append(patterns, config.SharedOwnership.Merged...)
```

to:

```go
	var patterns []string
	for _, e := range config.UpstreamOwned {
		patterns = append(patterns, e.SourcePattern())
	}
	patterns = append(patterns, config.SharedOwnership.Merged...)
```

(Full destination-space delta handling is Task 3; this keeps the build green and current behavior for plain entries. `DownstreamOwned` is intentionally not referenced here.)

- [ ] **Step 7: Make mv/rm operate on the entry source side, for both lists**

In `internal/upstream-mv-rm.go`, inside `ComputeUpstreamMvFromConfig`, add an entry-aware closure alongside the existing `rewritePatterns` closure (place it just after the `rewritePatterns := func(...) {...}` block, so it can capture `oldPath`, `newPath`, and `warnings`):

```go
	rewriteOwned := func(entries []OwnedEntry) []OwnedEntry {
		result := make([]OwnedEntry, len(entries))
		for i, e := range entries {
			src := e.SourcePattern()
			prefix := globNonWildcardPrefix(src)
			var newSrc string
			switch {
			case prefix == "":
				warnings = append(warnings, fmt.Sprintf("pattern %q has a leading wildcard — update manually", src))
				newSrc = src
			case src == oldPath:
				newSrc = newPath
			case prefix == oldPath || strings.HasPrefix(prefix, oldPath+"/"):
				newSrc = newPath + src[len(oldPath):]
			default:
				newSrc = src
			}
			if e.IsRename() {
				result[i] = OwnedEntry{From: newSrc, To: e.To}
			} else {
				result[i] = OwnedEntry{Pattern: newSrc}
			}
		}
		return result
	}
```

Then change the two owned-list assignments (lines 57-58) from:

```go
	config.UpstreamOwned = rewritePatterns(config.UpstreamOwned)
	config.DownstreamOwned = rewritePatterns(config.DownstreamOwned)
```

to:

```go
	config.UpstreamOwned = rewriteOwned(config.UpstreamOwned)
	config.DownstreamOwned = rewriteOwned(config.DownstreamOwned)
```

(Leave the `rewritePatterns` closure and the three `SharedOwnership.*` assignments unchanged — those lists are still `[]string`.)

In `ComputeUpstreamRmFromConfig`, add an entry-aware closure alongside `filterPatterns` (capturing `path`, `recursive`, `warnings`):

```go
	filterOwned := func(entries []OwnedEntry) []OwnedEntry {
		var result []OwnedEntry
		for _, e := range entries {
			src := e.SourcePattern()
			if src == path {
				continue
			}
			if recursive {
				prefix := globNonWildcardPrefix(src)
				if prefix == "" {
					warnings = append(warnings, fmt.Sprintf("pattern %q has a leading wildcard — update manually", src))
					result = append(result, e)
					continue
				}
				if prefix == path || strings.HasPrefix(prefix, path+"/") {
					continue
				}
			}
			result = append(result, e)
		}
		return result
	}
```

Then change the two owned-list assignments (lines 128-129) from:

```go
	config.UpstreamOwned = filterPatterns(config.UpstreamOwned)
	config.DownstreamOwned = filterPatterns(config.DownstreamOwned)
```

to:

```go
	config.UpstreamOwned = filterOwned(config.UpstreamOwned)
	config.DownstreamOwned = filterOwned(config.DownstreamOwned)
```

(Leave the `filterPatterns` closure and the three `SharedOwnership.*` assignments unchanged.)

- [ ] **Step 8: Migrate existing unit-test fixtures to the new type**

In `internal/upstream-delta_test.go`, convert the four ownership fixtures:
- Line 34: `UpstreamOwned: []string{"docs/**"}` → `UpstreamOwned: []OwnedEntry{{Pattern: "docs/**"}}`
- Line 68: `DownstreamOwned: []string{"docs/**"}` → `DownstreamOwned: []OwnedEntry{{Pattern: "docs/**"}}`
- Line 82: `UpstreamOwned: []string{"docs/**"}` → `UpstreamOwned: []OwnedEntry{{Pattern: "docs/**"}}`
- Line 97: `UpstreamOwned: []string{"docs/**"}` → `UpstreamOwned: []OwnedEntry{{Pattern: "docs/**"}}`

In `internal/upstream-mv-rm_test.go`, convert each `UpstreamOwned`/`DownstreamOwned` literal and assertion:
- Inputs `UpstreamOwned: []string{"X"}` → `UpstreamOwned: []OwnedEntry{{Pattern: "X"}}` (and the same for `DownstreamOwned`); multi-element lists like `[]string{"a", "b"}` → `[]OwnedEntry{{Pattern: "a"}, {Pattern: "b"}}`.
- Assertions `assert.Equal(t, []string{"X"}, result.UpstreamOwned)` → `assert.Equal(t, []OwnedEntry{{Pattern: "X"}}, result.UpstreamOwned)` (and the same for `DownstreamOwned`).

Apply this to the following lines (input lines and their paired assertion lines): 23, 29 (upstream exact); 34, 40 (glob prefix); 45, 51 (leading-wildcard warning); 109, 115 (sub-prefix); 120, 126 (downstream_owned); 206, 212; 217, 223; 228, 234; 239, 245. Line 193 (`cfg.UpstreamOwned = append(cfg.UpstreamOwned, "docs/new.md")`) becomes `cfg.UpstreamOwned = append(cfg.UpstreamOwned, OwnedEntry{Pattern: "docs/new.md"})`.

- [ ] **Step 9: Add the config round-trip test and rename-aware mv/rm test cases**

Add the `"os"` import to `internal/owned_entry_test.go` and append the round-trip test that was deferred from Task 1:

```go
func TestGitSporkConfig_RenameRoundTripPreservesComments(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.gitspork.yml"
	src := `upstream_owned:
# keep me
- src/**
- from: a.txt
  to: b.txt
`
	require.NoError(t, os.WriteFile(path, []byte(src), 0644))
	cfg, err := ParseGitSporkConfig(path)
	require.NoError(t, err)
	require.Len(t, cfg.UpstreamOwned, 2)
	assert.Equal(t, "src/**", cfg.UpstreamOwned[0].Pattern)
	assert.Equal(t, "a.txt", cfg.UpstreamOwned[1].From)

	require.NoError(t, WriteGitSporkConfig(path, cfg))
	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(out), "keep me")
	assert.Contains(t, string(out), "- src/**")
	assert.Contains(t, string(out), "from: a.txt")
}
```

Append to the `Test_UpstreamMv` function in `internal/upstream-mv-rm_test.go` (an upstream rename case and a downstream rename case):

```go
	t.Run("upstream rename entry: mv rewrites source side, leaves destination", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{From: "source.txt", To: "dest.txt"}},
		})
		warnings, err := UpstreamMv(cfg, "source.txt", "new-source.txt")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{From: "new-source.txt", To: "dest.txt"}}, result.UpstreamOwned)
	})

	t.Run("downstream rename entry: mv rewrites source side, leaves destination", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			DownstreamOwned: []OwnedEntry{{From: "seed-from.md", To: "seed-to.md"}},
		})
		warnings, err := UpstreamMv(cfg, "seed-from.md", "renamed-seed.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{From: "renamed-seed.md", To: "seed-to.md"}}, result.DownstreamOwned)
	})
```

Append to the `Test_UpstreamRm` function (rm subtests, at `internal/upstream-mv-rm_test.go:203`):

```go
	t.Run("upstream rename entry: rm matches source side and removes entry", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{
				{From: "source.txt", To: "dest.txt"},
				{Pattern: "keep.txt"},
			},
		})
		warnings, err := UpstreamRm(cfg, "source.txt", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "keep.txt"}}, result.UpstreamOwned)
	})

	t.Run("downstream rename entry: rm matches source side and removes entry", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			DownstreamOwned: []OwnedEntry{
				{From: "seed-from.md", To: "seed-to.md"},
				{Pattern: "keep.md"},
			},
		})
		warnings, err := UpstreamRm(cfg, "seed-from.md", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "keep.md"}}, result.DownstreamOwned)
	})
```

- [ ] **Step 10: Build, vet, and run the full unit suite**

Run: `make test-unit`
Expected: PASS, no vet errors.

- [ ] **Step 11: Sanity-check the schema output by eye**

Run: `go run . schema | head -20`
Expected: both `upstream_owned:` and `downstream_owned:` blocks show their plain entry as a bare scalar (`- "upstream-owned.txt"` / `- "downstream-owned.md"`) followed by a `- from: ... / to: ...` rename entry — no `- pattern:` line anywhere.

- [ ] **Step 12: Commit**

```bash
git add internal/gitspork.go internal/integrator_upstream-owned.go internal/integrator_downstream-owned.go internal/upstream-mv-rm.go internal/upstream-delta.go internal/upstream-mv-rm_test.go internal/upstream-delta_test.go internal/owned_entry_test.go
git commit -m "feat: rename-aware upstream_owned & downstream_owned integration, mv/rm, schema"
```

---

## Task 3: Delta propagation in downstream-destination space (upstream_owned)

Today the delta loop propagates deletions/renames using the upstream path directly. For `upstream_owned` rename entries the downstream file lives at the *destination*, so we map upstream source paths through the matching entry before propagating. `downstream_owned` remains excluded from delta (it is not added to the managed set).

**Files:**
- Modify: `internal/upstream-delta.go` (`buildManagedGlobs` → matchers; delta loop)
- Test: `internal/upstream-delta_test.go`

- [ ] **Step 1: Write failing tests for destination-space propagation**

Add to `internal/upstream-delta_test.go`. This test calls the internal helpers directly and builds no git commits:

```go
func Test_buildManagedMatchers_resolvesRenameDest(t *testing.T) {
	cfg := &GitSporkConfig{UpstreamOwned: []OwnedEntry{
		{From: "configs/**", To: ".configs/**"},
		{Pattern: "docs/**"},
	}}
	matchers, err := buildManagedMatchers(cfg)
	require.NoError(t, err)

	dest, ok := resolveManagedDest("configs/app.yml", matchers)
	require.True(t, ok)
	assert.Equal(t, ".configs/app.yml", dest)

	dest, ok = resolveManagedDest("docs/x.md", matchers)
	require.True(t, ok)
	assert.Equal(t, "docs/x.md", dest)

	_, ok = resolveManagedDest("unmanaged.txt", matchers)
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run it, expect failure**

Run: `go test ./internal -run Test_buildManagedMatchers_resolvesRenameDest -v`
Expected: FAIL — `buildManagedMatchers`/`resolveManagedDest` undefined.

- [ ] **Step 3: Introduce matchers in `upstream-delta.go`**

In `internal/upstream-delta.go`, replace `buildManagedGlobs` (and `matchesAnyGlob`) with matcher-based helpers:

```go
type managedMatcher struct {
	glob  glob.Glob
	entry *OwnedEntry // non-nil only for rename entries; nil means identity dest
}

func buildManagedMatchers(config *GitSporkConfig) ([]managedMatcher, error) {
	var matchers []managedMatcher
	for i := range config.UpstreamOwned {
		e := config.UpstreamOwned[i]
		g, err := glob.Compile(e.SourcePattern())
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q in .gitspork.yml: %v", e.SourcePattern(), err)
		}
		var ref *OwnedEntry
		if e.IsRename() {
			ref = &e
		}
		matchers = append(matchers, managedMatcher{glob: g, entry: ref})
	}
	var plain []string
	plain = append(plain, config.SharedOwnership.Merged...)
	plain = append(plain, config.SharedOwnership.Structured.PreferUpstream...)
	plain = append(plain, config.SharedOwnership.Structured.PreferDownstream...)
	for _, p := range plain {
		g, err := glob.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q in .gitspork.yml: %v", p, err)
		}
		matchers = append(matchers, managedMatcher{glob: g})
	}
	return matchers, nil
}

// resolveManagedDest returns the downstream destination for an upstream source
// path if any managed matcher matches it.
func resolveManagedDest(srcPath string, matchers []managedMatcher) (string, bool) {
	for _, m := range matchers {
		if m.glob.Match(srcPath) {
			if m.entry != nil {
				return m.entry.ResolveDest(srcPath), true
			}
			return srcPath, true
		}
	}
	return "", false
}
```

- [ ] **Step 4: Rewrite the delta loop to use destination space**

In `computeUpstreamDelta`, replace the `prevManagedGlobs` setup. Change:

```go
	prevManagedGlobs, err := buildManagedGlobs(prevConfig)
	if err != nil {
		return delta, err
	}
```

to:

```go
	prevMatchers, err := buildManagedMatchers(prevConfig)
	if err != nil {
		return delta, err
	}
	newMatchers, err := buildManagedMatchers(config)
	if err != nil {
		return delta, err
	}
```

And replace the `switch action { case merkletrie.Delete: ... case merkletrie.Modify: ... }` body with:

```go
		switch action {
		case merkletrie.Delete:
			fromPath := stripSubpath(change.From.Name, upstreamSubpath)
			if dest, ok := resolveManagedDest(fromPath, prevMatchers); ok {
				delta.Deletions = append(delta.Deletions, dest)
			}
		case merkletrie.Modify:
			// A Modify with different From/To names is a rename (after rename detection)
			if change.From.Name != change.To.Name {
				fromPath := stripSubpath(change.From.Name, upstreamSubpath)
				toPath := stripSubpath(change.To.Name, upstreamSubpath)
				oldDest, ok := resolveManagedDest(fromPath, prevMatchers)
				if !ok {
					break
				}
				newDest, ok := resolveManagedDest(toPath, newMatchers)
				if !ok {
					newDest = toPath
				}
				if oldDest != newDest {
					delta.Renames = append(delta.Renames, upstreamRename{OldPath: oldDest, NewPath: newDest})
				}
			}
		}
```

- [ ] **Step 5: Run the new and existing delta tests**

Run: `go test ./internal -run 'Delta|buildManagedMatchers' -v`
Expected: PASS — the new matcher test, all pre-existing delta tests, and crucially the "downstream_owned file deleted does not appear in delta" test (downstream is not in the managed set).

- [ ] **Step 6: Full unit suite**

Run: `make test-unit`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/upstream-delta.go internal/upstream-delta_test.go
git commit -m "feat: propagate upstream deletes/renames in downstream-destination space"
```

---

## Task 4: Document the rename form in README

**Files:**
- Modify: `docs/README.md`

- [ ] **Step 1: Update the schema example block**

In `docs/README.md`, update the `upstream_owned` and `downstream_owned` blocks (around lines 19-22) to show the rename form. Change:

```yaml
upstream_owned: # file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the upstream gitspork repo
- "upstream-owned.txt"
downstream_owned: # file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the downstream repo once it's been initially integrated
- "downstream-owned.md"
```

to:

```yaml
upstream_owned: # file patterns (https://github.com/gobwas/glob) fully owned by the upstream; an entry may instead be a {from, to} map to rename a file as it syncs to the downstream
- "upstream-owned.txt"
- from: "upstream-owned-renamed-from.txt" # (rename) upstream source glob/path
  to: "downstream-renamed-to.txt" # (rename) downstream destination glob/path
downstream_owned: # file patterns (https://github.com/gobwas/glob) fully owned by the downstream once initially integrated; an entry may instead be a {from, to} map to seed a file at a different downstream path
- "downstream-owned.md"
- from: "downstream-owned-seed-from.md"
  to: "downstream-owned-seed-to.md"
```

(If the exact comment text in the README differs from the above, match the README's existing wording for the lead-in and just add the `- from:/to:` lines and the rename note in the comment.)

- [ ] **Step 2: Add a short prose explanation after the schema block**

Find the prose immediately following the closing ```` ``` ```` of that schema block in `docs/README.md` and insert this section before the next `##` heading:

```markdown
### Renaming files on sync

An `upstream_owned` or `downstream_owned` entry is normally a glob string and the
matched files land at the same relative path in the downstream. To have a file
land at a *different* downstream path, use the `{from, to}` map form. `from` is
matched against the upstream tree exactly like a plain pattern; `to` is the
downstream destination. For glob renames (e.g. `from: configs/**`,
`to: .configs/**`) the destination is computed by swapping the source's
non-wildcard prefix for the destination's, so `configs/app/db.yml` lands at
`.configs/app/db.yml`.

The two lists differ only in *when* the copy happens: `upstream_owned` files are
overwritten on every integrate, while `downstream_owned` files are seeded once
(at the `to` path) and never overwritten afterward.
```

- [ ] **Step 3: Commit**

```bash
git add docs/README.md
git commit -m "docs: document upstream_owned/downstream_owned {from, to} rename form"
```

---

## Task 5: Functional tests (native binary, end-to-end)

**Files:**
- Create: `test/functional/rename_test.go`

- [ ] **Step 1: Write the functional rename + delta tests**

Create `test/functional/rename_test.go`:

```go
//go:build functional || functional_docker

package functional

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const upstreamRenameGitsporkYML = `upstream_owned:
- from: .markdownlint-cli2-downstream.jsonc
  to: .markdownlint-cli2.jsonc
- from: configs/**
  to: .configs/**
`

const downstreamRenameGitsporkYML = `downstream_owned:
- from: seed-from.md
  to: seed-to.md
`

func TestIntegrate_upstream_rename_exact_and_glob(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".markdownlint-cli2-downstream.jsonc": "{\"config\":true}\n",
		"configs/app.yml":                     "app: true\n",
		"configs/nested/db.yml":               "db: true\n",
	}, upstreamRenameGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "integrate exited non-zero:\n%s", out)

	// exact rename landed at destination, absent at source
	AssertFileContains(t, downstreamDir, ".markdownlint-cli2.jsonc", "\"config\":true")
	AssertFileAbsent(t, downstreamDir, ".markdownlint-cli2-downstream.jsonc")

	// glob rename: prefix-substituted destinations, absent at source prefix
	AssertFileContains(t, downstreamDir, ".configs/app.yml", "app: true")
	AssertFileContains(t, downstreamDir, ".configs/nested/db.yml", "db: true")
	AssertFileAbsent(t, downstreamDir, "configs/app.yml")
}

func TestIntegrate_upstream_rename_delete_propagates_to_destination(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".markdownlint-cli2-downstream.jsonc": "{\"config\":true}\n",
		"configs/app.yml":                     "app: true\n",
	}, upstreamRenameGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	AssertFileContains(t, downstreamDir, ".markdownlint-cli2.jsonc", "\"config\":true")
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// Delete the upstream source file (the entry stays in config), commit.
	require.NoError(t, os.Remove(filepath.Join(upstreamDir, ".markdownlint-cli2-downstream.jsonc")))
	CommitAll(t, OpenRepo(t, upstreamDir), upstreamDir, "remove renamed source upstream")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after upstream delete failed:\n%s", out)

	// Deletion must propagate to the DESTINATION path downstream.
	AssertFileAbsent(t, downstreamDir, ".markdownlint-cli2.jsonc")
}

func TestIntegrate_downstream_rename_seeds_and_survives_reintegrate(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"seed-from.md": "seed content\n",
	}, downstreamRenameGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)

	// seeded at destination, absent at source
	AssertFileContains(t, downstreamDir, "seed-to.md", "seed content")
	AssertFileAbsent(t, downstreamDir, "seed-from.md")

	// downstream customizes the seeded destination file, commits
	WriteFiles(t, downstreamDir, map[string]string{"seed-to.md": "downstream edit\n"})
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "customize seeded file")

	// re-integrate must NOT overwrite the downstream-owned destination
	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)
	content := ReadFile(t, downstreamDir, "seed-to.md")
	require.Contains(t, content, "downstream edit",
		"downstream-owned rename destination should survive re-integrate")
}
```

- [ ] **Step 2: Run the functional tests (native)**

Run: `go test -tags functional ./test/functional -run 'TestIntegrate_upstream_rename|TestIntegrate_downstream_rename' -v`
Expected: all three tests PASS.

- [ ] **Step 3: Run the whole functional suite to check for regressions**

Run: `make test-functional`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add test/functional/rename_test.go
git commit -m "test: functional coverage for upstream/downstream renames and delete propagation"
```

---

## Final verification

- [ ] **Step 1: Full unit + functional suites**

Run: `make test-unit && make test-functional`
Expected: all PASS.

- [ ] **Step 2: Confirm schema/init round-trips cleanly**

Run: `go run . schema` and visually confirm both `upstream_owned` and `downstream_owned` render plain entries as scalars and the rename entry as a `from/to` map.

- [ ] **Step 3: Confirm no stray files remain**

Run: `git status` and confirm the working tree is clean and `internal/upstream_owned_marshal_test.go` was renamed to `internal/owned_entry_test.go` (no leftover spike file).
