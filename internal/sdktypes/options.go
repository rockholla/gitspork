package sdktypes

import (
	"io"
	"io/fs"
	"time"
)

// IntegrateOptions configures a call to Integrate. Populate Upstreams (one
// or more entries) for multi-upstream integration.
type IntegrateOptions struct {
	Upstreams          []UpstreamSpec
	DownstreamRepoPath string
	ForceRePrompt      bool
	Logger             Logger

	// CacheTTL controls the machine-scoped upstream mirror cache freshness
	// threshold. A cache entry younger than CacheTTL is used as-is; older
	// triggers a `git fetch` refresh. Zero-value means "use GITSPORK_CACHE_TTL
	// env var if set, else the compiled default (2h)". Ignored for
	// IntegrateLocalOptions because IntegrateLocal doesn't clone remotes.
	CacheTTL time.Duration

	// NoCache, when true, bypasses the machine-scoped upstream mirror cache
	// entirely — a direct network clone runs on every invocation. Overrides
	// CacheTTL. Also settable via GITSPORK_NO_CACHE env var. Ignored for
	// IntegrateLocalOptions because IntegrateLocal doesn't clone remotes.
	NoCache bool

	// Progress, if non-nil, receives git progress output during upstream cache
	// clone/fetch operations (populating the machine-scoped mirror on first use,
	// refreshing on TTL expiry). Consumers typically pass os.Stderr for
	// terminal-style progress; leave nil to suppress. Ignored when NoCache is
	// true (direct clone path).
	Progress io.Writer
}

// IntegrateLocalOptions configures a call to IntegrateLocal. Populate
// UpstreamPaths, UpstreamFSes, or both (processed in order: paths first,
// then FSes).
type IntegrateLocalOptions struct {
	UpstreamPaths []string
	// UpstreamFSes holds in-memory filesystems to integrate as local upstreams.
	// Each FS must expose .gitspork.yml (or .gitspork.yaml) at its root.
	// Callers embedding files with dot-prefixed names via embed.FS must use the
	// "all:" directive: //go:embed all:_upstream.
	// IntegrateLocal materializes each FS to a temp directory internally; no
	// temporary files are visible to the caller.
	UpstreamFSes   []fs.FS
	DownstreamPath string
	ForceRePrompt  bool
	Logger         Logger

	// CacheTTL controls the machine-scoped upstream mirror cache freshness
	// threshold. A cache entry younger than CacheTTL is used as-is; older
	// triggers a `git fetch` refresh. Zero-value means "use GITSPORK_CACHE_TTL
	// env var if set, else the compiled default (2h)". Ignored for
	// IntegrateLocalOptions because IntegrateLocal doesn't clone remotes.
	CacheTTL time.Duration

	// NoCache, when true, bypasses the machine-scoped upstream mirror cache
	// entirely — a direct network clone runs on every invocation. Overrides
	// CacheTTL. Also settable via GITSPORK_NO_CACHE env var. Ignored for
	// IntegrateLocalOptions because IntegrateLocal doesn't clone remotes.
	NoCache bool

	// SeedInputs provides pre-computed values for templated inputs, keyed by
	// input name. A seeded value pre-populates the named input across all
	// templated instructions in the run, preventing interactive prompts for
	// those inputs. Seeded values win over the on-disk input cache;
	// from_destination_structured still wins when the path resolves in the
	// existing rendered file. Nil or empty map means no seeding.
	SeedInputs map[string]string
}

// CheckDriftOptions configures a call to CheckDrift. Leave Upstreams empty
// to use the recorded state; supply entries to override with different
// URLs/tokens for the same recorded commit hashes.
type CheckDriftOptions struct {
	Upstreams          []UpstreamSpec
	DownstreamRepoPath string
	Logger             Logger

	// CacheTTL controls the machine-scoped upstream mirror cache freshness
	// threshold. A cache entry younger than CacheTTL is used as-is; older
	// triggers a `git fetch` refresh. Zero-value means "use GITSPORK_CACHE_TTL
	// env var if set, else the compiled default (2h)". Ignored for
	// IntegrateLocalOptions because IntegrateLocal doesn't clone remotes.
	CacheTTL time.Duration

	// NoCache, when true, bypasses the machine-scoped upstream mirror cache
	// entirely — a direct network clone runs on every invocation. Overrides
	// CacheTTL. Also settable via GITSPORK_NO_CACHE env var. Ignored for
	// IntegrateLocalOptions because IntegrateLocal doesn't clone remotes.
	NoCache bool

	// Progress, if non-nil, receives git progress output during upstream cache
	// clone/fetch operations (populating the machine-scoped mirror on first use,
	// refreshing on TTL expiry). Consumers typically pass os.Stderr for
	// terminal-style progress; leave nil to suppress. Ignored when NoCache is
	// true (direct clone path).
	Progress io.Writer
}

// UpstreamSpec identifies a single upstream to integrate from.
//
// Version may be one of:
//   - A branch name (e.g. "main", "feature/x") — resolved as refs/heads/<v>.
//   - A tag name (e.g. "v1.2.3") — resolved as refs/tags/<v>. When both a
//     branch and a tag share a name (rare), the tag wins, matching
//     `git checkout`'s precedence for ambiguous refs.
//   - An explicit "tags/<name>" form — always treated as a tag, useful
//     when the caller wants to bypass tag/branch disambiguation.
//   - A commit hash — 7 to 40 hex characters, short or full. The upstream
//     is cloned with full history and the hash is resolved via git's
//     revision parser, so `abc1234` and the full 40-char SHA both work.
//
// An empty Version selects the remote's default branch.
type UpstreamSpec struct {
	URL     string
	Version string
	Subpath string
	Token   string
}
