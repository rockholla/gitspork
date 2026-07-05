# SDK Phase 3: Expose the SDK as `github.com/rockholla/gitspork/v2` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote the SDK-facing vocabulary from `internal/types/` up to a new `package gitspork` at the module root, bump the module path to `github.com/rockholla/gitspork/v2` (Go semantic import versioning for major v2+), relocate `main.go` under `cmd/gitspork/`, and add a black-box `test/sdk/` tier that imports the SDK as an external caller would.

**Architecture:** Third and final phase of the SDK effort. Phases 1 and 2 reshaped return signatures and split `internal/` into subpackages, leaving `internal/types/` as the shared vocabulary parking spot. Phase 3 promotes that package to the module root as `package gitspork` and cleans up the exported surface (rename state types, hide internal-only options fields, add doc-comments/deprecation markers) so v2.0.0 ships a stable, documented SDK.

**Tech Stack:** Go 1.26, existing dependencies unchanged. Module path changes from `github.com/rockholla/gitspork` to `github.com/rockholla/gitspork/v2` (SIV requirement).

---

## File Structure

### Before (post-Phase-2 main)

```
├── main.go                          # package main; imports internal/cli
├── go.mod                           # module github.com/rockholla/gitspork
├── internal/
│   ├── cli/
│   ├── config/
│   ├── drift/
│   ├── input/
│   ├── integrate/
│   ├── logutil/
│   ├── testharness/
│   └── types/                       # will move to module root
└── test/
    ├── examples/
    └── functional/
```

### After (Phase 3 target)

```
├── gitspork.go                      # package gitspork; Integrate, IntegrateLocal, CheckDrift thin coordinators
├── options.go                       # package gitspork; Options structs, UpstreamSpec
├── results.go                       # package gitspork; IntegrateResult, IntegratedUpstream, DriftReport, DriftedFile
├── logger.go                        # package gitspork; Logger interface + NoopLogger
├── errors.go                        # package gitspork; ErrDriftDetected sentinel
├── state.go                         # package gitspork; DownstreamState, UpstreamState (moved from internal/types/state.go)
├── doc.go                           # package gitspork; package-level docs + usage example
├── go.mod                           # module github.com/rockholla/gitspork/v2
├── cmd/
│   └── gitspork/
│       └── main.go                  # package main; the 6-line CLI entry
├── internal/                        # unchanged directory layout minus types/
│   ├── cli/
│   ├── config/
│   ├── drift/
│   ├── input/
│   ├── integrate/
│   ├── logutil/
│   └── testharness/
└── test/
    ├── examples/
    ├── functional/                  # main_test.go build command updated
    └── sdk/                         # new: black-box SDK tests
```

### API surface changes (public `package gitspork` after Phase 3)

Compared to `internal/types/` today:

| Change | Before | After | Rationale |
|---|---|---|---|
| Rename | `GitSporkDownstreamState` | `DownstreamState` | Avoid `gitspork.GitSporkDownstreamState` stutter |
| Rename | `GitSporkUpstreamState` | `UpstreamState` | Same |
| Remove | `IntegrateOptions.UpstreamRepoURL/Version/Subpath/Token` | (gone) | CLI reconstructs `Upstreams` from flags |
| Remove | `IntegrateOptions.UpstreamRepoCommit`, `ForDriftCheck`, `PrevUpstreamCommitHash` | (gone) | Moved to `internal/integrate.IntegrateForDriftCheck` — internal wiring |
| Remove | `IntegrateLocalOptions.UpstreamPath` | (gone) | CLI reconstructs `UpstreamPaths` from flags |
| Keep | `IntegrateOptions.Upstreams`, `DownstreamRepoPath`, `ForceRePrompt`, `Logger` | (unchanged) | Public SDK surface |
| Keep | `IntegrateLocalOptions.UpstreamPaths`, `DownstreamPath`, `ForceRePrompt`, `Logger` | (unchanged) | Public SDK surface |
| Keep | `CheckDriftOptions.Upstreams`, `DownstreamRepoPath`, `Logger` | (unchanged) | Public SDK surface |
| Keep | Everything in `Logger`, `NoopLogger`, `IntegrateResult`, `DriftReport`, `DriftedFile`, `UpstreamSpec`, `ErrDriftDetected` | (unchanged) | Already clean |

---

## Task 1: API prep — rename state types, add doc-comments and Deprecated markers

**Goal:** Reduce diff noise in later tasks by doing pure-rename and doc-only edits while types still live in `internal/types/`. No behavior changes.

**Files:**
- Modify: `internal/types/state.go` (rename types, add doc comments)
- Modify: `internal/types/options.go` (add Deprecated markers, add doc comments)
- Modify: `internal/types/results.go` (add doc comments on non-nil invariant, last-writer-wins)
- Modify: every file that references the two state types (there are ~15 across `internal/`, `test/functional/`, tests)

- [ ] **Step 1: Rename `GitSporkDownstreamState` → `DownstreamState` in `internal/types/state.go`**

Replace the entire file with:

```go
package types

// DownstreamState is the on-disk state stored at
// .gitspork/downstream-state.json in the downstream repo. It records each
// integrated upstream so subsequent runs (integrate, check-drift) can locate
// the previous commit hash and detect drift.
type DownstreamState struct {
	MigrationsComplete []string        `json:"migrations_complete"`
	Upstreams          []UpstreamState `json:"upstreams,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamRepoURL string `json:"last_upstream_repo_url,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamCommitHash string `json:"last_upstream_commit_hash,omitempty"`
}

