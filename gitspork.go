package gitspork

import (
	"github.com/rockholla/gitspork/v2/internal/drift"
	"github.com/rockholla/gitspork/v2/internal/integrate"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// Type aliases re-export the SDK types defined in internal/sdktypes so that
// consumers see them as first-class members of package gitspork. The alias
// pattern (rather than moving the definitions to the root package) is
// required to avoid an import cycle: internal/integrate and internal/drift
// need these types in their signatures, and this root package needs to call
// into those subpackages to implement the coordinator entry-points below.
type (
	IntegrateOptions      = sdktypes.IntegrateOptions
	IntegrateLocalOptions = sdktypes.IntegrateLocalOptions
	CheckDriftOptions     = sdktypes.CheckDriftOptions
	UpstreamSpec          = sdktypes.UpstreamSpec
	IntegrateResult       = sdktypes.IntegrateResult
	IntegratedUpstream    = sdktypes.IntegratedUpstream
	DriftReport           = sdktypes.DriftReport
	DriftedFile           = sdktypes.DriftedFile
	DownstreamState       = sdktypes.DownstreamState
	UpstreamState         = sdktypes.UpstreamState
	Logger                = sdktypes.Logger
)

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
