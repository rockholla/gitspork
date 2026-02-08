<picture>
  <img alt="gitspork" src="./docs/gitspork.png">
</picture>

[![Gitspork](https://github.com/rockholla/gitspork/actions/workflows/main.yml/badge.svg)](https://github.com/rockholla/gitspork/actions/workflows/main.yml)

## When a fork just ain't good enough

Solutions like git forks or templates are useful, but fall short in certain cases when staying up-to-date with their upstreams. Enter `gitspork`.

## Getting Started

First, quickly some terminology:

* **upstream**: a repo that contains some standards, utility, etc. to be distributed to other repos over time. Think of an "upstream" like a git template or a git fork upstream.
* **downstream**: a repo that integrates with an upstream repo to reuse its standards and utility. Think of a downstream like a repo that has been forked from another git repo, or generated from a git template.

### Install

Download the appropriate binary from the [Github releases](https://github.com/rockholla/gitspork/releases), and install on your system's `PATH`.

### Simple Demo

In an empty directory on your machine:

```
% git init
% gitspork integrate \
  --upstream-repo-url https://github.com/rockholla/gitspork \
  --upstream-repo-subpath ./docs/examples/simple/upstream \
  --upstream-repo-version main \
  --downstream-repo-path ./
```

You'll see that your repo has been initially integrated with [one of our example upstreams in this repo](./docs/examples/simple/upstream). Start to explore your repo source now that it's been integrated, and learn about each of the advanced, yet simple integration types at play here, [controlled by the upstream `.gitspork.yml` configuration](./docs/examples/simple/upstream/).

* **Upstream-Owned Resources**: those that the upstream controls entirely, and will overwrite in downstreams on each integration
* **Downstream-Owned Resources**: the gitspork integration will make sure these types of files get bootstrapped in the downstream, but then let's the downstream take over full ownership from there
* **Co-Owned Resources to be Merged (Generic)**: certain files can be owned by both the upstream and and downstream, upstream defining blocks surrounded by `::gitspork::begin-upstream-owned-block`/`::gitspork::end-upstream-owned-block`, typically in comments to maintain upstream-owned content alongside downstream-owned content
* **Co-Owned Resources to be Merged (Structured Data)**: json/yaml resources that can be merged in a structured way, with a switch to say whether upstream or downstream values should be preferred/take precedence when doing the merging
* **Templated Upstream -> Downstream Rendered Files**: Utilizing Go templates, allowing for configuration of JSON data files or user prompts as inputs to fill in the needed data to render the resulting file in downstream, including features:
  * Supporting structured merges after template rendering preferring either upstream or downstream changes in the merge
  * Caching previous prompt input values, allowing the choices to be re-used over numerous integrations
* **Migrations Support**: some ability for the upstream to instruct downstream repos in particular migration-related operations:
  * **Exec**: arbitrary commands or scripts defined in the upstream to run against the downstream either _pre_ `integrate` or _post_ integrate

### Initialize a Repo as a `gitspork` Upstream

```
% gitspork init
```

This will simply initialize a `.gitspork.yml` file so you can begin configuring how your upstream should share out and integrate w/ downstreams.

## Additional Documentation

See [./docs](./docs) for further information on how `gitspork` works and usage.
