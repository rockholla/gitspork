# CI Release Pipeline & Homebrew Tap Design

**Goal:** Move goreleaser execution from the local machine into GitHub Actions, triggered by a semver tag push, while adding Homebrew tap support.

**Architecture:** `make release` becomes tag-only; a reusable `tests.yml` workflow is shared between `main.yml` and a new `release.yml`; goreleaser runs in CI with Docker Hub and Homebrew tap credentials from Actions secrets.

---

## `scripts/release.sh`

Remove all goreleaser invocation, `IS_LATEST` logic, and `require_binary "goreleaser"`. New responsibilities only:

1. **TTY check** — fail immediately if stdin is not a terminal (`[[ ! -t 0 ]]`), so the script cannot be piped non-interactively.
2. **Origin remote guard** — fail if no `origin` remote is configured.
3. **Fetch and display latest tags** — two separate lookups from a single `git ls-remote` call: `latest_stable_tag` (no pre-release identifier) and `latest_prerelease_tag` (contains `-`). Both are printed for context.
4. **Prompt for next version** — semver regex requires a `v` prefix (`'^v(0|[1-9][0-9]*)...'`). Warn message says "v prefix".
5. **Branch guard** — if not on `main`, the version must contain a pre-release identifier (e.g. `-rc.1`); stable releases require `main`.
6. **Validate tag doesn't exist locally** — `git tag -l "${version}"`.
7. **Validate tag doesn't exist remotely** — `git ls-remote --exit-code --tags origin "${version}"`.
8. **Prompt for tag description** (becomes the annotated tag message; goreleaser uses it for GitHub Release notes).
9. **Confirm, then** `git tag -a <version> -m <description>` and `git push origin <version>`.
10. **Final info message** — includes the GitHub Actions URL constructed from `git remote get-url origin`.

On push failure the local tag is deleted (same rollback as today). Script exits after the push — CI takes it from there.

---

## `.github/workflows/tests.yml` (new — reusable workflow)

Extracts the four test jobs from `main.yml` into a called workflow. Accepts no inputs. All four jobs run in parallel:

- `unit-tests`: `make test-unit`
- `functional-tests`: `make test-functional`
- `functional-container-tests`: `make test-functional-docker` (requires Docker Buildx)
- `examples-tests`: `make test-examples`

All jobs use `actions/setup-go@v4` with `go-version-file: 'go.mod'` and `actions/checkout@v4`.

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

**Job: `release`** — `needs: tests`. Has `permissions: contents: write` (required for goreleaser to create the GitHub Release). Steps:
- `actions/checkout@v4` with `fetch-depth: 0` (goreleaser needs full history for changelog)
- `actions/setup-go@v4` with `go-version-file: 'go.mod'`
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

## Documentation changes

Two docs are updated:

- **`docs/README.md`** — Homebrew install snippet added:
  ```bash
  brew tap rockholla/gitspork
  brew install gitspork
  ```
- **`CONTRIBUTING.md`** (new file) — Contains the releasing workflow description (how to run `make release`, what the branch guard enforces, what CI does after the push) and the one-time pre-release setup prerequisites (tap repo, `HOMEBREW_TAP_GITHUB_TOKEN`, `DOCKER_HUB_TOKEN`).

The releasing instructions and prerequisites are in `CONTRIBUTING.md` rather than `docs/README.md` to keep the main README focused on usage.

---

## Summary of secrets required in `rockholla/gitspork`

| Secret | Purpose |
|---|---|
| `DOCKER_HUB_TOKEN` | Push images to Docker Hub (already set) |
| `HOMEBREW_TAP_GITHUB_TOKEN` | Push formula to `rockholla/homebrew-gitspork` |
| `GITHUB_TOKEN` | Built-in — goreleaser creates the GitHub Release |
