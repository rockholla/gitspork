# upstream_only Global Exclusion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a top-level `upstream_only` glob list to `.gitspork.yml` that prevents matched upstream paths from being synced to downstream, with a warning logged for each skipped file, taking precedence over `upstream_owned`, `downstream_owned`, and all `shared_ownership` entries.

**Architecture:** A new `UpstreamOnly []string` field on `GitSporkConfig` is read from YAML and injected into each affected integrator struct at construction time. A single `filterUpstreamOnly` helper centralizes the matching and warning logic; each integrator calls it once after `getIntegrateFiles` and before processing its file list.

**Tech Stack:** Go 1.26, `github.com/gobwas/glob` (already used for all pattern matching), `github.com/goccy/go-yaml` (already used for config parsing), `github.com/stretchr/testify` (already used for tests).

---

## File map

| Status | Path | What changes |
|--------|------|-------------|
| Modify | `internal/config/config.go` | Add `UpstreamOnly []string` field; add example entry to `GetGitSporkConfigSchema()` |
| Create | `internal/config/upstream_only_test.go` | Unit test: YAML with `upstream_only:` parses correctly |
| Modify | `internal/integrate/integrate.go` | Add `filterUpstreamOnly` helper; update 5 construction sites to pass `UpstreamOnly` |
| Create | `internal/integrate/filter_upstream_only_test.go` | Unit tests for `filterUpstreamOnly` (4 cases) |
| Modify | `internal/integrate/integrator_upstream_owned.go` | Add `UpstreamOnly []string` field; call `filterUpstreamOnly` per entry |
| Create | `internal/integrate/integrator_upstream_owned_test.go` | Unit test: excluded file absent from downstream |
| Modify | `internal/integrate/integrator_downstream_owned.go` | Add `UpstreamOnly []string` field; call `filterUpstreamOnly` per entry |
| Modify | `internal/integrate/integrator_shared_ownership_merged.go` | Add `UpstreamOnly []string` field; call `filterUpstreamOnly` after `getIntegrateFiles` |
| Modify | `internal/integrate/integrator_shared_ownership_structured_prefer_upstream.go` | Same pattern |
| Modify | `internal/integrate/integrator_shared_ownership_structured_prefer_downstream.go` | Same pattern |
| Create | `test/functional/upstream_only_test.go` | Functional test: excluded file absent, non-excluded file present, warning in output |

---

## Task 1: Config field and schema example

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/upstream_only_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/upstream_only_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitSporkConfig_upstreamOnly(t *testing.T) {
	dir := t.TempDir()
	content := "upstream_only:\n- cli/**\n- internal/generated/**\n"
	f := filepath.Join(dir, ".gitspork.yml")
	require.NoError(t, os.WriteFile(f, []byte(content), 0644))

	cfg, err := ParseGitSporkConfig(f)
	require.NoError(t, err)
	assert.Equal(t, []string{"cli/**", "internal/generated/**"}, cfg.UpstreamOnly)
}

func TestParseGitSporkConfig_upstreamOnly_absentMeansNil(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".gitspork.yml")
	require.NoError(t, os.WriteFile(f, []byte("upstream_owned:\n- foo/**\n"), 0644))

	cfg, err := ParseGitSporkConfig(f)
	require.NoError(t, err)
	assert.Nil(t, cfg.UpstreamOnly)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/... -run TestParseGitSporkConfig_upstreamOnly -v
