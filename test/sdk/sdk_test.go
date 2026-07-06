//go:build sdk

package sdk_test

import (
	"errors"
	"testing"

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
