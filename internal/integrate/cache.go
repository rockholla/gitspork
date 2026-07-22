package integrate

import (
	"fmt"
	"os"
	"path/filepath"
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
			return cacheConfig{}, fmt.Errorf("resolving user cache dir for upstream mirror cache: %v", err)
		}
		root = filepath.Join(userCache, "gitspork", "repos")
	}

	ttl := cliTTL
	if ttl == 0 {
		if envTTL := os.Getenv(envCacheTTL); envTTL != "" {
			parsed, err := time.ParseDuration(envTTL)
			if err != nil {
				return cacheConfig{}, fmt.Errorf("invalid %s %q: %v", envCacheTTL, envTTL, err)
			}
			ttl = parsed
		} else {
			ttl = defaultCacheTTL
		}
	}

	return cacheConfig{Root: root, TTL: ttl}, nil
}
