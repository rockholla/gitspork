# Golang SDK Design

## Goal

Expose gitspork's top-level operations (`integrate`, `integrate-local`, `check-drift`) as a public Go library so downstream Go projects can invoke them programmatically instead of shelling out to the CLI. Reshape the internals so callers receive structural results (drift reports, integration outcomes) rather than parsing log output. Preserve current CLI behavior — every existing invocation must continue producing the same exit codes, downstream filesystem effects, and functionally-equivalent stdout content.

## Architecture

The change is a three-phase refactor. Phase 1 reshapes return signatures inside today's `internal/` package so callers get structural data — no files move. Phase 2 splits the monolithic `internal/` into cohesive subpackages, relocates `cmd/` under `internal/cli/`, and parks the future SDK vocabulary in `internal/types/`. Phase 3 promotes those vocabulary types up to a new `package gitspork` at the module root, relocates `main.go` under `cmd/gitspork/`, and bumps the module path to `/v2` for the v2.0.0 release.

The CLI is preserved end-to-end throughout. At every phase boundary the existing `test/functional/` suite passes unchanged, meaning stdout, exit codes, and downstream filesystem effects match the pre-refactor tool.

## Tech Stack

Go, existing dependencies unchanged. Module path changes from `github.com/rockholla/gitspork` to `github.com/rockholla/gitspork/v2` in Phase 3 (Go module semantic import versioning requirement for major version ≥ 2).

---

## Section 1: Public API Surface

Root package `gitspork` exports only what these two use cases need:

- **Orchestrator/fleet tools** — a parent Go binary running integrate/check-drift across many downstreams.
- **Custom CI checks / drift bots** — embedding check-drift and consuming drift results structurally.

### Entry-point functions

```go
package gitspork

func Integrate(opts *IntegrateOptions) (*IntegrateResult, error)
func IntegrateLocal(opts *IntegrateLocalOptions) (*IntegrateResult, error)
func CheckDrift(opts *CheckDriftOptions) (*DriftReport, error)
```

### Options structs

```go
type IntegrateOptions struct {
    Upstreams          []UpstreamSpec
    DownstreamRepoPath string
    ForceRePrompt      bool
    Logger             Logger // nil = silent
}

type IntegrateLocalOptions struct {
    UpstreamPaths  []string
    DownstreamPath string
    ForceRePrompt  bool
    Logger         Logger
}

type CheckDriftOptions struct {
    Upstreams          []UpstreamSpec // optional overrides; empty = read from state
    DownstreamRepoPath string
    Logger             Logger
}

type UpstreamSpec struct {
    URL     string
    Version string
    Subpath string
    Token   string
}
```

Internal-only fields on today's Options structs (`PrevUpstreamCommitHash`, `ForDriftCheck`, deprecated single-upstream fields like `UpstreamRepoURL`) move to unexported sibling structs. SDK callers cannot set them.

### Result types

```go
type IntegrateResult struct {
    Upstreams []IntegratedUpstream // one entry per successfully integrated upstream, in order
}

type IntegratedUpstream struct {
    URL        string
    Subpath    string
    CommitHash string
}

type DriftReport struct {
    HasDrift bool
    Files    []DriftedFile
}

type DriftedFile struct {
    Path          string
    AttributedURL string // upstream URL that last wrote this file
    Diff          string // patch text; empty if drift-check ran without verbose diff capture
}

var ErrDriftDetected = errors.New("drift detected")
```

`CheckDrift` returns both a populated `*DriftReport` and `ErrDriftDetected` when drift is found — callers can branch on either. `Integrate` returns a populated `*IntegrateResult` even on partial failure (upstreams recorded so far) alongside the error.

### Logger interface

```go
type Logger interface {
    Log(msg string, args ...any)
    Error(msg string, args ...any)
}
```

Two methods are all the internal code needs for progress narration. `nil` is a valid value meaning silent. The concrete color-writing `CLILogger` used by the binary today lives in `internal/logutil/` and satisfies this interface.

### Explicitly not exported

Config parsing/writing (`ParseGitSporkConfig`, `WriteGitSporkConfig`, `FindGitSporkConfig`), schema rendering (`GetGitSporkConfigSchema`), upstream maintenance helpers (`UpstreamMv`, `UpstreamRm`, and their `Compute*` variants), `Init`, `ColorizeYAML`, `OwnedEntry`, and the concrete `Logger` implementation. If a future consumer needs any of these, adding them is a backward-compatible minor version bump.

---

## Section 2: Repo Layout

### Current (v1.1.x)

