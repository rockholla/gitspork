# SDK Phase 1: Structural Results Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape `Integrate`, `IntegrateLocal`, and `CheckDrift` to return structural result types (`*IntegrateResult`, `*DriftReport`). Move user-facing check-drift output (per-file lines, summary, verbose diffs) from `internal/` into the CLI's cobra RunE. Preserve current CLI behavior functionally — existing functional tests pass unchanged.

**Architecture:** Phase 1 of three toward a golang SDK. All types stay in `internal/gitspork.go`; they move to a public package in Phase 3. Progress narration ("cloning upstream X…") stays on `Logger`; drift-shaped structural data moves to the returned `*DriftReport`. The CLI walks the report to reproduce today's output.

**Tech Stack:** Go, existing `github.com/go-git/go-git/v6` (`plumbing/format/diff.UnifiedEncoder` for per-file patch encoding), `github.com/spf13/cobra`.

---

## File Structure

Modified files:

- `internal/gitspork.go` — add `IntegrateResult`, `IntegratedUpstream`, `DriftReport`, `DriftedFile`; remove `Verbose` field from `CheckDriftOptions`
- `internal/integrate.go` — change `Integrate` signature to `(*IntegrateResult, error)`; populate the result; update internal `integrateOne` to return `IntegratedUpstream`
- `internal/integrate-local.go` — change `IntegrateLocal` signature to `(*IntegrateResult, error)`; populate with paths (URL slot holds the local path)
- `internal/check-drift.go` — change `CheckDrift` signature to `(*DriftReport, error)`; build `DriftReport` in place of per-file `Logger.Log` calls; extract per-file diff into `DriftedFile.Diff`
- `internal/logger.go` — remove the `Diff(io.Reader) error` method (no longer used from internal)
- `internal/integrate_test.go` — update tests to assert on returned `*IntegrateResult`
- `internal/check-drift_test.go` — update tests to inspect `*DriftReport`
- `cmd/integrate.go` — call new signature (result discarded; CLI has no use for it beyond error handling)
- `cmd/integrate-local.go` — same
- `cmd/check-drift.go` — call new signature; walk `DriftReport`; print per-file lines, summary, and verbose diffs from the report; drive verbose from a CLI-local flag rather than the removed `CheckDriftOptions.Verbose`

New files: none in this phase.

---

## Task 1: Add `IntegrateResult` types and reshape `Integrate`

**Files:**
- Modify: `internal/gitspork.go` (types)
- Modify: `internal/integrate.go` (signature + population)
- Modify: `internal/integrate_test.go` (existing tests updated + one new test asserting result shape)
- Modify: `internal/check-drift.go` (internal callsite to `Integrate` at ~line 131)
- Modify: `cmd/integrate.go` (RunE update to match new signature)

- [ ] **Step 1: Add shared setup helpers** — the existing test file has `testCommitAll` (line ~160 of `internal/integrate_test.go`) but no upstream/downstream fixture builders. Add these helpers to `internal/integrate_test.go` just before the existing `testCommitAll`:

```go
// testMinimalUpstream initialises a local upstream git repo with a minimal
// .gitspork.yml (upstream_owned only, no templated block) and one file. Returns
// the temp dir and the initial commit hash.
func testMinimalUpstream(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "upstream-owned"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "upstream-owned", "file.txt"), []byte("upstream content\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitspork.yml"), []byte("upstream_owned:\n- upstream-owned/**\n"), 0644))
	hash := testCommitAll(t, repo, "initial")
	return dir, hash
}

// testEmptyDownstream initialises a bare local downstream git repo ready for
// Integrate to write into.
func testEmptyDownstream(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	return dir
}
```

- [ ] **Step 2: Write the failing test** — append to `internal/integrate_test.go`:

```go
func TestIntegrate_returns_result_with_upstream_url_and_hash(t *testing.T) {
	upstreamDir, upstreamHash := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)

	result, err := Integrate(&IntegrateOptions{
		Logger:             NewLogger(),
		Upstreams:          []UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, "file://"+upstreamDir, result.Upstreams[0].URL)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)
	assert.Equal(t, "", result.Upstreams[0].Subpath)
}
```

