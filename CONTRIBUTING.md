# Contributing to gitspork

## Releasing

Releases are published via `make release`. This will:

1. Show the most recent remote tag for context
2. Prompt for the new version (must be valid semver with `v` prefix, e.g. `v1.2.3`)
3. Prompt for a release description (used as the annotated tag message and GitHub Release notes)
4. Push the tag to GitHub

GitHub Actions then takes over: runs all test suites, builds multi-arch binaries and Docker images, publishes a GitHub Release, pushes Docker images to Docker Hub, and updates the Homebrew formula in `rockholla/homebrew-gitspork`.

### Pre-release setup (one-time)

Before the first release, ensure the following are in place:

1. **`rockholla/homebrew-gitspork` repo exists** on GitHub (public) with a `Formula/` directory.
2. **`HOMEBREW_TAP_GITHUB_TOKEN`** — a GitHub PAT with `Contents: write` on `rockholla/homebrew-gitspork`, added to this repo's Actions secrets.
3. **`DOCKER_HUB_TOKEN`** — already configured.
