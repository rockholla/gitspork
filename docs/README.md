# `gitspork` Documentation

## Examples

The `docs/examples/` directory contains fully worked scenarios showing gitspork in realistic contexts. Each example includes an upstream repo layout, a gitspork config, and a README explaining the real-world use case.

| Example | What it shows |
|---|---|
| [platform-team](examples/platform-team/README.md) | Platform engineering team distributing CI pipelines, Makefile targets, and deployment config to service repos |
| [open-source-template](examples/open-source-template/README.md) | Open source project template pushing GitHub Actions, license, and generated docs to forks |
| [standards-library](examples/standards-library/README.md) | Security/standards team enforcing linting rules, policy documents, and non-overridable security config |
| [integrate-local](examples/integrate-local/README.md) | Using a local upstream directory instead of a remote repo — useful during upstream development |

## For Upstream Maintainers

When getting started, you can run `gitspork init --help` or `gitspork schema` to see the schema and documentation for `.gitspork.yml`:

```yaml
upstream_owned: # file patterns (https://github.com/gobwas/glob) fully owned by the upstream; an entry may instead be a {from, to} map to rename a file as it syncs to the downstream
- "upstream-owned.txt"
- from: "upstream-owned-renamed-from.txt" # (rename) upstream source glob/path
  to: "downstream-renamed-to.txt" # (rename) downstream destination glob/path
downstream_owned: # file patterns (https://github.com/gobwas/glob) fully owned by the downstream once initially integrated; an entry may instead be a {from, to} map to seed a file at a different downstream path
- "downstream-owned.md"
- from: "downstream-owned-seed-from.md" # (rename) upstream source glob/path
  to: "downstream-owned-seed-to.md" # (rename) downstream destination glob/path
shared_ownership: # file patterns (https://github.com/gobwas/glob) that will be owned by both the upstream and downstream repos in some managed way
  merged: # file patterns (https://github.com/gobwas/glob) that should be treated as owned by both the upstream and downstream repos, with the ability for the upstream to own blocks w/in these types of files
  - "shared-ownership-merged.txt"
  structured: # file patterns (https://github.com/gobwas/glob) that contain structured data to maintain on both the upstream and downstream side, e.g. json/yaml configuration files
    prefer_upstream: # file patterns (https://github.com/gobwas/glob) that contain common structure data to merge, prefering the values set in the upstream repo
    - "shared-ownership-prefer-upstream.json"
    prefer_downstream: # file patterns (https://github.com/gobwas/glob) that contain common structure data to merge, prefering the values set in the downstream repo
    - "shared-ownership-prefer-downstream.json"
templated: # list of instruction for templated source files in the upstream that should be rendered in some way to a location in the downstream
- template: "meta.txt.go.tmpl" # source path of the Go template file to use in the upstream
  destination: "meta.txt" # destination path and file name in the dowstream where the template will be rendered
  inputs: # list of inputs to provide to the template, and how to determine them
  - name: "input_one" # name of the input as defined in the template like 'index .Inputs "[name]"'
    prompt: "What is the value of input_one?" # (optional, one-of required) prompt to present to the user in order to gather the input value
  - name: "input_two" # name of the input as defined in the template like 'index .Inputs "[name]"'
    json_data_path: "./.json/data.json" # (optional, one-of required) JSON data file path (relative to the downstream path) containing the input value at the root property equal to the 'name'. Contract is that downstream is responsible for maintaining this path.
  - name: "input_three" # name of the input as defined in the template like 'index .Inputs "[name]"'
    previous_input: # (optional, one-of-required) reference to an input already known from this template or another template defined before this one
      template: "meta.txt.go.tmpl" # Name of a previous template defined in the gitspork config from which to pull the value
      name: "input_one" # Name of the input from that template from which to pull the value
  merged: # optional instruction for merging with pre-existing file in the destination, if present, post-render
    structured: "prefer-downstream" # instruction for a structured merged post-render, either 'prefer-upstream' or 'prefer-downstream'
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

### Renaming files on sync

An `upstream_owned` or `downstream_owned` entry is normally a glob string and the
matched files land at the same relative path in the downstream. To have a file
land at a *different* downstream path, use the `{from, to}` map form. `from` is
matched against the upstream tree exactly like a plain pattern; `to` is the
downstream destination. For glob renames (e.g. `from: configs/**`,
`to: .configs/**`) the destination is computed by swapping the source's
non-wildcard prefix for the destination's, so `configs/app/db.yml` lands at
`.configs/app/db.yml`.

The two lists differ only in *when* the copy happens: `upstream_owned` files are
overwritten on every integrate, while `downstream_owned` files are seeded once
(at the `to` path) and never overwritten afterward.

### Special Support for `git mv` and `git rm` Operations

Say you have a file or directory you've previously defined as something to integrate out to downstreams.

Maybe things have changed and you no longer want to have that be something you distribute to downstreams, or maybe you simply want to reorganize some things you distribute to downstreams. With `gitspork`, this technically requires 2 things:

1. Removing or moving the resources in your upstream
2. Updating your `.gitspork.yml` config accordingly

There are `gitspork` helper commands for `mv` and `rm` operations that will perform both of those tasks for you:

* `gitspork mv` accepts all the same arguments as `git mv` and will perform a `git mv` and the related updates to `.gitspork.yml` for you
* Similarly, `gitspork rm` also accepts the same arguments as `git rm`, will perform the `git rm`, and the necessary updates to `.gitspork.yml`

`gitspork integrate` tracks git history for downstreams in a way that ensures files/directories are removed or renamed/moved when the upstream decides to move things around or take things out of the upstream that were previously part of the upstream to downstream contract and configuration.

## For Downstream Integrators

It's as simple as identifying your upstream gitspork repo, then on your downstream clone:

```
gitspork integrate \
  --upstream-repo-url [ssh or https upstream repo URL] \
  --upstream-repo-token [if using git https, you can provide your auth token here] \
  --upstream-repo-subpath [optional subpath within the repo to the .gitspork.yml config] \
  --upstream-repo-version [branch, tag, or commit hash from the upstream repo the represents the state you want to integrate with] \
  --downstream-repo-path [optional subpath in your repo where you want to integrate, defaults to pwd]
```

If your upstream is a local directory rather than a remote repo (e.g. a monorepo or co-located tooling), use `integrate-local` instead:

```
gitspork integrate-local \
  --upstream-path [local path to the upstream directory containing .gitspork.yml] \
  --downstream-path [local path to the downstream repo, defaults to pwd]
```

Once you've integrated, gitspork records awareness of the last state at which you integrated (upstream commit hash etc.), and you can check drift from upstream at any time by:

```
gitspork check-drift [ --verbose ] [ --upstream url=<override-url> ]
```

`check-drift` will by default simply report files that have drifted or that it's all clear. The `--verbose` flag will print out full diffs if drift is detected. The `--upstream` flag (repeatable) overrides the stored upstream list, useful when running in an environment where the original URL protocol (SSH vs HTTPS) needs to differ; overrides are matched to state entries by normalized URL + subpath so a protocol switch still finds the right recorded commit hash. It exits `0` if no drift is detected, `2` if drift is detected, and `1` on error.

### Multiple upstreams

`integrate`, `integrate-local`, and `check-drift` all accept multiple upstream sources in a single invocation. Later upstreams take precedence over earlier ones (left-to-right), so when two upstreams write the same file, the one specified later wins.

```
gitspork integrate \
  --upstream "url=git@github.com:org/base.git,version=main" \
  --upstream "url=git@github.com:org/platform.git,version=v1.2.0,subpath=infra" \
  --downstream-repo-path .
```

Valid `--upstream` keys are `url` (required), `version`, `subpath`, and `token`. All upstreams are recorded in downstream state and re-checked on `check-drift`, which reports drift per file attributed to whichever upstream last wrote it. `integrate-local` uses `--upstream-path` (also repeatable) with the same precedence semantics.

## Using gitspork as a Go SDK

The three top-level operations are exposed as a Go library at `github.com/rockholla/gitspork/v2`. Add it to your Go module:

    go get github.com/rockholla/gitspork/v2

Import and call directly:

```go
package main

import (
    "log"

    gitspork "github.com/rockholla/gitspork/v2"
)

func main() {
    report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
        DownstreamRepoPath: "/path/to/downstream",
    })
    if err != nil && err != gitspork.ErrDriftDetected {
        log.Fatal(err)
    }
    for _, f := range report.Files {
        log.Printf("drifted: %s (attributed to %s)", f.Path, f.AttributedURL)
    }
}
```

The SDK returns structural data (`*DriftReport`, `*IntegrateResult`) so orchestrators and drift bots can consume outcomes programmatically. Pass `Logger: nil` on any Options struct to suppress internal progress output.
