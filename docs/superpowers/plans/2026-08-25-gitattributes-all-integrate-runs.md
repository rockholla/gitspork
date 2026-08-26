# .gitattributes Auto-Management for All Integrate Runs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Broaden the `.gitattributes` managed pattern from `.gitspork/**/*.json` to `.gitspork/**/*` and ensure it is written on every integrate run, not only when templated instructions are present.

**Architecture:** The `ensureGitsporkAttributes` function in `gitattributes.go` moves from being called inside `IntegratorTemplated.Integrate()` to being called at the top of the shared `integrate()` function in `integrate.go`. The pattern constant and the filter helper are updated to handle the upgrade path from the old `.json`-scoped pattern.

**Tech Stack:** Go, `internal/integrate` package (same package for all test files).

---

## File Map

| File | Change |
|---|---|
| `internal/integrate/gitattributes.go` | Change pattern constant; update filter to use prefix match |
| `internal/integrate/gitattributes_test.go` | Add upgrade-path test |
| `internal/integrate/integrate.go` | Add `ensureGitsporkAttributes` call in `integrate()` |
| `internal/integrate/integrator_templated.go` | Remove `ensureGitsporkAttributes` call |
| `internal/integrate/integrator_templated_test.go` | Remove `.gitattributes` sub-test from consolidated-cache test |
| `internal/integrate/integrate_test.go` | Add test that `integrate()` writes `.gitattributes` with no templated instructions |

---

## Task 1: Write the failing upgrade-path test

**Files:**
- Modify: `internal/integrate/gitattributes_test.go`

- [ ] **Step 1: Write the failing test**

  Add after the last existing test in `internal/integrate/gitattributes_test.go`:

  ```go
  func TestEnsureGitsporkAttributes_upgradesLegacyJsonPattern(t *testing.T) {
  	dir := t.TempDir()
  	path := filepath.Join(dir, ".gitattributes")
  	// Simulate the pattern written by a prior gitspork version.
  	original := "# gitspork-managed: cache files under .gitspork/ are auto-generated\n" +
  		".gitspork/**/*.json linguist-generated=true -diff merge=binary\n" +
  		"*.md linguist-language=Markdown\n"
  	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

  	require.NoError(t, ensureGitsporkAttributes(dir))

  	got, err := os.ReadFile(path)
  	require.NoError(t, err)
  	assert.NotContains(t, string(got), ".gitspork/**/*.json", "stale JSON-scoped pattern must be removed")
  	assert.Contains(t, string(got), ".gitspork/**/*", "broad pattern must be present")
  	assert.Contains(t, string(got), "*.md linguist-language=Markdown", "user's rules must survive")
  }
  ```

- [ ] **Step 2: Run the test to confirm it fails**

  ```bash
  go test ./internal/integrate/... -run TestEnsureGitsporkAttributes_upgradesLegacyJsonPattern -v
  ```

  Expected: FAIL — the old `.gitspork/**/*.json` line is not stripped because `filterGitsporkAttributeLines` only matches the exact `gitsporkAttrPattern` constant.

---

## Task 2: Update the pattern constant and filter logic

**Files:**
- Modify: `internal/integrate/gitattributes.go`

- [ ] **Step 1: Change the pattern constant and update the filter**

  In `internal/integrate/gitattributes.go`, replace the `gitsporkAttrPattern` constant and the matching line inside `filterGitsporkAttributeLines`:

  Change:
  ```go
  gitsporkAttrPattern   = ".gitspork/**/*.json"
  ```
  To:
  ```go
  gitsporkAttrPattern   = ".gitspork/**/*"
  ```

  In `filterGitsporkAttributeLines`, change:
  ```go
  	fields := strings.Fields(line)
  	if len(fields) > 0 && fields[0] == gitsporkAttrPattern {
  		continue
  	}
  ```
  To:
  ```go
  	fields := strings.Fields(line)
  	if len(fields) > 0 && strings.HasPrefix(fields[0], ".gitspork/**/") {
  		continue
  	}
  ```

  The prefix check `.gitspork/**/` strips the old `.gitspork/**/*.json` line and the new `.gitspork/**/*` line (and any future variations) without hard-coding multiple constants.

- [ ] **Step 2: Run the upgrade-path test to confirm it passes**

  ```bash
  go test ./internal/integrate/... -run TestEnsureGitsporkAttributes -v
  ```

  Expected: all `TestEnsureGitsporkAttributes_*` tests PASS.

- [ ] **Step 3: Run the full unit suite to confirm nothing regressed**

  ```bash
  make test-unit
  ```

  Expected: PASS.

- [ ] **Step 4: Commit**

  ```bash
  git add internal/integrate/gitattributes.go internal/integrate/gitattributes_test.go
  git commit -m "feat(gitattributes): broaden managed pattern to .gitspork/**/* with upgrade-path filter"
  ```

---

## Task 3: Write the failing integrate-level test

**Files:**
- Modify: `internal/integrate/integrate_test.go`

