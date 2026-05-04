# Example: Platform Team

**Scenario:** A platform engineering team maintains a shared upstream repository that standardises CI pipelines, deployment configuration, and build tooling across every service in the organisation. Individual service teams fork or integrate from this upstream to get standard infrastructure without copying it manually.

## What this example demonstrates

| Feature | File(s) | Behaviour |
|---|---|---|
| `upstream_owned` | `ci/**`, `scripts/shared-bootstrap.sh` | Platform team owns these outright — they are always overwritten on integrate |
| `downstream_owned` | `README.md` | Service teams write their own README; gitspork seeds an initial one but never overwrites it again |
| `shared_ownership.merged` | `Makefile` | Platform team injects a fenced block of standard targets into the service's Makefile without owning the whole file |
| `shared_ownership.structured.prefer_upstream` | `deploy-config.yaml` | Deployment region config is set by the platform team; downstream changes are silently overwritten on re-integrate |
| `templated` | `service-manifest.yml` | A per-service manifest is rendered from `service-input-data.json`, pulling `service_name` and `team_name` |
| `migrations` | `.gitspork/migrations/0001-init.yml` | A one-time post-integrate script runs on first adoption to initialise any bootstrapping the platform requires |

## Real-world mapping

This pattern is common in organisations where a **platform or infrastructure team** wants to enforce consistency across many service repositories:

- CI pipeline files stay in sync — if the platform team upgrades the build image or adds a new lint step, every service picks it up on next integrate.
- The Makefile `merged` pattern lets service teams add their own targets while the platform's `build`, `deploy`, and `lint` targets are always present and up-to-date.
- `deploy-config.yaml` uses `prefer_upstream` so the platform team can mandate deployment regions or environment settings that individual teams cannot accidentally override.
- `service-manifest.yml` is generated per service, so the platform's service registry always has accurate metadata without requiring manual maintenance.

## Running this example

From the repo root:

```bash
make test-examples
```

Or to run just this scenario:

```bash
go test -tags examples -v ./test/examples/... -run TestPlatformTeamExample
```

## Directory structure

```
upstream/               ← the gitspork upstream repo (owned by the platform team)
  .gitspork.yml         ← gitspork configuration
  ci/                   ← upstream-owned CI pipeline files
  scripts/              ← upstream-owned shared scripts
  Makefile              ← merged — platform block injected into service Makefile
  deploy-config.yaml    ← prefer_upstream structured config
  .gitspork-templates/  ← Go templates rendered into each downstream
  .gitspork/migrations/ ← one-time migration scripts run on integrate
```
