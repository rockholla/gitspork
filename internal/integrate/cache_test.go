package integrate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/gofrs/flock"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
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

// Goroutines in one process must be excluded from each other the same way
// separate processes are: a writer waits for other writers and for readers,
// and readers only share with readers.
func Test_cacheEntryLocks_excludeWithinOneProcess(t *testing.T) {
	cases := []struct {
		name          string
		held, wanted  func(string) (func(), error)
		wantExclusion bool
	}{
		{"exclusive blocks exclusive", lockCacheEntry, lockCacheEntry, true},
		{"shared blocks exclusive", rLockCacheEntry, lockCacheEntry, true},
		{"exclusive blocks shared", lockCacheEntry, rLockCacheEntry, true},
		{"shared allows shared", rLockCacheEntry, rLockCacheEntry, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lockFile := filepath.Join(t.TempDir(), "entry.lock")
			unlockHeld, err := tc.held(lockFile)
			require.NoError(t, err)

			type result struct {
				unlock func()
				err    error
			}
			acquired := make(chan result, 1)
			go func() {
				unlock, err := tc.wanted(lockFile)
				acquired <- result{unlock, err}
			}()
			// waitAcquired fails the test unless the second lock is acquired
			// within a generous deadline, and releases it.
			waitAcquired := func(msg string) {
				t.Helper()
				select {
				case r := <-acquired:
					require.NoError(t, r.err)
					r.unlock()
				case <-time.After(10 * time.Second):
					t.Fatal(msg)
				}
			}

			if tc.wantExclusion {
				select {
				case r := <-acquired:
					if r.err == nil {
						r.unlock()
					}
					unlockHeld()
					t.Fatal("second lock was acquired while the first was held")
				case <-time.After(300 * time.Millisecond):
				}
				unlockHeld()
				waitAcquired("second lock was not acquired after the first was released")
				return
			}
			waitAcquired("second lock waited although both are shared")
			unlockHeld()
		})
	}
}

func Test_populateCache_localFileURL(t *testing.T) {
	upstreamDir, upstreamHash := testharness.MinimalUpstream(t)
	cacheDir := filepath.Join(t.TempDir(), "cache-entry")

	err := populateCache(cacheDir, "file://"+upstreamDir, authInfo{}, nil)
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
	err := populateCache(cacheDir, "file:///nonexistent/absolutely-not-a-repo", authInfo{}, nil)
	require.Error(t, err)
}

func Test_refreshCache_picksUpNewUpstreamCommits(t *testing.T) {
	upstreamDir, firstHash := testharness.MinimalUpstream(t)
	cacheDir := filepath.Join(t.TempDir(), "cache-entry")

	// Initial populate.
	require.NoError(t, populateCache(cacheDir, "file://"+upstreamDir, authInfo{}, nil))

	// Advance the upstream with a new commit.
	newFilePath := filepath.Join(upstreamDir, "added-later.txt")
	require.NoError(t, os.WriteFile(newFilePath, []byte("later"), 0644))
	upstreamRepo, err := gogit.PlainOpen(upstreamDir)
	require.NoError(t, err)
	secondHash := testharness.CommitAllWithMessage(t, upstreamRepo, "add another file")

	// Before refresh, cache has only firstHash.
	preRepo, err := gogit.PlainOpen(cacheDir)
	require.NoError(t, err)
	_, err = preRepo.CommitObject(secondHash)
	assert.Error(t, err, "second commit must NOT be present before refresh")

	// Refresh, then the cache carries secondHash too.
	// Re-open to get a fresh object-store view — go-git builds its packfile
	// index lazily and does not invalidate it on external writes (Reindex()).
	require.NoError(t, refreshCache(cacheDir, "file://"+upstreamDir, authInfo{}, nil))
	cacheRepo, err := gogit.PlainOpen(cacheDir)
	require.NoError(t, err)
	_, err = cacheRepo.CommitObject(secondHash)
	assert.NoError(t, err, "second commit must be reachable after refresh")

	// And the original commit is still there.
	_, err = cacheRepo.CommitObject(firstHash)
	assert.NoError(t, err)
}

func Test_ensureUpstreamCache_disabled_returnsEmpty(t *testing.T) {
	cfg := cacheConfig{Disabled: true}
	dir, err := ensureUpstreamCacheReleased(cfg, "file:///somewhere", authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)
	assert.Empty(t, dir, "disabled cache must return empty dir (caller falls back to direct clone)")
}

