# CI Release Pipeline & Homebrew Tap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move goreleaser execution into GitHub Actions triggered by a semver tag push, extract test jobs into a reusable workflow, and add Homebrew tap support via goreleaser.

**Architecture:** `scripts/release.sh` becomes tag-only (no goreleaser); a new `.github/workflows/tests.yml` reusable workflow is called by both `main.yml` and a new `release.yml`; `.goreleaser.yaml` gains a `brews:` block and drops env-var-based version/latest logic.

**Tech Stack:** Bash, GitHub Actions (reusable workflows), goreleaser v2, `goreleaser/goreleaser-action@v6`, `docker/login-action`, `docker/setup-buildx-action@v4`.

---

## File Map

| File | Change |
|---|---|
| `scripts/release.sh` | Remove goreleaser invocation, `IS_LATEST`, `semver_latest_regex`, `require_binary "goreleaser"`; add remote tag fetch + display of latest tag |
| `.github/workflows/tests.yml` | Create — reusable workflow containing the four test jobs |
| `.github/workflows/main.yml` | Replace four inline test jobs with a single `uses:` call to `tests.yml` |
| `.github/workflows/release.yml` | Create — triggered on `v*` tag push; calls `tests.yml` then runs goreleaser |
| `.goreleaser.yaml` | Update ldflags to use `{{ .Tag }}`; update `skip_push` to use `.Prerelease`; add `brews:` block |

---

### Task 1: Rewrite `scripts/release.sh`

**Files:**
- Modify: `scripts/release.sh`

- [ ] **Step 1: Replace the file contents**

```bash
#!/usr/bin/env bash

set -eo pipefail

this_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
. "${this_dir}/.lib.sh"

version=""
description=""

semver_regex='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?(\+([0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*))?$'

require_binary "git"

if [ -n "$(git status --porcelain)" ]; then
  err "Releasing only allowed on a clean working tree"
fi
handle_errors "exit 1"

if [[ ! -t 0 ]]; then
  err "This script must be run interactively (stdin is not a terminal)"
  exit 1
fi

if ! git remote get-url origin &>/dev/null; then
  err "No 'origin' remote configured"
  exit 1
fi

remote_tags="$(git ls-remote --tags --sort=-v:refname origin 'refs/tags/v*' \
  | grep -v '\^{}' \
  | sed 's|.*refs/tags/||')"
latest_stable_tag="$(echo "$remote_tags" | grep -v -- '-' | head -1)"
latest_prerelease_tag="$(echo "$remote_tags" | grep -- '-' | head -1)"
if [ -n "$latest_stable_tag" ]; then
  info "Most recent stable tag: ${latest_stable_tag}"
else
  info "No stable remote tags found yet"
fi
if [ -n "$latest_prerelease_tag" ]; then
  info "Most recent pre-release tag: ${latest_prerelease_tag}"
fi

while true; do
  version="$(get_user_input "What version do you want to release?")"
  if [[ "$version" =~ $semver_regex ]]; then
    break
  fi
  warn "Please enter a valid semver with v prefix (e.g. v1.2.3)"
done

current_branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$current_branch" != "main" && ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+- ]]; then
  err "Stable releases require the main branch. Current branch: ${current_branch}. Use a pre-release version (e.g. ${version}-rc.1) or switch to main."
  handle_errors "exit 1"
fi

if [ -n "$(git tag -l "${version}")" ]; then
  err "git tag for version ${version} already exists locally"
fi
if git ls-remote --exit-code --tags origin "${version}" &>/dev/null; then
  err "git tag for version ${version} already exists in remote origin repo"
fi
handle_errors "exit 1"

while true; do
  description="$(get_user_input "What's the description for this release?")"
  if [ -n "$description" ]; then
    break
  fi
  warn "Description can't be blank"
done

info "Releasing gitspork version: ${version}, tag description: ${description}"
confirm="$(get_user_input "Proceed? [y/N]")"
if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
  info "Aborted."
  exit 0
fi

git tag -a "${version}" -m "${description}"
git push origin "${version}" || { git tag -d "${version}" 2>/dev/null || true; exit 1; }

repo_url="$(git remote get-url origin | sed 's|git@github.com:|https://github.com/|; s|\.git$||')"
info "Tag ${version} pushed. Watch the release workflow at: ${repo_url}/actions?query=branch%3A${version}"
```

- [ ] **Step 2: Verify the script is executable and parses without error**

```bash
bash -n scripts/release.sh && echo "syntax OK"
```
Expected: `syntax OK`

- [ ] **Step 3: Commit**

```bash
git add scripts/release.sh
git commit -m "feat: make release script tag-only, goreleaser moved to CI"
```

---

### Task 2: Create `.github/workflows/tests.yml` (reusable workflow)

**Files:**
- Create: `.github/workflows/tests.yml`

- [ ] **Step 1: Create the file**

```yaml
name: Tests

on:
  workflow_call:

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - name: Setup Go
      uses: actions/setup-go@v4
      with:
        go-version-file: 'go.mod'
    - name: Unit Tests
      run: make test-unit

  functional-tests:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - name: Setup Go
      uses: actions/setup-go@v4
      with:
        go-version-file: 'go.mod'
    - name: Functional Tests
      run: make test-functional

  functional-container-tests:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - name: Setup Go
      uses: actions/setup-go@v4
      with:
        go-version-file: 'go.mod'
    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v4
    - name: Functional Container/Docker Tests
      run: make test-functional-docker

  examples-tests:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    - name: Setup Go
      uses: actions/setup-go@v4
      with:
        go-version-file: 'go.mod'
    - name: Examples Tests
      run: make test-examples
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/tests.yml
git commit -m "ci: extract test jobs into reusable workflow"
```

