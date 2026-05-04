# CLAUDE.md — gitspork

## What this repo is

`gitspork` is a Go CLI tool for managing upstream→downstream git repo relationships. An upstream repo publishes standards, tooling, and templates; downstream repos integrate from it and can check for drift over time. The core primitives are: `upstream_owned`, `downstream_owned`, `shared_ownership` (merged and structured), `templated`, and `migrations` — all configured via `.gitspork.yml`.

## Module and key dependencies

- Module: `github.com/rockholla/gitspork`
- Go 1.26
- `github.com/go-git/go-git/v6` — all git operations
- `github.com/gobwas/glob` — glob pattern matching for file ownership rules
- `github.com/goccy/go-yaml` — YAML parsing, marshaling, and schema colorization via its `lexer`/`printer` packages
- `github.com/rockholla/go-lib` — `marshal.YAMLWithComments` for annotated schema output
- `github.com/spf13/cobra` — CLI framework
- `github.com/fatih/color` — terminal color output with automatic TTY detection

## Layout

```
cmd/                    # Cobra subcommands (one file per command)
internal/               # All business logic
  gitspork.go           # Core types: GitSporkConfig, GitSporkDownstreamState, options structs
  integrate.go          # Integrate and cloneUpstreamForIntegrate
  integrate-local.go    # IntegrateLocal
  check-drift.go        # CheckDrift
  upstream-delta.go     # computeUpstreamDelta / applyUpstreamDelta
  upstream-mv-rm.go     # UpstreamMv / UpstreamRm
  integrator_*.go       # Per-ownership-type integration logic
  logger.go             # Logger, Diff colorizer, ColorizeYAML
  input/                # Prompt and JSON input resolution
  testharness/          # Shared test helpers (non-test package, importable by all test packages)
test/
  functional/           # Scenario tests against compiled binary or Docker image
  examples/             # Tests that run against the docs/examples/ upstream directories
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
make test-unit              # go test ./... (unit tests, no build tag)
make test-functional        # -tags functional (compiles binary, runs against synthetic repos)
make test-functional-docker # -tags functional_docker (same scenarios via Docker image)
make test-examples          # -tags examples (runs against docs/examples/ upstream dirs)
```

**Build tags are important:**
- `functional` — activates `test/functional/` tests using the native binary runner
- `functional_docker` — activates the same test scenarios using `DockerRunner`
- `examples` — activates `test/examples/`
- `harness_native.go` uses `//go:build functional && !functional_docker` to avoid gopls duplicate declaration errors when both tags are active

**Shared test helpers** live in `internal/testharness/testharness.go` (no build tag). Both `test/functional/` and `test/examples/` import from there. `test/functional/harness.go` wraps them with thin delegating functions.

## CI

- `main.yml` — triggered on push/PR to `main`; calls the reusable `tests.yml` workflow
- `release.yml` — triggered on `v*` tag push; runs `tests.yml` then goreleaser
- `tests.yml` — reusable workflow with four parallel jobs: unit, functional, functional-docker, examples
- goreleaser builds multi-arch Linux + Darwin binaries, multi-arch Docker images, pushes a Homebrew formula to `rockholla/homebrew-gitspork`

## Releasing

Run `make release` from the `main` branch. The script validates semver (requires `v` prefix, e.g. `v1.2.3`), checks for duplicate tags, shows the most recent stable and pre-release tags, enforces that stable releases (`v1.2.3`) can only be tagged from `main`, then pushes the annotated tag. GitHub Actions handles everything after the push.

Pre-release tags (e.g. `v1.2.3-rc.1`) can be pushed from any branch.

## Key design decisions worth knowing

**Upstream delta propagation:** When `integrate` runs against a new upstream commit, `computeUpstreamDelta` diffs `prevHash..newHash` in the upstream repo and applies file deletions/renames to the downstream before the normal integration logic runs. Critically, it builds managed globs from the **previous commit's `.gitspork.yml`** (with fallback to new config), not the new one — this ensures files removed by `gitspork rm` (which strips them from config in the same commit) are still recognized as managed and propagated as deletions.

**Drift detection isolation:** `CheckDrift` copies the downstream to a temp dir, `git init`s it as a baseline, then re-runs `Integrate` at the stored upstream commit hash (`ForDriftCheck: true` skips delta propagation and state saving). A `git diff HEAD` on the temp dir reveals drift.

**URL rewriting:** `resolveUpstreamURL(url, token string)` in `integrate.go` silently rewrites SSH↔HTTPS based on `SSH_AUTH_SOCK` and token presence. The caller (`CheckDrift`) selects which URL to pass (override or stored); the function only handles the protocol rewrite.

**State storage:** `.gitspork/downstream-state.json` in the downstream repo stores `last_upstream_repo_url`, `last_upstream_repo_subpath`, `last_upstream_commit_hash`, and `migrations_complete`. State is only written by `Integrate` (not `CheckDrift` and not `IntegrateLocal`).

**`ColorizeYAML`** in `internal/logger.go` uses `goccy/go-yaml`'s `lexer.Tokenize` + `printer.Printer` for token-accurate YAML highlighting — do not replace with regex-based approaches. Color is suppressed automatically when stdout is not a TTY (`color.NoColor`).