// UpstreamState records the last integration for a single upstream.
type UpstreamState struct {
	URL        string `json:"url"`
	Subpath    string `json:"subpath,omitempty"`
	CommitHash string `json:"commit_hash"`
}
```

- [ ] **Step 2: Sweep-rename `GitSporkDownstreamState` → `DownstreamState` and `GitSporkUpstreamState` → `UpstreamState` across the repo**

The two type names appear in these files (grep to double-check the current set before editing):

```bash
grep -rln "GitSporkDownstreamState\|GitSporkUpstreamState" --include="*.go" .
```

Expected locations (all in `internal/`):
- `internal/config/config.go` and its test — the parsed state I/O uses these types
- `internal/config/init.go` — writes a `DownstreamState{}` in some paths
- `internal/integrate/integrate.go` — state I/O signatures + callers
- `internal/integrate/integrate_test.go` — tests that construct state values
- `internal/drift/check_drift.go` — reads state via `integrate.LoadDownstreamState`
- `internal/drift/check_drift_test.go` — tests that construct state values (`saveDownstreamState`, etc.)

For each file, replace `GitSporkDownstreamState` → `DownstreamState` and `GitSporkUpstreamState` → `UpstreamState`. The `types.` package prefix stays the same; only the type name changes.

- [ ] **Step 3: Add doc-comment for non-nil invariant on `IntegrateResult` and `DriftReport`**

In `internal/types/results.go`, replace the doc comments on `IntegrateResult` and `DriftReport` with:

```go
// IntegrateResult is the structural return value of Integrate and IntegrateLocal.
// It records what was successfully integrated (in order); on partial failure
// the successful upstreams so far are still present in this result alongside
// the returned error.
//
// The returned *IntegrateResult is always non-nil — callers do not need to
// nil-check before inspecting Upstreams.
type IntegrateResult struct {
	Upstreams []IntegratedUpstream
}
```

```go
// DriftReport is the structural return value of CheckDrift. HasDrift is false
// when the downstream matches the recorded integration state; true when
// differences were found. Files enumerates the drifted entries with per-file
// attribution to whichever upstream last wrote each path.
//
// When two upstreams write the same file, AttributedURL on the corresponding
// DriftedFile records the last-writing upstream — matching the last-writer-wins
// semantics of multi-upstream integrate.
//
// The returned *DriftReport is always non-nil — callers do not need to
// nil-check before inspecting HasDrift or Files.
type DriftReport struct {
	HasDrift bool
	Files    []DriftedFile
}
```

- [ ] **Step 4: Clarify local-path overload on `IntegratedUpstream.URL`**

Also in `internal/types/results.go`, update the `IntegratedUpstream` doc comment:

```go
// IntegratedUpstream identifies a single successfully integrated upstream.
// For Integrate, URL is the remote repo URL (SSH or HTTPS, whichever the
// caller supplied). For IntegrateLocal, URL is the local filesystem path with
// no scheme, and CommitHash is empty (local paths have no commit-hash concept).
type IntegratedUpstream struct {
	URL        string
	Subpath    string
	CommitHash string
}
```

- [ ] **Step 5: Add Deprecated markers on legacy single-upstream options fields**

In `internal/types/options.go`, add `// Deprecated:` markers so `godoc`/`staticcheck` flag callers of the legacy fields:

```go
// IntegrateOptions configures a call to Integrate. Populate Upstreams (one
// or more entries) for multi-upstream integration.
type IntegrateOptions struct {
	Upstreams          []UpstreamSpec
	DownstreamRepoPath string
	ForceRePrompt      bool
	Logger             Logger
	// Task 2 removes ForDriftCheck / PrevUpstreamCommitHash / UpstreamRepo* from
	// this struct. Until then, they carry legacy-flag data from the CLI and
	// drift-check re-integration wiring.
	ForDriftCheck          bool
	PrevUpstreamCommitHash string
	// Deprecated: set Upstreams instead. The CLI accepts --upstream-repo-url
	// for backward compatibility and translates internally.
	UpstreamRepoURL string
	// Deprecated: set Upstreams instead.
	UpstreamRepoVersion string
	// Deprecated: set Upstreams instead.
	UpstreamRepoSubpath string
	// Deprecated: set Upstreams instead.
	UpstreamRepoToken string
	// Deprecated: internal drift-check wiring only.
	UpstreamRepoCommit string
}

// IntegrateLocalOptions configures a call to IntegrateLocal. Populate
// UpstreamPaths (one or more entries) for multi-path integration.
type IntegrateLocalOptions struct {
	UpstreamPaths  []string
	DownstreamPath string
	ForceRePrompt  bool
	Logger         Logger
	// Deprecated: set UpstreamPaths instead. The CLI accepts a single
	// --upstream-path for backward compatibility and translates internally.
	UpstreamPath string
}
```

- [ ] **Step 6: Build and test**

```bash
go build ./...
make test-unit
make test-functional
```

Expected: all PASS. Deprecation warnings (`★` markers in gopls / staticcheck) on the migration code paths that read the deprecated fields are expected — legitimate reads.

- [ ] **Step 7: Commit**

```bash
git add internal/types/state.go internal/types/options.go internal/types/results.go internal/config/ internal/integrate/ internal/drift/
git commit -m "refactor: rename state types + document non-nil invariant + Deprecated markers"
```

---

## Task 2: Move internal-only fields off public IntegrateOptions

**Goal:** Move `ForDriftCheck`, `PrevUpstreamCommitHash`, `UpstreamRepoCommit`, and the legacy `UpstreamRepo*` fields off `IntegrateOptions` and `IntegrateLocalOptions.UpstreamPath` off `IntegrateLocalOptions`. The public options structs end at 4 fields each. The CLI reconstructs `Upstreams`/`UpstreamPaths` from its old-style flags. Drift uses a new `internal/integrate.IntegrateForDriftCheck` function.

**Files:**
- Modify: `internal/types/options.go` (delete internal/deprecated fields)
- Create: `internal/integrate/drift_check.go` (new `IntegrateForDriftCheck` function)
- Modify: `internal/integrate/integrate.go` (remove legacy-field synthesis + ForDriftCheck branching; extract shared body used by both `Integrate` and `IntegrateForDriftCheck`)
- Modify: `internal/integrate/integrate_local.go` (remove `UpstreamPath` legacy synthesis)
- Modify: `internal/drift/check_drift.go` (call `integrate.IntegrateForDriftCheck` instead of `integrate.Integrate` with option flags)
- Modify: `internal/cli/integrate.go` (reconstruct `Upstreams` from `--upstream-repo-url` etc. before calling `integrate.Integrate`)
- Modify: `internal/cli/integrate_local.go` (accept multi `--upstream-path`; construct `UpstreamPaths` from flag)
- Modify: `internal/integrate/integrate_test.go` (adapt any test that used legacy fields)

- [ ] **Step 1: Add `IntegrateForDriftCheck` in `internal/integrate/drift_check.go`**

```go
package integrate

import (
	"fmt"

	"github.com/rockholla/gitspork/internal/types"
)

// DriftCheckRequest is the internal request shape used by drift-check
// re-integration. External SDK consumers should use Integrate; DriftCheckRequest
// is a package-integrate contract intended only for internal/drift.
type DriftCheckRequest struct {
	Logger             types.Logger
	DownstreamRepoPath string
	UpstreamURL        string
	UpstreamSubpath    string
	UpstreamToken      string
	UpstreamCommit     string
}

// IntegrateForDriftCheck runs a single-upstream integrate pinned to a specific
// commit hash and skips the state write. It's used by internal/drift to
// reconstruct the downstream at each recorded upstream's last-integrated
// commit and then diff against HEAD.
func IntegrateForDriftCheck(req *DriftCheckRequest) error {
	if req.Logger == nil {
		req.Logger = types.NoopLogger()
	}
	upstream := types.UpstreamSpec{
		URL:     req.UpstreamURL,
		Subpath: req.UpstreamSubpath,
		Token:   req.UpstreamToken,
	}
	if _, err := integrateOneInternal(&internalRequest{
		Logger:                 req.Logger,
		DownstreamRepoPath:     req.DownstreamRepoPath,
		forDriftCheck:          true,
		upstreamCommit:         req.UpstreamCommit,
	}, upstream); err != nil {
		return fmt.Errorf("drift-check re-integration failed: %v", err)
	}
	return nil
}
```

Where `internalRequest` (see Step 3) and `integrateOneInternal` (see Step 4) are unexported.

