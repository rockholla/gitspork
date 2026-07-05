# SDK Phase 2: Physical Repo Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the monolithic `internal/` package into cohesive subpackages (`types`, `logutil`, `config`, `integrate`, `drift`), and relocate `cmd/` under `internal/cli/`. No behavior changes. Each task ends in a green build + all test suites passing.

**Architecture:** Phase 2 of three toward a golang SDK. Phase 1 reshaped return signatures. Phase 2 physically reorganizes the code so Phase 3 can promote `internal/types/` to a public `package gitspork` at the module root with a straight file move. The intermediate `internal/types/` package holds the future SDK vocabulary (Options, Results, Logger interface, sentinel errors) so subpackages depend only on that shared vocabulary.

**Tech Stack:** Go, existing dependencies unchanged.

---

## File Structure

### Before (post-Phase-1 main)

```
├── main.go                          # package main
├── cmd/                             # package cmd; cobra subcommands
│   ├── check-drift.go
│   ├── init.go
│   ├── integrate-local.go
│   ├── integrate.go
│   ├── mv.go
│   ├── rm.go
│   ├── root.go
│   └── schema.go
├── internal/                        # package internal (monolithic)
│   ├── check-drift.go
│   ├── check-drift_test.go
│   ├── gitspork.go                  # types (config + SDK) + parsers + schema + state
│   ├── gitspork_test.go
│   ├── init.go
│   ├── init_test.go
│   ├── integrate.go                 # Integrate + integrateOne + state I/O + URL norm
│   ├── integrate_test.go
│   ├── integrate-local.go
│   ├── integrator_downstream-owned.go
│   ├── integrator_shared-ownership-merged.go
│   ├── integrator_shared-ownership-structured-prefer-downstream.go
│   ├── integrator_shared-ownership-structured-prefer-upstream.go
│   ├── integrator_templated.go
│   ├── integrator_upstream-owned.go
│   ├── logger.go                    # concrete Logger + ColorizeYAML
│   ├── owned-entry.go
│   ├── owned-entry_test.go
│   ├── upstream-delta.go
│   ├── upstream-delta_test.go
│   ├── upstream-mv-rm.go
│   ├── upstream-mv-rm_test.go
│   ├── input/
│   └── testharness/
└── test/
```

### After (Phase 2 target)

```
├── main.go                          # package main; imports internal/cli
├── internal/
│   ├── cli/                         # package cli; cobra subcommands (moved from cmd/)
│   │   ├── check-drift.go
│   │   ├── init.go
│   │   ├── integrate-local.go
│   │   ├── integrate.go
│   │   ├── mv.go
│   │   ├── rm.go
│   │   ├── root.go
│   │   ├── schema.go
│   │   ├── upstream_flag.go         # ParseUpstreamFlag + its test (moved from integrate/)
│   │   └── upstream_flag_test.go
│   ├── config/                      # package config
│   │   ├── config.go                # GitSporkConfig types + parsers + schema + Find
│   │   ├── config_test.go
│   │   ├── init.go                  # Init() scaffolder
│   │   ├── init_test.go
│   │   ├── owned_entry.go
│   │   ├── owned_entry_test.go
│   │   ├── upstream_mv_rm.go
│   │   └── upstream_mv_rm_test.go
│   ├── drift/                       # package drift
│   │   ├── check_drift.go           # CheckDrift + supporting helpers
│   │   └── check_drift_test.go
│   ├── integrate/                   # package integrate
│   │   ├── integrate.go             # Integrate + integrateOne
│   │   ├── integrate_test.go
│   │   ├── integrate_local.go       # IntegrateLocal
│   │   ├── integrator_downstream-owned.go
│   │   ├── integrator_shared-ownership-merged.go
│   │   ├── integrator_shared-ownership-structured-prefer-downstream.go
│   │   ├── integrator_shared-ownership-structured-prefer-upstream.go
│   │   ├── integrator_templated.go
│   │   ├── integrator_upstream-owned.go
│   │   ├── state.go                 # exported LoadDownstreamState / SaveDownstreamState / UpsertUpstreamState
│   │   ├── url.go                   # exported NormalizeUpstreamURL
│   │   ├── upstream_delta.go
│   │   └── upstream_delta_test.go
│   ├── logutil/                     # package logutil
│   │   ├── colorize.go              # ColorizeYAML
│   │   └── logger.go                # concrete Logger; implements types.Logger
│   ├── types/                       # package types (future SDK vocabulary)
│   │   ├── errors.go                # ErrDriftDetected
│   │   ├── logger.go                # Logger interface
│   │   ├── options.go               # IntegrateOptions, IntegrateLocalOptions, CheckDriftOptions, UpstreamSpec
│   │   ├── results.go               # IntegrateResult, IntegratedUpstream, DriftReport, DriftedFile
│   │   └── state.go                 # GitSporkDownstreamState, GitSporkUpstreamState
│   ├── input/                       # unchanged
│   └── testharness/                 # unchanged
└── test/                            # unchanged (build tag / harness updates only if needed)
```

### Package dependency graph (post-Phase-2)

```
types  ← logutil, config, integrate, drift, cli
config  ← integrate, drift, cli
integrate  ← drift, cli
drift  ← cli
logutil  ← cli
input   ← integrate
testharness  ← test/*
```

Strictly downstream one-way dependencies; no cycles.

---

## Task 1: Create `internal/types/` — shared vocabulary and Logger interface

**Goal:** Extract SDK-facing types and the sentinel error from `internal/gitspork.go` and `internal/check-drift.go` into a new `internal/types/` package. Define a `Logger` interface. Update all references.

**Files:**
- Create: `internal/types/logger.go`
- Create: `internal/types/options.go`
- Create: `internal/types/results.go`
- Create: `internal/types/state.go`
- Create: `internal/types/errors.go`
- Modify: `internal/gitspork.go` (remove moved types)
- Modify: `internal/check-drift.go` (remove `ErrDriftDetected` local decl)
- Modify: `internal/logger.go` (add compile-time interface assertion)
- Modify: `internal/integrate.go`, `internal/integrate-local.go`, `internal/check-drift.go`, `internal/integrator_*.go`, `internal/init.go`, `internal/upstream-delta.go`, `internal/*_test.go` (update Logger param types + type references)
- Modify: `cmd/*.go` (update all `internal.<Type>` refs for moved types)

- [ ] **Step 1: Create `internal/types/logger.go`**