```
├── main.go                  # package main; 6 lines
├── cmd/                     # package cmd; cobra subcommands
├── internal/                # package internal; all business logic
│   ├── gitspork.go          # types
│   ├── integrate*.go
│   ├── check-drift.go
│   ├── logger.go            # concrete color-writing Logger
│   ├── integrator_*.go
│   ├── upstream-*.go
│   ├── owned-entry.go
│   ├── input/               # prompt helpers
│   └── testharness/         # shared test setup
└── test/
```

### Target after Phase 3 (v2.0.0)

```
├── gitspork.go              # package gitspork; Integrate, IntegrateLocal, CheckDrift entry points
├── options.go               # package gitspork; Options structs, UpstreamSpec
├── results.go               # package gitspork; IntegrateResult, IntegratedUpstream, DriftReport, DriftedFile
├── logger.go                # package gitspork; Logger interface only
├── errors.go                # package gitspork; ErrDriftDetected sentinel
├── doc.go                   # package gitspork; package-level docs and usage example
├── cmd/
│   └── gitspork/
│       └── main.go          # package main; 6-line CLI entry point
├── internal/
│   ├── cli/                 # package cli; the cobra subcommands (moved from cmd/)
│   ├── config/              # GitSporkConfig, Parse/Write, schema, OwnedEntry, mv/rm helpers
│   ├── integrate/           # integrateOne, integrators/, upstream-delta, url normalization
│   ├── drift/               # CheckDrift internals: worktree diff, listWorktreeFiles, attribution
│   ├── logutil/             # concrete CLILogger (satisfies gitspork.Logger)
│   ├── input/               # unchanged
│   └── testharness/         # unchanged
└── test/
    ├── functional/          # unchanged: CLI end-to-end
    ├── examples/            # unchanged: docs/examples fixture-based
    └── sdk/                 # new in Phase 3: black-box SDK tests
```

The three exported entry-point functions at the module root are thin coordinators — they build internal request objects and call into `internal/integrate` and `internal/drift`. No business logic lives at the root.

---

## Section 3: Phase Breakdown

Three phases, each shipping as its own PR. Between phases the CLI's user-facing behavior stays identical, so each is independently mergeable.

### Phase 1: Refactor to structural results

**Goal:** reshape `Integrate` / `IntegrateLocal` / `CheckDrift` to return result structs, and pull user-facing output out of the business logic. No files move; this is a pure semantic refactor inside today's `internal/`.

- Add `IntegrateResult`, `IntegratedUpstream`, `DriftReport`, `DriftedFile` types in `internal/gitspork.go`.
- Change signatures to `(*Result, error)`; internal functions populate the result while running.
- Split user-facing output cleanly:
  - **Progress narration** (e.g., "cloning upstream X at commit Y") stays on `Logger` (still the concrete `internal.Logger` in this phase).
  - **Result-shaped output** (per-file drift lines, drift summary counts, verbose diff dumps) moves out of internal. The CLI (`cmd/check-drift.go`) walks the returned `DriftReport` and prints the same output it produces today.
- Remove `Logger.Diff(io.Reader)`; verbose full-patch printing becomes the CLI iterating over `DriftReport.Files[].Diff`.
- Update all `internal/*_test.go` tests to inspect the returned struct rather than a captured Logger.
- CLI user-visible output must remain functionally equivalent to pre-refactor (existing `test/functional/` substring assertions continue to pass).

**Acceptance:** `make test-unit`, `make test-functional`, `make test-functional-docker`, `make test-examples` all pass.

### Phase 2: Physical repo split

**Goal:** carve `internal/` into subpackages and relocate `cmd/`. No new code, no API changes.

- Create subpackages under `internal/`:
  - `internal/config/` — receives `GitSporkConfig`, all its sub-types, `ParseGitSporkConfig`, `WriteGitSporkConfig`, `GetGitSporkConfigSchema`, `FindGitSporkConfig`, `OwnedEntry`, `ComputeUpstreamMv`/`ComputeUpstreamRm`/`UpstreamMv`/`UpstreamRm`, `Init`, `ColorizeYAML`
  - `internal/integrate/` — receives `Integrate`, `IntegrateLocal`, `integrateOne`, all integrators (`integrator_*.go`), `upstream-delta.go`, URL normalization, state helpers, migrations
  - `internal/drift/` — receives `CheckDrift`, `listWorktreeFiles`, `diffWorktreeAgainstHEAD`, attribution logic, `ErrDriftDetected`
  - `internal/logutil/` — receives the concrete color-writing Logger implementation (renamed `CLILogger`)
- Create `internal/types/` — parked home for the future SDK vocabulary during Phase 2:
  - `IntegrateOptions`, `IntegrateLocalOptions`, `CheckDriftOptions`, `UpstreamSpec`
  - `IntegrateResult`, `IntegratedUpstream`, `DriftReport`, `DriftedFile`
  - `Logger` interface (as a new abstraction; `logutil.CLILogger` implements it)
  - `ErrDriftDetected` sentinel
  - This package deliberately has minimal dependencies so any subpackage can import it without cycles.