func Test_ensureUpstreamCache_missingEntry_populates(t *testing.T) {
	upstreamDir, _ := testharness.MinimalUpstream(t)
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 2 * time.Hour}

	dir, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, dir)
	assert.DirExists(t, dir)
	assert.FileExists(t, dir+".fetched-at")
}

func Test_ensureUpstreamCache_freshEntry_noFetch(t *testing.T) {
	upstreamDir, firstHash := testharness.MinimalUpstream(t)
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 2 * time.Hour}

	// First call populates.
	dir1, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)

	// Advance upstream — the fresh cache must NOT pick this up.
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "later.txt"), []byte("x"), 0644))
	upstreamRepo, err := gogit.PlainOpen(upstreamDir)
	require.NoError(t, err)
	newHash := testharness.CommitAllWithMessage(t, upstreamRepo, "advance")

	// Second call within TTL: no fetch.
	dir2, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)
	assert.Equal(t, dir1, dir2)

	// Re-open to get a fresh object-store view (go-git packfile-index quirk).
	repo, err := gogit.PlainOpen(dir2)
	require.NoError(t, err)
	_, err = repo.CommitObject(firstHash)
	assert.NoError(t, err, "original commit still reachable")
	_, err = repo.CommitObject(newHash)
	assert.Error(t, err, "new commit MUST NOT be present — fresh cache skipped the fetch")
}

func Test_ensureUpstreamCache_staleEntry_refreshes(t *testing.T) {
	upstreamDir, _ := testharness.MinimalUpstream(t)
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 1 * time.Nanosecond} // instantly stale

	_, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)

	// Advance upstream and re-run — the tiny TTL forces a fetch.
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "later.txt"), []byte("x"), 0644))
	upstreamRepo, err := gogit.PlainOpen(upstreamDir)
	require.NoError(t, err)
	newHash := testharness.CommitAllWithMessage(t, upstreamRepo, "advance")
	time.Sleep(2 * time.Nanosecond) // ensure now > fetched-at + ttl

	dir2, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)

	repo, err := gogit.PlainOpen(dir2)
	require.NoError(t, err)
	_, err = repo.CommitObject(newHash)
	assert.NoError(t, err, "stale cache must have been refreshed and now carries new commits")
}

func Test_ensureUpstreamCache_corruptEntry_wipesAndRetries(t *testing.T) {
	upstreamDir, upstreamHash := testharness.MinimalUpstream(t)
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 1 * time.Nanosecond}
	key := cacheKey("file://" + upstreamDir)
	dir, tsFile, _ := cacheEntryPaths(root, key)

	// Fabricate a corrupt "cache entry" that looks like a repo but isn't:
	// go-git PlainOpen will refuse to reopen it and refreshCache will error.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "objects"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "HEAD"), []byte("garbage"), 0644))
	require.NoError(t, writeFetchedAt(tsFile, time.Now()))
	time.Sleep(2 * time.Nanosecond)

	returnedDir, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err, "corrupt cache must be wiped and repopulated, not surfaced as an error")
	assert.Equal(t, dir, returnedDir)

	repo, err := gogit.PlainOpen(returnedDir)
	require.NoError(t, err)
	_, err = repo.CommitObject(upstreamHash)
	assert.NoError(t, err)
}

func Test_ensureUpstreamCache_bogusURL_boundedRetry(t *testing.T) {
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 2 * time.Hour}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := ensureUpstreamCacheReleased(cfg, "file:///absolutely-nonexistent-path-xyzzy", authInfo{}, sdktypes.NoopLogger(), nil)
		assert.Error(t, err)
	}()

	select {
	case <-done:
		// Fine — errored out promptly.
	case <-time.After(10 * time.Second):
		t.Fatal("ensureUpstreamCache did not surface an error within 10s — retry loop is not bounded")
	}
}

