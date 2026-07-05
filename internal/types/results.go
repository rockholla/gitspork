package types

// IntegrateResult is the structural return value of Integrate and IntegrateLocal.
// It records what was successfully integrated (in order); on partial failure
// the successful upstreams so far are still present in this result alongside
// the returned error.
type IntegrateResult struct {
	Upstreams []IntegratedUpstream
}

// IntegratedUpstream identifies a single successfully integrated upstream.
type IntegratedUpstream struct {
	URL        string
	Subpath    string
	CommitHash string
}

// DriftReport is the structural return value of CheckDrift. HasDrift is false
// when the downstream matches the recorded integration state; true when
// differences were found. Files enumerates the drifted entries with per-file
// attribution to whichever upstream last wrote each path.
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
