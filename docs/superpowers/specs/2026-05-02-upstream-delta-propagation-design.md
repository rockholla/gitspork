# Upstream Delta Propagation Design

## Goal

When `gitspork integrate` runs against a new upstream commit, automatically propagate upstream file deletions and renames to the downstream repo — so that files the upstream has removed or moved are removed or moved in the downstream as well, before the normal integration logic runs.

## Background

Issues addressed: [#3](https://github.com/rockholla/gitspork/issues/3), [#4](https://github.com/rockholla/gitspork/issues/4)

Previously, gitspork had no way to handle upstream files that were removed or renamed between integrations. The downstream would be left with stale files at old paths, and renames would result in both the old and new path existing. This required downstream integrators to handle cleanup manually (e.g. via migration `exec` scripts). Now that `integrate` tracks the previous upstream commit hash in `.gitspork/downstream-state.json`, we have the information needed to compute the delta between integrations and act on it automatically.

## Tenets

- `integrate` acts automatically — the resulting diff is the user's review surface, not confirmation prompts
- Migrations remain `exec`-only; this feature replaces the need to use migrations for moves/removes
- `integrate-local` is unaffected — no git history available, delta is skipped silently

## Scope

Delta propagation applies to all integration types **except** `downstream_owned`:

| Integration type | Delete behaviour | Rename/move behaviour |
|---|---|---|
| `upstream_owned` | Remove downstream file | Move downstream file to new path; normal integration overwrites it |
| `shared_ownership.merged` | Remove downstream file | Move downstream file to new path; normal integration merges on top |
| `shared_ownership.structured` | Remove downstream file | Move downstream file to new path; normal integration merges on top |
| `templated` | Remove downstream `destination` file when upstream `template` path is removed | Move downstream `destination` file when `destination` value changes in config; normal integration re-renders on top |
| `downstream_owned` | No action | No action |

## Architecture

### Data Flow

```
Integrate()
  └── cloneUpstreamForIntegrate()        // full history clone (no SingleBranch when prevHash != "")
  └── getGitSporkConfig()                // parse .gitspork.yml at newHash
  └── computeUpstreamDelta()             // new — walk prevHash..newHash + config diff
  └── applyUpstreamDelta()               // new — remove/move files in downstream
  └── integrate()                        // unchanged — runs on top of already-mutated downstream
```

Both `computeUpstreamDelta` and `applyUpstreamDelta` are skipped when:
- `prevHash` is empty (first integrate)
- `opts.ForDriftCheck` is true
- Called via `IntegrateLocal`

### New File: `internal/upstream-delta.go`

Single responsibility: compute and apply the upstream delta. No integration logic lives here.

```go
type upstreamRename struct {
    OldPath string // downstream-relative path
    NewPath string // downstream-relative path
}

type upstreamDelta struct {
    Deletions []string         // downstream-relative paths to remove
    Renames   []upstreamRename // downstream-relative old→new pairs
}

// computeUpstreamDelta returns the set of downstream mutations needed before integration.
// Returns an empty delta (no error) when prevHash is empty.
func computeUpstreamDelta(
    repo *gogit.Repository,
    prevHash, newHash string,
    config *GitSporkConfig,
    upstreamSubpath string,
) (*upstreamDelta, error)

// applyUpstreamDelta removes and renames files in downstreamPath.
func applyUpstreamDelta(delta *upstreamDelta, downstreamPath string, logger *Logger) error
```

### Delta Computation — Two Independent Parts

**Part 1: File-level delta** (covers `upstream_owned`, `shared_ownership`)

Walk go-git commit history `prevHash..newHash`. For each commit, diff parent→child and collect:
- `D` (deleted) entries whose paths match any glob in `upstream_owned`, `shared_ownership.merged`, `shared_ownership.structured.prefer_upstream`, or `shared_ownership.structured.prefer_downstream`
- `R` (renamed) entries matching the same globs

Map upstream-relative paths to downstream-relative paths (accounting for `UpstreamRepoSubpath` if set).

Use go-git's built-in rename detection (default 50% similarity threshold — same as git).

**Part 2: Config-level delta** (covers `templated`)

Read `.gitspork.yml` at `prevHash` and at `newHash` using `repo.CommitObject` + tree lookup. Diff the `Templated` slices keyed on `Template` path:
- Entry present in prev but absent in new → delete the old `Destination` from downstream
- Entry present in both but `Destination` changed → rename old `Destination` to new `Destination` in downstream

### Changes to `internal/integrate.go`

- Load `state.LastUpstreamCommitHash` as `prevHash` before calling `integrate()`
- Extend the "full history clone" condition: currently triggered when `UpstreamRepoCommit != ""`; also trigger when `prevHash != ""`
- After `getGitSporkConfig`, and before `integrate()`, call `computeUpstreamDelta` then `applyUpstreamDelta`
- Both calls gated on `!opts.ForDriftCheck && prevHash != ""`

### New Subcommands: `gitspork mv` and `gitspork rm`

Upstream-helper commands for upstream maintainers. Must be run from within an upstream gitspork repo (i.e. a directory containing `.gitspork.yml` or `.gitspork.yaml`).

**`gitspork mv <old-path> <new-path>`**
1. Runs `git mv <old-path> <new-path>` in the upstream repo
2. Updates `.gitspork.yml`: for `upstream_owned` / `shared_ownership`, replaces exact path entries (non-glob) matching `old-path`; for `templated`, updates the `template` or `destination` field on the matching entry

**`gitspork rm <path>`**
1. Runs `git rm <path>` in the upstream repo
2. Updates `.gitspork.yml`: removes exact path entries matching `path`; for `templated`, removes the entry whose `template` matches `path`

Both commands print a summary of `.gitspork.yml` changes made and remind the maintainer to commit.

Note: these commands only handle exact path matches in the config, not glob patterns. Glob patterns that happen to match the moved/removed file are left for the maintainer to update manually.

## Error Handling & Edge Cases

| Situation | Behaviour |
|---|---|
| First integrate (`prevHash` empty) | Skip delta entirely, no history walk |
| `ForDriftCheck: true` | Skip delta entirely |
| `integrate-local` | Skip delta entirely (no upstream repo) |
| Downstream file already absent (delete target) | Log and continue, no error |
| Downstream file already at new path (rename target) | Log warning, skip move, continue |
| `prevHash` not found in upstream history | Log warning, skip delta, normal integration continues |
| Rename detection below similarity threshold | Treated as delete+add; downstream file removed, new path populated by normal integration |

## Testing

**`Test_computeUpstreamDelta`** (`internal/upstream-delta_test.go`)

Uses in-memory go-git repos (no temp dirs needed for the upstream side):
- `upstream_owned` file deleted between commits → appears in `Deletions`
- `shared_ownership` file renamed between commits → appears in `Renames`
- `templated` destination changed in config between commits → appears in `Renames`
- `templated` template removed from config between commits → appears in `Deletions`
- `downstream_owned` file deleted → not in delta
- Empty `prevHash` → empty delta returned
- `prevHash` not in repo → warning logged, empty delta returned

**`Test_applyUpstreamDelta`** (`internal/upstream-delta_test.go`)

Uses temp dirs:
- File in `Deletions` exists → removed
- File in `Deletions` does not exist → no error
- File in `Renames` exists, target absent → moved
- File in `Renames`, target already exists → warning logged, not overwritten

**`TestIntegrate` additions** (`internal/integrate_test.go`)

- `ForDriftCheck: true` → `computeUpstreamDelta` not called
- `prevHash` empty → `computeUpstreamDelta` not called