// Test_ensureUpstreamCache_defaultRootUnwritable_fallsBackToTmp locks the
// container fallback: when the default cache root can't be created (e.g.
// container with no writable HOME → os.UserCacheDir() = "/.cache"), the code
// silently retries under os.TempDir() so cache still works within-invocation.
func Test_ensureUpstreamCache_defaultRootUnwritable_fallsBackToTmp(t *testing.T) {
	upstreamDir, upstreamHash := testharness.MinimalUpstream(t)

	// Force MkdirAll to fail by making the parent a regular file rather than
	// a directory. Works regardless of test uid (root or otherwise).
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))
	unwritable := filepath.Join(blocker, "cache") // MkdirAll fails: "not a directory"

	cfg := cacheConfig{Root: unwritable, TTL: time.Hour, RootIsDefault: true}
	dir, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err, "default-root mkdir failure must fall back to os.TempDir, not surface as error")
	require.NotEmpty(t, dir)

	// Returned dir lives under os.TempDir()/gitspork/repos/
	expectedRoot := filepath.Join(os.TempDir(), "gitspork", "repos")
	assert.True(t, strings.HasPrefix(dir, expectedRoot+string(filepath.Separator)),
		"expected fallback dir under %s, got %s", expectedRoot, dir)

	// Cache is actually populated in the fallback location.
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	_, err = repo.CommitObject(upstreamHash)
	assert.NoError(t, err)
}

// Test_ensureUpstreamCache_explicitRootUnwritable_errors verifies that when
// the user explicitly set GITSPORK_CACHE_DIR to an unwritable path, we
// surface the error rather than silently falling back.
func Test_ensureUpstreamCache_explicitRootUnwritable_errors(t *testing.T) {
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))
	unwritable := filepath.Join(blocker, "cache")

	cfg := cacheConfig{Root: unwritable, TTL: time.Hour, RootIsDefault: false}
	_, err := ensureUpstreamCacheReleased(cfg, "file:///anywhere", authInfo{}, sdktypes.NoopLogger(), nil)
	require.Error(t, err, "explicit user-configured unwritable root must surface as error, not silently fall back")
	assert.Contains(t, err.Error(), unwritable)
}

// A fresh timestamp must not be trusted on its own: if the mirror directory
// was removed (e.g. deleted by hand, leaving the .fetched-at sidecar), a
// "cache hit" would hand callers a path that doesn't exist and every working
// clone would fail with "repository ... does not exist" until the TTL lapsed.
func Test_ensureUpstreamCache_freshTimestampMissingMirror_repopulates(t *testing.T) {
	upstreamDir, upstreamHash := testharness.MinimalUpstream(t)
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 2 * time.Hour}

	dir, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(dir)) // mirror gone, fresh sidecar left behind
	require.FileExists(t, dir+".fetched-at")

	returnedDir, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)
	assert.Equal(t, dir, returnedDir)
	repo, err := gogit.PlainOpen(returnedDir)
	require.NoError(t, err, "missing mirror must be repopulated, not reported as a cache hit")
	_, err = repo.CommitObject(upstreamHash)
	assert.NoError(t, err)
}

// Same, for a mirror directory that exists but isn't a usable repository
// (e.g. partially deleted) while its timestamp is still fresh.
func Test_ensureUpstreamCache_freshTimestampBrokenMirror_repopulates(t *testing.T) {
	upstreamDir, upstreamHash := testharness.MinimalUpstream(t)
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 2 * time.Hour}
	key := cacheKey("file://" + upstreamDir)
	dir, tsFile, _ := cacheEntryPaths(root, key)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "objects"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leftover"), []byte("x"), 0644))
	require.NoError(t, writeFetchedAt(tsFile, time.Now()))

	returnedDir, err := ensureUpstreamCacheReleased(cfg, "file://"+upstreamDir, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)
	repo, err := gogit.PlainOpen(returnedDir)
	require.NoError(t, err, "broken mirror must be repopulated, not reported as a cache hit")
	_, err = repo.CommitObject(upstreamHash)
	assert.NoError(t, err)
}

// Callers such as a multi-repo upgrade tool integrate from several goroutines
// at once against the same upstream, all sharing one cache entry. Each call
// must be excluded from the others while it populates or refreshes the
// mirror, and a working clone must not read the mirror while another call is
// writing to (or wiping) it. Otherwise clones fail mid-read (git's "hardlink
// different from source" check, or vanished files) and a failed concurrent
// populate's wipe-and-retry can delete a mirror that another call just
// finished, leaving a fresh timestamp with no mirror behind it.
func Test_cloneUpstreamForIntegrate_concurrentCallsSharingCacheEntry_allSucceed(t *testing.T) {
	upstreamDir := largerUpstream(t, 100)
	const workers = 6
	const rounds = 3
	for round := range rounds {
		// Fresh cache per round so every round races on the initial populate;
		// the tiny TTL makes later calls in the round race on refresh too.
		t.Setenv("GITSPORK_CACHE_DIR", t.TempDir())
		errs := make([]error, workers)
		var wg sync.WaitGroup
		for i := range workers {
			wg.Go(func() {
				req := &internalRequest{Logger: sdktypes.NoopLogger(), cacheTTL: time.Nanosecond}
				_, errs[i] = cloneUpstreamForIntegrate(t.TempDir(), req, sdktypes.UpstreamSpec{URL: "file://" + upstreamDir})
			})
		}
		wg.Wait()
		for i, err := range errs {
			require.NoError(t, err, "round %d, worker %d", round, i)
		}
	}
}

