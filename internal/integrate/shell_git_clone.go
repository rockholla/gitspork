package integrate

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// shellGitCloneOptions captures the shell-git-mappable subset of go-git's
// CloneOptions used by cloneUpstreamForIntegrate. Only the fields actually
// exercised on the working-clone path are represented — the mapping is
// intentionally narrow so behavioral parity with go-git stays reviewable.
type shellGitCloneOptions struct {
	SingleBranch  bool
	ReferenceName string // e.g. "refs/tags/v1.2.3" or "refs/heads/main"; empty = default
	Depth         int    // >0 for shallow
	Local         bool   // --local flag (requires srcURL to be a filesystem path or file:// URL)
}

// shellGitClone runs `git clone` from srcURL to dest, mapping the subset of
// CloneOptions that shell git supports. Used as the fast path in
// cloneUpstreamForIntegrate when the git binary is available (typically
// 5-10x faster than go-git's PlainClone for large repos because --local
// hardlinks the object database instead of copying).
//
// progress is used as the command's stderr writer (shell git natively emits
// clone progress there). logger is currently unused but kept in the signature
// so callers can wire structured warnings later without a churn-y refactor.
func shellGitClone(ctx context.Context, srcURL, dest string, opts shellGitCloneOptions, progress io.Writer, logger sdktypes.Logger) error {
	_ = logger // reserved for future structured warnings
	args := []string{"clone"}
	if opts.Local {
		args = append(args, "--local")
	}
	if opts.SingleBranch {
		args = append(args, "--single-branch")
	}
	if opts.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(opts.Depth))
	}
	if opts.ReferenceName != "" {
		// shell git wants the short name for --branch, e.g. "main" or "v1.2.3"
		// (not "refs/heads/main"). Strip refs/heads/ or refs/tags/ prefix.
		branch := strings.TrimPrefix(opts.ReferenceName, "refs/heads/")
		branch = strings.TrimPrefix(branch, "refs/tags/")
		args = append(args, "--branch", branch)
	}
	// Strip "file://" prefix — shell git accepts either form but a bare path
	// is more portable across shell git versions.
	src := strings.TrimPrefix(srcURL, "file://")
	args = append(args, src, dest)

	cmd := exec.CommandContext(ctx, "git", args...)
	// shell git emits clone progress on stderr natively; wire it to the
	// caller-supplied progress writer. Stdout is left unset — git clone
	// doesn't write meaningful info there.
	cmd.Stderr = progress
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("git clone %s -> %s: %w", src, dest, err)
	}
	return nil
}