```go
package types

// Logger is the small interface gitspork uses for narration/progress and
// error messages. It is deliberately narrow so SDK consumers can wire their
// own logging (slog, zap, log/logr) with minimal glue. A nil Logger means
// silent — implementations that accept Logger as a field MUST check for nil
// before calling either method.
type Logger interface {
	Log(msg string, args ...any)
	Error(msg string, args ...any)
}
```

- [ ] **Step 2: Create `internal/types/options.go`** — copy the option-struct definitions from `internal/gitspork.go` (currently lines 143–172) into this new file, changing the `Logger` field type from `*Logger` (concrete) to `Logger` (the interface just defined). Full contents:

```go
package types

// IntegrateOptions configures a call to Integrate. Populate Upstreams (one
// or more entries) for multi-upstream integration, or the deprecated
// UpstreamRepo* single-value fields for backward compatibility.
type IntegrateOptions struct {
	Upstreams              []UpstreamSpec
	DownstreamRepoPath     string
	ForceRePrompt          bool
	Logger                 Logger
	ForDriftCheck          bool
	PrevUpstreamCommitHash string
	UpstreamRepoURL        string
	UpstreamRepoVersion    string
	UpstreamRepoSubpath    string
	UpstreamRepoToken      string
	UpstreamRepoCommit     string
}

// IntegrateLocalOptions configures a call to IntegrateLocal. Populate
// UpstreamPaths for multi-path integration; UpstreamPath (deprecated) is
// preserved for backward compatibility.
type IntegrateLocalOptions struct {
	UpstreamPaths  []string
	UpstreamPath   string
	DownstreamPath string
	ForceRePrompt  bool
	Logger         Logger
}

// CheckDriftOptions configures a call to CheckDrift. Leave Upstreams empty
// to use the recorded state; supply entries to override with different
// URLs/tokens for the same recorded commit hashes.
type CheckDriftOptions struct {
	Upstreams          []UpstreamSpec
	DownstreamRepoPath string
	Logger             Logger
}

// UpstreamSpec identifies a single upstream to integrate from.
type UpstreamSpec struct {
	URL     string
	Version string
	Subpath string
	Token   string
}
```

- [ ] **Step 3: Create `internal/types/results.go`** — copy result-type definitions from `internal/gitspork.go` (lines 70–99) verbatim, only changing the package to `types`:

```go
package types

// IntegrateResult is the structural return value of Integrate and IntegrateLocal.
// It records what was successfully integrated (in order); on partial failure
// the successful upstreams so far are still present in this result alongside
// the returned error.
type IntegrateResult struct {
	Upstreams []IntegratedUpstream
}

// IntegratedUpstream identifies a single successfully integrated upstream.
type IntegratedUpstream struct {
	URL        string
	Subpath    string
	CommitHash string
}

// DriftReport is the structural return value of CheckDrift. HasDrift is false
// when the downstream matches the recorded integration state; true when
// differences were found. Files enumerates the drifted entries with per-file
// attribution to whichever upstream last wrote each path.
type DriftReport struct {
	HasDrift bool
	Files    []DriftedFile
}

// DriftedFile is a single entry in a DriftReport.
type DriftedFile struct {
	Path          string
	AttributedURL string // upstream URL responsible for this file; empty means unattributed
	Diff          string // unified-diff text for this file; a `Binary files ... differ` marker line when the file is binary
}
```

- [ ] **Step 4: Create `internal/types/state.go`** — copy state-type definitions from `internal/gitspork.go` (lines 61–115). Full contents:

```go
package types

// GitSporkDownstreamState is the on-disk state stored at
// .gitspork/downstream-state.json in the downstream repo.
type GitSporkDownstreamState struct {
	MigrationsComplete []string                `json:"migrations_complete"`
	Upstreams          []GitSporkUpstreamState `json:"upstreams,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamRepoURL     string `json:"last_upstream_repo_url,omitempty"`
	LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
	LastUpstreamCommitHash  string `json:"last_upstream_commit_hash,omitempty"`
}

// GitSporkUpstreamState records the last integration for a single upstream.
type GitSporkUpstreamState struct {
	URL        string `json:"url"`
	Subpath    string `json:"subpath,omitempty"`
	CommitHash string `json:"commit_hash"`
}
```

- [ ] **Step 5: Create `internal/types/errors.go`** — move `ErrDriftDetected` from `internal/check-drift.go`:

```go
package types

import "errors"

// ErrDriftDetected is returned by CheckDrift when drift is found in the downstream.
var ErrDriftDetected = errors.New("drift detected")
```

- [ ] **Step 6: Remove the moved types from `internal/gitspork.go`**

Delete these declarations (currently lines 61–172):
- `type GitSporkDownstreamState struct { ... }`
- `type IntegrateResult struct { ... }`
- `type IntegratedUpstream struct { ... }`
- `type DriftReport struct { ... }`
- `type DriftedFile struct { ... }`
- `type UpstreamSpec struct { ... }`
- `type GitSporkUpstreamState struct { ... }`
- `type IntegrateOptions struct { ... }`
- `type IntegrateLocalOptions struct { ... }`
- `type CheckDriftOptions struct { ... }`

Keep: everything above line 61 (constants, `GitSporkConfig`, `GitSporkConfigSharedOwnership`, `GitSporkConfigSharedOwnershipStructured`, `GitSporkConfigMigration`, `GitSporkConfigMigrationInstructions`) and everything from `GitSporkConfigTemplated` (currently line 117) onward through `ParseGitSporkConfig`, `ParseMigrationConfig`, `GetGitSporkConfigSchema`, `WriteGitSporkConfig`, `FindGitSporkConfig`.

- [ ] **Step 7: Remove `ErrDriftDetected` from `internal/check-drift.go`**

Delete lines 22–23:

```go
// ErrDriftDetected is returned by CheckDrift when drift is found in the downstream
var ErrDriftDetected = errors.New("drift detected")
```

Remove the `"errors"` import from `internal/check-drift.go` if no other symbol in the file still uses it. (Note: `errors.Is` and `errors.New` are both from the same package — check both. Currently `errors.Is(err, gogit.ErrEmptyCommit)` is used inside `diffWorktreeAgainstHEAD`, so the import stays.)

- [ ] **Step 8: Add compile-time interface assertion to `internal/logger.go`**

Add near the top of the file, right after the `type Logger struct { ... }` declaration:

```go
import "github.com/rockholla/gitspork/internal/types"