// Working clones hold the shared lock while they read the mirror. A call
// that finds the cache fresh only reads it, so it must not wait for those
// clones to finish; otherwise every cache hit queues behind every clone in
// progress and concurrent integrates run one at a time.
func Test_ensureUpstreamCache_cacheHitDoesNotWaitForClonesInProgress(t *testing.T) {
	upstreamDir, _ := testharness.MinimalUpstream(t)
	url := "file://" + upstreamDir
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 2 * time.Hour}
	_, err := ensureUpstreamCacheReleased(cfg, url, authInfo{}, sdktypes.NoopLogger(), nil)
	require.NoError(t, err)

	// A clone in progress, as cloneUpstreamForIntegrate holds it.
	_, _, lockFile := cacheEntryPaths(root, cacheKey(url))
	unlockClone, err := rLockCacheEntry(lockFile)
	require.NoError(t, err)
	defer unlockClone()

	returned := make(chan error, 1)
	go func() {
		_, release, err := ensureUpstreamCache(cfg, url, authInfo{}, sdktypes.NoopLogger(), nil)
		if err == nil {
			release()
		}
		returned <- err
	}()
	select {
	case err := <-returned:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		unlockClone()
		<-returned
		t.Fatal("cache hit waited for a clone in progress to release its shared lock")
	}
}

// A working clone reads the mirror (git clone --local hardlinks its object
// files) after ensureUpstreamCache has returned. While it does, another call
// must not be able to take the exclusive lock and refresh or wipe the mirror;
// otherwise the clone sees pack files that are still being written or have
// been repacked away, and git aborts ("hardlink different from source",
// "failed to create link"). That must hold whichever way the call got the
// mirror: populated, refreshed, or served from the cache.
func Test_cloneUpstreamForIntegrate_holdsCacheLockWhileCloningFromMirror(t *testing.T) {
	if !useShellGitFastPath() {
		t.Skip("needs shell git: the check runs from git's clone progress output")
	}
	cases := []struct {
		name     string
		warm     bool          // populate the cache before the probed call
		ttl      time.Duration // the probed call's cache TTL
		wantPath string        // the cache log line that proves which path ran
	}{
		{"populated", false, 2 * time.Hour, "populating upstream cache"},
		{"refreshed", true, time.Nanosecond, "refreshing upstream cache"},
		{"cache hit", true, 2 * time.Hour, "upstream cache hit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstreamDir, _ := testharness.MinimalUpstream(t)
			url := "file://" + upstreamDir
			cacheRoot := t.TempDir()
			t.Setenv("GITSPORK_CACHE_DIR", cacheRoot)
			_, _, lockFile := cacheEntryPaths(cacheRoot, cacheKey(url))
			if tc.warm {
				warm := &internalRequest{Logger: sdktypes.NoopLogger(), cacheTTL: 2 * time.Hour}
				_, err := cloneUpstreamForIntegrate(t.TempDir(), warm, sdktypes.UpstreamSpec{URL: url})
				require.NoError(t, err)
			}

			// git writes this line to its progress stream once the working
			// clone has started, and blocks until Write returns, so the clone
			// is in progress while the probe runs.
			cloneDir := t.TempDir()
			probe := &lockProbeWriter{trigger: fmt.Sprintf("Cloning into '%s'", cloneDir), lockFile: lockFile}
			logger := &recordingLogger{}
			req := &internalRequest{Logger: logger, cacheTTL: tc.ttl, progress: probe}
			_, err := cloneUpstreamForIntegrate(cloneDir, req, sdktypes.UpstreamSpec{URL: url})
			require.NoError(t, err)

			require.True(t, slices.ContainsFunc(logger.logs, func(l string) bool { return strings.Contains(l, tc.wantPath) }),
				"expected the %q path, got logs: %q", tc.wantPath, logger.logs)
			require.True(t, probe.probed, "never saw the working clone start in git's progress output")
			assert.False(t, probe.writerGotLock, "a writer took the exclusive cache lock while the working clone was reading the mirror")
		})
	}
}

