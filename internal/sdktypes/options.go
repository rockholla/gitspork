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
//
// Version may be one of:
//   - A branch name (e.g. "main", "feature/x") — resolved as refs/heads/<v>.
//   - A tag name (e.g. "v1.2.3") — resolved as refs/tags/<v>. When both a
//     branch and a tag share a name (rare), the tag wins, matching
//     `git checkout`'s precedence for ambiguous refs.
//   - An explicit "tags/<name>" form — always treated as a tag, useful
//     when the caller wants to bypass tag/branch disambiguation.
//   - A commit hash — 7 to 40 hex characters, short or full. The upstream
//     is cloned with full history and the hash is resolved via git's
//     revision parser, so `abc1234` and the full 40-char SHA both work.
//
// An empty Version selects the remote's default branch.
type UpstreamSpec struct {
	URL     string
	Version string
	Subpath string
	Token   string
}
