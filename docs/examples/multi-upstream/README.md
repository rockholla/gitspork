# Example: Multi-Upstream Integration

**Scenario:** A downstream repo integrates from two upstream sources at once — a shared platform base and a language-specific overlay. This is the pattern for organisations layering standards: a common platform team owns baseline CI, security, and app config; a language guild adds language-specific CI and amends the shared config with tuning.

## What this example demonstrates

| Feature | File(s) | Behaviour |
|---|---|---|
| Multi-upstream `--upstream` (repeatable) | both upstreams | Two `--upstream` flags on one `gitspork integrate` call; each contributes files to the same downstream in the order they're specified |
| `upstream_owned` from each upstream | `ci/build.yml`, `ci/lint.yml` (from base), `ci/language-check.yml` (from overlay) | Both upstreams write into `ci/`. Non-overlapping files coexist; last-writer-wins on any overlap |
| Structured merge across upstreams | `app-config.yaml` | Each upstream lists `app-config.yaml` under `shared_ownership.structured.prefer_upstream`. Fields present in both merge; overlay's values overwrite base's on collision because it runs SECOND |
| State records both upstreams | `.gitspork/downstream-state.json` | The `upstreams` array carries one entry per integrated upstream (URL + commit_hash), so `check-drift` re-integrates each at its recorded commit |

## Real-world mapping

Common shapes this pattern fits:

- **Platform-team baseline + team overlay.** A central platform team publishes shared CI, security scanners, and app-config defaults. Each team layers their own upstream on top with team-specific CI (custom scanners, release workflows) and app-config tuning (retry counts, feature flags).
- **Compliance + language guild.** One upstream enforces compliance-required workflows and license headers; a second, language-specific upstream (Go, TypeScript, Python) adds lint/test tooling appropriate to the language.
- **Base template + org overlay.** A public open-source template plus a private org-specific upstream that adds internal dashboards, deploy hooks, and org-required annotations.

## How ordering + precedence works

Upstreams are integrated in the order they appear on the command line. For `upstream_owned` files that both upstreams write, the last integrated upstream wins. For `shared_ownership.structured.prefer_upstream` files, the merge is applied per-upstream in order: the second upstream's values overwrite the first upstream's values on any shared key, while unique keys from both sides survive.

If you want the base upstream to win in a specific conflict, invert the flag order.

## Running this example

From the repo root:

```bash
make test-examples
```

Or to run just this scenario:

```bash
go test -tags examples,testharness -v ./test/examples/... -run TestMultiUpstreamExample
```

## Directory structure

```
platform-base/                     ← first upstream (shared platform defaults)
  .gitspork.yml
  ci/
    build.yml
    lint.yml
  app-config.yaml                  ← baseline log_level, timeout, owner tag

language-overlay/                  ← second upstream (language-specific additions)
  .gitspork.yml
  ci/
    language-check.yml
  app-config.yaml                  ← overrides log_level, adds retry_count + language tag
```