// lockProbeWriter tries the exclusive cache lock, without waiting, the first
// time a progress write contains trigger.
type lockProbeWriter struct {
	trigger       string
	lockFile      string
	probed        bool
	writerGotLock bool
}

func (w *lockProbeWriter) Write(p []byte) (int, error) {
	if !w.probed && strings.Contains(string(p), w.trigger) {
		w.probed = true
		fl := flock.New(w.lockFile)
		if locked, err := fl.TryLock(); err == nil && locked {
			w.writerGotLock = true
			_ = fl.Unlock()
		}
	}
	return len(p), nil
}

// fetch can start git's auto-maintenance (gc/repack), which by default
// detaches and keeps rewriting the mirror after fetch returns — after the
// exclusive cache lock is released — deleting packs that concurrent working
// clones are reading. A refresh must run it in the foreground.
func Test_refreshCache_runsAutoMaintenanceInForeground(t *testing.T) {
	if !useShellGitFastPath() {
		t.Skip("auto-maintenance only runs on the shell git path")
	}
	upstreamDir, _ := testharness.MinimalUpstream(t)
	url := "file://" + upstreamDir
	mirrorDir := filepath.Join(t.TempDir(), "mirror")
	require.NoError(t, populateCache(mirrorDir, url, authInfo{}, nil))
	addUpstreamCommit(t, upstreamDir)

	// trace2 records every git child process, including the maintenance run
	// fetch spawns.
	trace := filepath.Join(t.TempDir(), "trace2.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	require.NoError(t, refreshCache(mirrorDir, url, authInfo{}, nil))

	events, err := os.ReadFile(trace)
	require.NoError(t, err)
	var maintenanceRuns, showsDetachChoice int
	for line := range strings.Lines(string(events)) {
		var ev struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Event != "child_start" {
			continue
		}
		if slices.Contains(ev.Argv, "maintenance") || slices.Contains(ev.Argv, "gc") {
			maintenanceRuns++
			if slices.Contains(ev.Argv, "--detach") || slices.Contains(ev.Argv, "--no-detach") {
				showsDetachChoice++
			}
			assert.NotContains(t, ev.Argv, "--detach", "fetch started detached auto-maintenance: %v", ev.Argv)
		}
	}
	require.NotZero(t, maintenanceRuns, "fetch started no auto-maintenance; the test no longer exercises it")
	if showsDetachChoice == 0 {
		// Older git decides inside the maintenance process whether to detach,
		// so its argv can't show the choice and this check would prove nothing.
		t.Skip("this git doesn't pass --detach/--no-detach to auto-maintenance; can't observe detaching")
	}
}

// addUpstreamCommit commits a new file to the upstream repo at dir.
func addUpstreamCommit(t *testing.T, dir string) {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "upstream-owned", "added.txt"), []byte("added\n"), 0644))
	testharness.CommitAllWithMessage(t, repo, "add a file")
}

// ensureUpstreamCacheReleased calls ensureUpstreamCache and releases the
// entry's shared lock straight away, for tests that inspect the cache rather
// than clone from it.
func ensureUpstreamCacheReleased(cfg cacheConfig, url string, auth authInfo, logger sdktypes.Logger, progress io.Writer) (string, error) {
	dir, release, err := ensureUpstreamCache(cfg, url, auth, logger, progress)
	if err != nil {
		return "", err
	}
	release()
	return dir, nil
}

// largerUpstream is MinimalUpstream plus n extra upstream-owned files spread
// over several commits, so cache populates and working clones take long
// enough for concurrent calls to overlap.
func largerUpstream(t *testing.T, n int) string {
	t.Helper()
	dir, _ := testharness.MinimalUpstream(t)
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	for i := range n {
		name := filepath.Join(dir, "upstream-owned", fmt.Sprintf("file-%03d.txt", i))
		require.NoError(t, os.WriteFile(name, []byte(strings.Repeat(fmt.Sprintf("line %d\n", i), 200)), 0644))
		if i%50 == 49 {
			testharness.CommitAllWithMessage(t, repo, fmt.Sprintf("add files up to %d", i))
		}
	}
	return dir
}