- [ ] **Step 2: Introduce `internalRequest` struct in `internal/integrate/integrate.go`**

At the top of `internal/integrate/integrate.go`, near the other type declarations, add:

```go
// internalRequest carries wiring needed by integrateOneInternal that is not
// part of the public IntegrateOptions surface. It exists to keep the SDK's
// IntegrateOptions minimal while still allowing drift-check to signal special
// behavior.
type internalRequest struct {
	Logger                 types.Logger
	DownstreamRepoPath     string
	ForceRePrompt          bool
	forDriftCheck          bool   // true = skip state write, skip delta
	upstreamCommit         string // when forDriftCheck: the pinned commit
	prevUpstreamCommitHash string // set by integrateOne between calls
}
```

- [ ] **Step 3: Extract `integrateOneInternal` from current `integrateOne`**

Refactor `integrateOne` in `internal/integrate/integrate.go`. The current function:

```go
func integrateOne(opts *types.IntegrateOptions, upstream types.UpstreamSpec) (types.IntegratedUpstream, error) {
	// ... uses opts.ForDriftCheck, opts.UpstreamRepoCommit, opts.PrevUpstreamCommitHash ...
}
```

Becomes two functions. First, a thin wrapper that adapts the public `IntegrateOptions`:

```go
func integrateOne(opts *types.IntegrateOptions, upstream types.UpstreamSpec) (types.IntegratedUpstream, error) {
	return integrateOneInternal(&internalRequest{
		Logger:                 opts.Logger,
		DownstreamRepoPath:     opts.DownstreamRepoPath,
		ForceRePrompt:          opts.ForceRePrompt,
		forDriftCheck:          false, // public Integrate never sets this
		upstreamCommit:         "",
		prevUpstreamCommitHash: "",
	}, upstream)
}
```

Then `integrateOneInternal` — a paste of the current `integrateOne` body with every `opts.ForDriftCheck` → `req.forDriftCheck`, `opts.UpstreamRepoCommit` → `req.upstreamCommit`, `opts.PrevUpstreamCommitHash` → `req.prevUpstreamCommitHash`, `opts.Logger` → `req.Logger`, `opts.DownstreamRepoPath` → `req.DownstreamRepoPath`, `opts.ForceRePrompt` → `req.ForceRePrompt`:

```go
func integrateOneInternal(req *internalRequest, upstream types.UpstreamSpec) (types.IntegratedUpstream, error) {
	// (body copied verbatim from current integrateOne, with the substitutions above)
}
```

Note: the inner `singleOpts := &types.IntegrateOptions{...}` at ~line 175 also needs updating — it currently sets `ForDriftCheck: opts.ForDriftCheck`, `UpstreamRepoCommit: opts.UpstreamRepoCommit`, etc. Since we're removing those from `IntegrateOptions`, that pattern breaks. Instead, use a nested `internalRequest`:

```go
singleReq := &internalRequest{
	Logger:                 req.Logger,
	DownstreamRepoPath:     req.DownstreamRepoPath,
	ForceRePrompt:          req.ForceRePrompt,
	forDriftCheck:          req.forDriftCheck,
	upstreamCommit:         req.upstreamCommit,
	prevUpstreamCommitHash: prevHash,
}
```

And update the recursive-ish `cloneUpstreamForIntegrate(cloneDir, singleReq)` call to accept `*internalRequest` instead of `*types.IntegrateOptions`.

**This is the biggest edit in Task 2.** Read `integrateOne` end-to-end first, then apply the substitution mechanically. The function is ~85 lines currently; the internal version is the same length.

- [ ] **Step 4: Update `cloneUpstreamForIntegrate` signature**

`cloneUpstreamForIntegrate` currently takes `*types.IntegrateOptions`. Change to `*internalRequest`:

```go
func cloneUpstreamForIntegrate(cloneDir string, req *internalRequest) (string, error) {
	// (body updated: opts.* → req.*)
}
```

Every field reference inside the function body: `opts.Logger` → `req.Logger`, `opts.UpstreamRepoURL` → (extract from upstream spec via req — see note below), etc.

Note: `cloneUpstreamForIntegrate` also reads `opts.UpstreamRepoURL`, `opts.UpstreamRepoVersion`, `opts.UpstreamRepoSubpath`, `opts.UpstreamRepoToken` — those came from the legacy fields. Now these come from the `upstream types.UpstreamSpec` parameter, which is already passed to `integrateOneInternal`. Restructure so `cloneUpstreamForIntegrate` takes both `*internalRequest` AND the `types.UpstreamSpec` explicitly:

```go
func cloneUpstreamForIntegrate(cloneDir string, req *internalRequest, upstream types.UpstreamSpec) (string, error) {
	// use upstream.URL, upstream.Version, upstream.Subpath, upstream.Token here
	// use req.Logger, req.upstreamCommit for the commit pin
}
```

Update all callsites to pass both.

- [ ] **Step 5: Remove legacy field synthesis + `Integrate` public shape**

Now that `IntegrateOptions` no longer has `UpstreamRepoURL` etc., the "synthesize from legacy fields" block at the top of `Integrate` can go. `Integrate` becomes:

```go
// Integrate will ensure that the downstream at opts.DownstreamRepoPath is
// integrated with each upstream in opts.Upstreams, in order.
func Integrate(opts *types.IntegrateOptions) (*types.IntegrateResult, error) {
	result := &types.IntegrateResult{}

	if opts.Logger == nil {
		opts.Logger = types.NoopLogger()
	}

	if opts.DownstreamRepoPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return result, fmt.Errorf("unable to get the present working directory: %v", err)
		}
		opts.DownstreamRepoPath = wd
	} else {
		abs, err := filepath.Abs(opts.DownstreamRepoPath)
		if err != nil {
			return result, fmt.Errorf("unable to determine local downstream repo path: %v", err)
		}
		opts.DownstreamRepoPath = abs
	}

	if len(opts.Upstreams) == 0 {
		return result, fmt.Errorf("no upstream specified: set Upstreams on IntegrateOptions")
	}

	for _, upstream := range opts.Upstreams {
		integrated, err := integrateOne(opts, upstream)
		if err != nil {
			return result, err
		}
		result.Upstreams = append(result.Upstreams, integrated)
	}
	return result, nil
}
```

Delete `IntegrateOptions.ForDriftCheck`, `.PrevUpstreamCommitHash`, `.UpstreamRepoURL`, `.UpstreamRepoVersion`, `.UpstreamRepoSubpath`, `.UpstreamRepoToken`, `.UpstreamRepoCommit` from `internal/types/options.go`. The public `IntegrateOptions` is now 4 fields.

- [ ] **Step 6: Same treatment for `IntegrateLocalOptions.UpstreamPath`**

In `internal/types/options.go`, remove `UpstreamPath` from `IntegrateLocalOptions`. In `internal/integrate/integrate_local.go`, remove the `if len(opts.UpstreamPaths) == 0 && opts.UpstreamPath != ""` synthesis block.

