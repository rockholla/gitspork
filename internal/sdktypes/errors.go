package sdktypes

import "errors"

// ErrDriftDetected is returned by CheckDrift when drift is found in the downstream.
var ErrDriftDetected = errors.New("drift detected")

// ErrGitBinaryMissing is returned when gitspork needs to shell out to the git
// binary but it is not present on PATH. SDK consumers can check via
// errors.Is(err, gitspork.ErrGitBinaryMissing) to detect this condition.
var ErrGitBinaryMissing = errors.New("git binary not found on PATH")

// ErrSelfIntegration is returned when integrate / integrate-local / check-drift
// detects that the upstream identifies the same repo as the downstream. SDK
// consumers can check via errors.Is(err, gitspork.ErrSelfIntegration).
var ErrSelfIntegration = errors.New("upstream and downstream identify the same repo")
