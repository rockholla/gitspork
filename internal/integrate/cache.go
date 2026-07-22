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