- [ ] **Step 7: Update `internal/drift/check_drift.go` to use `IntegrateForDriftCheck`**

Currently:

```go
if _, err := integrate.Integrate(&types.IntegrateOptions{
    Logger:              opts.Logger,
    UpstreamRepoURL:     entry.spec.URL,
    UpstreamRepoSubpath: entry.spec.Subpath,
    UpstreamRepoToken:   entry.spec.Token,
    UpstreamRepoCommit:  entry.commitHash,
    DownstreamRepoPath:  opts.DownstreamRepoPath,
    ForDriftCheck:       true,
}); err != nil {
    return report, fmt.Errorf("error running integration for drift check: %v", err)
}
```

Replace with:

```go
if err := integrate.IntegrateForDriftCheck(&integrate.DriftCheckRequest{
    Logger:             opts.Logger,
    DownstreamRepoPath: opts.DownstreamRepoPath,
    UpstreamURL:        entry.spec.URL,
    UpstreamSubpath:    entry.spec.Subpath,
    UpstreamToken:      entry.spec.Token,
    UpstreamCommit:     entry.commitHash,
}); err != nil {
    return report, fmt.Errorf("error running integration for drift check: %v", err)
}
```

- [ ] **Step 8: Update `internal/cli/integrate.go` — reconstruct Upstreams from CLI flags**

Currently the CLI sets `opts.UpstreamRepoURL = upstreamRepoURL`, etc. Change to construct `Upstreams`:

```go
opts := &types.IntegrateOptions{
    Logger:             logger,
    DownstreamRepoPath: downstreamRepoPath,
    ForceRePrompt:      forceRePrompt,
}
if upstreamRepoURL != "" && len(upstreamFlags) > 0 {
    return fmt.Errorf("cannot mix --upstream with --upstream-repo-url/version/subpath/token flags")
}
if upstreamRepoURL != "" {
    opts.Upstreams = []types.UpstreamSpec{{
        URL:     upstreamRepoURL,
        Version: upstreamRepoVersion,
        Subpath: upstreamRepoSubpath,
        Token:   upstreamRepoToken,
    }}
}
for _, f := range upstreamFlags {
    spec, err := ParseUpstreamFlag(f)
    if err != nil {
        return err
    }
    opts.Upstreams = append(opts.Upstreams, spec)
}
if _, err := integrate.Integrate(opts); err != nil {
    return err
}
return nil
```

The current `cmd/integrate.go` has this exact logic partly — verify the actual state and adapt. The key change: `UpstreamRepoURL`/`Version`/`Subpath`/`Token` are used to build `Upstreams` at the CLI layer, not passed into `Integrate` as legacy fields.

- [ ] **Step 9: Update `internal/cli/integrate_local.go` — reconstruct UpstreamPaths from CLI flag**

Similar shape: the `--upstream-path` flag is already repeatable (`StringArrayVarP`), so the current CLI already provides a `[]string`. Just verify the CLI passes it directly to `opts.UpstreamPaths` without any legacy `opts.UpstreamPath = firstEntry` synthesis. Adjust if needed.

- [ ] **Step 10: Update `internal/integrate/integrate_test.go` for removed fields**

Any test that constructs `types.IntegrateOptions{UpstreamRepoURL: ...}` or `types.IntegrateOptions{ForDriftCheck: true}` needs updating:
- Callsites that used legacy single-upstream form → construct `Upstreams []UpstreamSpec` instead
- Callsites that used `ForDriftCheck: true` → replace with `integrate.IntegrateForDriftCheck(...)` if that's the intent, or drop entirely

Specific case: `TestIntegrate_honors_UpstreamRepoCommit` at the top of the test file — this test relied on setting `UpstreamRepoCommit: commitV1.String()` and `ForDriftCheck: true` on IntegrateOptions. Rewrite as a call to `IntegrateForDriftCheck`:

```go
err = IntegrateForDriftCheck(&DriftCheckRequest{
    Logger:             logutil.New(),
    DownstreamRepoPath: downstreamDir,
    UpstreamURL:        "file://" + upstreamDir,
    UpstreamCommit:     commitV1.String(),
})
```

- [ ] **Step 11: Build and test**

```bash
go build ./...
make test-unit
make test-functional
make test-functional-docker
make test-examples
```

Expected: all PASS. The public API surface for `IntegrateOptions`, `IntegrateLocalOptions` is now the 4-field / 4-field shape shown in the file structure section above.

- [ ] **Step 12: Commit**

```bash
git add internal/types/options.go internal/integrate/ internal/drift/ internal/cli/
git commit -m "refactor: hide integrate internal wiring behind IntegrateForDriftCheck; slim IntegrateOptions to 4 public fields"
```

---

## Task 3: Honor `Logger == nil` as silent throughout

**Goal:** Route the remaining stdout-bypass paths through the Logger interface so `Logger: nil` genuinely silences all output. Two categories:

1. `fmt.Println("")` blank-line writes in `internal/integrate/integrate.go`
2. go-git's clone `Progress: os.Stdout` in `cloneUpstreamForIntegrate`

**Files:**
- Modify: `internal/integrate/integrate.go` (replace bare `fmt.Println("")` with Logger.Log or delete)
- Modify: `internal/integrate/integrate.go` (wrap clone `Progress` with a Logger-backed io.Writer)
- Create: `internal/logutil/writer.go` — an `io.Writer` adapter over `types.Logger`

- [ ] **Step 1: Create the writer adapter in `internal/logutil/writer.go`**

```go
package logutil

import (
	"strings"

	"github.com/rockholla/gitspork/internal/types"
)

// LoggerWriter is an io.Writer that forwards each line written to it to a
// types.Logger. Trailing newlines are trimmed since Logger implementations
// handle line boundaries themselves.
type LoggerWriter struct {
	L types.Logger
}

// Write splits input on newlines and calls L.Log for each non-empty line.
// If L is nil, Write is a no-op (bytes still accepted but discarded).
func (w *LoggerWriter) Write(p []byte) (int, error) {
	if w == nil || w.L == nil {
		return len(p), nil
	}
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return len(p), nil
	}
	for _, line := range strings.Split(text, "\n") {
		w.L.Log("%s", line)
	}
	return len(p), nil
}
```

- [ ] **Step 2: Replace bare `fmt.Println("")` calls in `internal/integrate/integrate.go`**

Grep for the call sites:

```bash
grep -n 'fmt\.Println("")' internal/integrate/integrate.go
```

Each occurrence is a blank line between sections of progress output. Options per site:
- **Preferred**: delete the blank line. Logger consumers can add their own spacing if desired; SDK-consumer stdout should not have gratuitous blank lines from library code.
- **Alternative**: replace with `req.Logger.Log("")` — passes through Logger, silent when nil.

Take the "delete" path unless a functional test depends on the blank line (grep the functional tests to confirm; expected: no dependency).

- [ ] **Step 3: Wrap go-git clone `Progress` in a Logger-backed writer**

Grep for the current `Progress:` reference:

```bash
grep -n "Progress:" internal/integrate/integrate.go
```