- [ ] **Step 1: Write the failing test**

  Add after the `Test_LoadDownstreamState_migration` test in `internal/integrate/integrate_test.go`:

  ```go
  func Test_integrate_writesGitattributesWithNoTemplatedInstructions(t *testing.T) {
  	upstreamDir := t.TempDir()
  	downstreamDir := t.TempDir()
  	// Minimal config — no templated instructions. Prior to this change,
  	// .gitattributes was only written when IntegratorTemplated ran.
  	cfg := &config.GitSporkConfig{}

  	require.NoError(t, integrate(cfg, upstreamDir, downstreamDir, false, false, sdktypes.NoopLogger()))

  	attrs, err := os.ReadFile(filepath.Join(downstreamDir, ".gitattributes"))
  	require.NoError(t, err)
  	assert.Contains(t, string(attrs), gitsporkAttrPattern)
  	assert.Contains(t, string(attrs), gitsporkAttrMarker)
  }
  ```

  Note: `config`, `sdktypes`, `gitsporkAttrPattern`, and `gitsporkAttrMarker` are all already imported or accessible in `integrate_test.go` (`package integrate`).

- [ ] **Step 2: Run the test to confirm it fails**

  ```bash
  go test ./internal/integrate/... -run Test_integrate_writesGitattributes -v
  ```

  Expected: FAIL — `.gitattributes` does not exist because `integrate()` only writes it via `IntegratorTemplated`, which has no instructions here.

---

## Task 4: Move the call site into `integrate()`

**Files:**
- Modify: `internal/integrate/integrate.go`
- Modify: `internal/integrate/integrator_templated.go`

- [ ] **Step 1: Add `ensureGitsporkAttributes` at the top of `integrate()`**

  In `internal/integrate/integrate.go`, in the `integrate()` function, add immediately after the `greenBold` variable declaration (line ~291):

  Change:
  ```go
  func integrate(gitSporkConfig *config.GitSporkConfig, upstreamPath string, downstreamPath string, forceRePrompt bool, forDriftCheck bool, logger sdktypes.Logger) error {
  	greenBold := color.New(color.FgHiGreen, color.Bold)

  	preIntegrateMigrations := []*config.GitSporkConfigMigrationInstructions{}
  ```
  To:
  ```go
  func integrate(gitSporkConfig *config.GitSporkConfig, upstreamPath string, downstreamPath string, forceRePrompt bool, forDriftCheck bool, logger sdktypes.Logger) error {
  	greenBold := color.New(color.FgHiGreen, color.Bold)

  	if err := ensureGitsporkAttributes(downstreamPath); err != nil {
  		return fmt.Errorf("error ensuring .gitattributes for .gitspork/ content: %v", err)
  	}

  	preIntegrateMigrations := []*config.GitSporkConfigMigrationInstructions{}
  ```

- [ ] **Step 2: Remove the call from `IntegratorTemplated`**

  In `internal/integrate/integrator_templated.go`, remove the two lines (currently ~227-229):

  ```go
  	if err := ensureGitsporkAttributes(downstreamPath); err != nil {
  		return fmt.Errorf("error ensuring .gitattributes entry for templated cache: %v", err)
  	}
  ```

  The function should now end:
  ```go
  	if err := saveTemplatedInputs(downstreamPath, nextCache); err != nil {
  		return fmt.Errorf("error writing templated inputs cache: %v", err)
  	}
  	return nil
  }
  ```

- [ ] **Step 3: Run the integrate-level test to confirm it now passes**

  ```bash
  go test ./internal/integrate/... -run Test_integrate_writesGitattributes -v
  ```

  Expected: PASS.

---

## Task 5: Update `integrator_templated_test.go`

**Files:**
- Modify: `internal/integrate/integrator_templated_test.go`

- [ ] **Step 1: Remove the `.gitattributes` sub-test**

  In `TestIntegratorTemplated_writesConsolidatedCacheAndGitattributes`, remove the sub-test at lines ~49-54:

  ```go
  	t.Run(".gitattributes marks cache as generated", func(t *testing.T) {
  		attrs, err := os.ReadFile(filepath.Join(downstreamDir, ".gitattributes"))
  		require.NoError(t, err)
  		assert.Contains(t, string(attrs), gitsporkAttrPattern)
  		assert.Contains(t, string(attrs), gitsporkAttrMarker)
  	})
  ```

  `IntegratorTemplated` is no longer responsible for `.gitattributes`; that concern belongs to `integrate()`. The test name may also be updated to drop the `AndGitattributes` suffix since the function is renamed in-place — leave it as-is or rename to `TestIntegratorTemplated_writesConsolidatedCache`; either is fine.

- [ ] **Step 2: Run the full unit suite**

  ```bash
  make test-unit
  ```

  Expected: PASS — all tests green, including the noop test at line 129 (`TestIntegratorTemplated_noopWithEmptyInstructionsAndNoCache`) which already asserts `.gitattributes` is *not* written by `IntegratorTemplated` alone; that assertion remains correct and still passes.

- [ ] **Step 3: Commit**

  ```bash
  git add internal/integrate/integrate.go internal/integrate/integrate_test.go \
           internal/integrate/integrator_templated.go internal/integrate/integrator_templated_test.go
  git commit -m "feat(gitattributes): write .gitattributes on all integrate runs, not just templated"
  ```
