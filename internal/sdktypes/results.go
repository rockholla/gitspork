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
}

// DriftedFile is a single entry in a DriftReport.
type DriftedFile struct {
	Path          string
	AttributedURL string // upstream URL responsible for this file; empty means unattributed
	Diff          string // unified-diff text for this file; a `Binary files ... differ` marker line when the file is binary
}
