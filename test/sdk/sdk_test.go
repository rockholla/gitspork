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
