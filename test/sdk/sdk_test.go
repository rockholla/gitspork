//go:build sdk

package sdk_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rockholla/gitspork/v2"
)

// integrate: single upstream returns a populated *IntegrateResult
func TestIntegrate_singleUpstream(t *testing.T) {
	upstreamDir, upstreamHash := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, "file://"+upstreamDir, result.Upstreams[0].URL)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)
}

// integrate: Version="" resolves to the default branch (HEAD)
//
// The empty-Version branch is explicitly documented in the code
// (`cloneOptions.SingleBranch = true`, integrate.go:432) and in the field's
// godoc but had no SDK-tier assertion. Empty means "clone the default
// branch"; the result's CommitHash should be the HEAD commit.
func TestIntegrate_version_empty_resolvesToDefaultBranch(t *testing.T) {
	upstreamDir, upstreamHash := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir /* Version intentionally omitted */}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err, "empty Version should clone the default branch")
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)
}

// integrate: Version=<bare-tag> resolves to the tag
//
// The bare-tag branch probes the remote for tag vs branch precedence
// (integrate.go:439-445). Tag wins over a same-named branch — the internal
// test Test_Integrate_bare_tag covers that at the package boundary; this
// pins the same contract at the SDK boundary so an SDK consumer using
// Version="v1.2.3" gets a reproducible result.
func TestIntegrate_version_bareTag(t *testing.T) {
	upstreamDir, upstreamHash := minimalUpstreamWithTag(t, "v1.0.0")
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "v1.0.0"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err, "bare tag name should resolve as a tag")
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)
}

// integrate: Version="tags/<name>" is the explicit-tag form (backward-compat
// with pre-bare-tag callers) — integrate.go:436-438.
func TestIntegrate_version_tagsPrefixed(t *testing.T) {
	upstreamDir, upstreamHash := minimalUpstreamWithTag(t, "v1.0.0")
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "tags/v1.0.0"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err, "tags/ prefix should resolve as a tag (backward compat)")
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)
}

// integrate: Version=<full-40-char-commit-hash> pins to that commit
//
// commitHashRe matches 7-40 hex chars (integrate.go:42). The full-hash case
// triggers a full-history clone + explicit worktree checkout at the given
// hash (integrate.go:433-435 + 468-480).
func TestIntegrate_version_fullCommitHash(t *testing.T) {
	upstreamDir, upstreamHash := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: upstreamHash.String()}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err, "full commit hash should be resolvable")
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)
}

// integrate: Version=<7-char-short-commit-hash> resolves to full hash
//
// The result's CommitHash is what state persistence uses to match on
// subsequent integrates. A short hash must resolve to the FULL 40-char hash
// in the returned IntegratedUpstream so state lookups stay stable across
// runs, regardless of how the caller specified the pin.
func TestIntegrate_version_shortCommitHash(t *testing.T) {
	upstreamDir, upstreamHash := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)
	shortHash := upstreamHash.String()[:7]

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: shortHash}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err, "short commit hash should be resolvable")
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash,
		"short hash should resolve to the full 40-char commit hash in the result — state persistence relies on this")
	assert.Len(t, result.Upstreams[0].CommitHash, 40,
		"IntegratedUpstream.CommitHash must be 40 chars, not a short-hash echo")
}

// integrate: Version=<unknown> surfaces a clear error naming the ref
func TestIntegrate_version_unknownRef_errorsWithRefName(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "no-such-ref"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-ref",
		"error must name the unresolved ref so the SDK consumer can act on it")
}

