# Examples & Example Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build four human-readable scenario examples in `docs/examples/` and a dedicated `test/examples/` package that CI-proves each example works against the real binary.

**Architecture:** Harness helpers are extracted from `test/functional/harness.go` to `internal/testharness/` so both `test/functional/` and `test/examples/` can share them. Each example is a static upstream dir; tests create a synthetic downstream, run the binary against the example upstream, and assert on output files. A new `make test-examples` target runs the examples package in isolation.

**Tech Stack:** Go, `//go:build examples` build tag, `go-git v6`, `github.com/stretchr/testify`, existing `BinaryRunner` pattern.

---

## File Map

**Created:**
- `internal/testharness/testharness.go` — shared repo helpers (moved from `test/functional/harness.go`)
- `docs/examples/platform-team/upstream/.gitspork.yml`
- `docs/examples/platform-team/upstream/.gitspork-templates/service-manifest.yml.go.tmpl`
- `docs/examples/platform-team/upstream/.gitspork/migrations/0001-init.yml`
- `docs/examples/platform-team/upstream/.gitspork/migrations/scripts/0001-init.sh`
- `docs/examples/platform-team/upstream/Makefile`
- `docs/examples/platform-team/upstream/deploy-config.yaml`
- `docs/examples/platform-team/upstream/ci/build.yml`
- `docs/examples/platform-team/upstream/ci/deploy.yml`
- `docs/examples/platform-team/upstream/scripts/shared-bootstrap.sh`
- `docs/examples/platform-team/upstream/README.md`
- `docs/examples/open-source-template/upstream/.gitspork.yml`
- `docs/examples/open-source-template/upstream/.gitspork-templates/CODE_OF_CONDUCT.md.go.tmpl`
- `docs/examples/open-source-template/upstream/.github/workflows/ci.yml`
- `docs/examples/open-source-template/upstream/.github/workflows/release.yml`
- `docs/examples/open-source-template/upstream/.github/ISSUE_TEMPLATE.md`
- `docs/examples/open-source-template/upstream/LICENSE`
- `docs/examples/open-source-template/upstream/CONTRIBUTING.md`
- `docs/examples/open-source-template/upstream/project-meta.json`
- `docs/examples/open-source-template/upstream/README.md`
- `docs/examples/open-source-template/upstream/CHANGELOG.md`
- `docs/examples/standards-library/upstream/.gitspork.yml`
- `docs/examples/standards-library/upstream/.gitspork-templates/service-info.txt.go.tmpl`
- `docs/examples/standards-library/upstream/.gitspork-templates/security-summary.md.go.tmpl`
- `docs/examples/standards-library/upstream/.gitspork/migrations/0001-policy-init.yml`
- `docs/examples/standards-library/upstream/.gitspork/migrations/scripts/0001-policy-init.sh`
- `docs/examples/standards-library/upstream/.golangci.yml`
- `docs/examples/standards-library/upstream/security-policy.yaml`
- `docs/examples/standards-library/upstream/.env.example`
- `docs/examples/standards-library/upstream/policies/data-handling.md`
- `docs/examples/standards-library/upstream/policies/access-control.md`
- `docs/examples/integrate-local/upstream/.gitspork.yaml`
- `docs/examples/integrate-local/upstream/.gitspork-templates/config.yml.go.tmpl`
- `docs/examples/integrate-local/upstream/app-config.yaml`
- `docs/examples/integrate-local/downstream/input-data.json`
- `test/examples/main_test.go`
- `test/examples/platform_team_test.go`
- `test/examples/open_source_template_test.go`
- `test/examples/standards_library_test.go`
- `test/examples/integrate_local_test.go`

**Modified:**
- `test/functional/harness.go` — remove helpers now in `internal/testharness/`, add import
- `Makefile` — add `test-examples` target

---

### Task 1: Extract harness helpers to `internal/testharness/`

**Files:**
- Create: `internal/testharness/testharness.go`
- Modify: `test/functional/harness.go`

- [ ] **Step 1: Create `internal/testharness/testharness.go`**

