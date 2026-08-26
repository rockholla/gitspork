# upstream_only Global Exclusion List

**Date:** 2026-08-26
**Branch:** feat/ignore-path

## Summary

Add a top-level `upstream_only` field to `.gitspork.yml` that lists glob patterns for upstream paths that must never be synced to downstream. It takes precedence over `upstream_owned`, `downstream_owned`, and `shared_ownership` entries — matched files are skipped with a warning log line. `templated` and `migrations` are not affected.

## Config layer

New field on `GitSporkConfig` (`internal/config/config.go`):

```go
UpstreamOnly []string `yaml:"upstream_only" comment:"file patterns (https://github.com/gobwas/glob) for upstream paths that must never be synced to downstream; takes precedence over upstream_owned, downstream_owned, and shared_ownership — matched files are skipped with a warning"`
```

No changes to `OwnedEntry` or any other config type. `GetGitSporkConfigSchema()` adds one example entry (e.g. `"upstream-only-example/**"`) so the field appears in `gitspork schema` output.

Example `.gitspork.yml` usage:

```yaml
upstream_owned:
  - '{,**/}.cloud-native-template/**'

upstream_only:
  - cli/**
```

In this example, `.cloud-native-template/` directories that exist under `cli/` in the upstream are not synced to downstream, even though they match the `upstream_owned` pattern.

## Filter helper

New package-level function `filterUpstreamOnly` in `internal/integrate/integrate.go`:

```go
func filterUpstreamOnly(files []string, patterns []string, logger sdktypes.Logger) ([]string, error) {
    if len(patterns) == 0 {
        return files, nil
    }
    globs := make([]glob.Glob, len(patterns))
    for i, p := range patterns {
        g, err := glob.Compile(p)
        if err != nil {
            return nil, fmt.Errorf("invalid upstream_only pattern %q: %v", p, err)
        }
        globs[i] = g
    }
    kept := files[:0:0]
    for _, f := range files {
        excluded := false
        for _, g := range globs {
            if g.Match(f) {
                logger.Log("⚠️ skipping %s — excluded by upstream_only", f)
                excluded = true
                break
            }
        }
        if !excluded {
            kept = append(kept, f)
        }
    }
    return kept, nil
}
```

Globs are compiled once per call (not per file). The early-break stops checking patterns once a match is found. An invalid pattern returns an error immediately.

## Integrator changes

Five integrators gain a `UpstreamOnly []string` struct field. Each calls `filterUpstreamOnly` immediately after `getIntegrateFiles`, before processing the file list.

Affected integrators:
- `IntegratorUpstreamOwned` — `filterUpstreamOnly` called per entry (inside the entry loop, after `getIntegrateFiles`)
- `IntegratorDownstreamOwned` — same, per entry
- `IntegratorSharedOwnershipMerged` — called once after the single `getIntegrateFiles` call
- `IntegratorSharedOwnershipStructuredPreferUpstream` — same
- `IntegratorSharedOwnershipStructuredPreferDownstream` — same

Example struct and call site pattern (same for all five):

```go
type IntegratorUpstreamOwned struct {
    UpstreamOnly []string
}

func (i *IntegratorUpstreamOwned) Integrate(entries []config.OwnedEntry, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error {
    for _, entry := range entries {
        files, err := getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
        if err != nil { ... }
        files, err = filterUpstreamOnly(files, i.UpstreamOnly, logger)
        if err != nil { ... }
        for _, integrateFile := range files { ... }
    }
    return nil
}
```

The construction sites in `integrate.go` (lines ~343–363) become:

```go
upstreamOnly := gitSporkConfig.UpstreamOnly
(&IntegratorUpstreamOwned{UpstreamOnly: upstreamOnly}).Integrate(...)
(&IntegratorDownstreamOwned{UpstreamOnly: upstreamOnly}).Integrate(...)
(&IntegratorSharedOwnershipMerged{UpstreamOnly: upstreamOnly}).Integrate(...)
(&IntegratorSharedOwnershipStructuredPreferUpstream{UpstreamOnly: upstreamOnly}).Integrate(...)
(&IntegratorSharedOwnershipStructuredPreferDownstream{UpstreamOnly: upstreamOnly}).Integrate(...)
```

## Testing

**Unit tests** (`internal/integrate/`): new test file or additions covering `filterUpstreamOnly` directly:
- empty `patterns` slice — all files returned unchanged
- pattern that excludes a subset of files — excluded files absent, warning logged
- invalid glob pattern — error returned
- pattern that matches nothing — all files returned unchanged

**Functional test** (`test/functional/`): new scenario with an upstream containing files matched by both `upstream_owned` and `upstream_only`. Minimum setup:
- `upstream_owned: ['{,**/}.cloud-native-template/**']`
- `upstream_only: [cli/**]`
- Upstream has `cli/.cloud-native-template/foo` and `.cloud-native-template/bar`
- Assert: `cli/.cloud-native-template/foo` is absent from downstream, `.cloud-native-template/bar` is present, warning appears in log output

No new SDK or examples tests — the functional scenario covers end-to-end behavior.

## What is not in scope

- `templated` entries — excluded by design; `upstream_only` patterns do not affect template rendering
- `migrations` — excluded by design; migration script paths are not file-copy operations
- `upstream_delta` propagation — files excluded by `upstream_only` were never synced, so there is nothing to delete in downstream during delta propagation; no change needed
- Per-entry exclusions — global top-level list is sufficient for the stated use case
