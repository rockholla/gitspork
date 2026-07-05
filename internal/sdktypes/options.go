package sdktypes

// IntegrateOptions configures a call to Integrate. Populate Upstreams (one
// or more entries) for multi-upstream integration.
type IntegrateOptions struct {
	Upstreams          []UpstreamSpec
	DownstreamRepoPath string
	ForceRePrompt      bool
	Logger             Logger
}

// IntegrateLocalOptions configures a call to IntegrateLocal. Populate
// UpstreamPaths (one or more entries) for multi-path integration.
type IntegrateLocalOptions struct {
	UpstreamPaths  []string
	DownstreamPath string
	ForceRePrompt  bool
	Logger         Logger
}

// CheckDriftOptions configures a call to CheckDrift. Leave Upstreams empty
// to use the recorded state; supply entries to override with different
// URLs/tokens for the same recorded commit hashes.
type CheckDriftOptions struct {
	Upstreams          []UpstreamSpec
	DownstreamRepoPath string
	Logger             Logger
}

// UpstreamSpec identifies a single upstream to integrate from.
type UpstreamSpec struct {
	URL     string
	Version string
	Subpath string
	Token   string
}