Currently likely:

```go
clone, err := git.PlainClone(cloneDir, false, &git.CloneOptions{
    URL:      resolveUpstreamURL(req.upstreamRepoURL, req.upstreamRepoToken),
    Progress: os.Stdout,
    // ...
})
```

Change `Progress: os.Stdout` to a Logger-backed writer:

```go
clone, err := git.PlainClone(cloneDir, false, &git.CloneOptions{
    URL:      resolveUpstreamURL(upstream.URL, upstream.Token),
    Progress: &logutil.LoggerWriter{L: req.Logger},
    // ...
})
```

Add `import "github.com/rockholla/gitspork/internal/logutil"` if not present. Note: creating a dependency edge `integrate → logutil`. This is fine — logutil already has no upward deps.

- [ ] **Step 4: Remove `"os"` import if no longer used** in `internal/integrate/integrate.go`.

- [ ] **Step 5: Build and test**

```bash
go build ./...
make test-unit
make test-functional
make test-functional-docker
make test-examples
```

Expected: all PASS. If any functional test asserts on blank-line output structure, adjust the assertion (blank lines are cosmetic).

- [ ] **Step 6: Commit**

```bash
git add internal/logutil/writer.go internal/integrate/integrate.go
git commit -m "refactor: route go-git Progress and blank-line output through Logger interface"
```

---

## Task 4: Promote `internal/types/` → `package gitspork` at module root; bump to `/v2`

**Goal:** The pivot task. Move every file from `internal/types/` to the module root as `package gitspork`. Add thin coordinator entry-point functions (`Integrate`, `IntegrateLocal`, `CheckDrift`). Bump `go.mod` to `github.com/rockholla/gitspork/v2`. Rewrite every internal import to use the `/v2` path. Move `main.go` under `cmd/gitspork/`. Update build system.

**Files:**
- Move: 7 files from `internal/types/` to module root, package rename `types` → `gitspork`
- Create: `gitspork.go` (public entry-point coordinators)
- Create: `doc.go` (package-level documentation)
- Move: `main.go` → `cmd/gitspork/main.go` (path only; still `package main`)
- Modify: `go.mod` (module path bump)
- Modify: every `.go` file that imports `github.com/rockholla/gitspork/...` (rewrite to `/v2` path)
- Modify: `.goreleaser.yaml`, `Dockerfile`, `Makefile`, `test/functional/main_test.go` (build path bump)

**Sequencing note:** This task is one atomic edit — intermediate steps will not compile. Do NOT run `go build` after individual steps; only Step 13 verifies the full end-state builds. In particular: as soon as any `package gitspork` file lives at the module root while `main.go` (`package main`) is still there, the root directory has two packages and Go rejects it. All steps land in a single commit.

- [ ] **Step 1: Move `internal/types/` files to module root and rename package**

```bash
git mv internal/types/logger.go       logger.go
git mv internal/types/options.go      options.go
git mv internal/types/results.go      results.go
git mv internal/types/state.go        state.go
git mv internal/types/errors.go       errors.go
git mv internal/types/noop_logger.go  noop_logger.go
```

- [ ] **Step 2: Change `package types` → `package gitspork` in each moved file**

Edit each of the 6 moved files: replace `package types` at line 1 with `package gitspork`.

- [ ] **Step 3: Delete the now-empty `internal/types/` directory**

```bash
rmdir internal/types 2>/dev/null || true
```

- [ ] **Step 4: Create `gitspork.go` — the public entry-point file**

At the module root, new file `gitspork.go`:

```go
package gitspork

import (
	"github.com/rockholla/gitspork/v2/internal/drift"
	"github.com/rockholla/gitspork/v2/internal/integrate"
)

// Integrate integrates one or more upstream repos into the downstream at
// opts.DownstreamRepoPath. See IntegrateOptions for configuration. On partial
// failure the returned *IntegrateResult still contains the upstreams that
// were successfully integrated before the error.
func Integrate(opts *IntegrateOptions) (*IntegrateResult, error) {
	return integrate.Integrate(opts)
}

// IntegrateLocal integrates one or more local upstream paths into the
// downstream at opts.DownstreamPath. Local integrations do not write to
// downstream state.
func IntegrateLocal(opts *IntegrateLocalOptions) (*IntegrateResult, error) {
	return integrate.IntegrateLocal(opts)
}

// CheckDrift re-runs each recorded upstream's integration at its pinned
// commit hash in an isolated copy of the downstream and reports any files
// that differ from the current downstream HEAD. Returns a populated
// *DriftReport alongside ErrDriftDetected when drift is found.
func CheckDrift(opts *CheckDriftOptions) (*DriftReport, error) {
	return drift.CheckDrift(opts)
}
```

- [ ] **Step 5: Create `doc.go` — package-level documentation and example**

```go
// Package gitspork exposes the top-level operations of the gitspork tool as a
// Go library. See https://github.com/rockholla/gitspork for the full CLI
// documentation.
//
// The three entry points are Integrate, IntegrateLocal, and CheckDrift. Each
// returns a structural result alongside an error, so consumers can inspect
// what was integrated or which files drifted without parsing log output.
//
// Example — check-drift bot:
//
//	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
//	    DownstreamRepoPath: "/path/to/downstream",
//	})
//	if err != nil && err != gitspork.ErrDriftDetected {
//	    log.Fatal(err)
//	}
//	for _, f := range report.Files {
//	    log.Printf("drifted: %s (attributed to %s)", f.Path, f.AttributedURL)
//	}
//
// Example — fleet integrator:
//
//	for _, downstream := range downstreamDirs {
//	    result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
//	        Upstreams: []gitspork.UpstreamSpec{{
//	            URL:     "git@github.com:org/platform.git",
//	            Version: "v1.2.0",
//	        }},
//	        DownstreamRepoPath: downstream,
//	    })
//	    if err != nil {
//	        log.Printf("failed on %s: %v", downstream, err)
//	        continue
//	    }
//	    log.Printf("%s: integrated %d upstream(s)", downstream, len(result.Upstreams))
//	}
package gitspork
```

- [ ] **Step 6: Update `go.mod` to `github.com/rockholla/gitspork/v2`**

Edit `go.mod` line 1:

```
module github.com/rockholla/gitspork/v2
```

- [ ] **Step 7: Rewrite every internal import to use the `/v2` path**

Every Go file that imports `github.com/rockholla/gitspork/...` needs `/v2` inserted. Grep the current set:

```bash
grep -rln "github.com/rockholla/gitspork" --include="*.go" .
```

Expected files (~30):
- Everything in `internal/cli/`, `internal/config/`, `internal/drift/`, `internal/integrate/`, `internal/logutil/`
- Everything in `test/functional/`, `test/examples/`
- `main.go`

