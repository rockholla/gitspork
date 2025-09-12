# `gitspork` Documentation

> [!NOTE]
> Gitspork is still in a relatively early development phase. Pre-1.0 release, we will continue to iterate here and dial in both functionality and documentation alike.

## For Upstream Developers

When getting started, you can run `gitspork init --help` to see the schema and documentation for `.gitspork.yml`:

```yaml
version: "v0.1.0" # version of gitspork relevant to the config
upstream_owned: # file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the upstream gitspork repo
- "upstream-owned.txt"
downstream_owned: # file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the downstream repo once it's been initially integrated
- "downstream-owned.md"
shared_ownership: # file patterns (https://github.com/gobwas/glob) that will be owned by both the upstream and downstream repos in some managed way
  merged: # file patterns (https://github.com/gobwas/glob) that should be treated as owned by both the upstream and downstream repos, with the ability for the upstream to own blocks w/in these types of files
  - "shared-ownership-merged.txt"
  structured: # file patterns (https://github.com/gobwas/glob) that contain structured data to maintain on both the upstream and downstream side, e.g. json/yaml configuration files
    prefer_upstream: # file patterns (https://github.com/gobwas/glob) that contain common structure data to merge, prefering the values set in the upstream repo
    - "shared-ownership-prefer-upstream.json"
    prefer_downstream: # file patterns (https://github.com/gobwas/glob) that contain common structure data to merge, prefering the values set in the downstream repo
    - "shared-ownership-prefer-downstream.json"
migrations: # list of YAML file paths in the upstream repo, relative to the upstream repo root or subpath if specified, containing downstream repo migration instructions
- ".gitspork/migrations/0001/migration.yml"
```

Additionally, the schema for migrations yaml files will also be provided in the output of that command:

```yaml
pre_integrate:
  exec: "./.gitspork/migrations/0001/pre-integrate.sh" # command, or path to a script relative to the upstream repo root or subpath if specified, to execute in the downstream repo as a migration-related operation
post_integrate:
  exec: "./.gitspork/migrations/0001/post-integrate.sh" # command, or path to a script relative to the upstream repo root or subpath if specified, to execute in the downstream repo as a migration-related operation
```

## For Downstream Integrators

More coming soon, see the [root README.md](../README.md) in the meantime for the info currently available.