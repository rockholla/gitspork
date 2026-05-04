# Contributing to gitspork

> [!NOTE]
> This is still a work in-progress, stay tuned while more is built out here to help developers contribute via AI tooling and otherwise

## Releasing

Releases are published via `make release`. This will:

1. Show the most recent remote tag for context
2. Prompt for the new version (must be valid semver with `v` prefix, e.g. `v1.2.3`)
3. Prompt for a release description (used as the annotated tag message and GitHub Release notes)
4. Push the tag to GitHub

GitHub Actions then takes over: runs all test suites, builds multi-arch binaries and Docker images, publishes a GitHub Release, pushes Docker images to Docker Hub, and updates the Homebrew formula in `rockholla/homebrew-gitspork`.