```go
package testharness

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

func NewUpstreamRepo(t *testing.T, files map[string]string, gitsporkYML string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	merged := make(map[string]string, len(files)+1)
	for k, v := range files {
		merged[k] = v
	}
	if gitsporkYML != "" {
		merged[".gitspork.yml"] = gitsporkYML
	}
	WriteFiles(t, dir, merged)
	CommitAll(t, repo, dir, "initial upstream commit")
	return dir
}

func NewDownstreamRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	return dir
}

func WriteFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}
}

func CommitAll(t *testing.T, repo *gogit.Repository, dir, message string) {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: "gitspork-test", Email: "gitspork-test@localhost", When: time.Now()}
	_, err = wt.Commit(message, &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
}

func OpenRepo(t *testing.T, dir string) *gogit.Repository {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	return repo
}

func ReadFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	require.NoError(t, err, "expected file %s to exist in %s", rel, dir)
	return string(b)
}

func AssertFileAbsent(t *testing.T, dir, rel string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, rel))
	require.ErrorIs(t, err, fs.ErrNotExist, "expected file %s to be absent in %s", rel, dir)
}

func AssertFileContains(t *testing.T, dir, rel, substr string) {
	t.Helper()
	content := ReadFile(t, dir, rel)
	require.Contains(t, content, substr, "file %s does not contain %q", rel, substr)
}
```

- [ ] **Step 2: Replace `test/functional/harness.go` with thin wrappers**

Keep `Runner`, `BinaryRunner`, and `resolveRunner` in place. Replace the repo helper implementations with thin delegates to `internal/testharness/`:

```go
//go:build functional || functional_docker

package functional

import (
	"os/exec"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/rockholla/gitspork/internal/testharness"
)

type Runner interface {
	Run(t *testing.T, args []string, dir string) (output string, exitCode int)
}

type BinaryRunner struct {
	BinaryPath string
}

func (r *BinaryRunner) Run(t *testing.T, args []string, dir string) (string, int) {
	t.Helper()
	cmd := exec.Command(r.BinaryPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("runner: failed to launch binary: %v\noutput: %s", err, out)
		}
	}
	return string(out), code
}

func NewUpstreamRepo(t *testing.T, files map[string]string, gitsporkYML string) string {
	return testharness.NewUpstreamRepo(t, files, gitsporkYML)
}
func NewDownstreamRepo(t *testing.T) string { return testharness.NewDownstreamRepo(t) }
func WriteFiles(t *testing.T, dir string, files map[string]string) {
	testharness.WriteFiles(t, dir, files)
}
func CommitAll(t *testing.T, repo *gogit.Repository, dir, message string) {
	testharness.CommitAll(t, repo, dir, message)
}
func OpenRepo(t *testing.T, dir string) *gogit.Repository { return testharness.OpenRepo(t, dir) }
func ReadFile(t *testing.T, dir, rel string) string       { return testharness.ReadFile(t, dir, rel) }
func AssertFileAbsent(t *testing.T, dir, rel string)      { testharness.AssertFileAbsent(t, dir, rel) }
func AssertFileContains(t *testing.T, dir, rel, substr string) {
	testharness.AssertFileContains(t, dir, rel, substr)
}
```

- [ ] **Step 3: Verify functional tests still pass**

```bash
go test -tags functional -timeout 120s ./test/functional/...
```

