package integrate

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/transport"
	githttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/rockholla/gitspork/v2/internal/gitbin"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// shellGitCloneOptions captures the shell-git-mappable subset of go-git's
// CloneOptions used by cloneUpstreamForIntegrate and populateCache. Only the
// fields actually exercised on the fast paths are represented — the mapping is
// intentionally narrow so behavioral parity with go-git stays reviewable.
type shellGitCloneOptions struct {
	SingleBranch  bool
	ReferenceName string // e.g. "refs/tags/v1.2.3" or "refs/heads/main"; empty = default
	Depth         int    // >0 for shallow
	Local         bool   // --local flag (requires srcURL to be a filesystem path or file:// URL)
	Mirror        bool   // --mirror flag (for cache populate — produces a bare mirror)
	// Token, when non-empty AND srcURL uses the https:// scheme, is embedded
	// into the URL as x-access-token:<token>@ so shell git authenticates
	// without needing a credential helper. Ignored for file:// and ssh sources
	// (ssh uses ssh-agent natively; file:// needs no auth).
	Token string
}

// shellGitFetchOptions is the fetch counterpart to shellGitCloneOptions. Only
// the auth shape is captured — the mirror-style refspec is fixed at
// "+refs/*:refs/*" so shellGitFetch can be called against any bare mirror
// without further configuration.
type shellGitFetchOptions struct {
	Token string
}

// rewriteHTTPSWithToken embeds an HTTPS token into a URL as an x-access-token
// basic-auth pair. Returns url unchanged for non-HTTPS URLs and for an empty
// token — so callers can pass the result through unconditionally.
//
// Extracted so the URL rewrite can be unit-tested without spinning up an HTTPS
// remote. The token appears in the shell git process's argv briefly; see the
// package comment in shell_git_clone.go for the threat-model rationale.
func rewriteHTTPSWithToken(url, token string) string {
	if token == "" || !strings.HasPrefix(url, "https://") {
		return url
	}
	return strings.Replace(url, "https://", "https://x-access-token:"+token+"@", 1)
}

// tokenFromAuth extracts the HTTP basic-auth password ("token" in the
// git+HTTPS convention) from a transport.AuthMethod. Returns empty for a
// nil auth or any non-BasicAuth (e.g. SSH agent auth, which needs no URL
// rewriting because shell git talks to ssh-agent natively).
func tokenFromAuth(auth transport.AuthMethod) string {
	if ba, ok := auth.(*githttp.BasicAuth); ok {
		return ba.Password
	}
	return ""
}

// useShellGitFastPath reports whether the shell git fast path can be used
// for cache/clone/fetch operations. All four call sites (populateCache,
// refreshCache, and the two branches of cloneUpstreamForIntegrate) gate on
// this same expression so the behavior stays consistent — a machine either
// runs shell git everywhere or go-git everywhere.
func useShellGitFastPath() bool {
	return gitbin.Require() == nil
}

