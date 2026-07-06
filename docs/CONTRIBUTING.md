# Contributing to gitspork

Thank you for your interest in contributing! This guide covers everything you need to get started.

## Table of Contents

- [Local Development Environment](#local-development-environment)
- [Project Layout](#project-layout)
- [Building](#building)
- [Test Suites](#test-suites)
- [Before You Push](#before-you-push)
- [Submitting a Pull Request](#submitting-a-pull-request)
- [What CI Runs on a PR](#what-ci-runs-on-a-pr)
- [Releasing](#releasing)

---

## Local Development Environment

**Required:**

| Tool | Minimum version | Notes |
|---|---|---|
| Go | 1.26 | See `go.mod` for exact version |
| git | 2.x | Used directly by the test harness to create synthetic repos |
| Docker | any recent version | Required for `make test-functional-docker` |

**Optional:**

- [goreleaser](https://goreleaser.com/) v2 — only needed if you are working on the release pipeline locally; normal development does not require it

**Editor:** If you use VS Code, the repo's checked-in `.vscode/settings.json` sets `gopls.build.buildFlags` to `-tags=functional,functional_docker,examples,sdk,testharness` so gopls type-checks every tagged test file. Without these tags, gopls will ignore files gated by `//go:build functional` and friends.

---

## Project Layout

```
gitspork.go             # Public SDK entry points: Integrate, IntegrateLocal, CheckDrift, type aliases
cmd/
  gitspork/             # main.go — CLI entry point
internal/               # Business logic (unexported to SDK consumers)
  cli/                  # Cobra subcommands (root, integrate, integrate-local, check-drift, mv, rm, init, schema)
  config/               # GitSporkConfig types, YAML load/save, init scaffold, mv/rm config rewrite, NormalizeUpstreamPath
  drift/                # CheckDrift orchestrator
  integrate/            # Integrate, IntegrateLocal, IntegrateForDriftCheck, per-ownership integrator_*.go, upstream_delta.go
  logutil/              # Default Logger, diff colorizer, ColorizeYAML
  sdktypes/             # Shared SDK types (options, results, errors, Logger interface)
  input/                # Prompt and JSON input resolution
  gitbin/               # Fail-fast check for the git binary on PATH
test/
  functional/           # Scenario tests against the compiled binary or Docker image (tag: functional / functional_docker)
  examples/             # Tests that run against the docs/examples/ upstream directories (tag: examples)
  sdk/                  # Black-box tests importing github.com/rockholla/gitspork/v2 as a library (tag: sdk)
  testharness/          # Shared test helpers, build-tag guarded (tag: testharness)
  security-gate/        # Bash unit tests for the CI security-gate script
docs/
  examples/             # Fully worked upstream examples with per-example READMEs
  superpowers/          # Design specs and implementation plans (specs/ and plans/)
scripts/
  release.sh            # Interactive tag-and-push script; CI handles the rest
.github/
  workflows/            # CI workflows (main.yml, release.yml, security.yml, tests.yml)
```

---

## Building

```bash
go build ./...
```

To produce a binary at dist/gitspork:

```bash
make build
```

---

## Test Suites

There are four test suites, each activated by a build tag (or the absence of one).

### Unit Tests

```bash
make test-unit
# equivalent: go vet ./... && go test -v ./...
```

**What's covered:** All `internal/` packages. Tests use in-memory or temp-dir state — no binary compilation, no network, no Docker required.

**Dependencies:** None beyond a working Go installation.

### Functional Tests

```bash
make test-functional
# equivalent: go test -tags functional -timeout 120s -v ./test/functional/...
```

**What's covered:** End-to-end scenarios against a compiled `gitspork` binary. The test harness (`test/functional/harness_native.go`) compiles the binary into a temp dir before running and invokes it as a subprocess. Scenarios cover the full integrate / check-drift / mv / rm lifecycle using synthetic git repos created on the fly.

**Build tag:** `functional`

**Dependencies:** Go (for compilation), git.

**Note on build tags:** `harness_native.go` has the constraint `//go:build functional && !functional_docker`. This prevents gopls from reporting duplicate declarations when both `functional` and `functional_docker` are active at the same time. If you add a new harness function, follow the same pattern.

### Functional Docker Tests

```bash
make test-functional-docker
# equivalent: go test -tags functional_docker -timeout 300s -v ./test/functional/...
```

**What's covered:** The same scenario tests as the functional suite, but run against the Docker image produced by `Dockerfile` instead of the native binary. Uses `DockerRunner` (`test/functional/harness_docker.go`) to build the image and invoke containers.

**Build tag:** `functional_docker`

**Dependencies:** Docker running locally.

**Timeout:** 300 seconds (longer than functional because image builds can take time on a cold cache).

### Example Tests

```bash
make test-examples
# equivalent: go test -tags examples -timeout 120s -v ./test/examples/...
```

**What's covered:** Validates the worked examples in `docs/examples/` (platform-team, open-source-template, standards-library, integrate-local, multi-upstream). Each test integrates from the example's upstream directory into a synthetic downstream and asserts the expected result. These tests serve as living documentation — if an example breaks, the test catches it.

**Build tag:** `examples`

**Dependencies:** Go, git.

### Shared Test Infrastructure

`test/testharness/testharness.go` contains helpers shared across the unit, functional, sdk, and example suites — creating synthetic git repos, asserting file contents, etc. It is guarded by `//go:build testharness` so a production import fails at compile time (`build constraints exclude all Go files in .../test/testharness`); every `make test-*` target passes `-tags testharness` chained with its tier tag (e.g. `-tags functional,testharness`). Direct `go test ./…` invocations that import the harness must include `-tags testharness` too.

---

## Before You Push

Run this checklist before opening a PR:

- [ ] `go build ./...` — no compile errors
- [ ] `make test-unit` — all unit tests pass
- [ ] `make test-functional` — all functional scenarios pass
- [ ] `make test-examples` — all example tests pass
- [ ] `make test-functional-docker` — Docker scenarios pass (skip only if Docker is unavailable; CI will catch it)
- [ ] New code follows existing patterns — check the surrounding files before inventing conventions

If you are touching `internal/logutil/colorize.go`'s `ColorizeYAML` function: the `goccy/go-yaml` lexer/printer approach is intentional. Do not replace it with regex-based colorization — regex misses list string items and other token types that the lexer handles correctly.

---

## Submitting a Pull Request

1. Fork the repo and create a branch from `main`.
2. Make your changes with focused commits.
3. Open a PR against `main`. Use the PR template — it prompts for a summary and test plan.
4. CI runs automatically (see [What CI Runs on a PR](#what-ci-runs-on-a-pr) below).
5. Address any review feedback; maintainers aim to review within a few business days.

**Branch naming:** There is no enforced convention, but `feat/short-description`, `fix/short-description`, and `chore/short-description` are common.

**Commit messages:** Prefer conventional commits (`feat:`, `fix:`, `chore:`, `docs:`, `ci:`) — goreleaser uses them to categorize changelog entries on release.

---

## What CI Runs on a PR

All four jobs run in parallel on every push to `main` and every pull request:

| Job | Build tag | Runner | What it does |
|---|---|---|---|
| `unit-tests` | _(none)_ | `ubuntu-latest` | `make test-unit` |
| `functional-tests` | `functional` | `ubuntu-latest` | `make test-functional` |
| `functional-container-tests` | `functional_docker` | `ubuntu-latest` | `make test-functional-docker` |
| `example-tests` | `examples` | `ubuntu-latest` | `make test-examples` |

All four jobs must pass before a PR can be merged.

The CI configuration lives in `.github/workflows/tests.yml` (the reusable workflow) and `.github/workflows/main.yml` (the trigger for `main`).

---

## Releasing

Releases are published via `make release`. This will:

1. Show the most recent stable and pre-release remote tags for context
2. Prompt for the new version (must be valid semver with `v` prefix, e.g. `v1.2.3`)
3. Prompt for a release description (used as the annotated tag message and GitHub Release notes)
4. Push the tag to GitHub

**Branch requirement:** Stable releases (e.g. `v1.2.3`) must be tagged from the `main` branch. Pre-release tags (e.g. `v1.2.3-rc.1`) can be pushed from any branch.

GitHub Actions then takes over: runs all test suites, builds multi-arch binaries and Docker images, publishes a GitHub Release, pushes Docker images to Docker Hub, and updates the Homebrew formula in `rockholla/homebrew-gitspork`.
