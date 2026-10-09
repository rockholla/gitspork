package sdktypes

// IntegrateResult is the structural return value of Integrate and IntegrateLocal.
// It records what was successfully integrated (in order); on partial failure
// the successful upstreams so far are still present in this result alongside
// the returned error.
//
// The returned *IntegrateResult is always non-nil — callers do not need to
// nil-check before inspecting Upstreams.
type IntegrateResult struct {
	Upstreams []IntegratedUpstream
}

// IntegratedUpstream identifies a single successfully integrated upstream.
// For Integrate, URL is the remote repo URL (SSH or HTTPS, whichever the
// caller supplied). For IntegrateLocal, URL is the local filesystem path with
// no scheme, and CommitHash is empty (local paths have no commit-hash concept).
type IntegratedUpstream struct {
	URL        string
	Subpath    string
	CommitHash string
	// Config is the ownership layout the upstream's .gitspork.yml declared at
	// CommitHash (or, for IntegrateLocal, at the local path). It is what drove
	// this integrate, so a caller that needs to know which downstream paths the
	// upstream owns can read it here instead of fetching the upstream again.
	Config *UpstreamConfig
}

// UpstreamConfig is the part of an upstream's .gitspork.yml that says who owns
// which downstream paths. Every pattern is a gobwas glob (the syntax .gitspork.yml
// uses), matched against the path relative to the downstream repo root.
//
// For an entry that renames a file as it syncs ({from, to}), the pattern listed
// here is the destination (to), because that is where the path lands in the
// downstream. UpstreamOnly patterns are the exception: they match paths in the
// upstream, which never reach the downstream.
type UpstreamConfig struct {
	// UpstreamOwned paths are overwritten from the upstream on every integrate.
	UpstreamOwned []string
	// DownstreamOwned paths are seeded from the upstream when missing and never
	// changed afterwards.
	DownstreamOwned []string
	// UpstreamOnly paths in the upstream are never synced to the downstream.
	UpstreamOnly []string
	// SharedOwnership paths are owned by both sides in a managed way.
	SharedOwnership SharedOwnership
	// Templated lists the downstream paths rendered from upstream templates.
	Templated []TemplatedDestination
}

// SharedOwnership mirrors the shared_ownership section of .gitspork.yml.
type SharedOwnership struct {
	// Merged files hold an upstream-owned block among downstream content.
	Merged []string
	// StructuredPreferUpstream files are JSON/YAML merged with the upstream's values winning.
	StructuredPreferUpstream []string
	// StructuredPreferDownstream files are JSON/YAML merged with the downstream's values winning.
	StructuredPreferDownstream []string
}

// TemplatedDestination is where a templated entry renders in the downstream.
type TemplatedDestination struct {
	Destination string
	// DownstreamOwned is true when the file is seeded once and never overwritten.
	DownstreamOwned bool
}

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
type DriftReport struct {
	HasDrift bool
	Files    []DriftedFile
	// Upstreams are the upstreams that were re-integrated for the check, each at
	// the commit the downstream last integrated, with the Config it declared.
	// Empty when the check failed before any upstream was integrated.
	Upstreams []IntegratedUpstream
}

// DriftedFile is a single entry in a DriftReport.
type DriftedFile struct {
	Path          string
	AttributedURL string // upstream URL responsible for this file; empty means unattributed
	Diff          string // unified-diff text for this file; a `Binary files ... differ` marker line when the file is binary
	ColorizedDiff string // same content as Diff with ANSI color codes applied by line prefix (headers bold, hunks cyan, additions green, removals red); always populated regardless of the process's TTY state so SDK consumers can render into any sink
}
