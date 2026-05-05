# Example: Open Source Template

**Scenario:** A maintainer publishes a template repository for open source Go projects. When someone starts a new project from this template, they get standard GitHub Actions workflows, a license, contributing guidelines, and a code of conduct — all managed by the upstream. The downstream project owns its own README, changelog, and a `project-meta.json` file that customises generated content.

## What this example demonstrates

| Feature | File(s) | Behaviour |
|---|---|---|
| `upstream_owned` | `.github/**`, `LICENSE`, `CONTRIBUTING.md` | The template maintainer owns these — they are always synced to downstream on integrate |
| `downstream_owned` | `README.md`, `CHANGELOG.md` | Seeded from upstream on first integrate, then never touched again — the project owner writes their own |
| `shared_ownership.structured.prefer_downstream` | `project-meta.json` | Upstream seeds this file with defaults; once the downstream customises it, their values survive all future re-integrates |
| `templated` | `CODE_OF_CONDUCT.md` | Rendered from `project-meta.json` using `project_name` — re-rendered on every integrate so it always reflects the current project name |

## Real-world mapping

This pattern fits **GitHub template repositories** or any **scaffolding tool** where:

- The template author wants to push workflow and policy updates to all projects derived from the template (e.g. updating CI to use a new runner image, or refreshing the license year).
- Each project needs to personalise shared documents. `prefer_downstream` on `project-meta.json` means a project can change their name or description and that change survives when they re-integrate to pick up CI updates.
- Generated files like `CODE_OF_CONDUCT.md` stay branded to the project automatically — the template author never needs to know the downstream project's name.

## Running this example

From the repo root:

```bash
make test-examples
```

Or to run just this scenario:

```bash
go test -tags examples -v ./test/examples/... -run TestOpenSourceTemplateExample
```

## Directory structure

```
upstream/                          ← the gitspork upstream repo (the template)
  .gitspork.yml                    ← gitspork configuration
  .github/                         ← upstream-owned GitHub Actions and issue templates
  LICENSE                          ← upstream-owned license file
  CONTRIBUTING.md                  ← upstream-owned contributing guide
  README.md                        ← seeded to downstream; downstream owns it thereafter
  CHANGELOG.md                     ← seeded to downstream; downstream owns it thereafter
  project-meta.json                ← prefer_downstream structured data; upstream seeds defaults
  .gitspork-templates/             ← Go templates rendered into each downstream
    CODE_OF_CONDUCT.md.go.tmpl     ← rendered using project_name from project-meta.json
```
