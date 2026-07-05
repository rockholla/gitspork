package gitspork

import (
	"github.com/rockholla/gitspork/v2/internal/drift"
	"github.com/rockholla/gitspork/v2/internal/integrate"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// The type aliases below re-export the SDK types defined in internal/sdktypes
// so that consumers see them as first-class members of package gitspork. The
// alias pattern (rather than moving the definitions to the root package)
// avoids an import cycle: internal/integrate and internal/drift need these
// types in their signatures, and this root package needs to call into those
// subpackages to implement the coordinator entry-points below.

// IntegrateOptions configures a call to Integrate. Populate Upstreams (one
// or more entries) for multi-upstream integration.
type IntegrateOptions = sdktypes.IntegrateOptions

// IntegrateLocalOptions configures a call to IntegrateLocal. Populate
// UpstreamPaths (one or more entries) for multi-path integration.
type IntegrateLocalOptions = sdktypes.IntegrateLocalOptions

// CheckDriftOptions configures a call to CheckDrift. Leave Upstreams empty
// to use the recorded state; supply entries to override with different
// URLs/tokens for the same recorded commit hashes.
type CheckDriftOptions = sdktypes.CheckDriftOptions

// UpstreamSpec identifies a single upstream to integrate from.
type UpstreamSpec = sdktypes.UpstreamSpec

// IntegrateResult is the structural return value of Integrate and IntegrateLocal.
// It records what was successfully integrated (in order); on partial failure
// the successful upstreams so far are still present in this result alongside
// the returned error.
//
// The returned *IntegrateResult is always non-nil — callers do not need to
// nil-check before inspecting Upstreams.
type IntegrateResult = sdktypes.IntegrateResult

// IntegratedUpstream identifies a single successfully integrated upstream.
// For Integrate, URL is the remote repo URL (SSH or HTTPS, whichever the
// caller supplied). For IntegrateLocal, URL is the local filesystem path with
// no scheme, and CommitHash is empty (local paths have no commit-hash concept).
type IntegratedUpstream = sdktypes.IntegratedUpstream

// DriftReport is the structural return value of CheckDrift. HasDrift is false
// when the downstream matches the recorded integration state; true when
// differences were found. Files enumerates the drifted entries with per-file
// attribution to whichever upstream last wrote each path.
//
// When two upstreams write the same file, AttributedURL on the corresponding
// DriftedFile records the last-writing upstream — matching the last-writer-wins
// semantics of multi-upstream integrate.
//
// The returned *DriftReport is always non-nil — callers do not need to
// nil-check before inspecting HasDrift or Files.
type DriftReport = sdktypes.DriftReport

// DriftedFile is a single entry in a DriftReport.
type DriftedFile = sdktypes.DriftedFile

// DownstreamState is the on-disk state stored at
// .gitspork/downstream-state.json in the downstream repo. It records each
// integrated upstream so subsequent runs (integrate, check-drift) can locate
// the previous commit hash and detect drift.
type DownstreamState = sdktypes.DownstreamState

// UpstreamState records the last integration for a single upstream.
type UpstreamState = sdktypes.UpstreamState

// Logger is the small interface gitspork uses for narration/progress and
// error messages. It is deliberately narrow so SDK consumers can wire their
// own logging (slog, zap, log/logr) with minimal glue. A nil Logger means
// silent — implementations that accept Logger as a field MUST check for nil
// before calling either method.
type Logger = sdktypes.Logger

// ErrDriftDetected is returned by CheckDrift when drift is present in the
// downstream relative to its recorded upstream state.
var ErrDriftDetected = sdktypes.ErrDriftDetected

// NoopLogger returns a Logger implementation that discards all messages.
// SDK consumers can pass this (or nil, which is treated equivalently by the
// coordinator entry-points) to silence gitspork output.
func NoopLogger() Logger { return sdktypes.NoopLogger() }

// Integrate integrates one or more upstream repos into the downstream at
// opts.DownstreamRepoPath. See IntegrateOptions for configuration. On partial
// failure the returned *IntegrateResult still contains the upstreams that
// were successfully integrated before the error.
func Integrate(opts *IntegrateOptions) (*IntegrateResult, error) {
	return integrate.Integrate(opts)
}

// IntegrateLocal integrates one or more local upstream paths into the
// downstream at opts.DownstreamPath. Local integrations do not write to
// downstream state.
func IntegrateLocal(opts *IntegrateLocalOptions) (*IntegrateResult, error) {
	return integrate.IntegrateLocal(opts)
}

// CheckDrift re-runs each recorded upstream's integration at its pinned
// commit hash in an isolated copy of the downstream and reports any files
// that differ from the current downstream HEAD. Returns a populated
// *DriftReport alongside ErrDriftDetected when drift is found.
func CheckDrift(opts *CheckDriftOptions) (*DriftReport, error) {
	return drift.CheckDrift(opts)
}