```

Expected: FAIL — `cfg.UpstreamOnly` is nil (field does not exist yet).

- [ ] **Step 3: Add `UpstreamOnly` field to `GitSporkConfig`**

In `internal/config/config.go`, find the `GitSporkConfig` struct. Add the new field directly after the `UpstreamOwned` line (line 31):

```go
// GitSporkConfig represents the config an upstream repo defines in .gitspork.yml
type GitSporkConfig struct {
	UpstreamOwned   []OwnedEntry                  `yaml:"upstream_owned" comment:"file patterns (https://github.com/gobwas/glob) fully owned by the upstream; an entry may instead be a {from, to} map to rename a file as it syncs to the downstream"`
	UpstreamOnly    []string                      `yaml:"upstream_only,omitempty" comment:"file patterns (https://github.com/gobwas/glob) for upstream paths that must never be synced to downstream; takes precedence over upstream_owned, downstream_owned, and shared_ownership — matched files are skipped with a warning"`
	DownstreamOwned []OwnedEntry                  `yaml:"downstream_owned" comment:"file patterns (https://github.com/gobwas/glob) fully owned by the downstream once initially integrated; an entry may instead be a {from, to} map to seed a file at a different downstream path"`
	SharedOwnership GitSporkConfigSharedOwnership `yaml:"shared_ownership" comment:"file patterns (https://github.com/gobwas/glob) that will be owned by both the upstream and downstream repos in some managed way"`
	Templated       []GitSporkConfigTemplated     `yaml:"templated" comment:"list of instruction for templated source files in the upstream that should be rendered in some way to a location in the downstream"`
	Migrations      []string                      `yaml:"migrations" comment:"list of YAML file paths in the upstream repo, relative to the upstream repo root or subpath if specified, containing downstream repo migration instructions"`

	// comments holds user-written YAML comments captured on parse, re-injected on write.
	comments yaml.CommentMap `yaml:"-"`
}
```

- [ ] **Step 4: Add example entry in `GetGitSporkConfigSchema()`**

In `internal/config/config.go`, find `GetGitSporkConfigSchema()` (around line 147). The `gitSporkExampleConfig` struct literal starts with `UpstreamOwned:`. Add `UpstreamOnly:` immediately after it:

```go
gitSporkExampleConfig := &GitSporkConfig{
    UpstreamOwned: []OwnedEntry{
        {Pattern: "upstream-owned.txt"},
        {From: "upstream-owned-renamed-from.txt", To: "downstream-renamed-to.txt"},
    },
    UpstreamOnly: []string{"upstream-only-example/**"},
    DownstreamOwned: []OwnedEntry{
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/config/... -run TestParseGitSporkConfig_upstreamOnly -v
```

Expected: PASS for both `TestParseGitSporkConfig_upstreamOnly` and `TestParseGitSporkConfig_upstreamOnly_absentMeansNil`.

- [ ] **Step 6: Run full unit suite to check for regressions**

```bash
make test-unit
```

Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/upstream_only_test.go
git commit -m "feat(config): add upstream_only glob exclusion field to GitSporkConfig"
```

---

## Task 2: `filterUpstreamOnly` helper

**Files:**
- Modify: `internal/integrate/integrate.go`
- Create: `internal/integrate/filter_upstream_only_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/integrate/filter_upstream_only_test.go`:

```go
package integrate

import (
	"fmt"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingLogger struct {
	logs []string
}

func (l *recordingLogger) Log(msg string, args ...any) {
	l.logs = append(l.logs, fmt.Sprintf(msg, args...))
}

func (l *recordingLogger) Error(msg string, args ...any) {
	l.logs = append(l.logs, fmt.Sprintf(msg, args...))
}

func Test_filterUpstreamOnly_noPatterns(t *testing.T) {
	files := []string{"a/b.txt", "c/d.txt"}
	got, err := filterUpstreamOnly(files, nil, sdktypes.NoopLogger())
	require.NoError(t, err)
	assert.Equal(t, files, got)
}

func Test_filterUpstreamOnly_excludesSomeFiles(t *testing.T) {
	files := []string{"cli/foo.txt", "lib/bar.txt", "cli/.cloud-native-template/baz.txt"}
	rl := &recordingLogger{}
	got, err := filterUpstreamOnly(files, []string{"cli/**"}, rl)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib/bar.txt"}, got)
	assert.Len(t, rl.logs, 2, "expected a warning for each excluded file")
	assert.Contains(t, rl.logs[0], "cli/foo.txt")
	assert.Contains(t, rl.logs[1], "cli/.cloud-native-template/baz.txt")
}

func Test_filterUpstreamOnly_invalidPattern(t *testing.T) {
	_, err := filterUpstreamOnly([]string{"a.txt"}, []string{"["}, sdktypes.NoopLogger())
	assert.Error(t, err)
}

func Test_filterUpstreamOnly_patternMatchesNothing(t *testing.T) {
	files := []string{"a/b.txt", "c/d.txt"}
	got, err := filterUpstreamOnly(files, []string{"nomatch/**"}, sdktypes.NoopLogger())
	require.NoError(t, err)
	assert.Equal(t, files, got)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/integrate/... -run Test_filterUpstreamOnly -v
```

Expected: compilation error — `filterUpstreamOnly` undefined.

- [ ] **Step 3: Add `filterUpstreamOnly` to `integrate.go`**

In `internal/integrate/integrate.go`, add the following function. A good place is immediately before `getIntegrateFiles` (around line 673):

```go
// filterUpstreamOnly removes files whose relative paths match any upstream_only
// pattern, logging a warning for each exclusion. Globs are compiled once per
// call. An uncompilable pattern returns an error immediately.
func filterUpstreamOnly(files []string, patterns []string, logger sdktypes.Logger) ([]string, error) {
	if len(patterns) == 0 {
		return files, nil
	}
	globs := make([]glob.Glob, len(patterns))
	for i, p := range patterns {
		g, err := glob.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid upstream_only pattern %q: %v", p, err)
		}
		globs[i] = g
	}
	kept := files[:0:0]
	for _, f := range files {
		excluded := false
		for _, g := range globs {
			if g.Match(f) {
				logger.Log("⚠️ skipping %s — excluded by upstream_only", f)
				excluded = true
				break
			}
		}
		if !excluded {
			kept = append(kept, f)
		}
	}
	return kept, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/integrate/... -run Test_filterUpstreamOnly -v
```

Expected: all 4 tests pass.

- [ ] **Step 5: Run full unit suite**

```bash
make test-unit
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/integrate/integrate.go internal/integrate/filter_upstream_only_test.go
git commit -m "feat(integrate): add filterUpstreamOnly helper with unit tests"
```

---

## Task 3: Wire `UpstreamOnly` into `IntegratorUpstreamOwned`

**Files:**
- Modify: `internal/integrate/integrator_upstream_owned.go`
- Create: `internal/integrate/integrator_upstream_owned_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/integrate/integrator_upstream_owned_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/integrate/... -run TestIntegratorUpstreamOwned_upstreamOnly -v
```

Expected: compilation error — `IntegratorUpstreamOwned` has no field `UpstreamOnly`.

- [ ] **Step 3: Update `integrator_upstream_owned.go`**

Replace the full file content of `internal/integrate/integrator_upstream_owned.go` with:

```go
package integrate

import (
	"fmt"
	"path/filepath"

	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// IntegratorUpstreamOwned will process a list of files to be managed as owned by the upstream gitspork repo
type IntegratorUpstreamOwned struct {
	UpstreamOnly []string
}

var _ Integrator[config.OwnedEntry] = (*IntegratorUpstreamOwned)(nil)

// Integrate copies each upstream-owned file to the downstream, applying rename
// entries' destination resolution. Files matching any upstream_only pattern are
// skipped with a warning.
func (i *IntegratorUpstreamOwned) Integrate(entries []config.OwnedEntry, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error {
	for _, entry := range entries {
		integrateFiles, err := getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
		if err != nil {
			return fmt.Errorf("error determining the list of files to integrate in %s from %q: %v", upstreamPath, entry.SourcePattern(), err)
		}
		integrateFiles, err = filterUpstreamOnly(integrateFiles, i.UpstreamOnly, logger)
		if err != nil {
			return err
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

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/integrate/... -run TestIntegratorUpstreamOwned_upstreamOnly -v
```

Expected: PASS.

- [ ] **Step 5: Run full unit suite**

```bash
make test-unit
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/integrate/integrator_upstream_owned.go internal/integrate/integrator_upstream_owned_test.go
git commit -m "feat(integrate): add UpstreamOnly field to IntegratorUpstreamOwned"
```

---

## Task 4: Wire `UpstreamOnly` into the remaining 4 integrators

**Files:**
- Modify: `internal/integrate/integrator_downstream_owned.go`
- Modify: `internal/integrate/integrator_shared_ownership_merged.go`
- Modify: `internal/integrate/integrator_shared_ownership_structured_prefer_upstream.go`
- Modify: `internal/integrate/integrator_shared_ownership_structured_prefer_downstream.go`

No new unit tests — `filterUpstreamOnly` is already tested; the functional test (Task 6) covers end-to-end wiring. Run the existing test suite after each file to catch regressions.

- [ ] **Step 1: Update `integrator_downstream_owned.go`**

Replace the full file content with:

```go
package integrate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// IntegratorDownstreamOwned will process a list of files to be managed as owned by the downstream gitspork repo, just initially bootstrapped by the upstream
type IntegratorDownstreamOwned struct {
	UpstreamOnly []string
}

var _ Integrator[config.OwnedEntry] = (*IntegratorDownstreamOwned)(nil)

// Integrate seeds each downstream-owned file from the upstream a single time,
// applying rename entries' destination resolution. A file is only copied when
// its downstream destination does not already exist — the downstream owns it
// thereafter. Files matching any upstream_only pattern are skipped with a warning.
func (i *IntegratorDownstreamOwned) Integrate(entries []config.OwnedEntry, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error {
	for _, entry := range entries {
		integrateFiles, err := getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
		if err != nil {
			return fmt.Errorf("error determining the list of files to integrate in %s from %q: %v", upstreamPath, entry.SourcePattern(), err)
		}
		integrateFiles, err = filterUpstreamOnly(integrateFiles, i.UpstreamOnly, logger)
		if err != nil {
			return err
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

- [ ] **Step 2: Update `integrator_shared_ownership_merged.go`**

In `internal/integrate/integrator_shared_ownership_merged.go`, make two changes:

1. Add `UpstreamOnly []string` to the struct (line 25 area):

```go
// IntegratorSharedOwnershipMerged will process a list of files to have shared ownership and generic merging based on blocks defined as owned by the upstream repo
type IntegratorSharedOwnershipMerged struct {
	UpstreamOnly []string
}
```

2. In `Integrate`, add the `filterUpstreamOnly` call after `getIntegrateFiles` (after line 37):

```go
func (i *IntegratorSharedOwnershipMerged) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamPath, configuredGlobPatterns, err)
	}
	integrateFiles, err = filterUpstreamOnly(integrateFiles, i.UpstreamOnly, logger)
	if err != nil {
		return err
	}
	for _, integrateFile := range integrateFiles {
		if err := mergeOneSharedOwnershipFile(upstreamPath, downstreamPath, integrateFile, logger); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: Update `integrator_shared_ownership_structured_prefer_upstream.go`**

Replace the full file content with:

```go
package integrate

import (
	"fmt"
	"path/filepath"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// IntegratorSharedOwnershipStructuredPreferUpstream will process a list of structured data files to be co-owned by upstream and downstream, merged with preference/precdence in favor of upstream
type IntegratorSharedOwnershipStructuredPreferUpstream struct {
	UpstreamOnly []string
}

var _ Integrator[string] = (*IntegratorSharedOwnershipStructuredPreferUpstream)(nil)

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipStructuredPreferUpstream) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamPath, configuredGlobPatterns, err)
	}
	integrateFiles, err = filterUpstreamOnly(integrateFiles, i.UpstreamOnly, logger)
	if err != nil {
		return err
	}
	for _, integrateFile := range integrateFiles {
		logger.Log("📝 gathering structured data for %s", integrateFile)
		upstreamData, downstreamData, structuredDataType, err := getStructuredData(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, integrateFile))
		if err != nil {
			return err
		}
		logger.Log("🔧 merging upstream and downstream data, prefering upstream data")
		merged := mergeNodes(downstreamData, upstreamData, true)
		if err := writeStructuredData(merged, structuredDataType, filepath.Join(downstreamPath, integrateFile)); err != nil {
			return fmt.Errorf("error writing merged structured data: %v", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Update `integrator_shared_ownership_structured_prefer_downstream.go`**

Replace the full file content with:

```go
package integrate

import (
	"fmt"
	"path/filepath"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// IntegratorSharedOwnershipStructuredPreferDownstream will process a list of structured data files to be co-owned by upstream and downstream, merged with preference/precdence in favor of downstream
type IntegratorSharedOwnershipStructuredPreferDownstream struct {
	UpstreamOnly []string
}

var _ Integrator[string] = (*IntegratorSharedOwnershipStructuredPreferDownstream)(nil)

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipStructuredPreferDownstream) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamPath, configuredGlobPatterns, err)
	}
	integrateFiles, err = filterUpstreamOnly(integrateFiles, i.UpstreamOnly, logger)
	if err != nil {
		return err
	}
	for _, integrateFile := range integrateFiles {
		logger.Log("📝 gathering structured data for %s", integrateFile)
		upstreamData, downstreamData, structuredDataType, err := getStructuredData(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, integrateFile))
		if err != nil {
			return err
		}
		logger.Log("🔧 merging upstream and downstream data, prefering downstream data")
		merged := mergeNodes(upstreamData, downstreamData, true)
		if err := writeStructuredData(merged, structuredDataType, filepath.Join(downstreamPath, integrateFile)); err != nil {
			return fmt.Errorf("error writing merged structured data: %v", err)
		}
	}
	return nil
}
```

- [ ] **Step 5: Run full unit suite**

```bash
make test-unit
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add \
  internal/integrate/integrator_downstream_owned.go \
  internal/integrate/integrator_shared_ownership_merged.go \
  internal/integrate/integrator_shared_ownership_structured_prefer_upstream.go \
  internal/integrate/integrator_shared_ownership_structured_prefer_downstream.go