// integrate: subpath is round-tripped end-to-end
//
// Pins the multi-PR (#62, #64) subpath fix at the black-box SDK boundary:
// an upstream whose .gitspork.yml lives under <upstream>/infra/ must
// integrate correctly when Subpath is set, and IntegratedUpstream must
// carry the (normalized) subpath back to the SDK caller for state matching.
func TestIntegrate_subpath(t *testing.T) {
	upstreamDir, upstreamHash := minimalUpstreamInSubpath(t, "infra")
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams: []gitspork.UpstreamSpec{{
			URL:     "file://" + upstreamDir,
			Version: "main",
			Subpath: "infra",
		}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, "file://"+upstreamDir, result.Upstreams[0].URL)
	assert.Equal(t, "infra", result.Upstreams[0].Subpath,
		"IntegratedUpstream.Subpath must round-trip so consumers can match state entries by URL+subpath")
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)

	// Files land in the downstream WITHOUT the subpath prefix — content comes
	// from <upstream>/infra/upstream-owned/file.txt but the downstream layout
	// is upstream-owned/file.txt (subpath is stripped by the integrator).
	require.NoError(t, fileExists(downstreamDir, "upstream-owned", "file.txt"),
		"upstream-owned file should be present in downstream stripped of the subpath prefix")

	// The unrelated repo-root file from the "monorepo shape" MUST NOT land in
	// the downstream — subpath scoping should exclude everything outside it.
	err = fileExists(downstreamDir, "README.md")
	assert.Error(t, err, "unrelated repo-root file should NOT land in the downstream when Subpath is scoped to a subdirectory")
}

// integrate: subpath shape variants ("infra/", "/infra", "./infra") all match
// the same state entry — pins the NormalizeUpstreamPath + NormalizeUpstreamURL
// fix from PR #64 at the SDK boundary.
func TestIntegrate_subpath_shape_variants_share_state_entry(t *testing.T) {
	upstreamDir, _ := minimalUpstreamInSubpath(t, "infra")
	downstreamDir := emptyDownstream(t)

	// First integrate with the canonical form.
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main", Subpath: "infra"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)

	// Second integrate with a trailing-slash variant — must update the same
	// state entry, not append a duplicate.
	_, err = gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main", Subpath: "infra/"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)

	// Inspect state directly: must have exactly one upstream entry after two
	// integrates against subpath-shape variants of the same upstream.
	state := readStateJSON(t, downstreamDir)
	assert.Len(t, state.Upstreams, 1,
		"integrating twice with subpath shape variants must not create duplicate state entries")
}

// integrate: multi-upstream order is preserved in the result
func TestIntegrate_multiUpstreamOrder(t *testing.T) {
	upstreamA, hashA := minimalUpstream(t)
	upstreamB, hashB := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams: []gitspork.UpstreamSpec{
			{URL: "file://" + upstreamA, Version: "main"},
			{URL: "file://" + upstreamB, Version: "main"},
		},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.Len(t, result.Upstreams, 2)
	assert.Equal(t, "file://"+upstreamA, result.Upstreams[0].URL)
	assert.Equal(t, "file://"+upstreamB, result.Upstreams[1].URL)
	assert.Equal(t, hashA.String(), result.Upstreams[0].CommitHash)
	assert.Equal(t, hashB.String(), result.Upstreams[1].CommitHash)
}

// integrate-local: multi-path; state file not written
func TestIntegrateLocal_multiPath_noStateFile(t *testing.T) {
	upstreamA, _ := minimalUpstream(t)
	upstreamB, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.IntegrateLocal(&gitspork.IntegrateLocalOptions{
		UpstreamPaths:  []string{upstreamA, upstreamB},
		DownstreamPath: downstreamDir,
	})
	require.NoError(t, err)
	require.Len(t, result.Upstreams, 2)

	// State file must NOT be written for IntegrateLocal.
	err = fileExists(downstreamDir, ".gitspork", "downstream-state.json")
	assert.Error(t, err, "expected no state file after IntegrateLocal")
}

// check-drift: no drift returns HasDrift=false and nil error
func TestCheckDrift_noDrift(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)

	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")

	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.False(t, report.HasDrift)
	assert.Empty(t, report.Files)
}