- Move `cmd/*.go` → `internal/cli/` (`package cli`). `ParseUpstreamFlag` (currently in `internal/integrate.go`) is a CLI-flag parser used only by cobra subcommands, so it moves to `internal/cli/` too. Update `main.go` at repo root to call `cli.Execute(version)`.
- Update imports throughout `internal/`, the test suites, and `main.go`.

**Acceptance:** module root, `main.go`, and build system are unchanged from Phase 1's end state (aside from the `cmd` import path). All four test suites still pass with no behavioral change.

### Phase 3: Expose the SDK

**Goal:** create `package gitspork` at the repo root, move the CLI binary under `cmd/gitspork/`, bump the module path to `/v2` for the v2.0.0 release.

- **Promote `internal/types/` → `package gitspork` at the module root.** Split into `gitspork.go`, `options.go`, `results.go`, `logger.go`, `errors.go`, `doc.go`. Delete `internal/types/`.
- Add public entry-point functions in `package gitspork` (`Integrate`, `IntegrateLocal`, `CheckDrift`) as thin coordinators that call into `internal/integrate` and `internal/drift`.
- `internal/integrate`, `internal/drift`, `internal/logutil`, `internal/cli` now import `github.com/rockholla/gitspork/v2` for the shared vocabulary types.
- Move `main.go` → `cmd/gitspork/main.go` (still `package main`, still 6 lines, still calls `cli.Execute(version)`).
- Update `go.mod`: `module github.com/rockholla/gitspork/v2`.
- Rewrite all internal imports from `github.com/rockholla/gitspork/...` to `github.com/rockholla/gitspork/v2/...`.
- Build-system updates:
  - `.goreleaser.yaml`: add `main: ./cmd/gitspork` to each `builds:` entry
  - `Dockerfile`: adjust `COPY` and `go build` paths
  - `Makefile`: any `go build .` targets become `go build ./cmd/gitspork`
  - `test/functional/main_test.go`: the binary-building setup changes from `go build .` to `go build ./cmd/gitspork`
- Add `doc.go` with a package-level example showing both use cases (orchestrator, drift bot).
- Add the new `test/sdk/` tier (see Section 4).

**Acceptance:** every CLI invocation and every existing consumer path works identically. `import "github.com/rockholla/gitspork/v2"` works from an external Go module. Tagged as v2.0.0 by maintainer.

#### Phase 3 follow-ups carried forward from Phase 1

Items surfaced by Phase 1 code reviews that fit naturally in Phase 3 (when the SDK surface goes public and gets its dedicated tests). These belong in the Phase 3 plan when we write it:

- **Document `IntegratedUpstream.URL` local-path overload.** `IntegrateLocal` stores an absolute filesystem path in the `URL` field (no scheme). The field's doc-comment on `internal/gitspork.go` doesn't call this out. When the type moves to `package gitspork`, add a one-line note: `// URL is the upstream URL, or the local filesystem path for IntegrateLocal (no scheme).`
- **Document non-nil invariant for `IntegrateResult` / `DriftReport`.** All three entry-point functions guarantee a non-nil pointer return even on error (Phase 1 established this). Add doc-comments on both types and the return signatures so SDK consumers don't add defensive `if result != nil` guards.
- **Add SDK-tier unit test for multi-upstream `IntegrateResult.Upstreams` ordering.** Phase 1's `TestIntegrate_returns_result_with_upstream_url_and_hash` only covers single-upstream. Multi-upstream ordering is functionally tested via state-file inspection in `TestIntegrate_multi_upstream_state_records_all`, but the new `test/sdk/` tier should assert `result.Upstreams` order directly.
- **Restore colored verbose diff output in the CLI.** Phase 1 removed `Logger.Diff(io.Reader)` which colorized `+`/`-`/`@@`/header lines with ANSI. `cmd/check-drift.go` now prints `DriftedFile.Diff` as plain text. Add a CLI-side colorizer that iterates lines and applies the old prefix-based coloring when `!color.NoColor`. Keep the SDK `DriftedFile.Diff` plain so machine consumers get clean data.
- **Honor `Logger == nil` as silent throughout the internal codebase.** Phase 3 promises `nil = silent` at the SDK boundary. Today `internal/integrate.go` has multiple `fmt.Println("")` blank-line writes (~lines 278, 290, 296, 302, 308, 314, 320, 327, 339) and go-git's clone `Progress: os.Stdout` at ~line 407 that bypass the Logger entirely. Route these through the Logger interface (or wrap the Progress writer) so a nil-logger SDK caller sees no stdout output.
- **Move `IntegrateOptions` internal fields to an unexported sibling.** `ForDriftCheck`, `PrevUpstreamCommitHash`, and the deprecated single-upstream fields (`UpstreamRepoURL`, `UpstreamRepoVersion`, `UpstreamRepoCommit`, `UpstreamRepoSubpath`, `UpstreamRepoToken`) live on the public-facing `IntegrateOptions` today. When it becomes SDK-facing, introduce an unexported `integrateRequest` struct inside `internal/integrate/` and have the public entry point copy the exported fields into it.
- **Document last-writer-wins attribution semantics on `DriftReport`.** When two upstreams write the same file, `DriftedFile.AttributedURL` records whichever wrote it last. This is a deliberate design choice already reflected in the multi-upstream check-drift tests, but not documented on the type. Add a sentence when promoting the type.

