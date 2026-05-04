# CI Release Pipeline & Homebrew Tap Design

**Goal:** Move goreleaser execution from the local machine into GitHub Actions, triggered by a semver tag push, while adding Homebrew tap support.

**Architecture:** `make release` becomes tag-only; a reusable `tests.yml` workflow is shared between `main.yml` and a new `release.yml`; goreleaser runs in CI with Docker Hub and Homebrew tap credentials from Actions secrets.

---

## `scripts/release.sh`

Remove all goreleaser invocation, `IS_LATEST` logic, and `require_binary "goreleaser"`. New responsibilities only:

1. Fetch latest tags from remote; print the most recent semver tag for context
2. Prompt for next version (same semver validation)
3. Validate tag doesn't exist locally or remotely
4. Prompt for tag description (becomes the annotated tag message; goreleaser uses it for GitHub Release notes)
5. Confirm, then `git tag -a <version> -m <description>` and `git push origin <version>`

On push failure the local tag is deleted (same rollback as today). Script exits after the push — CI takes it from there.

---

## `.github/workflows/tests.yml` (new — reusable workflow)

Extracts the four test jobs from `main.yml` into a called workflow. Accepts no inputs. All four jobs run in parallel:

- `unit-tests`: `make test-unit`
- `functional-tests`: `make test-functional`
- `functional-container-tests`: `make test-functional-docker` (requires Docker Buildx)
- `examples-tests`: `make test-examples`

All jobs use `actions/setup-go@v4` with `go-version: '1.26'` and `actions/checkout@v4`.

---

## `.github/workflows/main.yml` (updated)

Replace the four inline test job definitions with a single job that calls the reusable workflow:

```yaml
jobs:
  tests:
    uses: ./.github/workflows/tests.yml
```

---

## `.github/workflows/release.yml` (new)

Triggered by `push` to tags matching `v*`.

**Job: `tests`** — calls `./.github/workflows/tests.yml`. All four suites must pass.

**Job: `release`** — `needs: tests`. Steps:
- `actions/checkout@v4` with `fetch-depth: 0` (goreleaser needs full history for changelog)
- `actions/setup-go@v4` with `go-version: '1.26'`
- `docker/setup-buildx-action@v4` (multi-arch image builds)
- `docker/login-action` using `DOCKER_HUB_TOKEN` secret and username `rockholla`
- `goreleaser/goreleaser-action` running `goreleaser release --clean`
- Env vars passed to goreleaser step: `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`, `HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}`

---

## `.goreleaser.yaml` changes

**ldflags** (both `linux` and `darwin` build targets):
```yaml
ldflags:
  - -s -w -X main.version={{ .Tag }}
```
`GITSPORK_VERSION` env var injection removed.

**docker_manifests `latest` skip_push**:
```yaml
skip_push: '{{ if .Prerelease }}true{{ else }}false{{ end }}'
```
Replaces `{{ ne .Env.IS_LATEST "true" }}`. goreleaser derives pre-release status from the tag's semver automatically.

**Add `brews:` block**:
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

goreleaser generates the formula from the darwin `amd64` and `arm64` tarballs, writing correct SHA256 hashes and download URLs automatically.

---

## `rockholla/homebrew-gitspork` tap repo (new, created manually)

Minimal structure required before first release:

```
Formula/
  .gitkeep        ← goreleaser overwrites this with gitspork.rb on first release
README.md
```

`README.md` content:
```
brew tap rockholla/gitspork
brew install gitspork
```

**Required secret:** A GitHub Personal Access Token (classic or fine-grained) with `Contents: write` on `rockholla/homebrew-gitspork`, added to `rockholla/gitspork` Actions secrets as `HOMEBREW_TAP_GITHUB_TOKEN`.

`DOCKER_HUB_TOKEN` is already configured.

---

## Summary of secrets required in `rockholla/gitspork`

| Secret | Purpose |
|---|---|
| `DOCKER_HUB_TOKEN` | Push images to Docker Hub (already set) |
| `HOMEBREW_TAP_GITHUB_TOKEN` | Push formula to `rockholla/homebrew-gitspork` |
| `GITHUB_TOKEN` | Built-in — goreleaser creates the GitHub Release |