- [ ] **Step 3: Run test to verify it fails** — expect a compile error (the type `*IntegrateResult` doesn't exist yet).

```bash
go test ./internal/... -run TestIntegrate_returns_result_with_upstream_url_and_hash
```

Expected: `# github.com/rockholla/gitspork/internal ... undefined: IntegrateResult` — compile failure.

- [ ] **Step 4: Add result types to `internal/gitspork.go`** — add after the existing `GitSporkDownstreamState` type declaration:

```go
// IntegrateResult is the structural return value of Integrate and IntegrateLocal.
// It records what was successfully integrated (in order); on partial failure
// the successful upstreams so far are still present in this result alongside
// the returned error.
type IntegrateResult struct {
	Upstreams []IntegratedUpstream
}

// IntegratedUpstream identifies a single successfully integrated upstream.
type IntegratedUpstream struct {
	URL        string
	Subpath    string
	CommitHash string
}

// DriftReport is the structural return value of CheckDrift. HasDrift is false
// when the downstream matches the recorded integration state; true when
// differences were found. Files enumerates the drifted entries with per-file
// attribution to whichever upstream last wrote each path.
type DriftReport struct {
	HasDrift bool
	Files    []DriftedFile
}

// DriftedFile is a single entry in a DriftReport.
type DriftedFile struct {
	Path          string
	AttributedURL string // upstream URL responsible for this file; empty means unattributed
	Diff          string // unified-diff text for just this file; empty for binary or unattributable
}
```

- [ ] **Step 5: Update `Integrate` signature and body** in `internal/integrate.go`. Replace lines 112–146 (the `Integrate` function) with:

```go
// Integrate will ensure that the downstream at opts.DownstreamRepoPath is
// integrated with each upstream in opts.Upstreams, in order.
func Integrate(opts *IntegrateOptions) (*IntegrateResult, error) {
	var err error
	result := &IntegrateResult{}

	if opts.DownstreamRepoPath == "" {
		opts.DownstreamRepoPath, err = os.Getwd()
		if err != nil {
			return result, fmt.Errorf("unable to get the present working directory: %v", err)
		}
	} else {
		opts.DownstreamRepoPath, err = filepath.Abs(opts.DownstreamRepoPath)
		if err != nil {
			return result, fmt.Errorf("unable to determine local downstream repo path: %v", err)
		}
	}

	// Normalize: synthesize Upstreams from single-upstream fields for backward compat.
	if len(opts.Upstreams) == 0 && opts.UpstreamRepoURL != "" {
		opts.Upstreams = []UpstreamSpec{{
			URL:     opts.UpstreamRepoURL,
			Version: opts.UpstreamRepoVersion,
			Subpath: opts.UpstreamRepoSubpath,
			Token:   opts.UpstreamRepoToken,
		}}
	}
	if len(opts.Upstreams) == 0 {
		return result, fmt.Errorf("no upstream specified: provide --upstream or --upstream-repo-url")
	}

	for _, upstream := range opts.Upstreams {
		integrated, err := integrateOne(opts, upstream)
		if err != nil {
			return result, err
		}
		result.Upstreams = append(result.Upstreams, integrated)
	}
	return result, nil
}
```

- [ ] **Step 6: Update `integrateOne` signature and body** in `internal/integrate.go`. Change the function signature and its return path so it produces an `IntegratedUpstream`. Only the signature and the `return` statements change:

```go
func integrateOne(opts *IntegrateOptions, upstream UpstreamSpec) (IntegratedUpstream, error) {
	// ... existing body unchanged until the trailing return ...
```

At every `return err` inside `integrateOne`, change to `return IntegratedUpstream{}, err`. At the trailing `return nil` (currently line ~230), replace with:

```go
	return IntegratedUpstream{
		URL:        originalUpstreamURL,
		Subpath:    upstream.Subpath,
		CommitHash: commitHash,
	}, nil
```

`commitHash` is already in scope from the `cloneUpstreamForIntegrate` call earlier in the function.

- [ ] **Step 7: Update the callsite inside `CheckDrift`** at `internal/check-drift.go` around line 131. Change:

```go
if err := Integrate(&IntegrateOptions{ ... }); err != nil {
```

to:

```go
if _, err := Integrate(&IntegrateOptions{ ... }); err != nil {
```

(The `IntegrateResult` from a drift-check re-integration is not useful; discard it.)

- [ ] **Step 8: Update the CLI callsite** in `cmd/integrate.go` — update the `RunE`. Find the `internal.Integrate(...)` call inside `RunE` and change it to discard the result:

```go
if _, err := internal.Integrate(&internal.IntegrateOptions{ ... }); err != nil {
    return err
}
return nil
```

- [ ] **Step 9: Run the failing test — expect it to pass now**

```bash
go test ./internal/... -run TestIntegrate_returns_result_with_upstream_url_and_hash
```

Expected: PASS.

- [ ] **Step 10: Run the full unit suite to catch collateral compile errors**

```bash
make test-unit
```

Expected: all PASS. Any test that previously called `Integrate` and got a single `error` return now needs `_, err := Integrate(...)`. Fix each callsite inline.

- [ ] **Step 11: Run the functional suite for CLI-parity**

```bash
make test-functional
```

Expected: all PASS (byte-equivalent output).

- [ ] **Step 12: Commit**

```bash
git add internal/gitspork.go internal/integrate.go internal/integrate_test.go internal/check-drift.go cmd/integrate.go
git commit -m "feat: Integrate returns *IntegrateResult with per-upstream URL/hash records"
```

---

## Task 2: Reshape `IntegrateLocal` to return `*IntegrateResult`

**Files:**
- Modify: `internal/integrate-local.go` (signature + population)
- Modify: `cmd/integrate-local.go` (RunE update)
- Modify: test files that call `IntegrateLocal` (if any exist — likely none in `internal/`; the functional tests exercise the CLI, so their contract is unchanged)

- [ ] **Step 1: Write the failing test** — append to `internal/integrate_test.go`. Reuses `testMinimalUpstream` and `testEmptyDownstream` from Task 1 Step 1:

```go
func TestIntegrateLocal_returns_result_with_upstream_paths(t *testing.T) {
	upstreamDir, _ := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)

	result, err := IntegrateLocal(&IntegrateLocalOptions{
		Logger:         NewLogger(),
		UpstreamPaths:  []string{upstreamDir},
		DownstreamPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Upstreams, 1)
	// IntegrateLocal has no URL — record the path in the URL slot with no scheme.
	assert.Equal(t, upstreamDir, result.Upstreams[0].URL)
	assert.Equal(t, "", result.Upstreams[0].CommitHash)
}
```

- [ ] **Step 2: Run test to verify it fails** — expect compile error (signature mismatch).

```bash
go test ./internal/... -run TestIntegrateLocal_returns_result_with_upstream_paths
```

Expected: compile failure (`IntegrateLocal returns only error, cannot assign 2 values`).

- [ ] **Step 3: Update `IntegrateLocal` signature and body** in `internal/integrate-local.go`. Replace the entire function:

```go
// IntegrateLocal integrates one or more local upstream paths into the downstream.
func IntegrateLocal(opts *IntegrateLocalOptions) (*IntegrateResult, error) {
	result := &IntegrateResult{}

	// Normalize: single UpstreamPath -> UpstreamPaths slice.
	if len(opts.UpstreamPaths) == 0 && opts.UpstreamPath != "" {
		opts.UpstreamPaths = []string{opts.UpstreamPath}
	}
	if len(opts.UpstreamPaths) == 0 {
		return result, fmt.Errorf("no upstream path specified: provide --upstream-path")
	}

	for _, upstreamPath := range opts.UpstreamPaths {
		opts.Logger.Log("parsing the gitspork config file at %s or %s",
			filepath.Join(upstreamPath, gitSporkConfigFileName),
			filepath.Join(upstreamPath, gitSporkConfigFileNameAlt))
		gitSporkConfig, err := getGitSporkConfig(upstreamPath)
		if err != nil {
			return result, err
		}
		if err := integrate(gitSporkConfig, upstreamPath, opts.DownstreamPath, opts.ForceRePrompt, false, opts.Logger); err != nil {
			return result, err
		}
		result.Upstreams = append(result.Upstreams, IntegratedUpstream{
			URL: upstreamPath, // local path recorded in URL slot; no CommitHash concept for local
		})
	}
	return result, nil
}
```

- [ ] **Step 4: Update `cmd/integrate-local.go` RunE**:

```go
if _, err := internal.IntegrateLocal(&internal.IntegrateLocalOptions{ ... }); err != nil {
    return err
}
return nil
```

- [ ] **Step 5: Run the failing test — expect PASS**

```bash
go test ./internal/... -run TestIntegrateLocal_returns_result_with_upstream_paths
```

Expected: PASS.

- [ ] **Step 6: Run full unit + functional suites**

```bash
make test-unit && make test-functional
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/integrate-local.go internal/integrate_test.go cmd/integrate-local.go
git commit -m "feat: IntegrateLocal returns *IntegrateResult"
```

---

## Task 3: Add `DriftReport`, reshape `CheckDrift` signature and structural output (no drift, per-file, summary)

This task is deliberately atomic: internal produces the report, CLI prints from it. Between these two changes the functional test suite's expected output would break, so both changes ship together.

**Files:**
- Modify: `internal/check-drift.go` (signature, build DriftReport, remove three Logger.Log calls for user-facing structural output; keep progress-narration calls)
- Modify: `internal/check-drift_test.go` (update tests to assert on DriftReport)
- Modify: `cmd/check-drift.go` (walk DriftReport, print equivalent output)

- [ ] **Step 1: Add drift-setup helpers** — append to `internal/check-drift_test.go` (below `makeBaselineRepo`):

```go
// testIntegrateAndCommitBaseline integrates upstreamDir into downstreamDir and
// commits the resulting downstream state so the working tree is clean and
// CheckDrift can operate. Returns the post-integrate commit hash.
func testIntegrateAndCommitBaseline(t *testing.T, upstreamDir, downstreamDir string) plumbing.Hash {
	t.Helper()
	_, err := Integrate(&IntegrateOptions{
		Logger:             NewLogger(),
		Upstreams:          []UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	return testCommitAll(t, repo, "post-integrate baseline")
}

// testWriteAndCommitInDownstream writes content to a file inside downstreamDir
// and commits, simulating a downstream-side edit that check-drift should detect.
func testWriteAndCommitInDownstream(t *testing.T, downstreamDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(downstreamDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	testCommitAll(t, repo, "drift edit: "+relPath)
}
```

Add any missing imports to `internal/check-drift_test.go`: `plumbing` from go-git may already be imported (used by `makeBaselineRepo`); `testCommitAll` is defined in `internal/integrate_test.go` in the same package so it's directly callable.

- [ ] **Step 2: Write the failing tests** — append to `internal/check-drift_test.go`. Reuses `testMinimalUpstream` and `testEmptyDownstream` from Task 1 Step 1 (same package, same test file directory — directly callable):

```go
func TestCheckDrift_returns_report_no_drift(t *testing.T) {
	upstreamDir, _ := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)

	report, err := CheckDrift(&CheckDriftOptions{
		Logger:             NewLogger(),
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.HasDrift)
	assert.Empty(t, report.Files)
}

func TestCheckDrift_returns_report_with_drifted_file_and_attribution(t *testing.T) {
	upstreamDir, _ := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)
	testIntegrateAndCommitBaseline(t, upstreamDir, downstreamDir)
	testWriteAndCommitInDownstream(t, downstreamDir, "upstream-owned/file.txt", "drifted\n")

	report, err := CheckDrift(&CheckDriftOptions{
		Logger:             NewLogger(),
		DownstreamRepoPath: downstreamDir,
	})
	require.ErrorIs(t, err, ErrDriftDetected)
	require.NotNil(t, report)
	assert.True(t, report.HasDrift)
	require.Len(t, report.Files, 1)
	assert.Equal(t, "upstream-owned/file.txt", report.Files[0].Path)
	assert.Equal(t, "file://"+upstreamDir, report.Files[0].AttributedURL)
}
```

- [ ] **Step 3: Run the failing tests** — expect compile errors (CheckDrift returns only `error`).

```bash
go test ./internal/... -run 'TestCheckDrift_returns_report'
```

Expected: compile failure.

- [ ] **Step 4: Update `CheckDrift` signature** in `internal/check-drift.go` — change:

```go
func CheckDrift(opts *CheckDriftOptions) error {
```

to:

```go
func CheckDrift(opts *CheckDriftOptions) (*DriftReport, error) {
```

Add `report := &DriftReport{}` as the first statement inside the function. Change EVERY early `return err` in the function to `return report, err`. Change `return nil` to `return report, nil`.

- [ ] **Step 5: Build `DriftReport.Files` in place of per-file `Logger.Log`** — locate the block currently at approximately lines 165–187 that reads:

```go
if patch == nil {
    opts.Logger.Log("no drift detected")
    return nil
}

stats := patch.Stats()
opts.Logger.Log("drift detected: %d file(s) changed", len(stats))
for _, s := range stats {
    owner := fileOwner[s.Name]
    if owner == "" {
        owner = "(unknown upstream)"
    }
    opts.Logger.Log("  %s (upstream: %s)", s.Name, owner)
}
if opts.Verbose {
    pr, pw := io.Pipe()
    go func() { pw.CloseWithError(patch.Encode(pw)) }()
    if err := opts.Logger.Diff(pr); err != nil {
        return fmt.Errorf("error encoding diff: %v", err)
    }
}

return ErrDriftDetected
```

Replace with:

```go
if patch == nil {
    return report, nil
}

report.HasDrift = true
stats := patch.Stats()
for _, s := range stats {
    report.Files = append(report.Files, DriftedFile{
        Path:          s.Name,
        AttributedURL: fileOwner[s.Name], // empty string means unattributed
        // Diff populated in Task 4
    })
}

return report, ErrDriftDetected
```

Note: `opts.Verbose` handling is deliberately dropped in this task — Task 4 handles per-file Diff population and removes the field. For now leave `Verbose` on `CheckDriftOptions`; the field stays until Task 4.

- [ ] **Step 6: Update the CLI to walk the report** in `cmd/check-drift.go`. Modify the `RunE` closure to consume the return values and reproduce the pre-refactor output. Replace the current call:

```go
err := internal.CheckDrift(opts)
if errors.Is(err, internal.ErrDriftDetected) {
    os.Exit(2)
}
return err
```

with:

```go
report, err := internal.CheckDrift(opts)
if err != nil && !errors.Is(err, internal.ErrDriftDetected) {
    return err
}
if !report.HasDrift {
    logger.Log("no drift detected")
    return nil
}
logger.Log("drift detected: %d file(s) changed", len(report.Files))
for _, f := range report.Files {
    attribution := f.AttributedURL
    if attribution == "" {
        attribution = "(unknown upstream)"
    }
    logger.Log("  %s (upstream: %s)", f.Path, attribution)
}
// verbose/diff output moves to Task 4
os.Exit(2)
return nil // unreachable but keeps Go happy
```

- [ ] **Step 7: Run the two new tests — expect PASS**

```bash
go test ./internal/... -run 'TestCheckDrift_returns_report'
```

Expected: PASS.

- [ ] **Step 8: Update the existing `TestCheckDrift`** at `internal/check-drift_test.go:16`. Its two `t.Run` blocks currently call `err = CheckDrift(...)` — change both to `_, err = CheckDrift(...)` to match the new signature. Assertions on the error message content are unchanged (both existing sub-tests assert on error text, not on log output, so no other updates needed).

- [ ] **Step 9: Run full unit + functional suites**

```bash
make test-unit && make test-functional
```

Expected: all PASS. If a functional test assertion mismatches on output, verify the CLI print statement in Step 5 matches the exact previous format (including leading whitespace on per-file lines).

- [ ] **Step 10: Commit**

```bash
git add internal/gitspork.go internal/check-drift.go internal/check-drift_test.go cmd/check-drift.go
git commit -m "feat: CheckDrift returns *DriftReport; CLI walks report for structural output"
```

---

## Task 4: Populate per-file `DriftedFile.Diff`; move verbose output to CLI; remove `Logger.Diff` and `CheckDriftOptions.Verbose`

**Files:**
- Modify: `internal/check-drift.go` (populate Diff via per-file unified encoder; drop io/logger.Diff usage)
- Modify: `internal/gitspork.go` (remove `Verbose bool` from `CheckDriftOptions`)
- Modify: `internal/logger.go` (delete `Diff(io.Reader) error` method)
- Modify: `internal/check-drift_test.go` (add test asserting `DriftedFile.Diff` populated with unified diff header/hunks)
- Modify: `cmd/check-drift.go` (add local `verbose` var driven by the existing `--verbose` flag; iterate `report.Files[].Diff` for verbose output)

- [ ] **Step 1: Write the failing test** — append to `internal/check-drift_test.go`:

```go
func TestCheckDrift_report_files_include_unified_diff(t *testing.T) {
	upstreamDir, _ := testCreateUpstreamAndCommit(t)
	downstreamDir := testCreateDownstreamRepo(t)
	testPrepDownstreamInputData(t, downstreamDir)
	testDoInitialIntegrate(t, upstreamDir, downstreamDir)
	testWriteAndCommitFile(t, downstreamDir, "upstream-owned/file.txt", "drifted\n")
	testPrepDownstreamInputData(t, downstreamDir)

	report, err := CheckDrift(&CheckDriftOptions{
		Logger:             NewLogger(),
		DownstreamRepoPath: downstreamDir,
	})
	require.ErrorIs(t, err, ErrDriftDetected)
	require.Len(t, report.Files, 1)
	diff := report.Files[0].Diff
	assert.Contains(t, diff, "upstream-owned/file.txt",
		"expected the unified diff to reference the path, got:\n%s", diff)
	assert.Contains(t, diff, "-upstream content", "expected removed-line marker for old content")
	assert.Contains(t, diff, "+drifted", "expected added-line marker for new content")
}
```

- [ ] **Step 2: Run the failing test** — expect assertion failure (Diff is empty from Task 3).

```bash
go test ./internal/... -run TestCheckDrift_report_files_include_unified_diff
```

Expected: FAIL on `assert.Contains(t, diff, "upstream-owned/file.txt")`.

- [ ] **Step 3: Add a per-file patch encoder helper** in `internal/check-drift.go` — add above the existing `checkCleanWorkingTree` function:

```go
// singleFilePatch adapts a single fdiff.FilePatch to the fdiff.Patch interface
// so it can be run through UnifiedEncoder to produce a per-file unified diff.
type singleFilePatch struct {
    fp fdiff.FilePatch
}

func (s *singleFilePatch) FilePatches() []fdiff.FilePatch { return []fdiff.FilePatch{s.fp} }
func (s *singleFilePatch) Message() string                 { return "" }

// encodeFilePatch renders one file's unified diff to a string.
func encodeFilePatch(fp fdiff.FilePatch) (string, error) {
    var buf bytes.Buffer
    enc := fdiff.NewUnifiedEncoder(&buf, fdiff.DefaultContextLines)
    if err := enc.Encode(&singleFilePatch{fp: fp}); err != nil {
        return "", err
    }
    return buf.String(), nil
}
```

Add imports to `internal/check-drift.go`:

```go
"bytes"

fdiff "github.com/go-git/go-git/v6/plumbing/format/diff"
```

If `fdiff` (aliased or not) is not already imported, use that alias to avoid stuttering. Remove the now-unused `"io"` import if applicable (it was only there for `io.Pipe` in the verbose branch, which is being removed).

- [ ] **Step 4: Populate `DriftedFile.Diff` per file** — modify the loop from Task 3 that builds `report.Files`. Replace the simple `stats` loop with an iteration over `patch.FilePatches()`:

```go
report.HasDrift = true
for _, fp := range patch.FilePatches() {
    from, to := fp.Files()
    var name string
    switch {
    case to != nil:
        name = to.Path()
    case from != nil:
        name = from.Path()
    default:
        continue
    }
    diffText, err := encodeFilePatch(fp)
    if err != nil {
        return report, fmt.Errorf("error encoding per-file diff for %s: %v", name, err)
    }
    report.Files = append(report.Files, DriftedFile{
        Path:          name,
        AttributedURL: fileOwner[name],
        Diff:          diffText,
    })
}
```

Delete the now-unused `stats := patch.Stats()` line and the block that used it.

- [ ] **Step 5: Remove `Verbose` from `CheckDriftOptions`** in `internal/gitspork.go`. Find:

```go
type CheckDriftOptions struct {
    Logger             *Logger
    DownstreamRepoPath string
    Upstreams          []UpstreamSpec
    Verbose            bool
}
```

Delete the `Verbose` field. The struct becomes:

```go
type CheckDriftOptions struct {
    Logger             *Logger
    DownstreamRepoPath string
    Upstreams          []UpstreamSpec
}
```

- [ ] **Step 6: Remove `Logger.Diff` method** in `internal/logger.go`. Delete the entire method:

```go
func (l *Logger) Diff(r io.Reader) error {
    // ... existing body ...
}
```

Remove the `io` import from `internal/logger.go` if no other symbol in that file uses it.

- [ ] **Step 7: Update `cmd/check-drift.go`** to drive verbose from a CLI-local flag and print `DriftedFile.Diff`. The `verbose` local variable already exists (it's the cobra flag). Where the CLI stopped its output block in Task 3 (before the `os.Exit(2)`), add:

```go
if verbose {
    for _, f := range report.Files {
        if f.Diff == "" {
            continue
        }
        fmt.Print(f.Diff)
    }
}
```

Add `"fmt"` import if not already present. Remove any code path that set `opts.Verbose = verbose` — that field no longer exists on `CheckDriftOptions`.

- [ ] **Step 8: Run the failing test — expect PASS**

```bash
go test ./internal/... -run TestCheckDrift_report_files_include_unified_diff
```

Expected: PASS.

- [ ] **Step 9: Verify no callers of `Logger.Diff` remain**

```bash
grep -rn "\.Diff(" internal/ cmd/
```

Expected: no matches referencing `Logger.Diff`. (Matches like `DriftedFile.Diff` are fine — that's a struct field, not the removed method.)

- [ ] **Step 10: Run all four test suites**

```bash
make test-unit && make test-functional && make test-functional-docker && make test-examples
```

Expected: all PASS. If the functional `--verbose` drift test asserts specific diff content, verify it still matches — the format shifts slightly from `patch.Encode` (whole-patch header once) to per-file `UnifiedEncoder.Encode` (per-file header prefixes). If a test breaks on this, update the assertion to match the new but still-valid unified-diff format.

- [ ] **Step 11: Commit**

```bash
git add internal/check-drift.go internal/check-drift_test.go internal/gitspork.go internal/logger.go cmd/check-drift.go
git commit -m "feat: populate DriftedFile.Diff per file; remove Verbose option and Logger.Diff"
```

---

## Task 5: Final verification and cleanup

- [ ] **Step 1: Run every test suite locally**

```bash
make test-unit
make test-functional
make test-functional-docker
make test-examples
```

Expected: all PASS. If any suite fails, treat as a regression from earlier tasks — don't proceed until fixed.

- [ ] **Step 2: Grep for stale references**

```bash
grep -rn "opts.Verbose\|options.Verbose\|CheckDriftOptions{.*Verbose" internal/ cmd/
```

Expected: no matches — the field is gone and nothing references it.

```bash
grep -rn "func (l \*Logger) Diff\|logger.Diff" internal/ cmd/
```

Expected: no matches for the Logger method (matches for `report.Files[i].Diff` or `DriftedFile.Diff` are struct-field references and are fine).

- [ ] **Step 3: Sanity-check the CLI end-to-end manually** with a real integrate + check-drift roundtrip:

```bash
go build -o /tmp/gitspork-phase1 .
# Set up a small upstream/downstream pair however you like, then:
/tmp/gitspork-phase1 check-drift --downstream-repo-path <dir>
/tmp/gitspork-phase1 check-drift --downstream-repo-path <dir> --verbose
```

Expected: output looks the same as pre-refactor. Compare against `main` build if in doubt.

- [ ] **Step 4: Commit any final tweaks**

If Steps 1–3 exposed any missed spots, fix them and commit as `fix: post-refactor cleanup for phase 1` (or similar). Do NOT amend earlier commits — new commits keep the review-diff traceable.

---

## Backward Compatibility

- CLI invocations unchanged. Every flag, exit code, and functionally-equivalent output preserved.
- Downstream state files (`.gitspork/downstream-state.json`) unchanged — Phase 1 only touches return signatures and CLI-visible output paths.
- Public API: no library exposure yet. `IntegrateResult`, `DriftReport`, and friends live in `internal/` and get promoted in Phase 3.
