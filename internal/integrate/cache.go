package integrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	git "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// cacheConfig is the resolved configuration for the machine-scoped upstream
// mirror cache. Produced by resolveCacheConfig from CLI flags + env vars +
// compiled defaults.
type cacheConfig struct {
	// Root is the absolute path to the cache root directory. Empty when
	// Disabled is true (cache is bypassed).
	Root string
	// TTL is how long a cache entry stays fresh before a fetch is triggered.
	// Zero when Disabled is true.
	TTL time.Duration
	// Disabled indicates the cache is bypassed entirely for this run.
	Disabled bool
}

const (
	defaultCacheTTL = 2 * time.Hour
	envCacheDir     = "GITSPORK_CACHE_DIR"
	envCacheTTL     = "GITSPORK_CACHE_TTL"
	envNoCache      = "GITSPORK_NO_CACHE"
)

// resolveCacheConfig produces a cacheConfig for one call, merging CLI-provided
// values (cliTTL, cliNoCache) with env vars and compiled defaults. cliNoCache
// or any non-empty GITSPORK_NO_CACHE short-circuits to Disabled=true; the
// remaining fields are then irrelevant.
func resolveCacheConfig(cliTTL time.Duration, cliNoCache bool) (cacheConfig, error) {
	if cliNoCache || os.Getenv(envNoCache) != "" {
		return cacheConfig{Disabled: true}, nil
	}

	root := os.Getenv(envCacheDir)
	if root == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return cacheConfig{}, fmt.Errorf("resolving user cache dir for upstream mirror cache: %w", err)
		}
		root = filepath.Join(userCache, "gitspork", "repos")
	}

	ttl := cliTTL
	if ttl == 0 {
		if envTTL := os.Getenv(envCacheTTL); envTTL != "" {
			parsed, err := time.ParseDuration(envTTL)
			if err != nil {
				return cacheConfig{}, fmt.Errorf("invalid %s %q: %w", envCacheTTL, envTTL, err)
			}
			ttl = parsed
		} else {
			ttl = defaultCacheTTL
		}
	}

	return cacheConfig{Root: root, TTL: ttl}, nil
}

// cacheKey derives a stable filesystem-safe identifier for an upstream URL,
// using NormalizeUpstreamURL for canonicalization so SSH/HTTPS variants and
// case-insensitive host names collapse to the same key. Result is the
// lowercase hex encoding of sha256(canonicalized-url), length 64.
func cacheKey(url string) string {
	canonical := NormalizeUpstreamURL(url, "")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// CacheKeyForURL is the exported name of cacheKey, provided so the CLI
// package can compute cache-entry paths in tests without duplicating the
// hashing logic. Not part of the public SDK — the wrapper lives in
// internal/ so external consumers do not see it.
func CacheKeyForURL(url string) string {
	return cacheKey(url)
}

// cacheEntryPaths returns the three filesystem paths associated with a cache
// entry: the bare-mirror directory itself, the .fetched-at sidecar timestamp
// file, and the .lock sentinel used by the per-URL flock.
func cacheEntryPaths(root, key string) (dir, tsFile, lockFile string) {
	dir = filepath.Join(root, key)
	tsFile = filepath.Join(root, key+".fetched-at")
	lockFile = filepath.Join(root, key+".lock")
	return
}

// isCacheFresh reports whether a cache entry whose last fetch happened at
// fetchedAt is still within the configured TTL. A ttl of 0 (or negative) is
// treated as "never fresh" — any positive age causes a refresh.
func isCacheFresh(fetchedAt time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	return time.Since(fetchedAt) <= ttl
}

// readFetchedAt reads the Unix-timestamp sidecar file written by
// writeFetchedAt. Returns the timestamp on success. On a missing file the
// error satisfies os.IsNotExist so callers can treat it as "cache absent".
func readFetchedAt(path string) (time.Time, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing timestamp from %s: %w", path, err)
	}
	return time.Unix(secs, 0), nil
}

// writeFetchedAt records t as a Unix-timestamp sidecar file. Callers already
// hold the per-URL flock when this is invoked, so no atomicity beyond
// os.WriteFile is required.
func writeFetchedAt(path string, t time.Time) error {
	return os.WriteFile(path, []byte(strconv.FormatInt(t.Unix(), 10)), 0644)
}

// populateCache clones url as a bare mirror at dir. Uses go-git's
// PlainClone with Mirror: true, which fetches all reachable refs (branches,
// tags, notes) so subsequent working clones can resolve any Version the
// integrator asks for.
func populateCache(dir, url string, auth transport.AuthMethod) error {
	opts := &git.CloneOptions{
		URL:    url,
		Mirror: true,
	}
	if auth != nil {
		opts.Auth = auth
	}
	if _, err := git.PlainClone(dir, opts); err != nil {
		return fmt.Errorf("cloning mirror for upstream cache at %s: %w", dir, err)
	}
	return nil
}