For each file, replace:
- `"github.com/rockholla/gitspork/internal/types"` → `"github.com/rockholla/gitspork/v2"` (types package promoted to module root)
- `"github.com/rockholla/gitspork/internal/config"` → `"github.com/rockholla/gitspork/v2/internal/config"`
- `"github.com/rockholla/gitspork/internal/integrate"` → `"github.com/rockholla/gitspork/v2/internal/integrate"`
- `"github.com/rockholla/gitspork/internal/drift"` → `"github.com/rockholla/gitspork/v2/internal/drift"`
- `"github.com/rockholla/gitspork/internal/logutil"` → `"github.com/rockholla/gitspork/v2/internal/logutil"`
- `"github.com/rockholla/gitspork/internal/cli"` → `"github.com/rockholla/gitspork/v2/internal/cli"`
- `"github.com/rockholla/gitspork/internal/input"` → `"github.com/rockholla/gitspork/v2/internal/input"`
- `"github.com/rockholla/gitspork/internal/testharness"` → `"github.com/rockholla/gitspork/v2/internal/testharness"`

**Important**: also update every `types.<Type>` reference to `gitspork.<Type>` — since types moved from `package types` to `package gitspork`. The import alias changes:
- Was: `"github.com/rockholla/gitspork/internal/types"` → package `types` → `types.IntegrateOptions`
- Now: `"github.com/rockholla/gitspork/v2"` → package `gitspork` → `gitspork.IntegrateOptions`

Wide sweep. Use `sed` or a series of targeted `Edit` calls. Search:

```bash
grep -rn "types\." internal/ test/ | wc -l
```

to gauge scope. Do them file-by-file to avoid mistakes.

- [ ] **Step 8: Move `main.go` → `cmd/gitspork/main.go`**

```bash
mkdir -p cmd/gitspork
git mv main.go cmd/gitspork/main.go
```

Update the moved file's import path if needed (it currently imports `internal/cli` — this must become `github.com/rockholla/gitspork/v2/internal/cli` per Step 7). Verify:

```go
package main

import "github.com/rockholla/gitspork/v2/internal/cli"

var (
	version = "dev"
)

func main() {
	cli.Execute(version)
}
```

- [ ] **Step 9: Update `.goreleaser.yaml` build entries**

The two `builds:` entries (`- id: linux`, `- id: darwin`) currently rely on goreleaser's default (`go build .`). Add `main:` to each:

```yaml
builds:
  - id: linux
    main: ./cmd/gitspork
    env:
      - CGO_ENABLED=0
    # ...
  - id: darwin
    main: ./cmd/gitspork
    env:
      - CGO_ENABLED=0
    # ...
```

- [ ] **Step 10: Update `Makefile`**

The current Makefile has:

```makefile
schema:
	@go run main.go schema

build:
	@go build -o dist/gitspork .
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/.docker-build/gitspork .
```

Change to:

```makefile
schema:
	@go run ./cmd/gitspork schema

build:
	@go build -o dist/gitspork ./cmd/gitspork
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/.docker-build/gitspork ./cmd/gitspork
```

- [ ] **Step 11: Update `test/functional/main_test.go`**

The `buildBinary` and `buildDockerImageForTests` functions build with `go build ... .`. Update the build target from `.` to `./cmd/gitspork`:

```go
// In buildBinary:
cmd := exec.Command("go", "build", "-o", out, "./cmd/gitspork")

// In buildDockerImageForTests:
buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/gitspork")
```

- [ ] **Step 12: Dockerfile check**

The current `Dockerfile` (5 lines) doesn't build anything — it copies a pre-built `gitspork` binary. No changes needed.

- [ ] **Step 13: Build and test all suites**

```bash
go build ./...
go build ./cmd/gitspork
make test-unit
make test-functional
make test-functional-docker
make test-examples
```

Expected: all PASS. If a subpackage's import path is still stale, the build points directly at the file.

- [ ] **Step 14: Commit**

```bash
git add go.mod gitspork.go doc.go options.go results.go state.go logger.go errors.go noop_logger.go cmd/gitspork/main.go internal/ test/ Makefile .goreleaser.yaml
git commit -m "feat: promote SDK to package gitspork at module root; bump module path to /v2; relocate main.go under cmd/gitspork/"
```

---

## Task 5: Add `test/sdk/` black-box test tier

**Goal:** New test suite that imports `github.com/rockholla/gitspork/v2` as an external caller would. Validates the public API from outside the module's internals.

**Files:**
- Create: `test/sdk/sdk_test.go` — test entry file
- Create: `test/sdk/helpers_test.go` — helpers wrapping testharness
- Modify: `Makefile` (add `test-sdk` target)

- [ ] **Step 1: Add `make test-sdk` target to Makefile**

After `test-examples`:

```makefile
.PHONY: test-sdk
test-sdk: ## Run black-box SDK tests
	@go test -tags sdk -timeout 120s -v ./test/sdk/...
```

Also add `test-sdk` to `test-all`:

```makefile
.PHONY: test-all
test-all: test-unit test-security-gate test-functional test-functional-docker test-sdk
```

- [ ] **Step 2: Create `test/sdk/helpers_test.go`** — fixture helpers using testharness

```go
//go:build sdk

package sdk_test

import (
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"

	"github.com/rockholla/gitspork/v2/internal/testharness"
)

// minimalUpstream builds a local upstream git repo with a minimal .gitspork.yml
// (upstream_owned only) and one file. Returns the repo dir and its HEAD hash.
func minimalUpstream(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	return testharness.MinimalUpstream(t)
}

// emptyDownstream returns a fresh non-bare local downstream git repo dir.
func emptyDownstream(t *testing.T) string {
	t.Helper()
	return testharness.EmptyDownstream(t)
}

// writeAndCommit writes a file in downstreamDir and commits it, returning the
// resulting commit hash.
func writeAndCommit(t *testing.T, downstreamDir, relPath, content string) plumbing.Hash {
	t.Helper()
	full := filepath.Join(downstreamDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	return testharness.CommitAllWithMessage(t, repo, "edit: "+relPath)
}
```

Note: `package sdk_test` — external test package (import path ends in `_test`). This forces the file to import `github.com/rockholla/gitspork/v2` externally, mirroring what a real SDK consumer would do.

- [ ] **Step 3: Create `test/sdk/sdk_test.go`** — the black-box tests

