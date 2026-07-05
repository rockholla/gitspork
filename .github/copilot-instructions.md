# CLAUDE.md — gitspork

## What this repo is

`gitspork` is a Go CLI tool for managing upstream→downstream git repo relationships. An upstream repo publishes standards, tooling, and templates; downstream repos integrate from it and can check for drift over time. The core primitives are: `upstream_owned`, `downstream_owned`, `shared_ownership` (merged and structured), `templated`, and `migrations` — all configured via `.gitspork.yml`.

## Module and key dependencies

- Module: `github.com/rockholla/gitspork/v2`
- Go 1.26
- `github.com/go-git/go-git/v6` — all git operations
- `github.com/gobwas/glob` — glob pattern matching for file ownership rules
- `github.com/goccy/go-yaml` — YAML parsing, marshaling, and schema colorization via its `lexer`/`printer` packages
- `github.com/rockholla/go-lib` — `marshal.YAMLWithComments` for annotated schema output
- `github.com/spf13/cobra` — CLI framework
- `github.com/fatih/color` — terminal color output with automatic TTY detection

## Layout

```
gitspork.go             # Root SDK package: Integrate, IntegrateLocal, CheckDrift entry-points and re-exported type aliases
doc.go                  # Package-level godoc for the root SDK
cmd/
  gitspork/             # main.go — CLI entry point
internal/               # All business logic (unexported to SDK consumers)
  cli/                  # Cobra subcommands (root, integrate, integrate-local, check-drift, mv, rm, init, schema)
  config/               # GitSporkConfig types, YAML load/save, init scaffold, mv/rm config rewrite (UpstreamMv / UpstreamRm)
  drift/                # CheckDrift orchestrator
  integrate/            # Integrate, IntegrateLocal, IntegrateForDriftCheck, integrator_*.go per-ownership logic, upstream_delta.go (computeUpstreamDelta / applyUpstreamDelta)
  logutil/              # Default Logger, diff colorizer, ColorizeYAML
  sdktypes/             # Shared SDK types (options, results, errors, Logger interface) — reason: avoids import cycles between root gitspork, drift, and integrate packages
  input/                # Prompt and JSON input resolution
  testharness/          # Shared test helpers (non-test package, importable by all test packages)
test/
  functional/           # Scenario tests against compiled binary or Docker image
  examples/             # Tests that run against the docs/examples/ upstream directories
  sdk/                  # Black-box tests importing github.com/rockholla/gitspork/v2 as a library
docs/
  examples/             # Four fully worked upstream examples (platform-team, open-source-template, standards-library, integrate-local)
  superpowers/          # Design specs and implementation plans (specs/ and plans/)
scripts/
  release.sh            # Interactive tag-and-push script; CI handles the rest
```

## CLI subcommands

| Command | Who uses it | What it does |
|---|---|---|
| `init` | Upstream maintainer | Creates `.gitspork.yml` scaffold |
| `schema` | Anyone | Prints annotated `.gitspork.yml` and migration YAML schemas with TTY color |
| `integrate` | Downstream integrator | Clones upstream at version/commit, applies all ownership rules to downstream |
| `integrate-local` | Downstream integrator | Same as integrate but uses a local upstream path |
| `check-drift` | Downstream integrator | Re-integrates at the stored commit in an isolated temp dir, diffs, reports drift |
| `mv` | Upstream maintainer | `git mv` + updates `.gitspork.yml` entries |
| `rm` | Upstream maintainer | `git rm` + updates `.gitspork.yml` entries |

## Testing

```bash
make test-unit              # go vet ./... && go test ./... (unit tests, no build tag)
make test-functional        # -tags functional (compiles binary, runs against synthetic repos)
make test-functional-docker # -tags functional_docker (same scenarios via Docker image)
make test-examples          # -tags examples (runs against docs/examples/ upstream dirs)
make test-sdk               # -tags sdk (black-box tests importing github.com/rockholla/gitspork/v2)
```

**Build tags are important:**
- `functional` — activates `test/functional/` tests using the native binary runner
- `functional_docker` — activates the same test scenarios using `DockerRunner`
- `examples` — activates `test/examples/`
- `sdk` — activates `test/sdk/` (black-box library tests)
- `harness_native.go` uses `//go:build functional && !functional_docker` to avoid gopls duplicate declaration errors when both tags are active

**Shared test helpers** live in `internal/testharness/testharness.go` (no build tag). Both `test/functional/`, `test/examples/`, and `test/sdk/` import from there. `test/functional/harness.go` wraps them with thin delegating functions.

## CI

- `main.yml` — triggered on push/PR to `main`; calls the reusable `tests.yml` workflow
- `release.yml` — triggered on `v*` tag push; runs `tests.yml` then goreleaser
- `tests.yml` — reusable workflow with five parallel jobs: unit, functional, functional-docker, examples, sdk
- goreleaser builds multi-arch Linux + Darwin binaries, multi-arch Docker images, pushes a Homebrew formula to `rockholla/homebrew-gitspork`

## Releasing

Run `make release` from the `main` branch. The script validates semver (requires `v` prefix, e.g. `v1.2.3`), checks for duplicate tags, shows the most recent stable and pre-release tags, enforces that stable releases (`v1.2.3`) can only be tagged from `main`, then pushes the annotated tag. GitHub Actions handles everything after the push.

Pre-release tags (e.g. `v1.2.3-rc.1`) can be pushed from any branch.

## Key design decisions worth knowing

**Upstream delta propagation:** When `integrate` runs against a new upstream commit, `computeUpstreamDelta` (in `internal/integrate/upstream_delta.go`) diffs `prevHash..newHash` in the upstream repo and applies file deletions/renames to the downstream before the normal integration logic runs. Critically, it builds managed globs from the **previous commit's `.gitspork.yml`** (with fallback to new config), not the new one — this ensures files removed by `gitspork rm` (which strips them from config in the same commit) are still recognized as managed and propagated as deletions.

**Drift detection isolation:** `CheckDrift` (in `internal/drift/check_drift.go`) copies the downstream to a temp dir, `git init`s it as a baseline, then re-runs the integrate pipeline at the stored upstream commit hash via `integrate.IntegrateForDriftCheck` (skips delta propagation and state saving). A `git diff HEAD` on the temp dir reveals drift.

**URL rewriting:** `resolveUpstreamURL(url, token string)` in `internal/integrate/integrate.go` silently rewrites SSH↔HTTPS based on token presence: a token forces the HTTPS form; no token forces the SSH form. `CheckDrift` selects which URL to pass (override or stored) to `IntegrateForDriftCheck`; the function only handles the protocol rewrite.

**State storage:** `.gitspork/downstream-state.json` in the downstream repo stores a `migrations_complete` list and an `upstreams` slice, where each entry has `url`, `subpath`, and `commit_hash`. The state schema also carries three deprecated fields (`last_upstream_repo_url`, `last_upstream_repo_subpath`, `last_upstream_commit_hash`) for backward compatibility — `LoadDownstreamState` auto-migrates them into the `upstreams` slice on first read and clears them on next save. State is only written by `Integrate` (not `CheckDrift` and not `IntegrateLocal`).

**`ColorizeYAML`** in `internal/logutil/colorize.go` uses `goccy/go-yaml`'s `lexer.Tokenize` + `printer.Printer` for token-accurate YAML highlighting — do not replace with regex-based approaches. Color is suppressed automatically when stdout is not a TTY (`color.NoColor`).
