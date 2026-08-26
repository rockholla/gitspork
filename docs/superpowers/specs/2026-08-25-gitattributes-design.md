# Design: Auto-manage `.gitattributes` for all `.gitspork/**/*` content

**Date:** 2026-08-25  
**Status:** Approved

## Problem

gitspork already manages `.gitattributes` entries for downstream repos — but only partially. The `ensureGitsporkAttributes` function writes a `.gitattributes` entry marking gitspork-managed cache files as generated, but it has two gaps:

1. **Pattern too narrow.** The pattern is `.gitspork/**/*.json`, covering only JSON files. All content under `.gitspork/` is machine-generated and should be marked as such.
2. **Trigger too narrow.** The call site is inside `IntegratorTemplated.Integrate()`, so `.gitattributes` is only written when there are templated instructions. A downstream using `integrate` with no templated instructions gets `downstream-state.json` written to `.gitspork/` but no corresponding `.gitattributes` entry.

## Design

### Pattern and upgrade path (`gitattributes.go`)

Change `gitsporkAttrPattern` from `.gitspork/**/*.json` to `.gitspork/**/*`. The attribute flags (`linguist-generated=true -diff merge=binary`) are unchanged — all `.gitspork/` content is machine-generated, so suppressing diffs and using binary merge remains correct.

Update `filterGitsporkAttributeLines` to strip any line whose first field starts with `.gitspork/**/` rather than matching the exact `gitsporkAttrPattern` constant. This handles the upgrade path: existing downstreams with the old `.gitspork/**/*.json` entry will have it replaced cleanly on the next integrate run, with no stale entry left behind. It is also forward-compatible with future pattern changes.

### Call site (`integrate.go` and `integrator_templated.go`)

Move `ensureGitsporkAttributes(downstreamPath)` from `IntegratorTemplated.Integrate()` to `integrate()` in `integrate.go`, called once near the top before any integrators run. Since both `Integrate` and `IntegrateLocal` funnel through `integrate()`, this gives uniform coverage with a single call site.

The `forDriftCheck` path also runs through `integrate()`. Writing `.gitattributes` to the temporary downstream copy used by drift check is harmless — the temp directory is discarded after the check.

Remove the `ensureGitsporkAttributes` call from `IntegratorTemplated.Integrate()` entirely. `IntegratorTemplated` has no responsibility for `.gitattributes`.

### Tests

| File | Change |
|---|---|
| `gitattributes_test.go` | Add upgrade-path test: old `.gitspork/**/*.json` entry is replaced by `.gitspork/**/*`; no stale `.json`-scoped line remains. |
| `integrator_templated_test.go` | Remove assertion that `.gitattributes` is written when templated integration runs — no longer `IntegratorTemplated`'s responsibility. The existing assertion that `.gitattributes` is *not* created when there are no templated instructions stays and continues to pass, since `IntegratorTemplated` no longer touches `.gitattributes`. |
| `integrate_test.go` | Add test confirming `integrate()` writes `.gitattributes` even when the upstream has zero templated instructions, explicitly covering the prior gap. |

## Files touched

- `internal/integrate/gitattributes.go` — pattern constant, filter logic
- `internal/integrate/integrator_templated.go` — remove `ensureGitsporkAttributes` call
- `internal/integrate/integrate.go` — add `ensureGitsporkAttributes` call in `integrate()`
- `internal/integrate/gitattributes_test.go` — upgrade-path test
- `internal/integrate/integrator_templated_test.go` — remove `.gitattributes` assertion
- `internal/integrate/integrate_test.go` — new coverage test
