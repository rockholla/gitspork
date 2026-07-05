package types

// GitSporkDownstreamState is the on-disk state stored at
// .gitspork/downstream-state.json in the downstream repo.
type GitSporkDownstreamState struct {
	MigrationsComplete []string                `json:"migrations_complete"`
	Upstreams          []GitSporkUpstreamState `json:"upstreams,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamRepoURL     string `json:"last_upstream_repo_url,omitempty"`
	LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
	LastUpstreamCommitHash  string `json:"last_upstream_commit_hash,omitempty"`
}

// GitSporkUpstreamState records the last integration for a single upstream.
type GitSporkUpstreamState struct {
	URL        string `json:"url"`
	Subpath    string `json:"subpath,omitempty"`
	CommitHash string `json:"commit_hash"`
}