// Compile-time assertion that Logger satisfies types.Logger.
var _ types.Logger = (*Logger)(nil)
```

Add the `types` import to the existing import block (or create one if there isn't one — currently `internal/logger.go` has an existing import block).

- [ ] **Step 9: Update references across `internal/*.go`**

Every occurrence of the following identifiers needs a `types.` prefix (or the containing file needs to import `github.com/rockholla/gitspork/internal/types`). Do this via targeted edits, not global sed — the following files each need the `types` import added and the identifiers prefixed:

Files that reference moved types (add `import "github.com/rockholla/gitspork/internal/types"` to the imports and prefix as shown):

| File | Identifiers to prefix with `types.` |
|---|---|
| `internal/integrate.go` | `IntegrateOptions`, `IntegrateResult`, `IntegratedUpstream`, `UpstreamSpec`, `GitSporkDownstreamState`, `GitSporkUpstreamState` |
| `internal/integrate-local.go` | `IntegrateLocalOptions`, `IntegrateResult`, `IntegratedUpstream` |
| `internal/check-drift.go` | `CheckDriftOptions`, `DriftReport`, `DriftedFile`, `UpstreamSpec`, `IntegrateOptions`, `ErrDriftDetected` |
| `internal/integrator_downstream-owned.go` | (nothing type-wise, but Logger signature updates — see Step 10) |
| `internal/integrator_shared-ownership-merged.go` | (Logger signature only) |
| `internal/integrator_shared-ownership-structured-prefer-downstream.go` | (Logger signature only) |
| `internal/integrator_shared-ownership-structured-prefer-upstream.go` | (Logger signature only) |
| `internal/integrator_templated.go` | `GitSporkConfigTemplated` stays local (still in gitspork.go); Logger signature only |
| `internal/integrator_upstream-owned.go` | (Logger signature only) |
| `internal/init.go` | (Logger signature only) |
| `internal/upstream-delta.go` | (Logger signature only) |
| `internal/integrate_test.go` | `IntegrateOptions`, `IntegrateResult`, `IntegratedUpstream`, `UpstreamSpec`, `GitSporkDownstreamState`, `GitSporkUpstreamState` |
| `internal/check-drift_test.go` | `CheckDriftOptions`, `DriftReport`, `DriftedFile`, `UpstreamSpec`, `GitSporkDownstreamState`, `ErrDriftDetected` |

- [ ] **Step 10: Update Logger param types in signatures**

Every function signature currently accepting `logger *Logger` (the concrete internal type) becomes `logger types.Logger` (the interface). Specific spots:

| File:Line | Old signature | New signature |
|---|---|---|
| `internal/integrate.go:46` | `Integrate(items []T, upstreamPath string, downstreamPath string, logger *Logger) error` | `Integrate(items []T, upstreamPath string, downstreamPath string, logger types.Logger) error` |
| `internal/integrate.go:53` | `Integrate(instructions []GitSporkConfigTemplated, upstreamPath string, downstreamPath string, forceRePrompt bool, logger *Logger) error` | replace `*Logger` with `types.Logger` |
| `internal/integrate.go:241` | `func integrate(gitSporkConfig *GitSporkConfig, upstreamPath string, downstreamPath string, forceRePrompt bool, forDriftCheck bool, logger *Logger) error` | same replacement |
| `internal/integrator_upstream-owned.go:15` | `func (i *IntegratorUpstreamOwned) Integrate(entries []OwnedEntry, upstreamPath string, downstreamPath string, logger *Logger) error` | same |
| `internal/integrator_downstream-owned.go:18` | same shape | same |
| `internal/integrator_shared-ownership-merged.go:28` | same shape | same |
| `internal/integrator_shared-ownership-structured-prefer-downstream.go:16` | same shape | same |
| `internal/integrator_shared-ownership-structured-prefer-upstream.go:16` | same shape | same |
| `internal/integrator_templated.go:33` | same shape | same |
| `internal/upstream-delta.go:236` | `func applyUpstreamDelta(delta *upstreamDelta, downstreamPath string, logger *Logger) error` | same |
| `internal/init.go:14` | `func Init(initPath string, logger *Logger) error` | same |

Each of these files needs the `types` import added.

- [ ] **Step 11: Update references in `cmd/*.go`**

The CLI constructs options structs and calls entry points. All references to types that moved need the `types.` prefix. Specific replacements:

| File | Replacements |
|---|---|
| `cmd/integrate.go` | `&internal.IntegrateOptions{...}` → `&types.IntegrateOptions{...}`; add `import "github.com/rockholla/gitspork/internal/types"` |
| `cmd/integrate-local.go` | `&internal.IntegrateLocalOptions{...}` → `&types.IntegrateLocalOptions{...}`; add types import |
| `cmd/check-drift.go` | `&internal.CheckDriftOptions{...}` → `&types.CheckDriftOptions{...}`; `internal.ErrDriftDetected` → `types.ErrDriftDetected`; add types import |

`internal.ParseUpstreamFlag`, `internal.Integrate`, `internal.IntegrateLocal`, `internal.CheckDrift`, `internal.NewLogger`, etc. stay as `internal.*` for now — they'll relocate to subpackages in later tasks. Only the types moved to `types` in this task change form.

- [ ] **Step 12: Update test files that construct options**

`internal/integrate_test.go` and `internal/check-drift_test.go` construct `IntegrateOptions`, `CheckDriftOptions`, etc. directly. These references need the `types.` prefix.

Note: because these test files are in `package internal`, they don't need a package-qualified prefix for internal symbols. But since the types moved to a separate package, they do need the prefix now.

Add `import "github.com/rockholla/gitspork/internal/types"` to each test file.

Specific occurrences to update in `internal/integrate_test.go`:
- `TestIntegrate_honors_UpstreamRepoCommit`: `&IntegrateOptions{...}` → `&types.IntegrateOptions{...}`
- `TestIntegrate_returns_result_with_upstream_url_and_hash`: `&IntegrateOptions{...}` → `&types.IntegrateOptions{...}`, `[]UpstreamSpec{...}` → `[]types.UpstreamSpec{...}`
- `TestIntegrateLocal_returns_result_with_upstream_paths`: `&IntegrateLocalOptions{...}` → `&types.IntegrateLocalOptions{...}`

And in `internal/check-drift_test.go`:
- `TestCheckDrift` sub-tests: `&CheckDriftOptions{...}` → `&types.CheckDriftOptions{...}`; also `&GitSporkDownstreamState{...}` → `&types.GitSporkDownstreamState{...}`
- New Phase-1 tests: same replacements plus `types.ErrDriftDetected` if referenced

Also in `internal/gitspork_test.go` — the test file references `GitSporkDownstreamState` — needs the prefix.

- [ ] **Step 13: Build and verify**

```bash
go build ./...
```

Expected: clean. If any file misses an import or prefix, the compiler will point to the exact spot.

```bash
make test-unit
```

Expected: PASS.

```bash
make test-functional
```

Expected: PASS.

- [ ] **Step 14: Commit**

```bash
git add internal/types/ internal/gitspork.go internal/check-drift.go internal/check-drift_test.go internal/logger.go internal/integrate.go internal/integrate-local.go internal/integrate_test.go internal/integrator_*.go internal/init.go internal/upstream-delta.go internal/gitspork_test.go cmd/integrate.go cmd/integrate-local.go cmd/check-drift.go
git commit -m "refactor: extract types package for SDK-facing vocabulary + Logger interface"
```

---

## Task 2: Create `internal/logutil/` — concrete Logger and colorize helper

**Goal:** Extract the concrete Logger and `ColorizeYAML` from `internal/logger.go` into a new `internal/logutil/` package. The concrete type stays available for the CLI to use (which needs `Fatal()`); internal packages continue using the `types.Logger` interface.

**Files:**
- Create: `internal/logutil/logger.go`
- Create: `internal/logutil/colorize.go`
- Delete: `internal/logger.go`
- Modify: `cmd/root.go` (Logger reference)
- Modify: `cmd/init.go`, `cmd/schema.go` (ColorizeYAML references)

- [ ] **Step 1: Create `internal/logutil/logger.go`** with the concrete Logger type moved from `internal/logger.go`:

```go
// Package logutil provides gitspork's concrete Logger implementation used by
// the CLI binary. It satisfies internal/types.Logger. SDK consumers can pass
// this or any other types.Logger implementation (including nil for silent).
package logutil

import (
	"log"
	"os"

	"github.com/fatih/color"

	"github.com/rockholla/gitspork/internal/types"
)

// Logger writes log/error messages to stdout/stderr with ANSI color when
// stdout is a TTY. Its Log and Error methods satisfy types.Logger; Fatal
// stays concrete because it terminates the process and isn't needed by SDK
// consumers.
type Logger struct {
	infoLogger  *log.Logger
	errorLogger *log.Logger
}

// Compile-time assertion.
var _ types.Logger = (*Logger)(nil)

// New returns a Logger configured for the CLI.
func New() *Logger {
	return &Logger{
		infoLogger:  log.New(os.Stdout, color.New(color.FgHiBlue, color.Bold).Sprint("INFO: "), 0),
		errorLogger: log.New(os.Stderr, color.New(color.FgHiRed, color.Bold).Sprint("ERROR: "), 0),
	}
}

// Log writes an informational message to stdout.
func (l *Logger) Log(msg string, v ...any) {
	l.infoLogger.Printf(msg, v...)
}

// Error writes an error message to stderr.
func (l *Logger) Error(msg string, v ...any) {
	l.errorLogger.Printf(msg, v...)
}

// Fatal writes an error message to stderr and exits with code 1.
func (l *Logger) Fatal(msg string, v ...any) {
	l.errorLogger.Fatalf(msg, v...)
}
```

- [ ] **Step 2: Create `internal/logutil/colorize.go`** with the `ColorizeYAML` function moved from `internal/logger.go`. Copy the entire `ColorizeYAML` function verbatim, only changing the package to `logutil` and ensuring imports are correct. Read `internal/logger.go` current content lines 37–55 for the exact function body; wrap in the new file:

```go
package logutil

import (
	// same imports the current internal/logger.go uses for ColorizeYAML
	// (verify with `grep -A 2 "^import" internal/logger.go` before this step)
)

// ColorizeYAML returns src with YAML syntax colorized for TTY display.
// Color is suppressed automatically when stdout is not a TTY.
func ColorizeYAML(src string) string {
	// (moved verbatim from internal/logger.go)
}
```

- [ ] **Step 3: Delete `internal/logger.go`**

```bash
git rm internal/logger.go
```

- [ ] **Step 4: Update `cmd/root.go`**

Change line 19:
```go
logger  *internal.Logger
```
to:
```go
logger  *logutil.Logger
```

Update line 45 and 48:
```go
logger = internal.NewLogger()
```
to:
```go
logger = logutil.New()
```

Replace the `github.com/rockholla/gitspork/internal` import with `github.com/rockholla/gitspork/internal/logutil` (or add logutil alongside — check whether cmd/root.go still needs internal for other symbols. If yes, keep both imports; if no, replace).

- [ ] **Step 5: Update `cmd/init.go`**

Replace `internal.ColorizeYAML` → `logutil.ColorizeYAML` (2 occurrences on line 38). Add `import "github.com/rockholla/gitspork/internal/logutil"`. Keep the `internal` import — the file still calls `internal.GetGitSporkConfigSchema` and `internal.Init` (they'll relocate in Task 3).

- [ ] **Step 6: Update `cmd/schema.go`**

Replace `internal.ColorizeYAML` → `logutil.ColorizeYAML` (2 occurrences on lines 32, 33). Add the logutil import.

- [ ] **Step 7: Build and test**

```bash
go build ./...
make test-unit
make test-functional
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/logutil/ internal/logger.go cmd/root.go cmd/init.go cmd/schema.go
git commit -m "refactor: extract logutil package with concrete Logger and ColorizeYAML"
```

(`internal/logger.go` is captured in the `git add` as a deletion via `git rm` from Step 3.)

---

## Task 3: Create `internal/config/` — GitSporkConfig types, parsers, schema, mv/rm, init, owned-entry

**Goal:** Move all config-domain code out of `internal/` into a new `internal/config/` subpackage.

**Files:**
- Create dir: `internal/config/`
- Move: `internal/gitspork.go` → `internal/config/config.go` (contents restructured — see below)
- Move: `internal/gitspork_test.go` → `internal/config/config_test.go`
- Move: `internal/init.go` → `internal/config/init.go`
- Move: `internal/init_test.go` → `internal/config/init_test.go`
- Move: `internal/owned-entry.go` → `internal/config/owned_entry.go` (underscore convention within the new package)
- Move: `internal/owned-entry_test.go` → `internal/config/owned_entry_test.go`
- Move: `internal/upstream-mv-rm.go` → `internal/config/upstream_mv_rm.go`
- Move: `internal/upstream-mv-rm_test.go` → `internal/config/upstream_mv_rm_test.go`
- Modify: `internal/integrate.go`, `internal/integrate-local.go`, `internal/check-drift.go`, `internal/integrator_*.go`, `internal/upstream-delta.go`, and their tests (update references to config-owned symbols)
- Modify: `cmd/*.go` (update `internal.<config symbol>` → `config.<config symbol>`)

- [ ] **Step 1: Create the new directory and move the files with `git mv`**

```bash
mkdir -p internal/config
git mv internal/gitspork.go       internal/config/config.go
git mv internal/gitspork_test.go  internal/config/config_test.go
git mv internal/init.go           internal/config/init.go
git mv internal/init_test.go      internal/config/init_test.go
git mv internal/owned-entry.go    internal/config/owned_entry.go
git mv internal/owned-entry_test.go internal/config/owned_entry_test.go
git mv internal/upstream-mv-rm.go internal/config/upstream_mv_rm.go
git mv internal/upstream-mv-rm_test.go internal/config/upstream_mv_rm_test.go
```

- [ ] **Step 2: Update the package declaration in every moved file**

Change `package internal` → `package config` in all 8 files.

- [ ] **Step 3: Move constants out of `internal/config/config.go` that are still needed by non-config packages**

The file previously known as `internal/gitspork.go` defines these constants at the top:

```go
const (
	gitSpork                  string = "gitspork"
	gitSSHUsername            string = "git"
	gitSporkConfigFileName    string = ".gitspork.yml"
	gitSporkConfigFileNameAlt string = ".gitspork.yaml"
	gitSporkMarkerSeparator   string = "::"
)

var (
	gitSporkCommentMarker string = fmt.Sprintf("%s%s%s", gitSporkMarkerSeparator, gitSpork, gitSporkMarkerSeparator)
)
```

`gitSpork`, `gitSporkConfigFileName`, `gitSporkConfigFileNameAlt`, `gitSporkMarkerSeparator`, and `gitSporkCommentMarker` are used by integrate/drift/logutil code as well as config. Export them so they can be imported from `internal/config`:

Rename in `internal/config/config.go`:
- `gitSpork` → `GitSpork`
- `gitSSHUsername` → `GitSSHUsername`
- `gitSporkConfigFileName` → `GitSporkConfigFileName`
- `gitSporkConfigFileNameAlt` → `GitSporkConfigFileNameAlt`
- `gitSporkMarkerSeparator` → `GitSporkMarkerSeparator`
- `gitSporkCommentMarker` → `GitSporkCommentMarker`

Update all references to these constants throughout the codebase to use `config.GitSpork` / `config.GitSporkConfigFileName` etc.

- [ ] **Step 4: Update all cross-package references**

Files that previously used config-owned symbols (`GitSporkConfig`, `ParseGitSporkConfig`, `WriteGitSporkConfig`, `GetGitSporkConfigSchema`, `FindGitSporkConfig`, `OwnedEntry`, `ComputeUpstreamMv`, `ComputeUpstreamRm`, `ComputeUpstreamMvFromConfig`, `ComputeUpstreamRmFromConfig`, `UpstreamMv`, `UpstreamRm`, `Init`, `ParseMigrationConfig`, and the `GitSporkConfig*` sub-types) need `config.` prefixes and a `github.com/rockholla/gitspork/internal/config` import.

Files needing this update (inside internal/):
- `internal/integrate.go` — uses `GitSporkConfig`, `ParseGitSporkConfig`, `GitSporkConfigMigration`, `ParseMigrationConfig`
- `internal/integrate-local.go` — uses `getGitSporkConfig` helper (currently in gitspork.go — verify with `grep -n "func getGitSporkConfig" internal/*.go` before Step 1). If `getGitSporkConfig` is lowercase, export it as `config.GetGitSporkConfig` when moving.
- `internal/integrator_templated.go` — uses `GitSporkConfigTemplated`, `GitSporkConfigTemplatedInput`, `GitSporkConfigTemplatedInputPrevious`
- `internal/upstream-delta.go` — uses `GitSporkConfig`, `OwnedEntry`
- All test files that touch these types

Files in cmd/ needing this update:
- `cmd/init.go` — `internal.GetGitSporkConfigSchema` → `config.GetGitSporkConfigSchema`, `internal.Init` → `config.Init`
- `cmd/schema.go` — `internal.GetGitSporkConfigSchema` → `config.GetGitSporkConfigSchema`
- `cmd/mv.go` — `internal.FindGitSporkConfig` → `config.FindGitSporkConfig`, `internal.ComputeUpstreamMv` → `config.ComputeUpstreamMv`, `internal.ComputeUpstreamMvFromConfig` → `config.ComputeUpstreamMvFromConfig`, `internal.WriteGitSporkConfig` → `config.WriteGitSporkConfig`
- `cmd/rm.go` — similar: `internal.FindGitSporkConfig`, `internal.ComputeUpstreamRm`, `internal.ComputeUpstreamRmFromConfig`, `internal.WriteGitSporkConfig`

Add `import "github.com/rockholla/gitspork/internal/config"` to each.

- [ ] **Step 5: Check for any remaining unexported symbols in `internal/config/` that non-config code depends on**

Run:
```bash
grep -rn "internal/config" internal/ cmd/
```
to find every import site, then verify each qualified reference points to an exported symbol. Any lowercase-first-letter references from outside `internal/config/` will fail to compile — export those symbols and rename callsites accordingly.

Likely candidates to check and export:
- `collapsePlainOwnedEntries` (helper used by schema output — should stay lowercase if only used within config)

- [ ] **Step 6: Build and test**

```bash
go build ./...
make test-unit
make test-functional
make test-examples
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/ internal/ cmd/
git commit -m "refactor: extract config package (GitSporkConfig, parsers, schema, mv/rm, init, owned-entry)"
```

---

## Task 4: Create `internal/integrate/` — Integrate, IntegrateLocal, integrators, upstream-delta, state I/O, URL normalization

**Goal:** Move all integrate-domain code out of `internal/` into `internal/integrate/`. Export state I/O and URL normalization for `internal/drift/` to use in Task 5.

**Files:**
- Create dir: `internal/integrate/`
- Move: `internal/integrate.go` → `internal/integrate/integrate.go`
- Move: `internal/integrate_test.go` → `internal/integrate/integrate_test.go`
- Move: `internal/integrate-local.go` → `internal/integrate/integrate_local.go`
- Move: `internal/integrator_downstream-owned.go` → `internal/integrate/integrator_downstream_owned.go`
- Move: `internal/integrator_shared-ownership-merged.go` → `internal/integrate/integrator_shared_ownership_merged.go`
- Move: `internal/integrator_shared-ownership-structured-prefer-downstream.go` → `internal/integrate/integrator_shared_ownership_structured_prefer_downstream.go`
- Move: `internal/integrator_shared-ownership-structured-prefer-upstream.go` → `internal/integrate/integrator_shared_ownership_structured_prefer_upstream.go`
- Move: `internal/integrator_templated.go` → `internal/integrate/integrator_templated.go`
- Move: `internal/integrator_upstream-owned.go` → `internal/integrate/integrator_upstream_owned.go`
- Move: `internal/upstream-delta.go` → `internal/integrate/upstream_delta.go`
- Move: `internal/upstream-delta_test.go` → `internal/integrate/upstream_delta_test.go`
- Create: `internal/integrate/state.go` (extracted state I/O)
- Create: `internal/integrate/url.go` (extracted URL normalization)
- Modify: `internal/check-drift.go` (update references to now-external symbols)
- Modify: `cmd/*.go` (update `internal.<integrate symbol>` → `integrate.<integrate symbol>`)

- [ ] **Step 1: Move files**

```bash
mkdir -p internal/integrate
git mv internal/integrate.go        internal/integrate/integrate.go
git mv internal/integrate_test.go   internal/integrate/integrate_test.go
git mv internal/integrate-local.go  internal/integrate/integrate_local.go
git mv internal/integrator_downstream-owned.go internal/integrate/integrator_downstream_owned.go
git mv internal/integrator_shared-ownership-merged.go internal/integrate/integrator_shared_ownership_merged.go
git mv internal/integrator_shared-ownership-structured-prefer-downstream.go internal/integrate/integrator_shared_ownership_structured_prefer_downstream.go
git mv internal/integrator_shared-ownership-structured-prefer-upstream.go internal/integrate/integrator_shared_ownership_structured_prefer_upstream.go
git mv internal/integrator_templated.go internal/integrate/integrator_templated.go
git mv internal/integrator_upstream-owned.go internal/integrate/integrator_upstream_owned.go
git mv internal/upstream-delta.go   internal/integrate/upstream_delta.go
git mv internal/upstream-delta_test.go internal/integrate/upstream_delta_test.go
```

- [ ] **Step 2: Update package declaration in every moved file**

Change `package internal` → `package integrate` in all 11 files.

- [ ] **Step 3: Split state I/O out into `internal/integrate/state.go` and export symbols drift will need**

Cut these functions from `internal/integrate/integrate.go` and paste into a new `internal/integrate/state.go`. Rename to exported forms:

- `func loadDownstreamState(downstreamRepoPath string) (*types.GitSporkDownstreamState, error)` → `func LoadDownstreamState(downstreamRepoPath string) (*types.GitSporkDownstreamState, error)`
- `func saveDownstreamState(downstreamRepoPath string, state *types.GitSporkDownstreamState) error` → `func SaveDownstreamState(downstreamRepoPath string, state *types.GitSporkDownstreamState) error`
- `func upsertUpstreamState(state *types.GitSporkDownstreamState, entry types.GitSporkUpstreamState)` → `func UpsertUpstreamState(state *types.GitSporkDownstreamState, entry types.GitSporkUpstreamState)`
- `func migrationCompletedInDownstream(migrationID string, downstreamRepoPath string) (bool, error)` — stays lowercase (only integrate package uses it), keep in state.go or integrate.go — engineer's choice

Update every callsite of these functions within `internal/integrate/` to the new exported names.

The `state.go` file becomes:

```go
package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rockholla/gitspork/internal/config"
	"github.com/rockholla/gitspork/internal/types"
)

const downstreamStateFileName = "downstream-state.json"

// LoadDownstreamState reads the .gitspork/downstream-state.json file at
// downstreamRepoPath. Migrates deprecated single-upstream fields into the
// Upstreams slice on first load.
func LoadDownstreamState(downstreamRepoPath string) (*types.GitSporkDownstreamState, error) {
	// (existing body, updated to use exported names)
}

// SaveDownstreamState writes state back to .gitspork/downstream-state.json.
func SaveDownstreamState(downstreamRepoPath string, state *types.GitSporkDownstreamState) error {
	// (existing body)
}

// UpsertUpstreamState inserts or updates an upstream entry in state.Upstreams,
// matched by normalized URL + subpath.
func UpsertUpstreamState(state *types.GitSporkDownstreamState, entry types.GitSporkUpstreamState) {
	// (existing body, calling NormalizeUpstreamURL — see url.go)
}

// migrationCompletedInDownstream stays package-private.
func migrationCompletedInDownstream(migrationID string, downstreamRepoPath string) (bool, error) {
	// (existing body)
}
```

- [ ] **Step 4: Split URL normalization out into `internal/integrate/url.go`**

Cut `normalizeUpstreamURL` from `internal/integrate/integrate.go` and rename to `NormalizeUpstreamURL` (exported) in a new file:

```go
package integrate

import (
	"regexp"
	"strings"
)

var (
	reSSHURL    = regexp.MustCompile(`^git@([^:]+):(.+)$`)
	reHTTPProto = regexp.MustCompile(`^https?://`)
)

// NormalizeUpstreamURL returns a canonicalized form of rawURL+subpath so that
// SSH and HTTPS variants of the same repo match to a single key.
func NormalizeUpstreamURL(rawURL string, subpath string) string {
	// (existing body — currently at internal/integrate.go:84–98)
}
```

Update all callers within `internal/integrate/` to use `NormalizeUpstreamURL` (uppercase).

Also update the caller in `internal/check-drift.go` — that's an inter-package call in this task's scope; the drift package doesn't exist yet, but check-drift.go is still where CheckDrift lives. Update its reference from `normalizeUpstreamURL(...)` to `integrate.NormalizeUpstreamURL(...)`, and add an import for `internal/integrate` there. It'll be moved to `internal/drift/` in Task 5.

- [ ] **Step 5: Update `ParseUpstreamFlag` for now**

`ParseUpstreamFlag` currently lives in `internal/integrate.go` (Phase 1 code). It's a CLI-flag parser — Task 6 relocates it to `internal/cli/`. For this task, leave it in `internal/integrate/integrate.go` (or move to its own file `internal/integrate/upstream_flag.go` if the engineer prefers). Update callers in `cmd/*.go`:

`internal.ParseUpstreamFlag` → `integrate.ParseUpstreamFlag`

- [ ] **Step 6: Update `internal/check-drift.go`**

`internal/check-drift.go` at this point is the last remaining file in `package internal`. Its references to now-external symbols need updates:

- `Integrate(...)` → `integrate.Integrate(...)`
- `IntegrateOptions{...}` → `types.IntegrateOptions{...}` (already done in Task 1, verify)
- `loadDownstreamState(...)` → `integrate.LoadDownstreamState(...)`
- `normalizeUpstreamURL(...)` → `integrate.NormalizeUpstreamURL(...)`

Add imports for `github.com/rockholla/gitspork/internal/integrate` and `github.com/rockholla/gitspork/internal/types` (types import should already be present from Task 1).

- [ ] **Step 7: Update `cmd/*.go`**

Replace references to integrate-domain symbols:
- `cmd/integrate.go` — `internal.Integrate` → `integrate.Integrate`, `internal.ParseUpstreamFlag` → `integrate.ParseUpstreamFlag`
- `cmd/integrate-local.go` — `internal.IntegrateLocal` → `integrate.IntegrateLocal`
- `cmd/check-drift.go` — `internal.ParseUpstreamFlag` → `integrate.ParseUpstreamFlag`

Add `import "github.com/rockholla/gitspork/internal/integrate"` to each.

- [ ] **Step 8: Build and test**

```bash
go build ./...
make test-unit
make test-functional
make test-functional-docker
make test-examples
```

Expected: all PASS. If a functional test regressed on stdout, verify progress log ordering wasn't changed inadvertently.

- [ ] **Step 9: Commit**

```bash
git add internal/integrate/ internal/check-drift.go cmd/integrate.go cmd/integrate-local.go cmd/check-drift.go
git commit -m "refactor: extract integrate package (Integrate, IntegrateLocal, integrators, delta, state, url normalization)"
```

---

## Task 5: Create `internal/drift/` — CheckDrift and supporting helpers

**Goal:** Move `CheckDrift` and its helpers out of `internal/` into `internal/drift/`.

**Files:**
- Create dir: `internal/drift/`
- Move: `internal/check-drift.go` → `internal/drift/check_drift.go`
- Move: `internal/check-drift_test.go` → `internal/drift/check_drift_test.go`
- Modify: `cmd/check-drift.go` (update `internal.CheckDrift` → `drift.CheckDrift`)

- [ ] **Step 1: Move files**

```bash
mkdir -p internal/drift
git mv internal/check-drift.go      internal/drift/check_drift.go
git mv internal/check-drift_test.go internal/drift/check_drift_test.go
```

- [ ] **Step 2: Update package declaration**

Change `package internal` → `package drift` in both files.

- [ ] **Step 3: Verify imports inside `internal/drift/check_drift.go`**

The file at this point imports:
- `github.com/rockholla/gitspork/internal/types` (for CheckDriftOptions, DriftReport, DriftedFile, UpstreamSpec, IntegrateOptions, ErrDriftDetected, GitSporkDownstreamState)
- `github.com/rockholla/gitspork/internal/integrate` (for Integrate, LoadDownstreamState, NormalizeUpstreamURL)
- Standard library + go-git

Any leftover unqualified references from the old `package internal` days will fail to compile — fix them by qualifying with the correct subpackage.

- [ ] **Step 4: Update test file imports**

`internal/drift/check_drift_test.go` references:
- Test helpers `testCommitAll`, `testMinimalUpstream`, `testEmptyDownstream` — these live in `internal/integrate/integrate_test.go`. Since drift and integrate are now separate packages, test helpers can't be shared across package boundaries in `_test.go` files unless they're exported (uppercase) and callers import from an external test file.

Two options:
- **Option A (preferred): move shared helpers to `internal/testharness/`** as exported functions. Then both `internal/integrate/integrate_test.go` and `internal/drift/check_drift_test.go` import them.
- **Option B: duplicate the helpers** into `internal/drift/check_drift_test.go`. Faster but violates DRY.

Choose Option A. Add these to `internal/testharness/testharness.go` (existing file) as exported functions:

```go
// MinimalUpstream initializes a local upstream git repo with a minimal
// .gitspork.yml (upstream_owned only, no templated block) and one file.
// Returns the temp dir and the initial commit hash. Test helper.
func MinimalUpstream(t *testing.T) (string, plumbing.Hash) {
	// (adapt from internal/integrate/integrate_test.go's testMinimalUpstream)
}

// EmptyDownstream initializes a bare local downstream git repo.
// Test helper.
func EmptyDownstream(t *testing.T) string {
	// (adapt from internal/integrate/integrate_test.go's testEmptyDownstream)
}

// CommitAllWithMessage stages and commits all changes in repo with the given
// message, returning the resulting commit hash. Test helper.
func CommitAllWithMessage(t *testing.T, repo *gogit.Repository, message string) plumbing.Hash {
	// (adapt from internal/integrate/integrate_test.go's testCommitAll)
}
```

Then update:
- `internal/integrate/integrate_test.go`: replace `testMinimalUpstream(t)` with `testharness.MinimalUpstream(t)`, etc. Add `import "github.com/rockholla/gitspork/internal/testharness"` if not present.
- `internal/drift/check_drift_test.go`: same replacements, plus its own helpers `testIntegrateAndCommitBaseline` and `testWriteAndCommitInDownstream` stay local to that file (they call the shared testharness helpers now).

Note: `testCommitAll` in `internal/integrate/integrate_test.go` shadows the new `testharness.CommitAllWithMessage` — pick a single naming convention. The existing `testCommitAll` name conflicts with the existing `testharness.CommitAll(t, repo, dir, message)` signature (different args — takes `dir` too). Introduce a new name `testharness.CommitAllWithMessage(t, repo, message)` to avoid confusion. Update callers accordingly.

- [ ] **Step 5: Update `cmd/check-drift.go`**

`internal.CheckDrift` → `drift.CheckDrift`. Add `import "github.com/rockholla/gitspork/internal/drift"`. Remove `internal` import if no longer needed.

- [ ] **Step 6: Build and test**

```bash
go build ./...
make test-unit
make test-functional
make test-functional-docker
make test-examples
```

Expected: all PASS.

- [ ] **Step 7: Verify `internal/` is now empty (except subdirs)**

```bash
ls internal/*.go 2>/dev/null
```

Expected: no output. All `.go` files in `internal/` should now live inside a subpackage.

- [ ] **Step 8: Commit**

```bash
git add internal/drift/ internal/testharness/ internal/integrate/integrate_test.go cmd/check-drift.go
git commit -m "refactor: extract drift package (CheckDrift and helpers); move shared test helpers to testharness"
```

---

## Task 6: Move `cmd/` → `internal/cli/` and relocate `ParseUpstreamFlag`

**Goal:** Move all CLI cobra command definitions out of `cmd/` and into `internal/cli/`. Also move `ParseUpstreamFlag` from `internal/integrate/` to `internal/cli/` since it's a CLI-flag parser.

**Files:**
- Create dir: `internal/cli/`
- Move: `cmd/check-drift.go` → `internal/cli/check_drift.go`
- Move: `cmd/init.go` → `internal/cli/init.go`
- Move: `cmd/integrate-local.go` → `internal/cli/integrate_local.go`
- Move: `cmd/integrate.go` → `internal/cli/integrate.go`
- Move: `cmd/mv.go` → `internal/cli/mv.go`
- Move: `cmd/rm.go` → `internal/cli/rm.go`
- Move: `cmd/root.go` → `internal/cli/root.go`
- Move: `cmd/schema.go` → `internal/cli/schema.go`
- Create: `internal/cli/upstream_flag.go` (extracted from `internal/integrate/`)
- Create: `internal/cli/upstream_flag_test.go` (extracted `Test_ParseUpstreamFlag`)
- Modify: `internal/integrate/integrate.go` (remove `ParseUpstreamFlag`)
- Modify: `internal/integrate/integrate_test.go` (remove `Test_ParseUpstreamFlag`)
- Modify: `main.go` (import `internal/cli` and call `cli.Execute(version)`)
- Delete dir: `cmd/`

- [ ] **Step 1: Move all cmd files**

```bash
mkdir -p internal/cli
git mv cmd/check-drift.go     internal/cli/check_drift.go
git mv cmd/init.go            internal/cli/init.go
git mv cmd/integrate-local.go internal/cli/integrate_local.go
git mv cmd/integrate.go       internal/cli/integrate.go
git mv cmd/mv.go              internal/cli/mv.go
git mv cmd/rm.go              internal/cli/rm.go
git mv cmd/root.go            internal/cli/root.go
git mv cmd/schema.go          internal/cli/schema.go
```

- [ ] **Step 2: Update package declaration**

Change `package cmd` → `package cli` in all 8 files.

- [ ] **Step 3: Move `ParseUpstreamFlag` from integrate to cli**

Cut `ParseUpstreamFlag` (currently in `internal/integrate/integrate.go`) and its test `Test_ParseUpstreamFlag` (in `internal/integrate/integrate_test.go`). Paste into new files:

`internal/cli/upstream_flag.go`:
```go
package cli

import (
	"fmt"
	"strings"

	"github.com/rockholla/gitspork/internal/types"
)

// ParseUpstreamFlag parses a comma-separated key=value --upstream flag value.
// Valid keys: url (required), version, subpath, token.
func ParseUpstreamFlag(val string) (types.UpstreamSpec, error) {
	// (moved verbatim from internal/integrate/integrate.go, references
	// UpstreamSpec via types.UpstreamSpec)
}
```

`internal/cli/upstream_flag_test.go`:
```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rockholla/gitspork/internal/types"
)

func Test_ParseUpstreamFlag(t *testing.T) {
	// (moved verbatim from internal/integrate/integrate_test.go, with type
	// refs qualified as types.UpstreamSpec)
}
```

- [ ] **Step 4: Update CLI callers of `ParseUpstreamFlag`**

In `internal/cli/integrate.go` and `internal/cli/check_drift.go`, `integrate.ParseUpstreamFlag(f)` becomes `ParseUpstreamFlag(f)` (same package now). Remove any now-unused `github.com/rockholla/gitspork/internal/integrate` imports if this was the only symbol used from it (unlikely — integrate is still called for `Integrate`).

- [ ] **Step 5: Update `main.go`**

```go
package main

import "github.com/rockholla/gitspork/internal/cli"

var (
	version = "dev"
)

func main() {
	cli.Execute(version)
}
```

- [ ] **Step 6: Delete now-empty `cmd/` directory**

```bash
rmdir cmd
```

If the directory is not empty (e.g., stale files git wouldn't track), inspect and remove those files or move them explicitly with `git mv`.

- [ ] **Step 7: Update any references to `github.com/rockholla/gitspork/cmd`**

Verify nothing else in the repo imports `cmd`:

```bash
grep -rn "\"github.com/rockholla/gitspork/cmd\"" .
```

Expected: no matches. If matches exist, update them to `internal/cli`.

- [ ] **Step 8: Build and test**

```bash
go build ./...
make test-unit
make test-functional
make test-functional-docker
make test-examples
```

Expected: all PASS.

If functional tests fail because `main_test.go` (the functional-test harness) built the binary via `go build .` — this should still work since main.go is still at the repo root. The change from Phase 3 (moving main.go under `cmd/gitspork/`) is NOT part of Phase 2.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/ internal/integrate/integrate.go internal/integrate/integrate_test.go main.go
git commit -m "refactor: move cmd/ to internal/cli/ and relocate ParseUpstreamFlag"
```

---

## Task 7: Final verification and cleanup

- [ ] **Step 1: Run every test suite**

```bash
make test-unit
make test-functional
make test-functional-docker
make test-examples
```

Expected: all PASS. If any suite fails, treat as a regression from earlier tasks — don't proceed until fixed.

- [ ] **Step 2: Verify no stale `internal` package references remain**

```bash
grep -rn "\"github.com/rockholla/gitspork/internal\"" .
```

Expected: no matches. Every consumer should now import a specific subpackage (`internal/types`, `internal/config`, `internal/integrate`, `internal/drift`, `internal/logutil`, `internal/cli`, `internal/input`, or `internal/testharness`).

- [ ] **Step 3: Confirm final directory layout matches spec target**

```bash
ls internal/
```

Expected:
```
cli  config  drift  input  integrate  logutil  testharness  types
```

- [ ] **Step 4: Run `go vet ./...`**

Expected: clean.

- [ ] **Step 5: Manual sanity check**

Build the CLI binary and run a quick integrate + check-drift roundtrip:

```bash
go build -o /tmp/gitspork-phase2 .
# Small upstream/downstream setup:
/tmp/gitspork-phase2 --help
/tmp/gitspork-phase2 schema
```

Expected: output looks the same as pre-refactor.

- [ ] **Step 6: Commit any final tweaks**

If Steps 1–5 exposed missed spots, fix them and commit as `fix: post-refactor cleanup for phase 2`. Do NOT amend earlier commits.

---

## Backward Compatibility

- CLI invocations unchanged. Every flag, exit code, and downstream filesystem effect preserved.
- Downstream state files (`.gitspork/downstream-state.json`) unchanged.
- Public API: no library exposure yet — `internal/types/` is still under `internal/` and gets promoted to `package gitspork` in Phase 3.
- Module path unchanged (still `github.com/rockholla/gitspork` on v1.x). Module path bump to `/v2` happens in Phase 3.
