package types

import "errors"

// ErrDriftDetected is returned by CheckDrift when drift is found in the downstream.
var ErrDriftDetected = errors.New("drift detected")
