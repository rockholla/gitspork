package types

// IntegrateOptions configures a call to Integrate. Populate Upstreams (one
// or more entries) for multi-upstream integration.
type IntegrateOptions struct {
	Upstreams          []UpstreamSpec
	DownstreamRepoPath string
	ForceRePrompt      bool
	Logger             Logger
	// Task 2 removes ForDriftCheck / PrevUpstreamCommitHash / UpstreamRepo* from
	// this struct. Until then, they carry legacy-flag data from the CLI and
	// drift-check re-integration wiring.
	ForDriftCheck          bool
	PrevUpstreamCommitHash string
	// Deprecated: set Upstreams instead. The CLI accepts --upstream-repo-url
	// for backward compatibility and translates internally.
	UpstreamRepoURL string
	// Deprecated: set Upstreams instead.
	UpstreamRepoVersion string
	// Deprecated: set Upstreams instead.
	UpstreamRepoSubpath string
	// Deprecated: set Upstreams instead.
	UpstreamRepoToken string
	// Deprecated: internal drift-check wiring only.
	UpstreamRepoCommit string
}

// IntegrateLocalOptions configures a call to IntegrateLocal. Populate
// UpstreamPaths (one or more entries) for multi-path integration.
type IntegrateLocalOptions struct {
	UpstreamPaths  []string
	DownstreamPath string
	ForceRePrompt  bool
	Logger         Logger
	// Deprecated: set UpstreamPaths instead. The CLI accepts a single
	// --upstream-path for backward compatibility and translates internally.
	UpstreamPath string
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
