package integrate

import (
	"fmt"
	"io"
	"time"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// DriftCheckRequest is the internal request shape used by drift-check
// re-integration. External SDK consumers should use Integrate; DriftCheckRequest
// is a package-integrate contract intended only for internal/drift.
type DriftCheckRequest struct {
	Logger             sdktypes.Logger
	DownstreamRepoPath string
	UpstreamURL        string
	UpstreamSubpath    string
	UpstreamToken      string
	UpstreamCommit     string
	CacheTTL           time.Duration
	NoCache            bool
	// Progress, when non-nil, is threaded into go-git as the Progress writer
	// for upstream mirror cache clone/fetch operations during drift-check
	// re-integration.
	Progress io.Writer
}

// IntegrateForDriftCheck runs a single-upstream integrate pinned to a specific
// commit hash and skips the state write. It's used by internal/drift to
// reconstruct the downstream at each recorded upstream's last-integrated
// commit and then diff against HEAD.
func IntegrateForDriftCheck(req *DriftCheckRequest) error {
	if req.Logger == nil {
		req.Logger = sdktypes.NoopLogger()
	}
	upstream := sdktypes.UpstreamSpec{
		URL:     req.UpstreamURL,
		Subpath: req.UpstreamSubpath,
		Token:   req.UpstreamToken,
	}
	internalReq := &internalRequest{
		Logger:             req.Logger,
		DownstreamRepoPath: req.DownstreamRepoPath,
		forDriftCheck:      true,
		upstreamCommit:     req.UpstreamCommit,
		cacheTTL:           req.CacheTTL,
		noCache:            req.NoCache,
		progress:           req.Progress,
	}
	if _, err := integrateOneInternal(internalReq, upstream); err != nil {
		return fmt.Errorf("drift-check re-integration failed: %w", err)
	}
	return nil
}