---

## Section 4: Testing Strategy

Three tiers, each with a clear job.

### Tier 1: Unit tests

Every `internal/*_test.go` file today gets moved with its production code in Phase 2. Once split:
- `internal/config/*_test.go` — parse/write/round-trip, schema rendering, `OwnedEntry`, `UpstreamMv`/`UpstreamRm`
- `internal/integrate/*_test.go` — `integrateOne`, URL normalization, state upsert/migration, delta propagation, per-integrator behavior, migrations
- `internal/drift/*_test.go` — `listWorktreeFiles`, `diffWorktreeAgainstHEAD`, per-upstream attribution
- `internal/logutil/*_test.go` — the concrete CLI logger's formatting

Pure relocation; assertions unchanged.

### Tier 2: Functional CLI tests

`test/functional/` (native + docker) is the CLI-parity safety net. Its assertions target the compiled binary's exit codes, stdout, and downstream filesystem effects. **Unchanged across all three phases.** If the same functional suite passes at each phase's tip, CLI behavior is preserved end-to-end.

The only test-infrastructure change is in Phase 3: `test/functional/main_test.go`'s build step updates from `go build .` to `go build ./cmd/gitspork`.

### Tier 3: SDK tests (new in Phase 3)

New directory `test/sdk/`, sibling to `functional/` and `examples/`. Tests import `github.com/rockholla/gitspork/v2` **as an external caller would** — black-box, from an outside package. This proves the public API works from Go code, not just via the CLI.

Coverage areas:
- **`Integrate`** — single upstream; multi-upstream precedence; `IntegrateResult.Upstreams` order and commit hashes match actual upstream HEADs; state persisted correctly.
- **`IntegrateLocal`** — multi-path precedence; `IntegrateResult` shape; no downstream state file written (spec §4 invariant from multi-upstream feature).
- **`CheckDrift`** — no-drift returns `HasDrift: false` and nil error; drift returns populated `DriftReport.Files` with correct `AttributedURL` per file and `ErrDriftDetected` sentinel; verbose diffs populated in `DriftedFile.Diff`.
- **Logger contract** — `nil` logger is silent (no output at all captured); custom `Logger` impl captures the progress calls the internals make; `internal/logutil.CLILogger` satisfies the `gitspork.Logger` interface (compile-time assertion).
- **Error paths** — no upstreams + no state; override upstream not in state; both must return the same errors the CLI returns, callable programmatically.

Tests reuse the existing `internal/testharness` helpers for building upstream/downstream fixtures. New build tag `sdk` (mirrors `functional`/`functional_docker`/`examples`). New Makefile target `make test-sdk`. New CI job `sdk-tests` alongside the existing four.

### CLI parity acceptance per phase

Every PR's CI must have all applicable suites green: unit + functional + functional-docker + examples (Phases 1 and 2); those plus sdk (Phase 3). No PR merges with any suite failing.

---

## Backward Compatibility

| Scenario | Behavior after v2.0.0 |
|---|---|
| Existing CLI invocations (all flags including backward-compat single-upstream forms) | Identical output, exit codes, and filesystem effects. |
| Existing downstream state files (`.gitspork/downstream-state.json`) | Read and used unchanged; auto-migration of legacy single-upstream fields (already present in v1.1) continues to work. |
| Third-party Go code that imported `github.com/rockholla/gitspork/internal/...` | Never supported — Go's `internal/` restriction prevented this. No breakage risk. |
| Third-party Go code that will use the v2 SDK | Imports `github.com/rockholla/gitspork/v2` and calls the three exported entry points. Semver-stable from v2.0.0 forward. |
| Third-party Go code that wants config parsing, mv/rm, init, or schema | Not exposed in v2.0.0. Additive minor version if requested later. |
