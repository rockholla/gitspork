# Examples & Example Tests Design

## Goal

Replace the ad-hoc `docs/examples/simple/` and `docs/examples/local/` with four well-crafted, human-readable scenario examples that together demonstrate all major gitspork features in realistic domain contexts. Add a dedicated `test/examples/` package that CI-proves each example works correctly against the real binary.

## Architecture

Examples live in `docs/examples/` and are the primary artifact — written for humans, not test harnesses. Tests in `test/examples/` use the example dirs directly (via `file://` URLs or local paths), running the actual binary against them. The existing `test/functional/` synthetic-repo suite is unchanged.

Shared harness helpers currently embedded in `test/functional/harness.go` are extracted to `internal/testharness/` so both test packages can import them without duplication.

## Tech Stack

Go test package, build tag `-tags examples`, `internal/testharness/` shared helpers, existing `BinaryRunner`-style binary execution, `go-git` for downstream repo setup.

---

## Examples

### `docs/examples/platform-team/upstream/`

**Domain:** A platform engineering team publishes CI/CD standards, shared Makefiles, and deployment scripts. Downstream service teams own their README and service-specific config.

**Features demonstrated:**
- `upstream_owned`: `ci/build.yml`, `ci/deploy.yml`, `scripts/shared-bootstrap.sh`
- `downstream_owned`: `README.md` (seeded from upstream on first integrate)
- `shared_ownership.merged`: `Makefile` (upstream owns a block; downstream can add targets)
- `shared_ownership.structured.prefer_upstream`: `deploy-config.yaml`
- `templated`: `service-manifest.yml` rendered from `service-manifest.yml.go.tmpl`, inputs `service_name` and `team_name` from `service-input-data.json` (downstream's responsibility)
- `migrations`: one migration demonstrating a pre/post-integrate script

**File layout:**
```
upstream/
  .gitspork.yml
  .gitspork-templates/
    service-manifest.yml.go.tmpl
  .gitspork/
    migrations/
      0001-init.yml
      scripts/
        0001-init.sh
  Makefile
  deploy-config.yaml
  ci/
    build.yml
    deploy.yml
  scripts/
    shared-bootstrap.sh
  README.md
```

---

### `docs/examples/open-source-template/upstream/`

**Domain:** An OSS project maintainer owns contributor tooling, GitHub Actions workflows, and LICENSE. Downstream forks own their README, CHANGELOG, and project metadata.

**Features demonstrated:**
- `upstream_owned`: `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.github/ISSUE_TEMPLATE.md`, `LICENSE`, `CONTRIBUTING.md`
- `downstream_owned`: `README.md`, `CHANGELOG.md`
- `shared_ownership.structured.prefer_downstream`: `project-meta.json` (forks set their own project name, description, etc.)
- `templated`: `CODE_OF_CONDUCT.md` rendered from template, input `project_name` sourced via `json_data_path: project-meta.json`

**File layout:**
```
upstream/
  .gitspork.yml
  .gitspork-templates/
    CODE_OF_CONDUCT.md.go.tmpl
  .github/
    workflows/
      ci.yml
      release.yml
    ISSUE_TEMPLATE.md
  LICENSE
  CONTRIBUTING.md
  project-meta.json
  README.md
  CHANGELOG.md
```

---

### `docs/examples/standards-library/upstream/`

**Domain:** A security/compliance team owns lint configs, policy documents, and a security scanning workflow. Downstream microservices extend a shared `.env.example` and own their own security summary.

**Features demonstrated:**
- `upstream_owned`: `.golangci.yml`, `policies/data-handling.md`, `policies/access-control.md`
- `shared_ownership.merged`: `.env.example` (upstream owns required vars block; downstream adds service-specific vars)
- `shared_ownership.structured.prefer_upstream`: `security-policy.yaml`
- `templated`: `service-info.txt` rendered from `service-info.txt.go.tmpl` with input `service_name` (from `service-input-data.json`); `security-summary.md` rendered from `security-summary.md.go.tmpl` with `service_name` sourced via `previous_input` referencing `service-info.txt.go.tmpl`
- `migrations`: one migration

**File layout:**
```
upstream/
  .gitspork.yml
  .gitspork-templates/
    service-info.txt.go.tmpl
    security-summary.md.go.tmpl
  .gitspork/
    migrations/
      0001-policy-init.yml
      scripts/
        0001-policy-init.sh
  .golangci.yml
  security-policy.yaml
  .env.example
  policies/
    data-handling.md
    access-control.md
```

---

### `docs/examples/integrate-local/`

**Domain:** Demonstrates `gitspork integrate-local` for teams that keep their upstream template as a local path rather than a remote git repo (monorepo, co-located tooling, etc.).

**Features demonstrated:**
- `templated` with `json_data_path` input sourced from downstream dir
- `upstream_owned`: one config file
- `integrate-local` command (no git remote required)

**File layout:**
```
upstream/
  .gitspork.yaml
  .gitspork-templates/
    config.yml.go.tmpl
  app-config.yaml
downstream/
  input-data.json        # pre-seeded; shows downstream's responsibility
```

---

## Harness Extraction: `internal/testharness/`

The following helpers are extracted from `test/functional/harness.go` into `internal/testharness/testharness.go` as a non-test package (no `_test.go` suffix, no build tag). The full implementations live in `internal/testharness/`:

- `NewUpstreamRepo(t, files map[string]string, gitsporkYML string) string`
- `NewDownstreamRepo(t) string`
- `CommitAll(t, repo, dir, message)`
- `OpenRepo(t, dir) *gogit.Repository`
- `WriteFiles(t, dir, files map[string]string)`
- `ReadFile(t, dir, path) string`
- `AssertFileContains(t, dir, path, substr)`
- `AssertFileAbsent(t, dir, path)`

`test/functional/harness.go` retains `Runner`, `BinaryRunner`, and the `resolveRunner` wiring. The repo helper functions in `harness.go` are thin wrappers that delegate to `internal/testharness/` — this allows both `test/functional/` and `test/examples/` to share the same implementations without duplication.

---

## Test Package: `test/examples/`

**Build tag:** `//go:build examples`

**Files:**
```
test/examples/
  main_test.go                   # builds binary once into os.MkdirTemp
  platform_team_test.go
  open_source_template_test.go
  standards_library_test.go
  integrate_local_test.go
```

`main_test.go` follows the same pattern as `test/functional/main_test.go`: builds the binary once in `TestMain`, stores it in a package-level `binaryPath` string. Each test file constructs a `BinaryRunner` directly (no Docker path needed for examples).

### `platform_team_test.go` assertions:
- `ci/build.yml` and `ci/deploy.yml` exist with expected content
- `scripts/shared-bootstrap.sh` exists
- `README.md` seeded on first integrate; downstream edit survives re-integrate
- `Makefile` contains upstream block marker and content
- `deploy-config.yaml` contains upstream value after re-integrate
- `service-manifest.yml` rendered with `service_name` and `team_name` from input JSON
- migrate script executed (assert post-integrate side-effect file or exit 0)
- check-drift exits 0 after baseline commit

### `open_source_template_test.go` assertions:
- `.github/workflows/ci.yml`, `LICENSE`, `CONTRIBUTING.md` exist
- `README.md` and `CHANGELOG.md` seeded, not overwritten on re-integrate
- `project-meta.json` downstream value survives re-integrate (prefer_downstream)
- `CODE_OF_CONDUCT.md` rendered with `project_name` from `project-meta.json`
- check-drift exits 0

### `standards_library_test.go` assertions:
- `.golangci.yml` and `policies/` files exist with expected content
- `.env.example` contains upstream block after integrate
- `security-policy.yaml` upstream value wins after re-integrate
- `security-summary.md` rendered; contains value sourced via `previous_input`
- migration runs without error
- check-drift exits 0

### `integrate_local_test.go` assertions:
- `gitspork integrate-local` exits 0
- `app-config.yaml` lands in downstream
- `config.yml` rendered with value from `input-data.json`

---

## Makefile

```makefile
.PHONY: test-examples
test-examples:
    @go test -tags examples -timeout 120s -v ./test/examples/...
```

`test-all` and `ci` targets remain unchanged — example tests are opt-in via `make test-examples`.

---

## Implementation Risk: Harness Extraction

Extracting helpers from `test/functional/harness.go` to `internal/testharness/` touches existing, passing tests. The plan must treat this as its own task with a verification step — run `make test-functional` after the refactor to confirm no regressions before any example code is written.

---

## What Is Not Changing

- `test/functional/` synthetic-repo tests are untouched except for importing helpers from `internal/testharness/` instead of defining them locally.
- `docs/examples/simple/` and `docs/examples/local/` are left in place; the user will remove them manually after the new examples are in.
- Docker runner path (`test/functional/harness_docker.go`) is unaffected.
