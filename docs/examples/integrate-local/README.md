# Example: Integrate Local

**Scenario:** A developer is using a gitspork upstream that lives on their local filesystem rather than a remote git repository. This is useful during upstream development and testing, or when the upstream is a checked-out monorepo subtree rather than a separate hosted repo.

## What this example demonstrates

| Feature | File(s) | Behaviour |
|---|---|---|
| `upstream_owned` | `app-config.yaml` | A configuration file owned entirely by the upstream — always overwritten on integrate |
| `templated` | `config.yml` | An app configuration file rendered from `input-data.json`, pulling `app_name` and `environment` |
| `integrate-local` command | — | Uses `--upstream-path` (a local directory) instead of `--upstream-repo-url` — no git clone required |

## Real-world mapping

`integrate-local` is the right tool when:

- You are **developing or testing an upstream** and want to iterate against a downstream without pushing to a remote repo first.
- Your upstream is a **directory within a monorepo** that you have checked out locally.
- You are running gitspork in a **CI environment** where the upstream has already been checked out as part of the pipeline.

The downstream in this example does not need to be a git repository — `integrate-local` works against any directory, making it convenient for quick local testing.

## Running this example

From the repo root:

```bash
make test-examples
```

Or to run just this scenario:

```bash
go test -tags examples -v ./test/examples/... -run TestIntegrateLocalExample
```

## Directory structure

```
upstream/                        ← the gitspork upstream directory (local, not a remote repo)
  .gitspork.yaml                 ← gitspork configuration
  app-config.yaml                ← upstream-owned application config
  .gitspork-templates/
    config.yml.go.tmpl           ← template rendered using app_name and environment

downstream/                      ← example downstream input data
  input-data.json                ← provides app_name and environment for template rendering
```

## Command

```bash
gitspork integrate-local \
  --upstream-path ./upstream \
  --downstream-path ./my-service
```
