# upstream_owned Rename Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an `upstream_owned` entry be either a plain glob (unchanged) or a `{from, to}` map that renames a file as it syncs from upstream to downstream.

**Architecture:** Introduce an `UpstreamOwnedEntry` type with custom goccy YAML (un)marshalers so the YAML list stays a heterogeneous scalar-or-map list. Integration, delta propagation, and `mv`/`rm` all consume entries via a small set of helper methods (`SourcePattern`, `IsRename`, `ResolveDest`). The reflection-based schema renderer is corrected with a post-processing pass that collapses plain entries back to scalars.

**Tech Stack:** Go 1.26, `github.com/goccy/go-yaml` (custom `BytesMarshaler`/`BytesUnmarshaler`), `github.com/gobwas/glob`, `github.com/rockholla/go-lib/marshal`, cobra, testify.

---

## Background (read before starting)

The design spec is `docs/superpowers/specs/2026-06-03-upstream-owned-rename-syntax-design.md`. A de-risking spike already proved the marshaling approach; its tests live in `internal/upstream_owned_marshal_test.go` using **temporary fixture types** (`marshalTestEntry`/`marshalTestConfig`) and a **test-local** `collapsePlainUpstreamOwned`. This plan replaces those fixtures with the real type and promotes `collapsePlainUpstreamOwned` to production code.

Key facts about the existing code:
- `internal/gitspork.go:26` — `UpstreamOwned []string`. Custom marshalers will only be honored by goccy, not by `marshal.YAMLWithComments` (reflection-based, see `GetGitSporkConfigSchema` at `internal/gitspork.go:155`).
- `globNonWildcardPrefix(pattern string) string` already exists in `internal/upstream-mv-rm.go:11` and returns the portion of a glob before the first `*`/`?`/`[` (e.g. `configs/**` → `configs`, `a.txt` → `a.txt`).
- Consumers of `UpstreamOwned` that must change when the field type flips: `internal/integrate.go:178`, `internal/integrator_upstream-owned.go`, `internal/upstream-delta.go:102`, `internal/upstream-mv-rm.go:57,128`, the schema example at `internal/gitspork.go:157`, and unit-test fixtures in `internal/upstream-delta_test.go` and `internal/upstream-mv-rm_test.go`.

Run unit tests with `make test-unit` (`go vet ./... && go test ./...`). Functional tests need a build tag: `make test-functional`.

---

## File Structure

- **Create** `internal/upstream-owned-entry.go` — the `UpstreamOwnedEntry` type, its marshalers, `SourcePattern`/`IsRename`/`ResolveDest`, and `collapsePlainUpstreamOwned`. One file, one responsibility: the entry abstraction.
- **Modify** `internal/upstream_owned_marshal_test.go` — drop fixture types, point tests at the real type and production `collapsePlainUpstreamOwned`.
- **Modify** `internal/gitspork.go` — flip the field type; update field comment; update schema example; wire `collapsePlainUpstreamOwned` into `GetGitSporkConfigSchema`.
- **Modify** `internal/integrator_upstream-owned.go` — per-entry integration using `ResolveDest`.
- **Modify** `internal/upstream-mv-rm.go` — entry-aware rewrite/filter matching the source side.
- **Modify** `internal/upstream-delta.go` — managed *matchers* that map upstream source paths to downstream destinations.
- **Modify** `internal/upstream-delta_test.go`, `internal/upstream-mv-rm_test.go` — fixture migration + new rename cases.
- **Create** `test/functional/rename_test.go` — end-to-end integrate + delta scenarios for renames.
- **Modify** `docs/README.md` — document the rename form.

---

## Task 1: `UpstreamOwnedEntry` type, helpers, and schema-collapse helper

**Files:**
- Create: `internal/upstream-owned-entry.go`
- Test: `internal/upstream_owned_marshal_test.go` (rewrite existing spike tests onto the real type)

- [ ] **Step 1: Create the entry type and helpers**

Create `internal/upstream-owned-entry.go`:

```go
package internal

import (
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// UpstreamOwnedEntry is a single upstream_owned entry. It is either a plain glob
// pattern (Pattern, from a YAML scalar) or a rename (From/To, from a {from, to}
// YAML map). The forms are mutually exclusive.
//
// The yaml/comment struct tags are consumed ONLY by the reflection-based
// marshal.YAMLWithComments schema renderer. goccy uses the custom
// UnmarshalYAML/MarshalYAML below, which ignore tags.
type UpstreamOwnedEntry struct {
	Pattern string `yaml:"pattern,omitempty" comment:"a single glob file pattern fully owned by the upstream"`
	From    string `yaml:"from,omitempty" comment:"(rename) upstream source glob/path"`
	To      string `yaml:"to,omitempty" comment:"(rename) downstream destination glob/path"`
}

// IsRename reports whether the entry renames a file (From/To form).
func (e UpstreamOwnedEntry) IsRename() bool { return e.From != "" }

// SourcePattern returns the glob matched against the upstream tree.
func (e UpstreamOwnedEntry) SourcePattern() string {
	if e.IsRename() {
		return e.From
	}
	return e.Pattern
}

// ResolveDest returns the downstream destination path for an upstream file that
// matched this entry's SourcePattern. Plain entries map to the same path; rename
// entries swap the source pattern's non-wildcard prefix for the destination's,
// preserving the remainder (prefix substitution).
func (e UpstreamOwnedEntry) ResolveDest(matchedFile string) string {
	if !e.IsRename() {
		return matchedFile
	}
	srcPrefix := globNonWildcardPrefix(e.From)
	dstPrefix := globNonWildcardPrefix(e.To)
	return dstPrefix + strings.TrimPrefix(matchedFile, srcPrefix)
}

// UnmarshalYAML accepts either a scalar (plain pattern) or a {from, to} map.
func (e *UpstreamOwnedEntry) UnmarshalYAML(b []byte) error {
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
func (e UpstreamOwnedEntry) MarshalYAML() ([]byte, error) {
	if e.IsRename() {
		return yaml.Marshal(yaml.MapSlice{
			{Key: "from", Value: e.From},
			{Key: "to", Value: e.To},
		})
	}
	return yaml.Marshal(e.Pattern)
}

// collapsePlainUpstreamOwned rewrites reflection-rendered `- pattern: "X"` lines
// within the upstream_owned: block of schema output back to bare scalars `- "X"`,
// leaving {from, to} rename entries and following sections untouched. Needed
// because marshal.YAMLWithComments is reflection-based and ignores MarshalYAML.
var upstreamOwnedPatternLineRE = regexp.MustCompile(`^(\s*)- pattern: (".*?"|\S+)(\s*#.*)?$`)

func collapsePlainUpstreamOwned(schema string) string {
	lines := strings.Split(schema, "\n")
	inBlock := false
	for i, line := range lines {
		if strings.HasPrefix(line, "upstream_owned:") {
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
		if m := upstreamOwnedPatternLineRE.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + "- " + m[2]
		}
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 2: Replace the spike tests with real-type tests**

Overwrite `internal/upstream_owned_marshal_test.go` entirely:

```go
package internal

// Verifies the scalar-or-map marshaling of UpstreamOwnedEntry across goccy
// (the config read/write path) and the schema post-processing that corrects the
// reflection-based marshal.YAMLWithComments output.

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/rockholla/go-lib/marshal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// test-local container so we can exercise list unmarshaling with the real type.
type upstreamOwnedYAML struct {
	UpstreamOwned []UpstreamOwnedEntry `yaml:"upstream_owned" comment:"plain pattern or {from,to} rename"`
}

func TestUpstreamOwnedEntry_UnmarshalUnion(t *testing.T) {
	src := `upstream_owned:
  - src/**
  - from: .markdownlint-downstream.jsonc
    to: .markdownlint.jsonc
  - configs/**
`
	var cfg upstreamOwnedYAML
	require.NoError(t, yaml.Unmarshal([]byte(src), &cfg))
	require.Len(t, cfg.UpstreamOwned, 3)
	assert.Equal(t, "src/**", cfg.UpstreamOwned[0].Pattern)
	assert.False(t, cfg.UpstreamOwned[0].IsRename())
	assert.Equal(t, ".markdownlint-downstream.jsonc", cfg.UpstreamOwned[1].From)
	assert.Equal(t, ".markdownlint.jsonc", cfg.UpstreamOwned[1].To)
	assert.True(t, cfg.UpstreamOwned[1].IsRename())
	assert.Equal(t, "configs/**", cfg.UpstreamOwned[2].Pattern)
}

func TestUpstreamOwnedEntry_MarshalRoundTrip(t *testing.T) {
	cfg := upstreamOwnedYAML{UpstreamOwned: []UpstreamOwnedEntry{
		{Pattern: "src/**"},
		{From: "a.txt", To: "b.txt"},
	}}
	out, err := yaml.Marshal(&cfg)
	require.NoError(t, err)
	assert.Contains(t, string(out), "- src/**")
	assert.Contains(t, string(out), "from: a.txt")
	assert.Contains(t, string(out), "to: b.txt")

	var back upstreamOwnedYAML
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, "src/**", back.UpstreamOwned[0].Pattern)
	assert.Equal(t, "a.txt", back.UpstreamOwned[1].From)
	assert.Equal(t, "b.txt", back.UpstreamOwned[1].To)
}

func TestUpstreamOwnedEntry_SourcePattern(t *testing.T) {
	assert.Equal(t, "src/**", UpstreamOwnedEntry{Pattern: "src/**"}.SourcePattern())
	assert.Equal(t, "a.txt", UpstreamOwnedEntry{From: "a.txt", To: "b.txt"}.SourcePattern())
}

func TestUpstreamOwnedEntry_ResolveDest(t *testing.T) {
	// plain entry: identity
	assert.Equal(t, "x/y.txt", UpstreamOwnedEntry{Pattern: "x/**"}.ResolveDest("x/y.txt"))
	// exact rename
	assert.Equal(t, "b.txt", UpstreamOwnedEntry{From: "a.txt", To: "b.txt"}.ResolveDest("a.txt"))
	// glob rename: prefix substitution
	e := UpstreamOwnedEntry{From: "configs/**", To: ".configs/**"}
	assert.Equal(t, ".configs/app/db.yml", e.ResolveDest("configs/app/db.yml"))
	assert.Equal(t, ".configs/x/y/z.txt", e.ResolveDest("configs/x/y/z.txt"))
}

func TestCollapsePlainUpstreamOwned(t *testing.T) {
	cfg := &upstreamOwnedYAML{UpstreamOwned: []UpstreamOwnedEntry{
		{Pattern: "upstream-owned.txt"},
		{From: ".markdownlint-downstream.jsonc", To: ".markdownlint.jsonc"},
	}}
	raw, err := marshal.YAMLWithComments(cfg, 0)
	require.NoError(t, err)
	out := collapsePlainUpstreamOwned(raw)
	assert.Contains(t, out, `- "upstream-owned.txt"`)
	assert.NotContains(t, out, "- pattern:")
	assert.Contains(t, out, `from: ".markdownlint-downstream.jsonc"`)
	assert.Contains(t, out, `to: ".markdownlint.jsonc"`)
}
```

- [ ] **Step 3: Run the tests, expect PASS**

Run: `go test ./internal -run 'TestUpstreamOwnedEntry|TestCollapsePlainUpstreamOwned' -v`
Expected: all PASS. (The field is still `[]string`; the type stands alone, so the whole package still builds.)

- [ ] **Step 4: Verify the package builds and vets**

Run: `go vet ./internal/`
Expected: exit 0, no output.

- [ ] **Step 5: Commit**

```bash
git add internal/upstream-owned-entry.go internal/upstream_owned_marshal_test.go
git commit -m "feat: add UpstreamOwnedEntry type with scalar-or-map YAML support"
```

---

## Task 2: Flip `UpstreamOwned` to entries; integrate, mv/rm, schema

This task changes `GitSporkConfig.UpstreamOwned` to `[]UpstreamOwnedEntry`, which breaks compilation across the package until every consumer is updated. Do all steps before running the suite.

**Files:**
- Modify: `internal/gitspork.go` (field type @ line 26, field comment, schema example @ line 157, `GetGitSporkConfigSchema` @ ~line 202)
- Modify: `internal/integrator_upstream-owned.go`
- Modify: `internal/upstream-mv-rm.go` (`ComputeUpstreamMvFromConfig`, `ComputeUpstreamRmFromConfig`)
- Modify: `internal/upstream-delta.go` (`buildManagedGlobs` @ line 100)
- Modify: `internal/upstream-mv-rm_test.go`, `internal/upstream-delta_test.go` (fixtures)
- Test: add cases to `internal/upstream-mv-rm_test.go`; add a config round-trip test to `internal/upstream_owned_marshal_test.go`

- [ ] **Step 1: Flip the field type and update its comment**

In `internal/gitspork.go`, change line 26 from:

```go
	UpstreamOwned   []string                      `yaml:"upstream_owned" comment:"file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the upstream gitspork repo"`
```

to:

```go
	UpstreamOwned   []UpstreamOwnedEntry          `yaml:"upstream_owned" comment:"file patterns (https://github.com/gobwas/glob) fully owned by the upstream; an entry may instead be a {from, to} map to rename a file as it syncs to the downstream"`
```

- [ ] **Step 2: Update the schema example to include a rename**

In `internal/gitspork.go` `GetGitSporkConfigSchema`, change the `UpstreamOwned` field of `gitSporkExampleConfig` (line ~157) from:

```go
		UpstreamOwned:   []string{"upstream-owned.txt"},
```

to:

```go
		UpstreamOwned: []UpstreamOwnedEntry{
			{Pattern: "upstream-owned.txt"},
			{From: "upstream-owned-renamed-from.txt", To: "downstream-renamed-to.txt"},
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
	renderedMain = collapsePlainUpstreamOwned(renderedMain)
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
func (i *IntegratorUpstreamOwned) Integrate(entries []UpstreamOwnedEntry, upstreamPath string, downstreamPath string, logger *Logger) error {
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

(The call site `internal/integrate.go:178` already passes `gitSporkConfig.UpstreamOwned`; no change needed there once the signature accepts `[]UpstreamOwnedEntry`.)

- [ ] **Step 5: Fix the delta managed-glob builder to compile (behavior-preserving)**

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

(Full destination-space delta handling is Task 3; this keeps the build green and current behavior for plain entries.)

- [ ] **Step 6: Make mv/rm operate on the entry source side**

In `internal/upstream-mv-rm.go`, inside `ComputeUpstreamMvFromConfig`, replace the line:

```go
	config.UpstreamOwned = rewritePatterns(config.UpstreamOwned)
```

with:

```go
	config.UpstreamOwned = rewriteUpstreamOwned(config.UpstreamOwned, oldPath, newPath, &warnings)
```

And add this helper function in the same file (after `ComputeUpstreamMvFromConfig`):

```go
// rewriteUpstreamOwned applies a move rewrite to the source side of each entry
// (Pattern for plain entries, From for renames); the rename destination (To) is
// left untouched.
func rewriteUpstreamOwned(entries []UpstreamOwnedEntry, oldPath, newPath string, warnings *[]string) []UpstreamOwnedEntry {
	result := make([]UpstreamOwnedEntry, len(entries))
	for i, e := range entries {
		src := e.SourcePattern()
		prefix := globNonWildcardPrefix(src)
		var newSrc string
		switch {
		case prefix == "":
			*warnings = append(*warnings, fmt.Sprintf("pattern %q has a leading wildcard — update manually", src))
			newSrc = src
		case src == oldPath:
			newSrc = newPath
		case prefix == oldPath || strings.HasPrefix(prefix, oldPath+"/"):
			newSrc = newPath + src[len(oldPath):]
		default:
			newSrc = src
		}
		if e.IsRename() {
			result[i] = UpstreamOwnedEntry{From: newSrc, To: e.To}
		} else {
			result[i] = UpstreamOwnedEntry{Pattern: newSrc}
		}
	}
	return result
}
```

In `ComputeUpstreamRmFromConfig`, replace:

```go
	config.UpstreamOwned = filterPatterns(config.UpstreamOwned)
```

with:

```go
	config.UpstreamOwned = filterUpstreamOwned(config.UpstreamOwned, path, recursive, &warnings)
```

And add this helper (after `ComputeUpstreamRmFromConfig`):

```go
// filterUpstreamOwned drops entries whose source side matches path (exact, or
// non-wildcard-prefix under path when recursive).
func filterUpstreamOwned(entries []UpstreamOwnedEntry, path string, recursive bool, warnings *[]string) []UpstreamOwnedEntry {
	var result []UpstreamOwnedEntry
	for _, e := range entries {
		src := e.SourcePattern()
		if src == path {
			continue
		}
		if recursive {
			prefix := globNonWildcardPrefix(src)
			if prefix == "" {
				*warnings = append(*warnings, fmt.Sprintf("pattern %q has a leading wildcard — update manually", src))
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

(Leave the existing `rewritePatterns`/`filterPatterns` closures in place — they are still used by `DownstreamOwned`, `SharedOwnership.*`.)

- [ ] **Step 7: Migrate existing unit-test fixtures to the new type**

In `internal/upstream-delta_test.go`, change each `UpstreamOwned: []string{"docs/**"}` (lines 34, 82, 97) to:

```go
		config := &GitSporkConfig{UpstreamOwned: []UpstreamOwnedEntry{{Pattern: "docs/**"}}}
```

In `internal/upstream-mv-rm_test.go`, convert each `UpstreamOwned` literal and assertion. The pattern for inputs:
- `UpstreamOwned: []string{"docs/old.md"}` → `UpstreamOwned: []UpstreamOwnedEntry{{Pattern: "docs/old.md"}}`
- `UpstreamOwned: []string{"docs/guide.md", "docs/other.md"}` → `UpstreamOwned: []UpstreamOwnedEntry{{Pattern: "docs/guide.md"}, {Pattern: "docs/other.md"}}`

And for assertions:
- `assert.Equal(t, []string{"docs/new.md"}, result.UpstreamOwned)` → `assert.Equal(t, []UpstreamOwnedEntry{{Pattern: "docs/new.md"}}, result.UpstreamOwned)`

Apply this conversion to lines 23, 29, 34, 40, 45, 51, 109, 115, 206, 212, 217, 223, 228, 234, 239, 245. Line 193 (`cfg.UpstreamOwned = append(cfg.UpstreamOwned, "docs/new.md")`) becomes `cfg.UpstreamOwned = append(cfg.UpstreamOwned, UpstreamOwnedEntry{Pattern: "docs/new.md"})`.

- [ ] **Step 8: Add rename-aware mv/rm test cases**

Append to the `Test_UpstreamMv` function in `internal/upstream-mv-rm_test.go`:

```go
	t.Run("rename entry: mv rewrites source side, leaves destination", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []UpstreamOwnedEntry{{From: "source.txt", To: "dest.txt"}},
		})
		warnings, err := UpstreamMv(cfg, "source.txt", "new-source.txt")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []UpstreamOwnedEntry{{From: "new-source.txt", To: "dest.txt"}}, result.UpstreamOwned)
	})
```

Append to the `Test_UpstreamRm` function (rm subtests, at `internal/upstream-mv-rm_test.go:203`):

```go
	t.Run("rename entry: rm matches source side and removes entry", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []UpstreamOwnedEntry{
				{From: "source.txt", To: "dest.txt"},
				{Pattern: "keep.txt"},
			},
		})
		warnings, err := UpstreamRm(cfg, "source.txt", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []UpstreamOwnedEntry{{Pattern: "keep.txt"}}, result.UpstreamOwned)
	})
```


- [ ] **Step 9: Add a full-config comment-preserving round-trip test**

Append to `internal/upstream_owned_marshal_test.go`:

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

Add `"os"` to that file's import block.

- [ ] **Step 10: Build, vet, and run the full unit suite**

Run: `make test-unit`
Expected: PASS, no vet errors.

- [ ] **Step 11: Sanity-check the schema output by eye**

Run: `go run . schema | head -10`
Expected: the `upstream_owned:` block shows `- "upstream-owned.txt"` (bare scalar) and a `- from: ... / to: ...` rename entry — no `- pattern:` line.

- [ ] **Step 12: Commit**

```bash
git add internal/gitspork.go internal/integrator_upstream-owned.go internal/upstream-mv-rm.go internal/upstream-delta.go internal/upstream-mv-rm_test.go internal/upstream-delta_test.go internal/upstream_owned_marshal_test.go
git commit -m "feat: rename-aware upstream_owned integration, mv/rm, and schema"
```

---

## Task 3: Delta propagation in downstream-destination space

Today the delta loop propagates deletions/renames using the upstream path directly. For rename entries the downstream file lives at the *destination*, so we map upstream source paths through the matching entry before propagating.

**Files:**
- Modify: `internal/upstream-delta.go` (`buildManagedGlobs` → matchers; delta loop)
- Test: `internal/upstream-delta_test.go`

- [ ] **Step 1: Write failing tests for destination-space propagation**

Add to `internal/upstream-delta_test.go`. This test calls the internal helpers directly and builds no git commits:

```go
func Test_buildManagedMatchers_resolvesRenameDest(t *testing.T) {
	cfg := &GitSporkConfig{UpstreamOwned: []UpstreamOwnedEntry{
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
	entry *UpstreamOwnedEntry // non-nil only for rename entries; nil means identity dest
}

func buildManagedMatchers(config *GitSporkConfig) ([]managedMatcher, error) {
	var matchers []managedMatcher
	for i := range config.UpstreamOwned {
		e := config.UpstreamOwned[i]
		g, err := glob.Compile(e.SourcePattern())
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q in .gitspork.yml: %v", e.SourcePattern(), err)
		}
		var ref *UpstreamOwnedEntry
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

In `computeUpstreamDelta`, replace the `prevManagedGlobs` setup and the `switch action` block. Change:

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
Expected: PASS (new matcher test + all pre-existing delta tests).

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

In `docs/README.md`, change the `upstream_owned` block (lines 19-20) from:

```yaml
upstream_owned: # file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the upstream gitspork repo
- "upstream-owned.txt"
```

to:

```yaml
upstream_owned: # file patterns (https://github.com/gobwas/glob) fully owned by the upstream; an entry may instead be a {from, to} map to rename a file as it syncs to the downstream
- "upstream-owned.txt"
- from: "upstream-owned-renamed-from.txt" # (rename) upstream source glob/path
  to: "downstream-renamed-to.txt" # (rename) downstream destination glob/path
```

- [ ] **Step 2: Add a short prose explanation after the schema block**

Find the prose immediately following the closing ```` ``` ```` of that schema block in `docs/README.md` and insert this paragraph before the next `##` heading:

```markdown
### Renaming files on sync

An `upstream_owned` entry is normally a glob string and the matched files land at
the same relative path in the downstream. To have a file land at a *different*
downstream path, use the `{from, to}` map form. `from` is matched against the
upstream tree exactly like a plain pattern; `to` is the downstream destination.
For glob renames (e.g. `from: configs/**`, `to: .configs/**`) the destination is
computed by swapping the source's non-wildcard prefix for the destination's, so
`configs/app/db.yml` lands at `.configs/app/db.yml`.
```

- [ ] **Step 3: Commit**

```bash
git add docs/README.md
git commit -m "docs: document upstream_owned {from, to} rename form"
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

const renameGitsporkYML = `upstream_owned:
- from: .markdownlint-cli2-downstream.jsonc
  to: .markdownlint-cli2.jsonc
- from: configs/**
  to: .configs/**
`

func TestIntegrate_rename_exact_and_glob(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".markdownlint-cli2-downstream.jsonc": "{\"config\":true}\n",
		"configs/app.yml":                     "app: true\n",
		"configs/nested/db.yml":               "db: true\n",
	}, renameGitsporkYML)
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

func TestIntegrate_rename_delete_propagates_to_destination(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".markdownlint-cli2-downstream.jsonc": "{\"config\":true}\n",
		"configs/app.yml":                     "app: true\n",
	}, renameGitsporkYML)
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
```

- [ ] **Step 2: Run the functional tests (native)**

Run: `go test -tags functional ./test/functional -run 'TestIntegrate_rename' -v`
Expected: both tests PASS.

- [ ] **Step 3: Run the whole functional suite to check for regressions**

Run: `make test-functional`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add test/functional/rename_test.go
git commit -m "test: functional coverage for upstream_owned renames and delete propagation"
```

---

## Final verification

- [ ] **Step 1: Full unit + functional suites**

Run: `make test-unit && make test-functional`
Expected: all PASS.

- [ ] **Step 2: Confirm schema/init round-trips cleanly**

Run: `go run . schema` and visually confirm `upstream_owned` renders plain entries as scalars and the rename entry as a `from/to` map.

- [ ] **Step 3: Remove the obsolete spike file reference (sanity)**

Run: `git status` and confirm `internal/spike_rename_marshal_test.go` no longer exists (it was deleted during the spike) and no stray files remain.
