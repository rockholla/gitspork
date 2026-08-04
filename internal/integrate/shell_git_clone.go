package integrate

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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

// shellGitAuthEnvVar is the environment variable name the credential helper
// installed by prepareShellGitAuth reads at auth time. Named distinctly from
// GITHUB_TOKEN so a caller-supplied token doesn't clobber (or leak from) any
// ambient GITHUB_TOKEN the child process may inherit.
const shellGitAuthEnvVar = "GITSPORK_HTTPS_TOKEN"

// prepareShellGitAuth prepares HTTPS + token auth for a shell git invocation
// WITHOUT putting the token in argv (previously we embedded it as
// `x-access-token:<token>@` in the URL, which exposed it to any `ps` listing
// and required URL-escaping for tokens containing special characters).
//
// Returns the git-config `-c` arg pair to insert BEFORE the subcommand (which
// installs a one-shot shell credential helper reading the token from env)
// and the environment to use for the child process (a copy of the current
// env with the token var set). SSH URLs and HTTPS URLs without a token
// return nil/nil — SSH goes through ssh-agent natively, and HTTPS without a
// token trusts the caller's ambient git config (~/.netrc, credential helper
// installed, etc.). file:// URLs need no auth.
//
// The credential helper is the shell `!f() {...}; f` form: git spawns it as
// a subshell, our snippet reads $GITSPORK_HTTPS_TOKEN from the child env
// (inherited from cmd.Env, which we set here), and prints the username /
// password lines git's credential-helper protocol expects. Git does not
// persist credentials from these ephemeral helpers, so nothing gets cached
// beyond the single subprocess.
func prepareShellGitAuth(url, token string) (credArgs []string, env []string) {
	if token == "" || !strings.HasPrefix(url, "https://") {
		return nil, nil
	}
	// credential.helper='' clears any inherited helper so ours is the
	// only one git considers.
	args := []string{
		"-c", "credential.helper=",
		"-c", `credential.helper=!f() { echo "username=x-access-token"; echo "password=$` + shellGitAuthEnvVar + `"; }; f`,
	}
	// Filter any pre-existing GITSPORK_HTTPS_TOKEN out of the parent env
	// so ours is definitive (POSIX doesn't define duplicate-key precedence
	// in getenv/environ, so being explicit avoids surprises).
	parent := os.Environ()
	childEnv := make([]string, 0, len(parent)+1)
	for _, e := range parent {
		if !strings.HasPrefix(e, shellGitAuthEnvVar+"=") {
			childEnv = append(childEnv, e)
		}
	}
	childEnv = append(childEnv, shellGitAuthEnvVar+"="+token)
	return args, childEnv
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
	args := []string{"-c", "safe.directory=*"}
	// HTTPS + token: install a scoped credential helper reading the token
	// from env at auth time. Keeps the token out of argv (see
	// prepareShellGitAuth). SSH URLs and file:// URLs skip auth wiring.
	credArgs, childEnv := prepareShellGitAuth(srcURL, opts.Token)
	args = append(args, credArgs...)
	args = append(args, "clone")
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
	// is more portable across shell git versions. HTTPS and SSH URLs pass
	// through unmodified — auth is handled via the credential helper set
	// above (HTTPS+token) or ssh-agent natively (SSH).
	src := strings.TrimPrefix(srcURL, "file://")
	args = append(args, src, dest)

	cmd := exec.CommandContext(ctx, "git", args...)
	if childEnv != nil {
		cmd.Env = childEnv
	}
	// shell git emits clone progress on stderr natively; wire it to the
	// caller-supplied progress writer when non-nil. Leaving Stderr unset when
	// progress is nil matches the "silent by default" semantics — shell git
	// writes to the calling process's stderr, which is fine for CLI/CI use.
	if progress != nil {
		cmd.Stderr = progress
	}
	if _, err := cmd.Output(); err != nil {
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
// Auth: HTTPS + token uses the shell credential helper installed by
// prepareShellGitAuth (keeps token out of argv). SSH URLs pass through
// untouched — shell git talks to ssh-agent natively.
func shellGitLsRemote(ctx context.Context, url, token string) (map[string]string, error) {
	if url == "" {
		return nil, fmt.Errorf("shellGitLsRemote: empty url")
	}
	// -c safe.directory=* — see the note on shellGitClone.
	args := []string{"-c", "safe.directory=*"}
	credArgs, childEnv := prepareShellGitAuth(url, token)
	args = append(args, credArgs...)
	args = append(args, "ls-remote", url)
	cmd := exec.CommandContext(ctx, "git", args...)
	if childEnv != nil {
		cmd.Env = childEnv
	}
	out, err := cmd.Output()
	if err != nil {
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
// repo at dir. remoteURL is the network URL of the origin — HTTPS + token
// uses the shell credential helper installed by prepareShellGitAuth (keeps
// token out of argv). This bypasses the configured origin remote entirely,
// keeping URL rewriting stateless (populateCache stores no token in the
// mirror's git config).
//
// Used by refreshCache as the shell git fast path counterpart to
// populateCache's `git clone --mirror`.
func shellGitFetch(ctx context.Context, dir, remoteURL string, opts shellGitFetchOptions, progress io.Writer) error {
	if remoteURL == "" {
		return fmt.Errorf("shellGitFetch: empty remoteURL")
	}
	// git fetch takes a bare filesystem path directly; file:// works too but
	// stripping it matches shellGitClone's convention and avoids version-
	// specific quirks in old shell gits.
	src := strings.TrimPrefix(remoteURL, "file://")

	// -c safe.directory=* — see the note on shellGitClone. Required when running
	// as root inside Docker against a cache mounted from a non-root-owned host
	// path.
	args := []string{"-c", "safe.directory=*"}
	credArgs, childEnv := prepareShellGitAuth(src, opts.Token)
	args = append(args, credArgs...)
	args = append(args, "-C", dir, "fetch", "--prune", src, "+refs/*:refs/*")
	cmd := exec.CommandContext(ctx, "git", args...)
	if childEnv != nil {
		cmd.Env = childEnv
	}
	if progress != nil {
		cmd.Stderr = progress
	}
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("git fetch %s in %s: %w", remoteURL, dir, err)
	}
	return nil
}
