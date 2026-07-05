package sdktypes

// DownstreamState is the on-disk state stored at
// .gitspork/downstream-state.json in the downstream repo. It records each
// integrated upstream so subsequent runs (integrate, check-drift) can locate
// the previous commit hash and detect drift.
type DownstreamState struct {
	MigrationsComplete []string        `json:"migrations_complete"`
	Upstreams          []UpstreamState `json:"upstreams,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamRepoURL string `json:"last_upstream_repo_url,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamCommitHash string `json:"last_upstream_commit_hash,omitempty"`
}

// UpstreamState records the last integration for a single upstream.
type UpstreamState struct {
	URL        string `json:"url"`
	Subpath    string `json:"subpath,omitempty"`
	CommitHash string `json:"commit_hash"`
}
