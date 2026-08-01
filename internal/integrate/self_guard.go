package integrate

import (
	"fmt"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v6"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// EnsureNotSelfIntegration errors when the upstream identity matches the
// downstream repo. Called at the top of every integration path (both SDK and
// CLI consumers go through this).
//
// upstreamURL: the upstream repo URL (empty for pure local-path integrations).
// upstreamLocalPath: absolute or relative local path to the upstream when
// invoked via IntegrateLocal (empty otherwise).
// downstreamRepoPath: the downstream repo path (already absolutized by callers).
//
// Returns nil when no self-integration is detected. Any returned error wraps
// sdktypes.ErrSelfIntegration.
func EnsureNotSelfIntegration(downstreamRepoPath, upstreamURL, upstreamLocalPath string) error {
	if err := selfGuardPathCheck(downstreamRepoPath, upstreamLocalPath); err != nil {
		return err
	}
	if err := selfGuardURLCheck(downstreamRepoPath, upstreamURL); err != nil {
		return err
	}
	return nil
}

func selfGuardPathCheck(downstreamRepoPath, upstreamLocalPath string) error {
	if upstreamLocalPath == "" {
		return nil
	}

	downAbs, err := filepath.Abs(downstreamRepoPath)
	if err != nil {
		return fmt.Errorf("resolving downstream path: %w", err)
	}
	upAbs, err := filepath.Abs(upstreamLocalPath)
	if err != nil {
		return fmt.Errorf("resolving upstream local path: %w", err)
	}

	downResolved := resolveSymlinksBestEffort(downAbs)
	upResolved := resolveSymlinksBestEffort(upAbs)

	if downResolved == upResolved {
		return newSelfIntegrationPathError(upResolved, downResolved)
	}
	if pathIsInside(downResolved, upResolved) || pathIsInside(upResolved, downResolved) {
		return newSelfIntegrationPathError(upResolved, downResolved)
	}
	return nil
}

// resolveSymlinksBestEffort applies filepath.EvalSymlinks when the path exists;
// falls back to the plain absolute path when it does not. This preserves the
// caller's intent for not-yet-created directories (e.g. a downstream we're
// about to write into) without producing false negatives.
func resolveSymlinksBestEffort(abs string) string {
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// pathIsInside reports whether child sits at or inside parent (but is not
// equal to parent). Uses filepath.Rel to handle OS separators uniformly.
func pathIsInside(parent, child string) bool {
	if parent == child {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func selfGuardURLCheck(downstreamRepoPath, upstreamURL string) error {
	if upstreamURL == "" {
		return nil
	}

	repo, err := gogit.PlainOpen(downstreamRepoPath)
	if err != nil {
		if err == gogit.ErrRepositoryNotExists {
			return nil // no git repo, nothing to compare
		}
		return fmt.Errorf("opening downstream repo for self-integration check: %w", err)
	}

	origin, err := repo.Remote("origin")
	if err != nil {
		if err == gogit.ErrRemoteNotFound {
			return nil // no origin, nothing to compare
		}
		return fmt.Errorf("reading origin remote: %w", err)
	}

	targetKey := NormalizeUpstreamURL(upstreamURL, "")
	for _, remoteURL := range origin.Config().URLs {
		if NormalizeUpstreamURL(remoteURL, "") == targetKey {
			return newSelfIntegrationURLError(upstreamURL, remoteURL)
		}
	}
	return nil
}

func newSelfIntegrationURLError(upstreamURL, matchedOriginURL string) error {
	return fmt.Errorf(
		"self-integration blocked: upstream URL matches the downstream's origin remote (upstream=%s, origin=%s) — cannot integrate a repo against itself: %w",
		upstreamURL, matchedOriginURL, sdktypes.ErrSelfIntegration,
	)
}

func newSelfIntegrationPathError(upstream, downstream string) error {
	return fmt.Errorf(
		"self-integration blocked: upstream local path resolves inside the downstream repo (upstream=%s, downstream=%s) — cannot integrate a repo against itself: %w",
		upstream, downstream, sdktypes.ErrSelfIntegration,
	)
}