// refreshCache runs the equivalent of `git fetch --prune` against the origin
// remote of an existing bare mirror at dir. Called when a cache entry is
// stale beyond its TTL. Callers must hold the per-URL flock while this
// runs — git-fetch inside the mirror is not safe against concurrent writers.
func refreshCache(dir string, auth transport.AuthMethod) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("opening upstream cache at %s: %w", dir, err)
	}
	opts := &git.FetchOptions{
		RemoteName: "origin",
		Prune:      true,
		RefSpecs: []gitconfig.RefSpec{
			// Mirror-style refspec: mirror all refs on the remote into the local
			// refs namespace, matching what `git clone --mirror` sets up.
			"+refs/*:refs/*",
		},
	}
	if auth != nil {
		opts.Auth = auth
	}
	if err := repo.Fetch(opts); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("fetching into upstream cache at %s: %w", dir, err)
	}
	return nil
}

// ensureUpstreamCache is the main entry point for the upstream mirror cache.
// It resolves the cache directory for a given canonical URL, populating or
// refreshing as needed under an exclusive per-URL flock, and returns the
// absolute path to a healthy bare mirror. Returns "" (no error) when the
// cache is disabled — the caller must fall back to a direct clone in that
// case.
//
// Corruption recovery: any cache-side error (populate or refresh) triggers
// a single wipe-and-repopulate retry. On second failure the wrapped error
// is surfaced. Retries are hard-bounded to prevent infinite loops against
// a genuinely broken remote.
func ensureUpstreamCache(cfg cacheConfig, url string, auth transport.AuthMethod, logger sdktypes.Logger) (string, error) {
	if cfg.Disabled {
		return "", nil
	}

	if err := os.MkdirAll(cfg.Root, 0755); err != nil {
		return "", fmt.Errorf("creating upstream cache root %s: %w", cfg.Root, err)
	}

	key := cacheKey(url)
	dir, tsFile, lockFile := cacheEntryPaths(cfg.Root, key)

	fl := getOrCreateFlock(lockFile)
	if err := fl.Lock(); err != nil {
		return "", fmt.Errorf("acquiring upstream cache lock at %s: %w", lockFile, err)
	}
	defer func() { _ = fl.Unlock() }()

	// First attempt.
	if err := runCacheOp(dir, tsFile, url, cfg.TTL, auth, logger); err != nil {
		// Wipe and retry once.
		_ = os.RemoveAll(dir)
		_ = os.Remove(tsFile)
		if err := populateCache(dir, url, auth); err != nil {
			return "", fmt.Errorf("upstream cache populate failed after wipe-and-retry: %w", err)
		}
		if err := writeFetchedAt(tsFile, time.Now()); err != nil {
			return "", fmt.Errorf("writing upstream cache timestamp after recovery: %w", err)
		}
		logger.Log("populating upstream cache for %s at %s", url, dir)
	}
	return dir, nil
}

// runCacheOp inspects the state of a cache entry and performs the appropriate
// operation — no-op if fresh, refresh if stale, populate if missing. Emits a
// distinct log line per branch matching Section 1's Log-line contract.
func runCacheOp(dir, tsFile, url string, ttl time.Duration, auth transport.AuthMethod, logger sdktypes.Logger) error {
	fetchedAt, tsErr := readFetchedAt(tsFile)
	tsPresent := tsErr == nil

	// Populate path: no timestamp file OR no cache dir yet.
	if !tsPresent {
		if err := populateCache(dir, url, auth); err != nil {
			return err
		}
		if err := writeFetchedAt(tsFile, time.Now()); err != nil {
			return err
		}
		logger.Log("populating upstream cache for %s at %s", url, dir)
		return nil
	}

	if isCacheFresh(fetchedAt, ttl) {
		age := time.Since(fetchedAt).Round(time.Second)
		logger.Log("upstream cache hit for %s (fetched %s ago, ttl: %s)", url, age, ttl)
		return nil
	}

	// Stale — refresh.
	age := time.Since(fetchedAt).Round(time.Second)
	if err := refreshCache(dir, auth); err != nil {
		return err
	}
	if err := writeFetchedAt(tsFile, time.Now()); err != nil {
		return err
	}
	logger.Log("refreshing upstream cache for %s (last fetch: %s ago, ttl: %s)", url, age, ttl)
	return nil
}