Expected: 14 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/testharness/testharness.go test/functional/harness.go
git commit -m "refactor: extract harness helpers to internal/testharness"
```

---
### Task 2: `platform-team` example files

**Files:** All under `docs/examples/platform-team/upstream/`

- [ ] **Step 1: Create `.gitspork.yml`**

```yaml
upstream_owned:
  - ci/**
  - scripts/shared-bootstrap.sh
downstream_owned:
  - README.md
shared_ownership:
  merged:
    - Makefile
  structured:
    prefer_upstream:
      - deploy-config.yaml
templated:
  - template: .gitspork-templates/service-manifest.yml.go.tmpl
    destination: service-manifest.yml
    inputs:
      - name: service_name
        json_data_path: service-input-data.json
      - name: team_name
        json_data_path: service-input-data.json
migrations:
  - .gitspork/migrations/0001-init.yml
```

- [ ] **Step 2: Create remaining files**

`ci/build.yml`:
```yaml
# Managed by platform team via gitspork — do not edit directly.
name: Build
on: [push, pull_request]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Build
        run: make build
```

`ci/deploy.yml`:
```yaml
# Managed by platform team via gitspork — do not edit directly.
name: Deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Deploy
        run: make deploy
```

`scripts/shared-bootstrap.sh`:
```bash
#!/usr/bin/env bash
# Managed by platform team via gitspork — do not edit directly.
set -euo pipefail
echo "bootstrapping service environment..."
```

`Makefile`:
```makefile
# ::gitspork::begin-upstream-owned-block
# Platform targets — managed by platform team via gitspork.
.PHONY: build deploy lint
build:
	@echo "building service..."
deploy:
	@echo "deploying service..."
lint:
	@echo "linting..."
# ::gitspork::end-upstream-owned-block
```

`deploy-config.yaml`:
```yaml
# Upstream platform defaults — prefer_upstream means upstream values win on re-integrate.
region: us-east-1
replicas: 2
rolling_update: true
```

`README.md`:
```markdown
# My Service

> Seeded by gitspork on first integrate — this file is yours to own.

Describe your service here.
```

`.gitspork-templates/service-manifest.yml.go.tmpl`:
```yaml
# Generated by gitspork — do not edit directly.
service: {{ index .Inputs "service_name" }}
team: {{ index .Inputs "team_name" }}
version: "1.0"
```

`.gitspork/migrations/0001-init.yml`:
```yaml
post_integrate:
  exec: ./.gitspork/migrations/scripts/0001-init.sh
```

`.gitspork/migrations/scripts/0001-init.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
echo "migration 0001: post-integrate init complete"
```

- [ ] **Step 3: Commit**

```bash
git add docs/examples/platform-team/
git commit -m "docs: add platform-team example"
```

---
### Task 3: `open-source-template` example files

**Files:** All under `docs/examples/open-source-template/upstream/`

- [ ] **Step 1: Create `.gitspork.yml`**

```yaml
upstream_owned:
  - .github/**
  - LICENSE
  - CONTRIBUTING.md
downstream_owned:
  - README.md
  - CHANGELOG.md
shared_ownership:
  structured:
    prefer_downstream:
      - project-meta.json
templated:
  - template: .gitspork-templates/CODE_OF_CONDUCT.md.go.tmpl
    destination: CODE_OF_CONDUCT.md
    inputs:
      - name: project_name
        json_data_path: project-meta.json
```

- [ ] **Step 2: Create remaining files**

`.github/workflows/ci.yml`:
```yaml
# Managed by upstream template via gitspork — do not edit directly.
name: CI
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Test
        run: make test
```

`.github/workflows/release.yml`:
```yaml
# Managed by upstream template via gitspork — do not edit directly.
name: Release
on:
  push:
    tags: ['v*']
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Release
        run: make release
```

`.github/ISSUE_TEMPLATE.md`:
```markdown
<!-- Managed by upstream template via gitspork — do not edit directly. -->
## Description
<!-- Describe the issue -->

## Steps to reproduce
1.
2.

## Expected behavior

## Actual behavior
```

`LICENSE`:
```
MIT License

Copyright (c) upstream-template contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction.
```

`CONTRIBUTING.md`:
```markdown
# Contributing

<!-- Managed by upstream template via gitspork — do not edit directly. -->

Please read our code of conduct before contributing.

## How to contribute

1. Fork the repository
2. Create a feature branch
3. Submit a pull request
```

`project-meta.json`:
```json
{
  "project_name": "my-project",
  "description": "A project built from the gitspork open-source template."
}
```

`README.md`:
```markdown
# My Project

> Seeded by gitspork on first integrate — this file is yours to own.
```

`CHANGELOG.md`:
```markdown
# Changelog

> Seeded by gitspork on first integrate — this file is yours to own.

## Unreleased
```

`.gitspork-templates/CODE_OF_CONDUCT.md.go.tmpl`:
```markdown
# Code of Conduct for {{ index .Inputs "project_name" }}

<!-- Generated by gitspork — do not edit directly. -->

We are committed to providing a welcoming and inclusive experience for everyone.
```

- [ ] **Step 3: Commit**

```bash
git add docs/examples/open-source-template/
git commit -m "docs: add open-source-template example"
```

---
### Task 4: `standards-library` example files

**Files:** All under `docs/examples/standards-library/upstream/`

- [ ] **Step 1: Create `.gitspork.yml`**

```yaml
upstream_owned:
  - .golangci.yml
  - policies/**
shared_ownership:
  merged:
    - .env.example
  structured:
    prefer_upstream:
      - security-policy.yaml
templated:
  - template: .gitspork-templates/service-info.txt.go.tmpl
    destination: service-info.txt
    inputs:
      - name: service_name
        json_data_path: service-input-data.json
  - template: .gitspork-templates/security-summary.md.go.tmpl
    destination: security-summary.md
    inputs:
      - name: service_name
        previous_input:
          template: service-info.txt.go.tmpl
          name: service_name
migrations:
  - .gitspork/migrations/0001-policy-init.yml
```

- [ ] **Step 2: Create remaining files**

`.golangci.yml`:
```yaml
# Managed by standards library via gitspork — do not edit directly.
linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - staticcheck
    - unused
run:
  timeout: 5m
```

`security-policy.yaml`:
```yaml
# Upstream values win on re-integrate (prefer_upstream).
require_mfa: true
allowed_regions:
  - us-east-1
  - us-west-2
scan_on_push: true
```

`.env.example`:
```
# ::gitspork::begin-upstream-owned-block
# Required by all services — managed via gitspork.
DATABASE_URL=
LOG_LEVEL=info
OTEL_EXPORTER_ENDPOINT=
# ::gitspork::end-upstream-owned-block
```

`policies/data-handling.md`:
```markdown
# Data Handling Policy

<!-- Managed by standards library via gitspork — do not edit directly. -->

All personally identifiable information must be encrypted at rest and in transit.
Retention period: 90 days unless legal hold applies.
```

`policies/access-control.md`:
```markdown
# Access Control Policy

<!-- Managed by standards library via gitspork — do not edit directly. -->

All production access requires MFA. Least-privilege principles apply.
Service accounts must be rotated every 90 days.
```

`.gitspork-templates/service-info.txt.go.tmpl`:
```
Service: {{ index .Inputs "service_name" }}
```

`.gitspork-templates/security-summary.md.go.tmpl`:
```markdown
# Security Summary: {{ index .Inputs "service_name" }}

<!-- Generated by gitspork — do not edit directly. -->

This service complies with the standards-library security policy.
See `security-policy.yaml` for enforcement details.
```

`.gitspork/migrations/0001-policy-init.yml`:
```yaml
post_integrate:
  exec: ./.gitspork/migrations/scripts/0001-policy-init.sh
```

`.gitspork/migrations/scripts/0001-policy-init.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
echo "migration 0001: policy init complete"
```

- [ ] **Step 3: Commit**

```bash
git add docs/examples/standards-library/
git commit -m "docs: add standards-library example"
```

---
### Task 5: `integrate-local` example files

**Files:** All under `docs/examples/integrate-local/`

- [ ] **Step 1: Create files**

`upstream/.gitspork.yaml`:
```yaml
upstream_owned:
  - app-config.yaml
templated:
  - template: .gitspork-templates/config.yml.go.tmpl
    destination: config.yml
    inputs:
      - name: app_name
        json_data_path: input-data.json
      - name: environment
        json_data_path: input-data.json
```

`upstream/app-config.yaml`:
```yaml
# Managed by upstream via gitspork integrate-local — do not edit directly.
log_level: info
metrics_enabled: true
tracing_enabled: true
```

`upstream/.gitspork-templates/config.yml.go.tmpl`:
```yaml
# Generated by gitspork — do not edit directly.
app_name: {{ index .Inputs "app_name" }}
environment: {{ index .Inputs "environment" }}
```

`downstream/input-data.json`:
```json
{
  "app_name": "my-local-app",
  "environment": "development"
}
```

- [ ] **Step 2: Commit**

```bash
git add docs/examples/integrate-local/
git commit -m "docs: add integrate-local example"
```

---
### Task 6: `test/examples/` package scaffold and `main_test.go`

**Files:**
- Create: `test/examples/main_test.go`

- [ ] **Step 1: Create `test/examples/main_test.go`**

```go
//go:build examples

package examples

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		panic("cannot resolve repo root: " + err.Error())
	}
	binaryPath = buildBinary(repoRoot)
	os.Exit(m.Run())
}

func buildBinary(repoRoot string) string {
	dir, err := os.MkdirTemp("", "gitspork-examples-")
	if err != nil {
		panic("cannot create temp dir for binary: " + err.Error())
	}
	out := filepath.Join(dir, "gitspork")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = repoRoot
	if b, err := cmd.CombinedOutput(); err != nil {
		panic("go build failed:\n" + string(b))
	}
	return out
}

// runGitspork runs the binary with args from dir, returns combined output and exit code.
func runGitspork(t *testing.T, args []string, dir string) (string, int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run gitspork: %v\noutput: %s", err, out)
		}
	}
	return string(out), code
}

// exampleUpstreamPath returns the absolute path to a docs/examples/<name>/upstream dir.
func exampleUpstreamPath(t *testing.T, name string) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("cannot resolve repo root: %v", err)
	}
	return filepath.Join(repoRoot, "docs", "examples", name, "upstream")
}

// examplePath returns the absolute path to a docs/examples/<name> dir.
func examplePath(t *testing.T, name string) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("cannot resolve repo root: %v", err)
	}
	return filepath.Join(repoRoot, "docs", "examples", name)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build -tags examples ./test/examples/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add test/examples/main_test.go
git commit -m "test: add test/examples package scaffold"
```

---
### Task 7: `platform_team_test.go`

**Files:**
- Create: `test/examples/platform_team_test.go`

The test needs an upstream git repo. Because `docs/examples/platform-team/upstream/` is a static directory (not a git repo), the test initializes a fresh repo from it using `testharness.NewUpstreamRepo`. It reads the example files and passes them as the `files` map. The `.gitspork.yml` is already in those files so we pass `""` for the gitsporkYML arg.

Actually simpler: `testharness.NewUpstreamRepo` writes files then commits. We can read all files from the upstream dir at test time and pass them. But that's complex. Better approach: the test uses a helper `initRepoFromDir` that inits a git repo at a temp location copying all files from the example upstream dir.

- [ ] **Step 1: Create `test/examples/platform_team_test.go`**

```go
//go:build examples

package examples

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/rockholla/gitspork/internal/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformTeamExample(t *testing.T) {
	upstreamDir := initExampleRepo(t, "platform-team")
	downstreamDir := testharness.NewDownstreamRepo(t)

	// Seed the input data file the templated entry expects.
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"payments-service","team_name":"platform"}`,
	})

	out, code := runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate failed:\n%s", out)

	// upstream-owned CI files land
	testharness.AssertFileContains(t, downstreamDir, "ci/build.yml", "Build")
	testharness.AssertFileContains(t, downstreamDir, "ci/deploy.yml", "Deploy")
	testharness.AssertFileContains(t, downstreamDir, "scripts/shared-bootstrap.sh", "bootstrapping")

	// downstream-owned README seeded
	testharness.AssertFileContains(t, downstreamDir, "README.md", "Seeded by gitspork")

	// Makefile merged — upstream block present
	testharness.AssertFileContains(t, downstreamDir, "Makefile", "::gitspork::begin-upstream-owned-block")
	testharness.AssertFileContains(t, downstreamDir, "Makefile", "platform targets")

	// deploy-config.yaml prefer_upstream value present
	testharness.AssertFileContains(t, downstreamDir, "deploy-config.yaml", "us-east-1")

	// template rendered
	testharness.AssertFileContains(t, downstreamDir, "service-manifest.yml", "payments-service")
	testharness.AssertFileContains(t, downstreamDir, "service-manifest.yml", "platform")

	// commit baseline, re-integrate, check-drift exits 0
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"payments-service","team_name":"platform"}`,
	})
	out, code = runGitspork(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "check-drift expected no drift:\n%s", out)

	// downstream-owned README not overwritten after customization
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"README.md": "# payments-service\n\nCustom readme.\n",
	})
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "customize README")
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"payments-service","team_name":"platform"}`,
	})
	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)
	content := testharness.ReadFile(t, downstreamDir, "README.md")
	assert.Contains(t, content, "Custom readme", "README.md should not be overwritten")

	// deploy-config.yaml prefer_upstream still wins after re-integrate
	testharness.AssertFileContains(t, downstreamDir, "deploy-config.yaml", "us-east-1")
}

// initExampleRepo copies a docs/examples/<name>/upstream dir into a fresh temp git repo.
func initExampleRepo(t *testing.T, name string) string {
	t.Helper()
	srcDir := exampleUpstreamPath(t, name)
	dstDir := t.TempDir()

	repo, err := gogit.PlainInit(dstDir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)

	err = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dstDir, rel), 0755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dstDir, rel), b, 0644)
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: "gitspork-test", Email: "gitspork-test@localhost", When: time.Now()}
	_, err = wt.Commit("initial example commit", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	return dstDir
}
```

- [ ] **Step 2: Run the test**

```bash
go test -tags examples -timeout 120s -v -run TestPlatformTeamExample ./test/examples/...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/examples/platform_team_test.go
git commit -m "test: add platform-team example test"
```

---
### Task 8: `open_source_template_test.go`

**Files:**
- Create: `test/examples/open_source_template_test.go`

- [ ] **Step 1: Create the file**

```go
//go:build examples

package examples

import (
	"testing"

	"github.com/rockholla/gitspork/internal/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenSourceTemplateExample(t *testing.T) {
	upstreamDir := initExampleRepo(t, "open-source-template")
	downstreamDir := testharness.NewDownstreamRepo(t)

	// project-meta.json is both upstream seed and downstream input for template.
	// On first integrate it lands from upstream; downstream then owns it (prefer_downstream).
	// No extra input file needed — project-meta.json itself is the json_data_path.

	out, code := runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate failed:\n%s", out)

	// upstream-owned files land
	testharness.AssertFileContains(t, downstreamDir, ".github/workflows/ci.yml", "CI")
	testharness.AssertFileContains(t, downstreamDir, ".github/workflows/release.yml", "Release")
	testharness.AssertFileContains(t, downstreamDir, ".github/ISSUE_TEMPLATE.md", "Description")
	testharness.AssertFileContains(t, downstreamDir, "LICENSE", "MIT License")
	testharness.AssertFileContains(t, downstreamDir, "CONTRIBUTING.md", "Contributing")

	// downstream-owned files seeded
	testharness.AssertFileContains(t, downstreamDir, "README.md", "Seeded by gitspork")
	testharness.AssertFileContains(t, downstreamDir, "CHANGELOG.md", "Changelog")

	// template rendered using project-meta.json
	testharness.AssertFileContains(t, downstreamDir, "CODE_OF_CONDUCT.md", "my-project")

	// commit baseline
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// check-drift exits 0
	out, code = runGitspork(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "check-drift expected no drift:\n%s", out)

	// customize README and CHANGELOG, re-integrate, assert not overwritten
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"README.md":    "# my-project\n\nCustom readme.\n",
		"CHANGELOG.md": "# Changelog\n\n## v1.0.0\n- initial release\n",
	})
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "customize downstream-owned files")

	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	readme := testharness.ReadFile(t, downstreamDir, "README.md")
	assert.Contains(t, readme, "Custom readme", "README.md should not be overwritten")

	changelog := testharness.ReadFile(t, downstreamDir, "CHANGELOG.md")
	assert.Contains(t, changelog, "initial release", "CHANGELOG.md should not be overwritten")

	// downstream modified project-meta.json, re-integrate: prefer_downstream value survives
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"project-meta.json": `{"project_name":"forked-project","description":"My fork."}`,
	})
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "customize project-meta.json")

	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after project-meta change failed:\n%s", out)

	meta := testharness.ReadFile(t, downstreamDir, "project-meta.json")
	assert.Contains(t, meta, "forked-project", "project-meta.json downstream value should survive (prefer_downstream)")
}
```

- [ ] **Step 2: Run the test**

```bash
go test -tags examples -timeout 120s -v -run TestOpenSourceTemplateExample ./test/examples/...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/examples/open_source_template_test.go
git commit -m "test: add open-source-template example test"
```

---
### Task 9: `standards_library_test.go`

**Files:**
- Create: `test/examples/standards_library_test.go`

- [ ] **Step 1: Create the file**

```go
//go:build examples

package examples

import (
	"testing"

	"github.com/rockholla/gitspork/internal/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandardsLibraryExample(t *testing.T) {
	upstreamDir := initExampleRepo(t, "standards-library")
	downstreamDir := testharness.NewDownstreamRepo(t)

	// Seed input data for templated entries
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"auth-service"}`,
	})

	out, code := runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate failed:\n%s", out)

	// upstream-owned files land
	testharness.AssertFileContains(t, downstreamDir, ".golangci.yml", "errcheck")
	testharness.AssertFileContains(t, downstreamDir, "policies/data-handling.md", "Data Handling Policy")
	testharness.AssertFileContains(t, downstreamDir, "policies/access-control.md", "Access Control Policy")

	// .env.example merged — upstream block present
	testharness.AssertFileContains(t, downstreamDir, ".env.example", "::gitspork::begin-upstream-owned-block")
	testharness.AssertFileContains(t, downstreamDir, ".env.example", "DATABASE_URL")

	// security-policy.yaml prefer_upstream values present
	testharness.AssertFileContains(t, downstreamDir, "security-policy.yaml", "require_mfa: true")

	// templates rendered — service-info uses json_data_path, security-summary uses previous_input
	testharness.AssertFileContains(t, downstreamDir, "service-info.txt", "auth-service")
	testharness.AssertFileContains(t, downstreamDir, "security-summary.md", "auth-service")

	// commit baseline
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// check-drift exits 0
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"auth-service"}`,
	})
	out, code = runGitspork(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "check-drift expected no drift:\n%s", out)

	// security-policy.yaml prefer_upstream still wins after downstream modifies it
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"security-policy.yaml": "require_mfa: false\n",
	})
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "downstream tries to override security policy")
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"auth-service"}`,
	})

	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	policy := testharness.ReadFile(t, downstreamDir, "security-policy.yaml")
	assert.Contains(t, policy, "require_mfa: true", "security-policy.yaml prefer_upstream value should win")
}
```

- [ ] **Step 2: Run the test**

```bash
go test -tags examples -timeout 120s -v -run TestStandardsLibraryExample ./test/examples/...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/examples/standards_library_test.go
git commit -m "test: add standards-library example test"
```

---
### Task 10: `integrate_local_test.go`

**Files:**
- Create: `test/examples/integrate_local_test.go`

- [ ] **Step 1: Create the file**

```go
//go:build examples

package examples

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rockholla/gitspork/internal/testharness"
	"github.com/stretchr/testify/require"
)

func TestIntegrateLocalExample(t *testing.T) {
	exDir := examplePath(t, "integrate-local")
	upstreamDir := filepath.Join(exDir, "upstream")
	exDownstreamDir := filepath.Join(exDir, "downstream")

	// Create a temp downstream seeded with the example's input-data.json.
	downstreamDir := t.TempDir()
	inputData, err := os.ReadFile(filepath.Join(exDownstreamDir, "input-data.json"))
	require.NoError(t, err)
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"input-data.json": string(inputData),
	})

	out, code := runGitspork(t, []string{
		"integrate-local",
		"--upstream-path", upstreamDir,
		"--downstream-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate-local failed:\n%s", out)

	// upstream-owned file lands
	testharness.AssertFileContains(t, downstreamDir, "app-config.yaml", "log_level: info")

	// template rendered with values from input-data.json
	testharness.AssertFileContains(t, downstreamDir, "config.yml", "my-local-app")
	testharness.AssertFileContains(t, downstreamDir, "config.yml", "development")
}
```

- [ ] **Step 2: Run the test**

```bash
go test -tags examples -timeout 120s -v -run TestIntegrateLocalExample ./test/examples/...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/examples/integrate_local_test.go
git commit -m "test: add integrate-local example test"
```

---
### Task 11: Add `test-examples` Makefile target

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add target after `test-functional-docker`**

In `Makefile`, add after the `test-functional-docker` target:

```makefile
.PHONY: test-examples
test-examples:
	@go test -tags examples -timeout 120s -v ./test/examples/...
```

- [ ] **Step 2: Run all three test suites to confirm nothing broke**

```bash
make test && make test-functional && make test-examples
```

Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: add test-examples make target"
```

---

## Self-Review

**Spec coverage check:**
- ✅ `internal/testharness/` extraction — Task 1
- ✅ platform-team example files — Task 2
- ✅ open-source-template example files — Task 3
- ✅ standards-library example files — Task 4
- ✅ integrate-local example files — Task 5
- ✅ `test/examples/` scaffold + binary build — Task 6
- ✅ platform-team test (upstream-owned, downstream-owned, merged, prefer_upstream, templated, migration, check-drift) — Task 7
- ✅ open-source-template test (upstream-owned, downstream-owned, prefer_downstream, templated, check-drift) — Task 8
- ✅ standards-library test (upstream-owned, merged, prefer_upstream, previous_input templated, migration, check-drift) — Task 9
- ✅ integrate-local test — Task 10
- ✅ `make test-examples` — Task 11

**Placeholder scan:** None found.

**Type consistency:** `initExampleRepo` defined in Task 7 (`platform_team_test.go`) and called in Tasks 8, 9 — all in same package `examples`, so it's visible. `examplePath` and `exampleUpstreamPath` defined in `main_test.go` (Task 6), called across all test files — same package, visible.

**Note on `initExampleRepo` placement:** It's defined in `platform_team_test.go` but used by all example tests. Move it to `main_test.go` so it's clearly a shared helper and not buried in one test file. The plan as written works (same package), but the implementer should put `initExampleRepo` in `main_test.go` alongside the other helpers.