git commit -m "feat(integrate): add UpstreamOnly field to remaining 4 integrators"
```

---

## Task 5: Update construction sites in `integrate.go`

**Files:**
- Modify: `internal/integrate/integrate.go:342-365`

- [ ] **Step 1: Update the 5 construction sites**

In `internal/integrate/integrate.go`, find the block around lines 342–365 that constructs and calls the integrators. Add `upstreamOnly := gitSporkConfig.UpstreamOnly` before the first integrator call, then inject it into each struct literal:

Replace:
```go
logger.Log("%s", greenBold.Sprint("integrating configured upstream-owned resources from upstream to downstream"))
if err := (&IntegratorUpstreamOwned{}).Integrate(gitSporkConfig.UpstreamOwned, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating upstream-owned: %v", err)
}

logger.Log("%s", greenBold.Sprint("integrating configured downstream-owned resources from upstream to downstream"))
if err := (&IntegratorDownstreamOwned{}).Integrate(gitSporkConfig.DownstreamOwned, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating downstream-owned: %v", err)
}

logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership generic resources to merge b/w upstream and downstream"))
if err := (&IntegratorSharedOwnershipMerged{}).Integrate(gitSporkConfig.SharedOwnership.Merged, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating shared-ownership.merged: %v", err)
}

logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering upstream data"))
if err := (&IntegratorSharedOwnershipStructuredPreferUpstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferUpstream, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating shared-ownership.structured.prefer_upstream: %v", err)
}

logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering downstream data"))
if err := (&IntegratorSharedOwnershipStructuredPreferDownstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferDownstream, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating shared-ownership.structured.prefer_downstream: %v", err)
}
```

With:
```go
upstreamOnly := gitSporkConfig.UpstreamOnly

logger.Log("%s", greenBold.Sprint("integrating configured upstream-owned resources from upstream to downstream"))
if err := (&IntegratorUpstreamOwned{UpstreamOnly: upstreamOnly}).Integrate(gitSporkConfig.UpstreamOwned, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating upstream-owned: %v", err)
}

logger.Log("%s", greenBold.Sprint("integrating configured downstream-owned resources from upstream to downstream"))
if err := (&IntegratorDownstreamOwned{UpstreamOnly: upstreamOnly}).Integrate(gitSporkConfig.DownstreamOwned, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating downstream-owned: %v", err)
}

logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership generic resources to merge b/w upstream and downstream"))
if err := (&IntegratorSharedOwnershipMerged{UpstreamOnly: upstreamOnly}).Integrate(gitSporkConfig.SharedOwnership.Merged, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating shared-ownership.merged: %v", err)
}

logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering upstream data"))
if err := (&IntegratorSharedOwnershipStructuredPreferUpstream{UpstreamOnly: upstreamOnly}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferUpstream, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating shared-ownership.structured.prefer_upstream: %v", err)
}

logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering downstream data"))
if err := (&IntegratorSharedOwnershipStructuredPreferDownstream{UpstreamOnly: upstreamOnly}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferDownstream, upstreamPath, downstreamPath, logger); err != nil {
	return fmt.Errorf("error integrating shared-ownership.structured.prefer_downstream: %v", err)
}
```

- [ ] **Step 2: Run full unit suite**

```bash
make test-unit
```

Expected: all tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/integrate/integrate.go
git commit -m "feat(integrate): wire UpstreamOnly into integrator construction sites"
```

---

## Task 6: Functional test

**Files:**
- Create: `test/functional/upstream_only_test.go`

- [ ] **Step 1: Write the failing test**

Create `test/functional/upstream_only_test.go`:

```go
//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrate_upstreamOnly_excludesMatchingFiles(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".cloud-native-template/shared.txt":    "shared template content\n",
		"cli/.cloud-native-template/local.txt": "cli-local template content\n",
		"upstream-owned/other.txt":             "other upstream content\n",
	}, `upstream_owned:
- '{,**/}.cloud-native-template/**'
- upstream-owned/**
upstream_only:
- cli/**
`)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate exited non-zero:\n%s", out)

	// top-level .cloud-native-template syncs — not matched by upstream_only
	AssertFileContains(t, downstreamDir, ".cloud-native-template/shared.txt", "shared template content")
	// cli/.cloud-native-template excluded by upstream_only: cli/**
	AssertFileAbsent(t, downstreamDir, "cli/.cloud-native-template/local.txt")
	// upstream-owned file outside cli/ syncs normally
	AssertFileContains(t, downstreamDir, "upstream-owned/other.txt", "other upstream content")
	// warning logged for the skipped file
	assert.Contains(t, out, "cli/.cloud-native-template/local.txt")
}
```

- [ ] **Step 2: Run test to verify it fails (binary not yet built)**

```bash
make test-functional 2>&1 | head -40
```

Expected: either the binary build catches a compile error (none expected at this point) or the test fails because no existing binary exists yet. If you already have a compiled binary from before Task 1, the test will fail with an assertion error (file present when it should be absent, or warning absent from output).

- [ ] **Step 3: Run the full functional suite**

```bash
make test-functional
```

Expected: all functional tests pass including `TestIntegrate_upstreamOnly_excludesMatchingFiles`.

- [ ] **Step 4: Commit**

```bash
git add test/functional/upstream_only_test.go
git commit -m "test(functional): add upstream_only exclusion functional test"
```

---

## Self-review checklist (already run by plan author)

- **Spec coverage:** Config field ✓, `filterUpstreamOnly` helper ✓, all 5 integrators ✓, construction sites ✓, warning logging ✓, functional test ✓, schema example ✓. `templated` and `migrations` intentionally excluded per spec.
- **No placeholders:** All steps contain exact code, exact commands, and expected output.
- **Type consistency:** `UpstreamOnly []string` used identically in all struct definitions and construction sites. `filterUpstreamOnly(files []string, patterns []string, logger sdktypes.Logger) ([]string, error)` signature matches all call sites. `recordingLogger` defined once in `filter_upstream_only_test.go` and used only in that file.
