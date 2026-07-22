package integrate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/rockholla/gitspork/v2/test/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_resolveCacheConfig(t *testing.T) {
	// Isolate the test from any ambient cache env.
	t.Setenv("GITSPORK_CACHE_DIR", "")
	t.Setenv("GITSPORK_CACHE_TTL", "")
	t.Setenv("GITSPORK_NO_CACHE", "")

	t.Run("defaults to os.UserCacheDir + 2h TTL", func(t *testing.T) {
		cfg, err := resolveCacheConfig(0, false)
		require.NoError(t, err)
		assert.False(t, cfg.Disabled)
		want, _ := os.UserCacheDir()
		assert.Equal(t, filepath.Join(want, "gitspork", "repos"), cfg.Root)
		assert.Equal(t, 2*time.Hour, cfg.TTL)
	})

	t.Run("GITSPORK_CACHE_DIR overrides root", func(t *testing.T) {
		t.Setenv("GITSPORK_CACHE_DIR", "/tmp/custom-cache")
		cfg, err := resolveCacheConfig(0, false)
		require.NoError(t, err)
		assert.Equal(t, "/tmp/custom-cache", cfg.Root)
	})

	t.Run("cliTTL non-zero wins over env and default", func(t *testing.T) {
		t.Setenv("GITSPORK_CACHE_TTL", "30m")
		cfg, err := resolveCacheConfig(5*time.Minute, false)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Minute, cfg.TTL)
	})

	t.Run("cliTTL zero falls back to env", func(t *testing.T) {
		t.Setenv("GITSPORK_CACHE_TTL", "45m")
		cfg, err := resolveCacheConfig(0, false)
		require.NoError(t, err)
		assert.Equal(t, 45*time.Minute, cfg.TTL)
	})

	t.Run("cliTTL zero + env unset falls back to 2h default", func(t *testing.T) {
		cfg, err := resolveCacheConfig(0, false)
		require.NoError(t, err)
		assert.Equal(t, 2*time.Hour, cfg.TTL)
	})

	t.Run("malformed GITSPORK_CACHE_TTL surfaces a wrapped error", func(t *testing.T) {
		t.Setenv("GITSPORK_CACHE_TTL", "not-a-duration")
		_, err := resolveCacheConfig(0, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GITSPORK_CACHE_TTL")
		assert.Contains(t, err.Error(), "not-a-duration")
	})

	t.Run("cliNoCache=true short-circuits", func(t *testing.T) {
		cfg, err := resolveCacheConfig(0, true)
		require.NoError(t, err)
		assert.True(t, cfg.Disabled)
	})

	t.Run("GITSPORK_NO_CACHE presence disables regardless of value", func(t *testing.T) {
		for _, val := range []string{"1", "true", "yes", "false", "0"} {
			t.Setenv("GITSPORK_NO_CACHE", val)
			cfg, err := resolveCacheConfig(0, false)
			require.NoError(t, err, "val=%q", val)
			assert.True(t, cfg.Disabled, "any non-empty GITSPORK_NO_CACHE must disable; val=%q", val)
		}
	})

	t.Run("GITSPORK_NO_CACHE empty string leaves cache enabled", func(t *testing.T) {
		t.Setenv("GITSPORK_NO_CACHE", "")
		cfg, err := resolveCacheConfig(0, false)
		require.NoError(t, err)
		assert.False(t, cfg.Disabled)
	})
}

func Test_cacheKey(t *testing.T) {
	t.Run("SSH and HTTPS variants of the same repo collapse to the same key", func(t *testing.T) {
		ssh := cacheKey("git@github.com:org/repo.git")
		https := cacheKey("https://github.com/org/repo")
		assert.Equal(t, ssh, https, "SSH and HTTPS variants must map to the same cache entry")
	})

	t.Run("mixed-case URLs collapse to the same key", func(t *testing.T) {
		lower := cacheKey("https://github.com/org/repo")
		upper := cacheKey("https://GitHub.com/Org/Repo")
		assert.Equal(t, lower, upper, "URL case-insensitivity is inherited from NormalizeUpstreamURL")
	})

	t.Run("different URLs produce different keys", func(t *testing.T) {
		a := cacheKey("https://github.com/org/repo-a")
		b := cacheKey("https://github.com/org/repo-b")
		assert.NotEqual(t, a, b)
	})

	t.Run("output is stable hex-encoded sha256 (64 chars)", func(t *testing.T) {
		k := cacheKey("https://github.com/org/repo")
		assert.Len(t, k, 64, "sha256 hex is 64 chars")
	})
}