// shellGitClone runs `git clone` from srcURL to dest, mapping the subset of
// CloneOptions that shell git supports. Used as the fast path in
// cloneUpstreamForIntegrate (working clones from cache) and populateCache
// (mirror clones from the network). Typically 5-10x faster than go-git's
// PlainClone for large repos because --local hardlinks the object database
// instead of copying, and shell git's packfile handling has been tuned for a
// couple of decades.
//
// progress is used as the command's stderr writer (shell git natively emits
// clone progress there). logger is currently unused but kept in the signature
// so callers can wire structured warnings later without a churn-y refactor.
func shellGitClone(ctx context.Context, srcURL, dest string, opts shellGitCloneOptions, progress io.Writer, logger sdktypes.Logger) error {
	_ = logger // reserved for future structured warnings
	if srcURL == "" {
		return fmt.Errorf("shellGitClone: empty srcURL")
	}
	// -c safe.directory=* disables git's owner-check on the working directory
	// and any repo it touches. Necessary when running as root inside Docker
	// with mounted volumes owned by another UID — git 2.35.2+ otherwise refuses
	// with "detected dubious ownership". Matches the pattern used by the other
	// shell-git callers in this repo (internal/cli/rm.go, internal/cli/mv.go,
	// internal/drift/check_drift.go).
	args := []string{"-c", "safe.directory=*", "clone"}
	if opts.Local {
		args = append(args, "--local")
	}
	if opts.Mirror {
		args = append(args, "--mirror")
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
	// is more portable across shell git versions. For https:// URLs, embed
	// the token (if any) as basic-auth. ssh URLs pass through untouched —
	// shell git uses ssh-agent natively.
	src := strings.TrimPrefix(srcURL, "file://")
	src = rewriteHTTPSWithToken(src, opts.Token)
	args = append(args, src, dest)

	cmd := exec.CommandContext(ctx, "git", args...)
	// shell git emits clone progress on stderr natively; wire it to the
	// caller-supplied progress writer when non-nil. Leaving Stderr unset when
	// progress is nil matches the "silent by default" semantics — shell git
	// writes to the calling process's stderr, which is fine for CLI/CI use.
	if progress != nil {
		cmd.Stderr = progress
	}
	if _, err := cmd.Output(); err != nil {
		// Redact the token from any error surface (srcURL, not src) so it
		// doesn't leak into logs.
		return fmt.Errorf("git clone %s -> %s: %w", srcURL, dest, err)
	}
	return nil
}

// shellGitLsRemote runs `git ls-remote <url>` and returns a map keyed by
// full refname (e.g. "refs/tags/v1.2.3", "refs/heads/main") to commit SHA.
// Used by resolveUpstreamVersionRef's fast path.
//
// Sidesteps go-git's Go-TLS stack, which fails in some macOS environments
// (corporate MITM proxies, keychain quirks) with a
// `SecPolicyCreateSSL error: 0` from crypto/x509's darwin path — shell
// git's TLS goes through the system trust store via libcurl and works
// through those same conditions.
//
// Auth: token is embedded as x-access-token:<token>@ for https:// URLs
// (same rewrite shellGitClone/shellGitFetch use). SSH URLs pass through
// and shell git talks to ssh-agent natively.
func shellGitLsRemote(ctx context.Context, url, token string) (map[string]string, error) {
	if url == "" {
		return nil, fmt.Errorf("shellGitLsRemote: empty url")
	}
	src := rewriteHTTPSWithToken(url, token)
	// -c safe.directory=* — see the note on shellGitClone.
	cmd := exec.CommandContext(ctx, "git", "-c", "safe.directory=*", "ls-remote", src)
	out, err := cmd.Output()
	if err != nil {
		// Redact the token from the error surface — use the caller-supplied url.
		return nil, fmt.Errorf("git ls-remote %s: %w", url, err)
	}
	refs := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		// Each ls-remote line is `<sha>\t<refname>`. Split on the first tab.
		i := strings.IndexByte(line, '\t')
		if i < 0 {
			continue
		}
		refs[line[i+1:]] = line[:i]
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan git ls-remote output: %w", err)
	}
	return refs, nil
}

// shellGitFetch runs a mirror-style `git fetch --prune` against a bare mirror
// repo at dir. remoteURL is the network URL of the origin — when opts.Token
// is non-empty and the URL uses https://, the token is embedded as
// x-access-token:<token>@ basic auth. This bypasses the configured origin
// remote entirely, keeping URL rewriting stateless (populateCache stores no
// token in the mirror's git config).
//
// Used by refreshCache as the shell git fast path counterpart to
// populateCache's `git clone --mirror`.
func shellGitFetch(ctx context.Context, dir, remoteURL string, opts shellGitFetchOptions, progress io.Writer) error {
	if remoteURL == "" {
		return fmt.Errorf("shellGitFetch: empty remoteURL")
	}
	src := rewriteHTTPSWithToken(remoteURL, opts.Token)
	// git fetch takes a bare filesystem path directly; file:// works too but
	// stripping it matches shellGitClone's convention and avoids version-
	// specific quirks in old shell gits.
	src = strings.TrimPrefix(src, "file://")

	// -c safe.directory=* — see the note on shellGitClone. Required when running
	// as root inside Docker against a cache mounted from a non-root-owned host
	// path.
	args := []string{"-c", "safe.directory=*", "-C", dir, "fetch", "--prune", src, "+refs/*:refs/*"}
	cmd := exec.CommandContext(ctx, "git", args...)
	if progress != nil {
		cmd.Stderr = progress
	}
	if _, err := cmd.Output(); err != nil {
		// Redact the token from the error surface — use remoteURL, not src.
		return fmt.Errorf("git fetch %s in %s: %w", remoteURL, dir, err)
	}
	return nil
}