// check-drift: drift returns HasDrift=true, populated Files, ErrDriftDetected sentinel
func TestCheckDrift_driftDetected_returnsErrSentinel(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")
	writeAndCommit(t, downstreamDir, "upstream-owned/file.txt", "drifted content\n")

	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
	})
	require.True(t, errors.Is(err, gitspork.ErrDriftDetected), "expected ErrDriftDetected, got %v", err)
	require.NotNil(t, report)
	assert.True(t, report.HasDrift)
	require.Len(t, report.Files, 1)
	assert.Equal(t, "upstream-owned/file.txt", report.Files[0].Path)
	assert.Equal(t, "file://"+upstreamDir, report.Files[0].AttributedURL)
	assert.Contains(t, report.Files[0].Diff, "upstream-owned/file.txt", "per-file diff should reference the path")

	// ColorizedDiff contains ANSI escape codes so SDK consumers can render
	// human-readable output. Populated regardless of process TTY state.
	assert.NotEqual(t, report.Files[0].Diff, report.Files[0].ColorizedDiff,
		"ColorizedDiff should differ from Diff (colors were applied)")
	assert.Contains(t, report.Files[0].ColorizedDiff, "\x1b[",
		"ColorizedDiff should contain ANSI escape codes")
	assert.Contains(t, report.Files[0].ColorizedDiff, "upstream-owned/file.txt",
		"ColorizedDiff should still reference the path")
}

// Logger contract: nil Logger means silent (no panic)
func TestLogger_nilIsSilent(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             nil,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
}

// Logger contract: custom implementation receives progress calls
func TestLogger_customImplementation(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	captured := &captureLogger{}
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             captured,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, captured.entries, "custom Logger should receive progress calls")
}

// TestCheckDrift_overrideMatchesState_noDrift is the CheckDrift override
// happy path — an explicit Upstreams override that matches the state entry.
// The existing sdk tests only cover the error path (override not in state);
// this one proves the matched-override branch actually runs.
func TestCheckDrift_overrideMatchesState_noDrift(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")

	// Override with the same URL — matched against state entry, integrator
	// pins at the recorded commit, no drift.
	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir}},
	})
	require.NoError(t, err, "matched override with no drifted files should return nil error")
	require.NotNil(t, report)
	assert.False(t, report.HasDrift, "no drift expected against a matched override")
	assert.Empty(t, report.Files)
}

// TestCheckDrift_overrideMatchesState_driftDetected: matched-override happy
// path with actual drift. Locks the DriftReport contract when the caller
// scoped the check to a specific upstream via override.
func TestCheckDrift_overrideMatchesState_driftDetected(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")
	writeAndCommit(t, downstreamDir, "upstream-owned/file.txt", "drifted content\n")

	overrideURL := "file://" + upstreamDir
	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
		Upstreams:          []gitspork.UpstreamSpec{{URL: overrideURL}},
	})
	require.ErrorIs(t, err, gitspork.ErrDriftDetected)
	require.NotNil(t, report)
	assert.True(t, report.HasDrift)
	require.Len(t, report.Files, 1)
	assert.Equal(t, "upstream-owned/file.txt", report.Files[0].Path)
	// AttributedURL is the CALLER-supplied override, not whatever shape the
	// state file records. Fixed by the current implementation
	// (check_drift.go: fileOwner[f] = entry.spec.URL) and worth pinning so
	// SDK consumers can rely on it.
	assert.Equal(t, overrideURL, report.Files[0].AttributedURL,
		"drifted-file attribution must echo the caller's override URL")
}

// TestCheckDrift_overrideNormalizesToStateEntry mirrors the functional-tier
// TestCheckDrift_upstream_override_url_normalized at the SDK boundary: the
// state entry is recorded under one URL form, the override is passed with a
// trailing `.git` suffix, and NormalizeUpstreamURL matches them. Uses a
// same-directory symlink so the `.git` alias clone-resolves to the same
// repo. Windows is skipped because symlink semantics differ.
func TestCheckDrift_overrideNormalizesToStateEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink alias not portable to Windows CI")
	}
	upstreamDir, _ := minimalUpstream(t)
	alias := upstreamDir + ".git"
	require.NoError(t, os.Symlink(upstreamDir, alias))

	downstreamDir := emptyDownstream(t)
	// Integrate with the un-suffixed URL; state records this form.
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")

	// Override URL adds ".git" — NormalizeUpstreamURL strips it, so this must
	// still match the state entry.
	overrideURL := "file://" + alias
	report, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
		Upstreams:          []gitspork.UpstreamSpec{{URL: overrideURL}},
	})
	require.NoError(t, err,
		"URL-shape-varying override must normalize to the state entry and run cleanly")
	assert.False(t, report.HasDrift)
}