func Test_cacheEntryPaths(t *testing.T) {
	dir, ts, lock := cacheEntryPaths("/var/cache/gitspork/repos", "abc123")
	assert.Equal(t, "/var/cache/gitspork/repos/abc123", dir)
	assert.Equal(t, "/var/cache/gitspork/repos/abc123.fetched-at", ts)
	assert.Equal(t, "/var/cache/gitspork/repos/abc123.lock", lock)
}

func Test_isCacheFresh(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		fetchedAt time.Time
		ttl       time.Duration
		want      bool
	}{
		{name: "fetched 1 min ago, 2h ttl → fresh", fetchedAt: now.Add(-1 * time.Minute), ttl: 2 * time.Hour, want: true},
		{name: "fetched 3 hours ago, 2h ttl → stale", fetchedAt: now.Add(-3 * time.Hour), ttl: 2 * time.Hour, want: false},
		{name: "fetched just inside boundary → fresh (<= is inclusive)", fetchedAt: now.Add(-2*time.Hour + time.Second), ttl: 2 * time.Hour, want: true},
		{name: "zero TTL → never fresh (any age is stale)", fetchedAt: now, ttl: 0, want: false},
		{name: "negative TTL → never fresh", fetchedAt: now, ttl: -1 * time.Second, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isCacheFresh(tc.fetchedAt, tc.ttl))
		})
	}
}

func Test_fetchedAtRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "some-key.fetched-at")

	now := time.Now().Round(time.Second) // sidecar stores second-precision
	require.NoError(t, writeFetchedAt(path, now))

	got, err := readFetchedAt(path)
	require.NoError(t, err)
	assert.Equal(t, now.Unix(), got.Unix())
}

func Test_readFetchedAt_missingFile(t *testing.T) {
	_, err := readFetchedAt(filepath.Join(t.TempDir(), "does-not-exist"))
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err), "must surface os.IsNotExist so callers can branch on it")
}

func Test_readFetchedAt_malformedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.fetched-at")
	require.NoError(t, os.WriteFile(path, []byte("not-a-number"), 0644))

	_, err := readFetchedAt(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func Test_getOrCreateFlock_returnsSameInstancePerPath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "one.lock")
	b := filepath.Join(dir, "two.lock")

	// Same path → same instance (identity check).
	assert.Same(t, getOrCreateFlock(a), getOrCreateFlock(a),
		"repeated calls with the same path must return the same *flock.Flock")

	// Different paths → different instances.
	assert.NotSame(t, getOrCreateFlock(a), getOrCreateFlock(b),
		"different paths must yield distinct *flock.Flock instances")
}

func Test_populateCache_localFileURL(t *testing.T) {
	upstreamDir, upstreamHash := testharness.MinimalUpstream(t)
	cacheDir := filepath.Join(t.TempDir(), "cache-entry")

	err := populateCache(cacheDir, "file://"+upstreamDir, nil)
	require.NoError(t, err)

	// A bare mirror has HEAD and packed-refs (or refs/) but NO working tree.
	_, err = os.Stat(filepath.Join(cacheDir, "HEAD"))
	assert.NoError(t, err, "bare mirror must have HEAD")
	_, err = os.Stat(filepath.Join(cacheDir, ".git"))
	assert.True(t, os.IsNotExist(err), "bare mirror must NOT have a nested .git dir")

	// The upstream's HEAD commit hash is reachable in the mirror.
	repo, err := gogit.PlainOpen(cacheDir)
	require.NoError(t, err)
	_, err = repo.CommitObject(upstreamHash)
	assert.NoError(t, err, "mirror must carry the upstream's HEAD commit")
}

func Test_populateCache_bogusURL_returnsError(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache-entry")
	err := populateCache(cacheDir, "file:///nonexistent/absolutely-not-a-repo", nil)
	require.Error(t, err)
}