```go
//go:build sdk

package sdk_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rockholla/gitspork/v2"
)

// integrate: single upstream returns a populated *IntegrateResult
func TestIntegrate_singleUpstream(t *testing.T) {
	upstreamDir, upstreamHash := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, "file://"+upstreamDir, result.Upstreams[0].URL)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)
}

// integrate: multi-upstream order is preserved in the result
func TestIntegrate_multiUpstreamOrder(t *testing.T) {
	upstreamA, hashA := minimalUpstream(t)
	upstreamB, hashB := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams: []gitspork.UpstreamSpec{
			{URL: "file://" + upstreamA, Version: "main"},
			{URL: "file://" + upstreamB, Version: "main"},
		},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.Len(t, result.Upstreams, 2)
	assert.Equal(t, "file://"+upstreamA, result.Upstreams[0].URL)
	assert.Equal(t, "file://"+upstreamB, result.Upstreams[1].URL)
	assert.Equal(t, hashA.String(), result.Upstreams[0].CommitHash)
	assert.Equal(t, hashB.String(), result.Upstreams[1].CommitHash)
}

// integrate-local: multi-path precedence; state file not written
func TestIntegrateLocal_multiPath_noStateFile(t *testing.T) {
	upstreamA, _ := minimalUpstream(t)
	upstreamB, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.IntegrateLocal(&gitspork.IntegrateLocalOptions{
		UpstreamPaths:  []string{upstreamA, upstreamB},
		DownstreamPath: downstreamDir,
	})
	require.NoError(t, err)
	require.Len(t, result.Upstreams, 2)

	// State file must NOT be written for IntegrateLocal.
	_, err = os_Stat_helper(downstreamDir, ".gitspork", "downstream-state.json")
	assert.True(t, err != nil, "expected no state file after IntegrateLocal, got err=%v", err)
}

// check-drift: no drift returns HasDrift=false and nil error
func TestCheckDrift_noDrift(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)

	// commit the integrated files so check-drift sees a clean tree
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")

	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.HasDrift)
	assert.Empty(t, report.Files)
}

// check-drift: drift returns HasDrift=true, populated Files, ErrDriftDetected sentinel
func TestCheckDrift_driftDetected_returnsErrSentinel(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")
	writeAndCommit(t, downstreamDir, "upstream-owned/file.txt", "drifted content\n")

	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
	})
	require.True(t, errors.Is(err, gitspork.ErrDriftDetected), "expected ErrDriftDetected, got %v", err)
	require.NotNil(t, report)
	assert.True(t, report.HasDrift)
	require.Len(t, report.Files, 1)
	assert.Equal(t, "upstream-owned/file.txt", report.Files[0].Path)
	assert.Equal(t, "file://"+upstreamDir, report.Files[0].AttributedURL)
	assert.Contains(t, report.Files[0].Diff, "upstream-owned/file.txt", "per-file diff should reference the path")
}

// Logger contract: nil Logger means silent (no panic, no output)
func TestLogger_nilIsSilent(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	// Pass Logger: nil explicitly.
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             nil,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	// Passing nil should not panic and should complete without error.
}

// Logger contract: custom implementation receives the internal progress calls
func TestLogger_customImplementation(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	captured := &captureLogger{}
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             captured,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, captured.entries, "custom Logger should receive progress calls")
}

// Error path: no upstreams and no override → error
func TestCheckDrift_noStateNoOverride_errors(t *testing.T) {
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitspork integrate")
}

// Error path: override URL not in state → error names the URL
func TestCheckDrift_overrideMissingState_errors(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")

	bogus := "file:///tmp/gitspork-never-integrated-sdk-test"
	_, err = gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
		Upstreams:          []gitspork.UpstreamSpec{{URL: bogus}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), bogus)
}

// captureLogger records the strings sent to it.
type captureLogger struct {
	entries []string
}

func (c *captureLogger) Log(msg string, args ...any)   { c.entries = append(c.entries, "log: "+msg) }
func (c *captureLogger) Error(msg string, args ...any) { c.entries = append(c.entries, "err: "+msg) }
```

Note: `os_Stat_helper` is a placeholder for a small helper — see Step 4.

- [ ] **Step 4: Add the `os_Stat_helper` helper to `test/sdk/helpers_test.go`**

```go
// os_Stat_helper checks whether a file exists relative to a base dir. Returns
// nil if it exists, error otherwise.
func os_Stat_helper(base string, parts ...string) (os.FileInfo, error) {
	return os.Stat(filepath.Join(append([]string{base}, parts...)...))
}
```

- [ ] **Step 5: Compile-time assertion that `logutil.Logger` satisfies the public Logger**

Add a small compile-time test in `test/sdk/sdk_test.go` — but since `logutil` is in `internal/`, external test consumers can't import it. Skip this check here; it's already asserted inside `logutil/logger.go`.

- [ ] **Step 6: Run the SDK tests**

```bash
make test-sdk
```

Expected: all PASS.

- [ ] **Step 7: Confirm no regression in other suites**

```bash
make test-unit
make test-functional
```

Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add test/sdk/ Makefile
git commit -m "test: add black-box test/sdk/ tier importing github.com/rockholla/gitspork/v2"
```

---

## Task 6: CLI polish — restore colored `--verbose` diff output

**Goal:** In Phase 1, `Logger.Diff(io.Reader)` was removed and `--verbose` output became plain-text unified diff. This task restores ANSI colorization at the CLI layer (headers bold, hunks cyan, additions green, removals red) while keeping the SDK's `DriftedFile.Diff` field plain.

**Files:**
- Modify: `internal/cli/check_drift.go` — colorize per-file `Diff` when `verbose` is set and stdout is a TTY

- [ ] **Step 1: Add a small colorizer helper in `internal/cli/check_drift.go`**

Near the top of `internal/cli/check_drift.go`, add:

```go
import (
	// existing imports…
	"strings"

	"github.com/fatih/color"
)

// colorizeUnifiedDiff applies ANSI colors to a unified diff string based on
// per-line prefix: `diff --git` and `+++`/`---` headers bold, `@@` hunks cyan,
// `+`/`-` change lines green/red. Falls through unchanged when color output
// is disabled (non-TTY / NO_COLOR).
func colorizeUnifiedDiff(diff string) string {
	if color.NoColor {
		return diff
	}
	var out strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			out.WriteString(color.New(color.Bold).Sprint(line))
		case strings.HasPrefix(line, "@@"):
			out.WriteString(color.CyanString(line))
		case strings.HasPrefix(line, "+"):
			out.WriteString(color.GreenString(line))
		case strings.HasPrefix(line, "-"):
			out.WriteString(color.RedString(line))
		default:
			out.WriteString(line)
		}
		out.WriteString("\n")
	}
	return out.String()
}
```

- [ ] **Step 2: Call the colorizer in the verbose print loop**

Find the verbose-print block in `internal/cli/check_drift.go` (currently prints `f.Diff` via `fmt.Print`):

```go
if verbose {
	for _, f := range report.Files {
		if f.Diff == "" {
			continue
		}
		fmt.Print(f.Diff)
	}
}
```

Change to:

```go
if verbose {
	for _, f := range report.Files {
		if f.Diff == "" {
			continue
		}
		fmt.Print(colorizeUnifiedDiff(f.Diff))
	}
}
```

- [ ] **Step 3: Build and test**

```bash
go build ./...
make test-functional
```

Expected: PASS. The functional test `TestCheckDrift_drift_detected` at `test/functional/check_drift_test.go` asserts on the file-path substring, which colorized output still contains. No test assertion changes needed.

- [ ] **Step 4: Manual sanity check**

```bash
go build -o /tmp/gitspork-phase3 ./cmd/gitspork
# Set up an integrate + drift scenario, then:
/tmp/gitspork-phase3 check-drift --downstream-repo-path <dir> --verbose | cat
```

Expected: with `| cat` (non-TTY), plain output. Without `| cat` (TTY), colored output with green additions, red removals, cyan hunks, bold headers.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/check_drift.go
git commit -m "feat: restore colored --verbose diff output in CLI (SDK output stays plain)"
```