// TestIntegrate_partialFailure_populatesSuccessfulUpstreams locks the
// documented contract at gitspork.go:36-38: on partial failure, the
// upstreams that DID succeed before the failure remain in result.Upstreams
// alongside the returned error. SDK consumers wiring retry / cleanup logic
// against a partial-failure result need this to be reliable.
func TestIntegrate_partialFailure_populatesSuccessfulUpstreams(t *testing.T) {
	upstreamA, hashA := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	// Second upstream URL points at a directory that isn't a git repo — the
	// clone step in integrateOneInternal fails, aborting the loop mid-way.
	bogusURL := "file:///tmp/gitspork-never-cloned-partial-failure-test"

	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams: []gitspork.UpstreamSpec{
			{URL: "file://" + upstreamA, Version: "main"},
			{URL: bogusURL, Version: "main"},
		},
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err, "the bogus second upstream must surface as an error")
	require.NotNil(t, result, "*IntegrateResult must be non-nil even on error — gitspork.go:37-38")

	require.Len(t, result.Upstreams, 1,
		"the first (successful) upstream must remain in result.Upstreams after the second one fails")
	assert.Equal(t, "file://"+upstreamA, result.Upstreams[0].URL,
		"the recorded IntegratedUpstream must reflect the first upstream, not the failing one")
	assert.Equal(t, hashA.String(), result.Upstreams[0].CommitHash,
		"CommitHash of the successful integration must be populated")

	// The failing upstream is NOT recorded — only successes make it into
	// result.Upstreams (see integrate.go:145-148).
	for _, u := range result.Upstreams {
		assert.NotEqual(t, bogusURL, u.URL, "failed upstream must not appear in result.Upstreams")
	}
}

// TestIntegrateLocal_partialFailure_populatesSuccessfulUpstreams mirrors the
// same contract for IntegrateLocal — see integrate_local.go:23-37.
func TestIntegrateLocal_partialFailure_populatesSuccessfulUpstreams(t *testing.T) {
	upstreamA, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	// Second path does not exist — getGitSporkConfig fails, aborting the loop.
	bogusPath := filepath.Join(t.TempDir(), "does-not-exist")

	result, err := gitspork.IntegrateLocal(&gitspork.IntegrateLocalOptions{
		UpstreamPaths:  []string{upstreamA, bogusPath},
		DownstreamPath: downstreamDir,
	})
	require.Error(t, err, "second (nonexistent) upstream path must fail")
	require.NotNil(t, result)
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, upstreamA, result.Upstreams[0].URL,
		"IntegrateLocal records the local path in URL — see IntegratedUpstream godoc")
	assert.Empty(t, result.Upstreams[0].CommitHash,
		"IntegrateLocal has no commit-hash concept — the field must be empty on local integrations")
}

// TestIntegrate_earlyError_returnsNonNilResult exercises the docs contract
// (gitspork.go:37-38) that *IntegrateResult is ALWAYS non-nil, even on the
// earliest error paths where no upstream ever runs. Consumers don't need to
// nil-check before inspecting Upstreams.
func TestIntegrate_earlyError_returnsNonNilResult(t *testing.T) {
	downstreamDir := emptyDownstream(t)

	// Empty Upstreams — the earliest post-validation error path.
	result, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          nil,
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err)
	require.NotNil(t, result, "*IntegrateResult must be non-nil even when no upstream ever ran")
	assert.Empty(t, result.Upstreams,
		"an errored-early integrate must not fabricate entries in Upstreams")
}

