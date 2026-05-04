# `gitspork` Documentation

## Examples

The `docs/examples/` directory contains fully worked scenarios showing gitspork in realistic contexts. Each example includes an upstream repo layout, a gitspork config, and a README explaining the real-world use case.

| Example | What it shows |
|---|---|
| [platform-team](examples/platform-team/README.md) | Platform engineering team distributing CI pipelines, Makefile targets, and deployment config to service repos |
| [open-source-template](examples/open-source-template/README.md) | Open source project template pushing GitHub Actions, license, and generated docs to forks |
| [standards-library](examples/standards-library/README.md) | Security/standards team enforcing linting rules, policy documents, and non-overridable security config |
| [integrate-local](examples/integrate-local/README.md) | Using a local upstream directory instead of a remote repo — useful during upstream development |

## For Upstream Developers

When getting started, you can run `gitspork init --help` to see the schema and documentation for `.gitspork.yml`:

```yaml
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
templated: # list of instruction for templated source files in the upstream that should be rendered in some way to a location in the downstream
- template: "meta.txt.go.tmpl" # source path of the Go template file to use in the upstream
  destination: "meta.txt" # destination path and file name in the dowstream where the template will be rendered
  inputs: # list of inputs to provide to the template, and how to determine them
  - name: "input_one" # name of the input as defined in the template like 'index .Inputs "[name]"'
    prompt: "What is the value of input_one?" # (optional, one-of required) prompt to present to the user in order to gather the input value
  - name: "input_two" # name of the input as defined in the template like 'index .Inputs "[name]"'
    json_data_path: "./.json/data.json" # (optional, one-of required) JSON data file path (relative to the downstream path) containing the input value at the root property equal to the 'name'
  - name: "input_three" # name of the input as defined in the template like 'index .Inputs "[name]"'
    previous_input: # (optional, one-of-required) reference to an input already known from this template or another template defined before this one
      template: "meta.txt.go.tmpl" # Name of a previous template defined in the gitspork config from which to pull the value
      name: "input_one" # Name of the input from that template from which to pull the value
  merged: # instruction for merging with pre-existing file in the destination, if present, post-render
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

## For Downstream Integrators

It's as simple as identifying your upstream gitspork repo, then on your downstream clone:

```
% gitspork integrate \
  --upstream-repo-url <ssh or https upstream repo URL> \
  --upstream-repo-token <if using git https, you can provide your auth token here> \
  --upstream-repo-subpath <optional subpath within the repo to the .gitspork.yml config> \
  --upstream-repo-version <branch, tag, or commit hash from the upstream repo the represents the state you want to integrate with> \
  --downstream-repo-path <optional subpath in your repo where you want to integrate, defaults to pwd>
```

Once you've integrated, gitspork stashes awareness of the last state at which you integrated (upstream commit hash etc.), and you can check drift from upstream at any time by:

```
% gitspork check-drift [ --verbose ]
```

`check-drift` will by default simply report files that have drifted or that it's all clear. The `--verbose` flag will print out full diffs if drift is detected. It exits `0` if no drift is detected, `1` if drift is detected, and other non-zero codes on error.

## Installing via Homebrew

```bash
brew tap rockholla/gitspork
brew install gitspork
```
