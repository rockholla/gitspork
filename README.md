<picture>
  <img alt="gitspork" src="./docs/gitspork.png">
</picture>

[![ci](https://github.com/rockholla/gitspork/actions/workflows/main.yml/badge.svg)](https://github.com/rockholla/gitspork/actions/workflows/main.yml)
[![security](https://github.com/rockholla/gitspork/actions/workflows/security.yml/badge.svg?branch=main)](https://github.com/rockholla/gitspork/actions/workflows/security.yml)

## When a fork just ain't good enough

Solutions like git forks or templates are useful, but fall short in certain cases when staying up-to-date with their upstreams. Enter `gitspork`.

### Terminology

* **Upstream**: a repo that contains some standards, utility, etc. to be distributed to other repos over time. Think of an "upstream" like a git template or a git fork upstream.
* **Downstream**: a repo that integrates with an upstream repo to reuse its standards and utility. Think of a downstream like a repo that has been forked from another git repo, or generated from a git template.

### Features

What `gitspork` provides for upstream -> downstream integrations

* **Upstream-Owned Resources**: those that the upstream controls entirely, and will overwrite in downstreams on each integration
* **Downstream-Owned Resources**: the gitspork integration will make sure these types of files get bootstrapped in the downstream, but then let's the downstream take over full ownership from there
* **Co-Owned Resources to be Merged (Generic)**: certain files can be owned by both the upstream and and downstream, upstream defining blocks surrounded by `::gitspork::begin-upstream-owned-block`/`::gitspork::end-upstream-owned-block`, typically in comments to maintain upstream-owned content alongside downstream-owned content
* **Co-Owned Resources to be Merged (Structured Data)**: json/yaml resources that can be merged in a structured way, with a switch to say whether upstream or downstream values should be preferred/take precedence when doing the merging
* **Templated Upstream -> Downstream Rendered Files**: Utilizing Go templates, allowing for configuration of JSON data files or user prompts as inputs to fill in the needed data to render the resulting file in downstream, including features:
  * Supporting structured merges after template rendering preferring either upstream or downstream changes in the merge
  * Caching previous prompt input values, allowing the choices to be re-used over numerous integrations
* **Drift Detection**: downstreams can always easily see if and how they might have drifted from their current upstream version, with per-file attribution to whichever upstream last wrote the file
* **Multiple Upstreams**: downstreams can integrate from several upstream repos in a single invocation, with explicit left-to-right precedence — later upstreams win when the same file is touched by more than one
* **Upstream -> Downstream delta resolutions** for moves, renames, and deletes. As the upstream evolves, downstreams will follow along with these types of iterations.
* **Migrations Support**: some ability for the upstream to instruct downstream repos in particular migration-related operations:
  * **Exec**: arbitrary commands or scripts defined in the upstream to run against the downstream either _pre_ `integrate` or _post_ integrate
* **Machine-Level Upstream Cache**: subsequent `integrate` and `check-drift` invocations reuse a bare-mirror cache under your OS user cache directory (`os.UserCacheDir()`), only fetching from remote when the entry is older than the configured TTL (default: 2h). Purpose-built for coordinator scenarios that fan out across hundreds of downstreams against a small set of shared upstreams from one machine. Per-URL cross-process locking via `flock`. Opt-out via `--no-cache` or `GITSPORK_NO_CACHE`.

## Getting Started

### Install

### Via Homebrew

```bash
brew tap rockholla/gitspork
brew install gitspork
```

### Container Image

Docker/container images are published for every release, and available at: https://hub.docker.com/r/rockholla/gitspork

### Manually

Download the appropriate binary from the [Github releases](https://github.com/rockholla/gitspork/releases), and install on your system's `PATH`.

## Programmatic use (Go SDK)

gitspork is also importable as a Go library:

    go get github.com/rockholla/gitspork/v2

```go
import gitspork "github.com/rockholla/gitspork/v2"
```

See `pkg.go.dev/github.com/rockholla/gitspork/v2` for the API reference. The three top-level operations mirror the CLI: `Integrate`, `IntegrateLocal`, and `CheckDrift`. Each returns a structural result so orchestrators and CI drift bots can consume outcomes without parsing log output.

## Initialize a Repo as a `gitspork` Upstream

```
gitspork init
```

This will initialize a `.gitspork.yml` file so you can begin configuring how your upstream should share out and integrate w/ downstreams. Run `gitspork schema` at any time to see the full annotated config reference.

## Downstream Integration

From the root of a downstream repo clone:

```
gitspork integrate \
  --upstream-repo-url [ssh or https upstream repo URL] \
  --upstream-repo-token [if using git https, you can provide your auth token here] \
  --upstream-repo-version [branch, tag, or commit hash from the upstream repo the represents the state you want to integrate with]
```

## Additional Documentation

See [./docs](./docs) for further information on how `gitspork` works and usage.

## Contributing

See our [contributors doc](./docs/CONTRIBUTING.md) for more info on how to help build out `gitspork`.