// TestIntegrateLocal_earlyError_returnsNonNilResult: same contract for the
// local variant. Empty UpstreamPaths hits the earliest validation error.
func TestIntegrateLocal_earlyError_returnsNonNilResult(t *testing.T) {
	downstreamDir := emptyDownstream(t)

	result, err := gitspork.IntegrateLocal(&gitspork.IntegrateLocalOptions{
		UpstreamPaths:  nil,
		DownstreamPath: downstreamDir,
	})
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Upstreams)
}

// TestIntegrate_concurrent_distinctDownstreams pins the SDK-level
// contract: concurrent Integrate calls against DISTINCT downstream repos
// must produce independent, correct results — one goroutine's result and
// state file must not be corrupted by another's. This is the realistic
// bulk-operation shape (e.g. a controller integrating N repos in parallel).
//
// Concurrent Integrate against the SAME downstream is out of scope — that
// would be inherently racy on the state file and the worktree, and is not
// claimed to be supported.
//
// Test-fixture note: each goroutine gets its OWN upstream too, not just
// its own downstream. go-git's file:// transport shells out to
// git-upload-pack --stateless-rpc, and concurrent invocations against
// the SAME repo path have been observed to interleave stdio at the
// pkt-line boundary under Linux CI timing (surfaces as
// "pkt-line 1: NULL not found"). That's a file-transport-specific
// concurrency limitation of the test harness, not a real-user shape —
// real consumers use HTTPS/SSH URLs where every connection is a separate
// socket to a real server. Distinct upstreams here still lock the
// contract we care about (per-downstream state integrity across
// goroutines) without depending on that transport's concurrency behavior.
//
// Note on `go test -race`: this test passes WITHOUT the race detector.
// Under `-race`, an unrelated race triggers inside go-git's own
// PackSession.Handshake (a helper goroutine writes to a stderr buffer that
// the parent reads at defer-time) — that race is INTERNAL to a single
// Remote.List call and fires even on non-concurrent single-Integrate tests.
// It's a pre-existing go-git issue, not a bug this test is claiming
// gitspork is free of. If it gets fixed upstream, enabling -race for this
// suite will start giving meaningful signal about cross-goroutine races on
// gitspork's own state.
func TestIntegrate_concurrent_distinctDownstreams(t *testing.T) {
	const n = 4

	// Build all upstreams up front (sequentially) — the concurrency contract
	// under test is about Integrate, not fixture setup.
	upstreamDirs := make([]string, n)
	upstreamHashes := make([]string, n)
	for i := 0; i < n; i++ {
		dir, hash := minimalUpstream(t)
		upstreamDirs[i] = dir
		upstreamHashes[i] = hash.String()
	}

	type outcome struct {
		result *gitspork.IntegrateResult
		err    error
		down   string
	}
	results := make(chan outcome, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			downstreamDir := emptyDownstream(t)
			r, err := gitspork.Integrate(&gitspork.IntegrateOptions{
				Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDirs[idx], Version: "main"}},
				DownstreamRepoPath: downstreamDir,
			})
			results <- outcome{result: r, err: err, down: downstreamDir}
		}(i)
	}
	wg.Wait()
	close(results)

	// Build URL → expected-hash map so the assertions can identify which
	// upstream each outcome corresponds to (order isn't guaranteed via the
	// results channel).
	hashByURL := make(map[string]string, n)
	for i := 0; i < n; i++ {
		hashByURL["file://"+upstreamDirs[i]] = upstreamHashes[i]
	}

	seenDownstreams := map[string]bool{}
	for o := range results {
		require.NoError(t, o.err, "concurrent integrate must not surface errors on distinct downstreams")
		require.NotNil(t, o.result)
		require.Len(t, o.result.Upstreams, 1)

		wantHash, ok := hashByURL[o.result.Upstreams[0].URL]
		require.True(t, ok, "result URL %q does not match any upstream we set up — indicates cross-goroutine data leak", o.result.Upstreams[0].URL)
		assert.Equal(t, wantHash, o.result.Upstreams[0].CommitHash,
			"IntegratedUpstream.CommitHash must reflect THIS goroutine's upstream HEAD — a mismatch would indicate cross-goroutine result corruption")

		// Every goroutine ran against a unique tempdir; ensure the SDK didn't
		// return a result attributed to someone else's downstream.
		assert.False(t, seenDownstreams[o.down], "duplicate downstream in results — goroutine cross-contamination")
		seenDownstreams[o.down] = true

		// Each downstream's state file should reflect its own upstream — a
		// concurrent bug that mixed state paths would produce garbage here.
		state := readStateJSON(t, o.down)
		require.Len(t, state.Upstreams, 1, "each downstream must have exactly one upstream in its state")
		assert.Equal(t, wantHash, state.Upstreams[0].CommitHash)
	}

	assert.Len(t, seenDownstreams, n,
		"expected %d distinct downstream outcomes, got %d", n, len(seenDownstreams))
}

