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
2. Updates `.gitspork.yml`:
   - Exact path entries matching `old-path` are replaced with `new-path`
   - Glob entries whose non-wildcard prefix matches `old-path` have that prefix rewritten to `new-path` (e.g. `docs/cloud-native/**` → `docs/cloud/**` when moving `docs/cloud-native` → `docs/cloud`)
   - For `templated`, updates `template` or `destination` fields on the matching entry
   - Glob patterns with a wildcard before the moved path segment (e.g. `**/cloud-native/*.md`) are left unchanged and a warning is printed

**`gitspork rm [-r] <path>`**
1. Runs `git rm [-r] <path>` in the upstream repo
2. Updates `.gitspork.yml`:
   - Exact path entries matching `path` are removed
   - With `-r`: exact path entries that are children of `path` (i.e. have `path` as a prefix) are also removed
   - With `-r`: glob entries whose non-wildcard prefix starts with `path` are also removed (e.g. `docs/cloud-native/**` removed when `gitspork rm -r docs/cloud-native`)
   - For `templated`, removes entries whose `template` matches `path` or (with `-r`) is a child of `path`
   - Glob patterns with a wildcard before the removed path segment are left unchanged and a warning is printed

Both commands print a summary of `.gitspork.yml` changes made and remind the maintainer to commit.

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

**`Test_upstreamMv`** (`internal/upstream-mv-rm_test.go`)

- Exact path entry → replaced with new path
- Glob entry with matching non-wildcard prefix → prefix rewritten
- Glob with wildcard before moved segment → unchanged, warning emitted
- `templated` entry with matching `template` → `template` field updated
- `templated` entry with matching `destination` → `destination` field updated

**`Test_upstreamRm`** (`internal/upstream-mv-rm_test.go`)

- Exact path entry → removed
- With `-r`: exact child path entries → removed
- With `-r`: glob entry whose non-wildcard prefix starts with removed path → removed
- With `-r`: glob with wildcard before removed segment → unchanged, warning emitted
- `templated` entry with matching `template` → entry removed
- With `-r`: `templated` entry whose `template` is a child of removed path → entry removed

**`TestIntegrate` additions** (`internal/integrate_test.go`)

- `ForDriftCheck: true` → `computeUpstreamDelta` not called
- `prevHash` empty → `computeUpstreamDelta` not called
