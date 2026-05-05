# Example: Standards Library

**Scenario:** A security or standards team maintains a repository of linting rules, policy documents, and environment configuration that must be present in every service. Services adopt the standards library via gitspork and can detect when their copy has drifted from the canonical version.

## What this example demonstrates

| Feature | File(s) | Behaviour |
|---|---|---|
| `upstream_owned` | `.golangci.yml`, `policies/**` | Linting config and policy documents are fully owned by the standards team — always overwritten on integrate |
| `shared_ownership.merged` | `.env.example` | Upstream injects a fenced block of required environment variables into the service's `.env.example` without owning the whole file |
| `shared_ownership.structured.prefer_upstream` | `security-policy.yaml` | Security enforcement settings are set by the standards team; any downstream attempt to soften them is reverted on re-integrate |
| `templated` (json_data_path) | `service-info.txt` | A service identity file rendered from `service-input-data.json`, pulling `service_name` directly |
| `templated` (previous_input) | `security-summary.md` | A security summary rendered using `service_name` — but sourced from the previously-resolved `service-info.txt` template input rather than re-reading the JSON file |
| `migrations` | `.gitspork/migrations/0001-policy-init.yml` | A post-integrate script runs once to perform any first-time policy initialisation in the downstream |
| `check-drift` | — | After integrate and commit, `gitspork check-drift` verifies the downstream hasn't drifted from the upstream state |

## Real-world mapping

This pattern suits a **security, compliance, or developer-experience team** that needs to:

- Mandate linting and policy standards across a large number of service repositories without granting write access to each.
- Keep security configuration non-negotiable. `prefer_upstream` on `security-policy.yaml` means a service team cannot accidentally (or intentionally) weaken MFA requirements or permitted regions — the next integrate will restore the correct values.
- Use `previous_input` to avoid asking the same question twice when multiple templates share an input. Once `service_name` is resolved for `service-info.txt`, `security-summary.md` reuses that value rather than requiring another `json_data_path` lookup.
- Give services an easy way to check whether they are still aligned with the current standard (`check-drift`), suitable for running in CI.

## Running this example

From the repo root:

```bash
make test-examples
```

Or to run just this scenario:

```bash
go test -tags examples -v ./test/examples/... -run TestStandardsLibraryExample
```

## Directory structure

```
upstream/                              ← the gitspork upstream repo (standards library)
  .gitspork.yml                        ← gitspork configuration
  .golangci.yml                        ← upstream-owned linter config
  policies/                            ← upstream-owned policy documents
  .env.example                         ← merged — upstream block injected into service file
  security-policy.yaml                 ← prefer_upstream structured security config
  .gitspork-templates/
    service-info.txt.go.tmpl           ← renders service identity using service_name
    security-summary.md.go.tmpl        ← renders security summary using previous_input
  .gitspork/migrations/
    0001-policy-init.yml               ← one-time post-integrate migration
```