// Error path: no upstreams in state and no override → error mentions integrate
func TestCheckDrift_noStateNoOverride_errors(t *testing.T) {
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitspork integrate")
}

// Error path: override URL not in state → error names the URL
func TestCheckDrift_overrideMissingState_errors(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")

	bogus := "file:///tmp/gitspork-never-integrated-sdk-test"
	_, err = gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
		Upstreams:          []gitspork.UpstreamSpec{{URL: bogus}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), bogus)
}

// CheckDrift fails fast with gitspork.ErrGitBinaryMissing when git is not on PATH.
func TestCheckDrift_gitBinaryMissing_errorsWithSentinel(t *testing.T) {
	downstreamDir := emptyDownstream(t)

	// Scrub PATH so exec.LookPath cannot find git.
	t.Setenv("PATH", "/nonexistent-path-for-gitspork-tests")

	_, err := gitspork.CheckDrift(&gitspork.CheckDriftOptions{
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, gitspork.ErrGitBinaryMissing),
		"CheckDrift should fail with ErrGitBinaryMissing when git is not on PATH")
}

// captureLogger implements gitspork.Logger and records calls.
type captureLogger struct {
	entries []string
}

func (c *captureLogger) Log(msg string, args ...any)   { c.entries = append(c.entries, "log: "+msg) }
func (c *captureLogger) Error(msg string, args ...any) { c.entries = append(c.entries, "err: "+msg) }

// TestIntegrate_cache_SDK_CacheTTL_honored: calling Integrate twice from the
// SDK with a tiny CacheTTL on the second call must trigger a refresh
// (asserted via a captureLogger).
func TestIntegrate_cache_SDK_CacheTTL_honored(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	captured := &captureLogger{}
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             captured,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	assert.True(t, hasLog(captured, "populating upstream cache"),
		"first Integrate must populate the cache")

	// Commit the downstream so the second Integrate can proceed with a clean tree.
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")

	captured2 := &captureLogger{}
	_, err = gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             captured2,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
		CacheTTL:           1 * time.Nanosecond,
	})
	require.NoError(t, err)
	assert.True(t, hasLog(captured2, "refreshing upstream cache"),
		"CacheTTL=1ns on the second Integrate must trigger a refresh log line")
	assert.False(t, hasLog(captured2, "upstream cache hit"),
		"tiny TTL must not be a cache hit")
}

// TestIntegrate_cache_SDK_NoCache_bypasses: NoCache=true bypasses the cache
// entirely — no cache log lines emitted and no cache dir populated.
func TestIntegrate_cache_SDK_NoCache_bypasses(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	captured := &captureLogger{}
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             captured,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
		NoCache:            true,
	})
	require.NoError(t, err)

	// None of the three cache log lines appear.
	assert.False(t, hasLog(captured, "populating upstream cache"))
	assert.False(t, hasLog(captured, "refreshing upstream cache"))
	assert.False(t, hasLog(captured, "upstream cache hit"))

	// Cache dir remains empty.
	entries, err := os.ReadDir(cacheDir)
	if err == nil {
		assert.Empty(t, entries, "NoCache=true must leave the cache dir untouched")
	}
}

// hasLog reports whether any captured entry contains substr.
func hasLog(c *captureLogger, substr string) bool {
	for _, e := range c.entries {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}