---

## Task 7: Documentation updates

**Goal:** Update `CLAUDE.md`'s stale `resolveUpstreamURL` note, add the SDK availability announcement to README, and refresh `docs/README.md` with an SDK section.

**Files:**
- Modify: `CLAUDE.md` (fix stale resolveUpstreamURL description)
- Modify: `README.md` (add SDK announcement)
- Modify: `docs/README.md` (add SDK usage section)

- [ ] **Step 1: Fix `CLAUDE.md` stale note about `resolveUpstreamURL`**

Find the section that says `resolveUpstreamURL(url, token string)` "silently rewrites SSH↔HTTPS based on `SSH_AUTH_SOCK` and token presence". The `SSH_AUTH_SOCK` check does not exist in the current implementation — it's based on token presence only. Replace with:

```markdown
**URL rewriting:** `resolveUpstreamURL(url, token string)` in `internal/integrate/integrate.go` silently rewrites SSH↔HTTPS based on token presence: a token forces HTTPS rewrite; no token keeps SSH form (or, if given an HTTPS URL with no token, rewrites to SSH so key-auth flows). The caller (`CheckDrift`) selects which URL to pass (override or stored); the function only handles the protocol rewrite.
```

Also update any file-path references in `CLAUDE.md` that still say `internal/integrate.go` (should now be `internal/integrate/integrate.go`).

- [ ] **Step 2: Add SDK announcement to root `README.md`**

Under the "Getting Started" section (or a new "Programmatic Use" subsection at the top of the doc), add:

```markdown
## Programmatic use (Go SDK)

gitspork is also importable as a Go library:

```go
import gitspork "github.com/rockholla/gitspork/v2"
```

See `pkg.go.dev/github.com/rockholla/gitspork/v2` for the API reference. The three top-level operations mirror the CLI: `Integrate`, `IntegrateLocal`, and `CheckDrift`. Each returns a structural result so orchestrators and CI drift bots can consume outcomes without parsing log output.
```

- [ ] **Step 3: Add SDK usage section to `docs/README.md`**

After the "For Downstream Integrators" section, add:

```markdown
## Using gitspork as a Go SDK

The three top-level operations are exposed as a Go library at `github.com/rockholla/gitspork/v2`. Add it to your Go module:

```bash
go get github.com/rockholla/gitspork/v2
```

Import and call as in the CLI:

```go
package main

import (
    "log"

    gitspork "github.com/rockholla/gitspork/v2"
)

func main() {
    report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
        DownstreamRepoPath: "/path/to/downstream",
    })
    if err != nil && err != gitspork.ErrDriftDetected {
        log.Fatal(err)
    }
    for _, f := range report.Files {
        log.Printf("drifted: %s (attributed to %s)", f.Path, f.AttributedURL)
    }
}
```

The SDK returns structural data (`*DriftReport`, `*IntegrateResult`) so orchestrators and drift bots can consume outcomes programmatically. Pass `Logger: nil` on any Options struct to suppress internal progress output.
```

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md README.md docs/README.md
git commit -m "docs: announce Go SDK availability, fix stale resolveUpstreamURL note"
```

---

## Task 8: Final verification and cleanup

- [ ] **Step 1: Run every test suite**

```bash
make test-unit
make test-functional
make test-functional-docker
make test-examples
make test-sdk
```

Expected: all PASS.

- [ ] **Step 2: Grep for stale imports**

```bash
grep -rn "github.com/rockholla/gitspork\"" --include="*.go" .
```

Expected: no matches. Every import path in Go source ends with `/v2` or `/v2/internal/...`.

```bash
grep -rn "internal/types" --include="*.go" .
```

Expected: no matches. The `types` package no longer exists.

- [ ] **Step 3: Confirm directory layout matches target**

```bash
ls
```

Expected top level: `cmd/`, `docs/`, `dist/`, `internal/`, `scripts/`, `test/`, `Dockerfile`, `Makefile`, `README.md`, `go.mod`, `go.sum`, plus the new `.go` files at root: `gitspork.go`, `options.go`, `results.go`, `state.go`, `logger.go`, `errors.go`, `noop_logger.go`, `doc.go`.

Expected `internal/`: `cli/`, `config/`, `drift/`, `input/`, `integrate/`, `logutil/`, `testharness/`. Notably NO `types/`.

Expected `cmd/`: only `gitspork/main.go`.

- [ ] **Step 4: `go vet ./...` clean**

```bash
go vet ./...
```

- [ ] **Step 5: Manual sanity — SDK import from an external module**

In a scratch dir outside this repo:

```bash
mkdir /tmp/gitspork-sdk-smoke && cd /tmp/gitspork-sdk-smoke
cat > go.mod <<EOF
module smoketest
go 1.26
require github.com/rockholla/gitspork/v2 v2.0.0
EOF
cat > main.go <<'EOF'
package main
import gitspork "github.com/rockholla/gitspork/v2"
func main() {
    _ = &gitspork.IntegrateOptions{}
    _ = &gitspork.CheckDriftOptions{}
}
EOF
go build .
```

Expected: build succeeds. Note: this requires the v2.0.0 tag to actually be published, so this smoke test happens post-merge/post-tag. Before tag, `go get` will pin to the branch's commit; use `replace github.com/rockholla/gitspork/v2 => /path/to/this/repo` in the scratch go.mod for local verification.

- [ ] **Step 6: Manual CLI sanity**

```bash
go build -o /tmp/gitspork-phase3 ./cmd/gitspork
/tmp/gitspork-phase3 --help
/tmp/gitspork-phase3 schema
```

Expected: CLI works identically to pre-refactor.

- [ ] **Step 7: Commit any final tweaks**

If Steps 1–6 exposed missed spots, fix and commit as `fix: post-refactor cleanup for phase 3`.

---

## Backward Compatibility

| Scenario | Behavior after v2.0.0 |
|---|---|
| Existing CLI invocations (all flags including backward-compat `--upstream-repo-url` etc.) | Identical output, exit codes, and filesystem effects. CLI internally reconstructs `Upstreams` from legacy flags. |
| Existing downstream state files | Read and used unchanged; auto-migration of legacy single-upstream state fields continues to work. |
| Third-party Go code that imported `github.com/rockholla/gitspork/internal/...` | Never supported — Go's `internal/` restriction prevented this. No breakage risk. |
| Third-party Go code that will use the v2 SDK | Imports `github.com/rockholla/gitspork/v2` and calls the three exported entry points. Semver-stable from v2.0.0 forward. |
| Third-party Go code that wants config parsing, mv/rm, init, or schema helpers | Not exposed in v2.0.0. Additive minor version if requested later. |
| Old (v1.x) module path | Not automatically updated — v1 users must change their import path to `/v2` to pick up v2.0.0. Standard Go SIV behavior. |
