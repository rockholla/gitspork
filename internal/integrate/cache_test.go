package integrate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