---

### Task 3: Update `.github/workflows/main.yml`

**Files:**
- Modify: `.github/workflows/main.yml`

- [ ] **Step 1: Replace the file contents**

```yaml
name: CI

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  tests:
    uses: ./.github/workflows/tests.yml
```

- [ ] **Step 2: Verify it is valid YAML**

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/main.yml'))" && echo "valid YAML"
```
Expected: `valid YAML`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/main.yml
git commit -m "ci: use reusable tests workflow in main CI"
```

---

### Task 4: Create `.github/workflows/release.yml`

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create the file**

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  tests:
    uses: ./.github/workflows/tests.yml

  release:
    needs: tests
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
    - uses: actions/checkout@v4
      with:
        fetch-depth: 0

    - name: Setup Go
      uses: actions/setup-go@v4
      with:
        go-version-file: 'go.mod'

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v4

    - name: Login to Docker Hub
      uses: docker/login-action@v3
      with:
        username: rockholla
        password: ${{ secrets.DOCKER_HUB_TOKEN }}

    - name: Run goreleaser
      uses: goreleaser/goreleaser-action@v6
      with:
        version: '~> v2'
        args: release --clean
      env:
        GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

- [ ] **Step 2: Verify it is valid YAML**

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))" && echo "valid YAML"
```
Expected: `valid YAML`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add release workflow triggered on v* tag push"
```

---

### Task 5: Update `.goreleaser.yaml`

**Files:**
- Modify: `.goreleaser.yaml`

- [ ] **Step 1: Update ldflags in both build targets**

In the `linux` build:
```yaml
  - id: linux
    env:
      - CGO_ENABLED=0
    goos:
      - linux
    goarch:
      - amd64
      - arm64
      - arm
      - "386"
    goarm:
      - "6"
      - "7"
    ldflags:
      - -s -w -X main.version={{ .Tag }}
```

In the `darwin` build:
```yaml
  - id: darwin
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{ .Tag }}
```

- [ ] **Step 2: Update `skip_push` in the `latest` docker_manifest**

Change:
```yaml
  - name_template: "rockholla/gitspork:latest"
    skip_push: '{{ ne .Env.IS_LATEST "true" }}'
```
To:
```yaml
  - name_template: "rockholla/gitspork:latest"
    skip_push: '{{ if .Prerelease }}true{{ else }}false{{ end }}'
```

- [ ] **Step 3: Add `brews:` block**

Add after the `docker_manifests:` block and before the `release:` block:

```yaml
brews:
  - name: gitspork
    repository:
      owner: rockholla
      name: homebrew-gitspork
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    homepage: "https://github.com/rockholla/gitspork"
    description: "A tool for managing upstream/downstream git repo relationships"
    license: "MIT"
    install: |
      bin.install "gitspork"
    test: |
      system "#{bin}/gitspork", "--version"
```

- [ ] **Step 4: Verify goreleaser config is valid**

```bash
goreleaser check
```
Expected: output with no errors (warnings about missing env vars like `HOMEBREW_TAP_GITHUB_TOKEN` are fine — they're only needed at release time)

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml
git commit -m "feat: update goreleaser for CI execution and add Homebrew tap"
```

---

### Task 6: Document the manual setup prerequisites

**Files:**
- Modify: `docs/README.md` — add Homebrew install snippet
- Create: `CONTRIBUTING.md` — add releasing workflow and pre-release setup prerequisites

- [ ] **Step 1: Add Homebrew install snippet to `docs/README.md`**

Add at the end of the file:

```markdown
### Installing via Homebrew

```bash
brew tap rockholla/gitspork
brew install gitspork
```
```

- [ ] **Step 2: Create `CONTRIBUTING.md` with releasing and prerequisites content**

```markdown
## Releasing

Releases are published via `make release`. This will:

1. Show the most recent stable and pre-release remote tags for context
2. Prompt for the new version (must be valid semver with `v` prefix, e.g. `v1.2.3`)
3. Enforce branch guard: stable releases require the `main` branch; pre-release versions (e.g. `v1.2.3-rc.1`) may be released from any branch
4. Prompt for a release description (used as the annotated tag message and GitHub Release notes)
5. Push the tag to GitHub

GitHub Actions then takes over: runs all test suites, builds multi-arch binaries and Docker images, publishes a GitHub Release, pushes Docker images to Docker Hub, and updates the Homebrew formula in `rockholla/homebrew-gitspork`.

### Pre-release setup (one-time)

Before the first release, ensure the following are in place:

1. **`rockholla/homebrew-gitspork` repo exists** on GitHub (public) with a `Formula/` directory.
2. **`HOMEBREW_TAP_GITHUB_TOKEN`** — a GitHub PAT with `Contents: write` on `rockholla/homebrew-gitspork`, added to this repo's Actions secrets.
3. **`DOCKER_HUB_TOKEN`** — already configured.
```

- [ ] **Step 3: Commit**

```bash
git add docs/README.md CONTRIBUTING.md
git commit -m "docs: add homebrew install snippet to README; add releasing/prerequisites to CONTRIBUTING.md"
```
