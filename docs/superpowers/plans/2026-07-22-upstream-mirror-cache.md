# Upstream Mirror Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a machine-scoped bare-mirror cache of upstream repositories, gated by a per-run TTL, so a coordinator running fan-out across many downstreams against a small set of upstreams no longer re-clones the same upstream N times per run.

**Architecture:** Bare-mirror cache under `os.UserCacheDir()+gitspork/repos/` (overridable via `GITSPORK_CACHE_DIR`), keyed by `sha256(NormalizeUpstreamURL(url))`. Cross-process safety via `github.com/gofrs/flock`; in-process safety via a package-level `map[string]*flock.Flock` singleton. `cloneUpstreamForIntegrate` calls `ensureUpstreamCache` (populate or refresh under exclusive per-URL flock) then performs the working `PlainClone` from `file://<cache-dir>` (outside the flock, at `Depth: 1` when applicable). Opt-out via `--no-cache` / `GITSPORK_NO_CACHE`.

**Tech Stack:** Go 1.26; `github.com/gofrs/flock` (new dependency); existing go-git (`v6`), cobra, testify.

**Spec:** `docs/superpowers/specs/2026-07-22-upstream-mirror-cache-design.md`

---

## Task 1: Add gofrs/flock dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/gofrs/flock@latest`
Expected: `go.mod` gains a `require github.com/gofrs/flock v...` line; `go.sum` gains matching checksums.

- [ ] **Step 2: Verify build still passes**

Run: `go build ./...`
Expected: no output (clean build). If there is output, resolve before continuing.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/gofrs/flock for cross-process cache locking"
```

---

## Task 2: cacheConfig type and resolveCacheConfig

**Files:**
- Create: `internal/integrate/cache.go`
- Create: `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/integrate/cache_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags testharness ./internal/integrate/ -run Test_resolveCacheConfig -v`
Expected: FAIL — `cacheConfig`, `resolveCacheConfig` undefined.

- [ ] **Step 3: Implement**

Create `internal/integrate/cache.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags testharness ./internal/integrate/ -run Test_resolveCacheConfig -v`
Expected: PASS on all 8 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache.go internal/integrate/cache_test.go
git commit -m "feat(cache): resolveCacheConfig — merges CLI flags, env vars, and defaults"
```

---

## Task 3: cacheKey (pure)

**Files:**
- Modify: `internal/integrate/cache.go`, `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/integrate/cache_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags testharness ./internal/integrate/ -run Test_cacheKey -v`
Expected: FAIL — `cacheKey` undefined.

- [ ] **Step 3: Implement**

Append to `internal/integrate/cache.go`:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	// ... existing imports
)

// cacheKey derives a stable filesystem-safe identifier for an upstream URL,
// using NormalizeUpstreamURL for canonicalization so SSH/HTTPS variants and
// case-insensitive host names collapse to the same key. Result is the
// lowercase hex encoding of sha256(canonicalized-url), length 64.
func cacheKey(url string) string {
	canonical := NormalizeUpstreamURL(url, "")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
```

Also add `"crypto/sha256"` and `"encoding/hex"` to the existing import block if they aren't already there.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags testharness ./internal/integrate/ -run Test_cacheKey -v`
Expected: PASS on all 4 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache.go internal/integrate/cache_test.go
git commit -m "feat(cache): cacheKey — sha256(NormalizeUpstreamURL(url)) as filesystem-safe id"
```

---

## Task 4: cacheEntryPaths (pure)

**Files:**
- Modify: `internal/integrate/cache.go`, `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/integrate/cache_test.go`:

```go
func Test_cacheEntryPaths(t *testing.T) {
	dir, ts, lock := cacheEntryPaths("/var/cache/gitspork/repos", "abc123")
	assert.Equal(t, "/var/cache/gitspork/repos/abc123", dir)
	assert.Equal(t, "/var/cache/gitspork/repos/abc123.fetched-at", ts)
	assert.Equal(t, "/var/cache/gitspork/repos/abc123.lock", lock)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags testharness ./internal/integrate/ -run Test_cacheEntryPaths -v`
Expected: FAIL — `cacheEntryPaths` undefined.

- [ ] **Step 3: Implement**

Append to `internal/integrate/cache.go`:

```go
// cacheEntryPaths returns the three filesystem paths associated with a cache
// entry: the bare-mirror directory itself, the .fetched-at sidecar timestamp
// file, and the .lock sentinel used by the per-URL flock.
func cacheEntryPaths(root, key string) (dir, tsFile, lockFile string) {
	dir = filepath.Join(root, key)
	tsFile = filepath.Join(root, key+".fetched-at")
	lockFile = filepath.Join(root, key+".lock")
	return
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags testharness ./internal/integrate/ -run Test_cacheEntryPaths -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache.go internal/integrate/cache_test.go
git commit -m "feat(cache): cacheEntryPaths — dir/timestamp/lock path derivation"
```

---

## Task 5: isCacheFresh (pure)

**Files:**
- Modify: `internal/integrate/cache.go`, `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/integrate/cache_test.go`:

```go
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
		{name: "fetched exactly at boundary → fresh (<= is inclusive)", fetchedAt: now.Add(-2 * time.Hour), ttl: 2 * time.Hour, want: true},
		{name: "zero TTL → never fresh (any age is stale)", fetchedAt: now, ttl: 0, want: false},
		{name: "negative TTL → never fresh", fetchedAt: now, ttl: -1 * time.Second, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isCacheFresh(tc.fetchedAt, tc.ttl))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags testharness ./internal/integrate/ -run Test_isCacheFresh -v`
Expected: FAIL — `isCacheFresh` undefined.

- [ ] **Step 3: Implement**

Append to `internal/integrate/cache.go`:

```go
// isCacheFresh reports whether a cache entry whose last fetch happened at
// fetchedAt is still within the configured TTL. A ttl of 0 (or negative) is
// treated as "never fresh" — any positive age causes a refresh.
func isCacheFresh(fetchedAt time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	return time.Since(fetchedAt) <= ttl
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags testharness ./internal/integrate/ -run Test_isCacheFresh -v`
Expected: PASS on all 5 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache.go internal/integrate/cache_test.go
git commit -m "feat(cache): isCacheFresh — age vs TTL predicate"
```

---

## Task 6: readFetchedAt / writeFetchedAt

**Files:**
- Modify: `internal/integrate/cache.go`, `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/integrate/cache_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags testharness ./internal/integrate/ -run 'Test_(fetchedAtRoundTrip|readFetchedAt_)' -v`
Expected: FAIL — `readFetchedAt`, `writeFetchedAt` undefined.

- [ ] **Step 3: Implement**

Append to `internal/integrate/cache.go`:

```go
import (
	"strconv"
	// ... existing imports
)

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
		return time.Time{}, fmt.Errorf("parsing timestamp from %s: %v", path, err)
	}
	return time.Unix(secs, 0), nil
}

// writeFetchedAt records t as a Unix-timestamp sidecar file. Callers already
// hold the per-URL flock when this is invoked, so no atomicity beyond
// os.WriteFile is required.
func writeFetchedAt(path string, t time.Time) error {
	return os.WriteFile(path, []byte(strconv.FormatInt(t.Unix(), 10)), 0644)
}
```

Also add `"strconv"` and `"strings"` to the imports if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags testharness ./internal/integrate/ -run 'Test_(fetchedAtRoundTrip|readFetchedAt_)' -v`
Expected: PASS on all 3 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache.go internal/integrate/cache_test.go
git commit -m "feat(cache): fetched-at sidecar file read/write"
```

---

## Task 7: In-process flock singleton

**Files:**
- Create: `internal/integrate/cache_lock.go`
- Modify: `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/integrate/cache_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags testharness ./internal/integrate/ -run Test_getOrCreateFlock -v`
Expected: FAIL — `getOrCreateFlock` undefined.

- [ ] **Step 3: Implement**

Create `internal/integrate/cache_lock.go`:

```go
package integrate

import (
	"sync"

	"github.com/gofrs/flock"
)

// In-process singleton registry of *flock.Flock instances keyed by lock-file
// path. POSIX flock(2) is per-open-file-description (not per-process): two
// goroutines in the same process each calling flock.New(path).Lock() would
// obtain separate fds and could BOTH claim the lock simultaneously. Routing
// every in-process caller through the same *flock.Flock instance for a given
// path resolves that — flock's own mutex serialises the shared instance.
//
// Cross-process callers each construct their own map entry in their own
// address space; the OS flock coordinates them via the kernel.
var (
	flocksMu sync.Mutex
	flocks   = map[string]*flock.Flock{}
)

func getOrCreateFlock(path string) *flock.Flock {
	flocksMu.Lock()
	defer flocksMu.Unlock()
	if f, ok := flocks[path]; ok {
		return f
	}
	f := flock.New(path)
	flocks[path] = f
	return f
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags testharness ./internal/integrate/ -run Test_getOrCreateFlock -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache_lock.go internal/integrate/cache_test.go
git commit -m "feat(cache): in-process flock singleton via package-level map"
```

---

## Task 8: populateCache — git clone --mirror wrapper

**Files:**
- Modify: `internal/integrate/cache.go`, `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/integrate/cache_test.go`:

```go
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
```

Note: this test uses `testharness.MinimalUpstream` and `gogit`. Ensure the import block at the top of `internal/integrate/cache_test.go` includes:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags testharness ./internal/integrate/ -run Test_populateCache -v`
Expected: FAIL — `populateCache` undefined.

- [ ] **Step 3: Implement**

Append to `internal/integrate/cache.go`:

```go
import (
	// existing imports
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

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
		return fmt.Errorf("cloning mirror for upstream cache at %s: %v", dir, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags testharness ./internal/integrate/ -run Test_populateCache -v`
Expected: PASS on both subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache.go internal/integrate/cache_test.go
git commit -m "feat(cache): populateCache — git clone --mirror wrapper"
```

---

## Task 9: refreshCache — git fetch --prune wrapper

**Files:**
- Modify: `internal/integrate/cache.go`, `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/integrate/cache_test.go`:

```go
func Test_refreshCache_picksUpNewUpstreamCommits(t *testing.T) {
	upstreamDir, firstHash := testharness.MinimalUpstream(t)
	cacheDir := filepath.Join(t.TempDir(), "cache-entry")

	// Initial populate.
	require.NoError(t, populateCache(cacheDir, "file://"+upstreamDir, nil))

	// Advance the upstream with a new commit.
	newFilePath := filepath.Join(upstreamDir, "added-later.txt")
	require.NoError(t, os.WriteFile(newFilePath, []byte("later"), 0644))
	upstreamRepo, err := gogit.PlainOpen(upstreamDir)
	require.NoError(t, err)
	secondHash := testharness.CommitAllWithMessage(t, upstreamRepo, "add another file")

	// Before refresh, cache has only firstHash.
	cacheRepo, err := gogit.PlainOpen(cacheDir)
	require.NoError(t, err)
	_, err = cacheRepo.CommitObject(secondHash)
	assert.Error(t, err, "second commit must NOT be present before refresh")

	// Refresh, then the cache carries secondHash too.
	require.NoError(t, refreshCache(cacheDir, nil))
	_, err = cacheRepo.CommitObject(secondHash)
	assert.NoError(t, err, "second commit must be reachable after refresh")

	// And the original commit is still there.
	_, err = cacheRepo.CommitObject(firstHash)
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags testharness ./internal/integrate/ -run Test_refreshCache -v`
Expected: FAIL — `refreshCache` undefined.

- [ ] **Step 3: Implement**

Append to `internal/integrate/cache.go`:

```go
import (
	// existing imports
	gitconfig "github.com/go-git/go-git/v6/config"
)

// refreshCache runs the equivalent of `git fetch --prune` against the origin
// remote of an existing bare mirror at dir. Called when a cache entry is
// stale beyond its TTL. Callers must hold the per-URL flock while this
// runs — git-fetch inside the mirror is not safe against concurrent writers.
func refreshCache(dir string, auth transport.AuthMethod) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("opening upstream cache at %s: %v", dir, err)
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
		return fmt.Errorf("fetching into upstream cache at %s: %v", dir, err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags testharness ./internal/integrate/ -run Test_refreshCache -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache.go internal/integrate/cache_test.go
git commit -m "feat(cache): refreshCache — git fetch --prune wrapper"
```

---

## Task 10: ensureUpstreamCache orchestrator

**Files:**
- Modify: `internal/integrate/cache.go`, `internal/integrate/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/integrate/cache_test.go`:

```go
func Test_ensureUpstreamCache_disabled_returnsEmpty(t *testing.T) {
	cfg := cacheConfig{Disabled: true}
	dir, err := ensureUpstreamCache(cfg, "file:///somewhere", nil, sdktypes.NoopLogger())
	require.NoError(t, err)
	assert.Empty(t, dir, "disabled cache must return empty dir (caller falls back to direct clone)")
}

func Test_ensureUpstreamCache_missingEntry_populates(t *testing.T) {
	upstreamDir, _ := testharness.MinimalUpstream(t)
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 2 * time.Hour}

	dir, err := ensureUpstreamCache(cfg, "file://"+upstreamDir, nil, sdktypes.NoopLogger())
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
	dir1, err := ensureUpstreamCache(cfg, "file://"+upstreamDir, nil, sdktypes.NoopLogger())
	require.NoError(t, err)

	// Advance upstream — the fresh cache must NOT pick this up.
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "later.txt"), []byte("x"), 0644))
	upstreamRepo, err := gogit.PlainOpen(upstreamDir)
	require.NoError(t, err)
	newHash := testharness.CommitAllWithMessage(t, upstreamRepo, "advance")

	// Second call within TTL: no fetch.
	dir2, err := ensureUpstreamCache(cfg, "file://"+upstreamDir, nil, sdktypes.NoopLogger())
	require.NoError(t, err)
	assert.Equal(t, dir1, dir2)

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

	_, err := ensureUpstreamCache(cfg, "file://"+upstreamDir, nil, sdktypes.NoopLogger())
	require.NoError(t, err)

	// Advance upstream and re-run — the tiny TTL forces a fetch.
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "later.txt"), []byte("x"), 0644))
	upstreamRepo, err := gogit.PlainOpen(upstreamDir)
	require.NoError(t, err)
	newHash := testharness.CommitAllWithMessage(t, upstreamRepo, "advance")
	time.Sleep(2 * time.Nanosecond) // ensure now > fetched-at + ttl

	dir2, err := ensureUpstreamCache(cfg, "file://"+upstreamDir, nil, sdktypes.NoopLogger())
	require.NoError(t, err)

	repo, err := gogit.PlainOpen(dir2)
	require.NoError(t, err)
	_, err = repo.CommitObject(newHash)
	assert.NoError(t, err, "stale cache must have been refreshed and now carries new commits")
}

func Test_ensureUpstreamCache_corruptEntry_wipesAndRetries(t *testing.T) {
	upstreamDir, upstreamHash := testharness.MinimalUpstream(t)
	root := t.TempDir()
	cfg := cacheConfig{Root: root, TTL: 2 * time.Hour}
	key := cacheKey("file://" + upstreamDir)
	dir, tsFile, _ := cacheEntryPaths(root, key)

	// Fabricate a corrupt "cache entry" — a directory that looks like a repo
	// but isn't, plus a stale timestamp so the code thinks it's fresh.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "objects"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "HEAD"), []byte("garbage"), 0644))
	require.NoError(t, writeFetchedAt(tsFile, time.Now())) // fresh

	// The freshness check will short-circuit to "fresh, no fetch". Then a
	// downstream working clone would fail. To exercise the wipe-and-retry
	// path, force staleness by setting TTL to 0.
	cfg.TTL = 1 * time.Nanosecond
	time.Sleep(2 * time.Nanosecond)

	returnedDir, err := ensureUpstreamCache(cfg, "file://"+upstreamDir, nil, sdktypes.NoopLogger())
	require.NoError(t, err, "corrupt cache must be wiped and repopulated, not surfaced as an error")
	assert.Equal(t, dir, returnedDir)

	// Post-recovery, the cache is a real mirror carrying the upstream's HEAD.
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
		_, err := ensureUpstreamCache(cfg, "file:///absolutely-nonexistent-path-xyzzy", nil, sdktypes.NoopLogger())
		require.Error(t, err)
	}()

	select {
	case <-done:
		// Fine — errored out promptly.
	case <-time.After(10 * time.Second):
		t.Fatal("ensureUpstreamCache did not surface an error within 10s — retry loop is not bounded")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags testharness ./internal/integrate/ -run Test_ensureUpstreamCache -v`
Expected: FAIL — `ensureUpstreamCache` undefined.

- [ ] **Step 3: Implement**

Append to `internal/integrate/cache.go`:

```go
import (
	// existing imports
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

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
		return "", fmt.Errorf("creating upstream cache root %s: %v", cfg.Root, err)
	}

	key := cacheKey(url)
	dir, tsFile, lockFile := cacheEntryPaths(cfg.Root, key)

	fl := getOrCreateFlock(lockFile)
	if err := fl.Lock(); err != nil {
		return "", fmt.Errorf("acquiring upstream cache lock at %s: %v", lockFile, err)
	}
	defer func() { _ = fl.Unlock() }()

	// First attempt.
	if err := runCacheOp(dir, tsFile, url, cfg.TTL, auth, logger); err != nil {
		// Wipe and retry once.
		_ = os.RemoveAll(dir)
		_ = os.Remove(tsFile)
		if err := populateCache(dir, url, auth); err != nil {
			return "", fmt.Errorf("upstream cache populate failed after wipe-and-retry: %v", err)
		}
		if err := writeFetchedAt(tsFile, time.Now()); err != nil {
			return "", fmt.Errorf("writing upstream cache timestamp after recovery: %v", err)
		}
		logger.Log("populating upstream cache for %s at %s (after corruption recovery)", url, dir)
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags testharness ./internal/integrate/ -run Test_ensureUpstreamCache -v`
Expected: PASS on all 6 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/cache.go internal/integrate/cache_test.go
git commit -m "feat(cache): ensureUpstreamCache orchestrator with retry-once corruption recovery"
```

---

## Task 11: SDK options for cache

**Files:**
- Modify: `internal/sdktypes/options.go`

- [ ] **Step 1: Read the current options file**

Run: `cat internal/sdktypes/options.go`
Note the existing struct shape — you'll add two fields to three structs.

- [ ] **Step 2: Add fields to IntegrateOptions, IntegrateLocalOptions, CheckDriftOptions**

Modify `internal/sdktypes/options.go` — add these two fields to each of the three option types:

```go
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
```

Also add `"time"` to the import block if not already present.

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/sdktypes/options.go
git commit -m "feat(sdk): CacheTTL and NoCache on Integrate/IntegrateLocal/CheckDrift options"
```

---

## Task 12: Wire ensureUpstreamCache into cloneUpstreamForIntegrate

**Files:**
- Modify: `internal/integrate/integrate.go`

- [ ] **Step 1: Add cacheTTL / noCache to internalRequest**

Locate the `internalRequest` struct in `internal/integrate/integrate.go` (search for `type internalRequest struct`). Add two fields at the end:

```go
type internalRequest struct {
	Logger                 sdktypes.Logger
	DownstreamRepoPath     string
	ForceRePrompt          bool
	forDriftCheck          bool
	upstreamCommit         string
	prevUpstreamCommitHash string

	// Cache controls, propagated from IntegrateOptions / CheckDriftOptions.
	cacheTTL time.Duration
	noCache  bool
}
```

- [ ] **Step 2: Propagate cache options in integrateOne**

Find `func integrateOne(opts *sdktypes.IntegrateOptions, ...)` and update its `internalRequest` construction to include the cache fields:

```go
req := &internalRequest{
	Logger:             opts.Logger,
	DownstreamRepoPath: opts.DownstreamRepoPath,
	ForceRePrompt:      opts.ForceRePrompt,
	cacheTTL:           opts.CacheTTL,
	noCache:            opts.NoCache,
}
```

- [ ] **Step 3: Propagate cache options in the nestedReq construction inside integrateOneInternal**

Find the `nestedReq := &internalRequest{...}` construction inside `integrateOneInternal` (used for the delta-computation clone) and add the cache fields:

```go
nestedReq := &internalRequest{
	Logger:                 req.Logger,
	DownstreamRepoPath:     req.DownstreamRepoPath,
	ForceRePrompt:          req.ForceRePrompt,
	forDriftCheck:          req.forDriftCheck,
	upstreamCommit:         req.upstreamCommit,
	prevUpstreamCommitHash: prevHash,
	cacheTTL:               req.cacheTTL,
	noCache:                req.noCache,
}
```

- [ ] **Step 4: Modify cloneUpstreamForIntegrate to consult the cache**

At the top of `cloneUpstreamForIntegrate`, after resolving `upstreamURL` and `authMethod` but BEFORE constructing `cloneOptions`, insert cache resolution:

```go
// Resolve cache configuration (merging CLI/env/defaults).
cacheCfg, err := resolveCacheConfig(req.cacheTTL, req.noCache)
if err != nil {
	return "", err
}

// If enabled, ensure the machine cache has a healthy mirror for this URL.
// Empty cacheDir means "cache disabled or bypassed" — fall through to a
// direct network clone below.
cacheDir, err := ensureUpstreamCache(cacheCfg, upstreamURL, authMethod, req.Logger)
if err != nil {
	return "", err
}
```

Then, further down where `cloneOptions.URL = upstreamURL` is set, replace it with the cache-aware selection:

```go
cloneOptions := &git.CloneOptions{
	URL:      upstreamURL,
	Progress: &logutil.LoggerWriter{L: req.Logger},
}
if cacheDir != "" {
	// Working clone reads from the local bare mirror. No network, no auth.
	cloneOptions.URL = "file://" + cacheDir
	cloneOptions.Auth = nil
} else if authMethod != nil {
	cloneOptions.Auth = authMethod
}
```

**Do NOT** delete the existing auth-setting block above `cloneOptions` (used for the direct-clone path); the block above is now the fallback path.

- [ ] **Step 5: Retry the working PlainClone once on failure**

The `git.PlainClone(cloneDir, cloneOptions)` call further down handles the working clone. Wrap it in a bounded retry to handle rare mid-fetch-prune races (per spec Section 4, failure boundary 3):

```go
var repo *git.Repository
{
	var cloneErr error
	repo, cloneErr = git.PlainClone(cloneDir, cloneOptions)
	if cloneErr != nil && cacheDir != "" {
		// Rare: a concurrent fetch-prune in the cache deleted a ref this
		// working clone snapshotted. Retry once against the same cache; the
		// deleting fetch is one-shot so a second attempt has fresh refs.
		_ = os.RemoveAll(cloneDir)
		if err := os.MkdirAll(cloneDir, 0755); err != nil {
			return "", fmt.Errorf("re-creating clone dir after cache-race retry: %v", err)
		}
		repo, cloneErr = git.PlainClone(cloneDir, cloneOptions)
	}
	if cloneErr != nil {
		return "", fmt.Errorf("error cloning upstream gitspork repo: %v", cloneErr)
	}
}
```

Replace the original single `git.PlainClone(cloneDir, cloneOptions)` call and its error handling with the block above.

- [ ] **Step 6: Verify all existing unit tests still pass**

Run: `make test-unit`
Expected: all tests pass. Any failures are regressions in existing behaviour — investigate before proceeding.

- [ ] **Step 7: Commit**

```bash
git add internal/integrate/integrate.go
git commit -m "feat(cache): wire ensureUpstreamCache into cloneUpstreamForIntegrate"
```

---

## Task 13: Wire IntegrateForDriftCheck

**Files:**
- Modify: `internal/integrate/drift_check.go`

- [ ] **Step 1: Read the current file**

Run: `cat internal/integrate/drift_check.go`

- [ ] **Step 2: Add fields to DriftCheckRequest**

Add these two fields at the end of the `DriftCheckRequest` struct:

```go
type DriftCheckRequest struct {
	Logger             sdktypes.Logger
	DownstreamRepoPath string
	UpstreamURL        string
	UpstreamSubpath    string
	UpstreamToken      string
	UpstreamCommit     string
	CacheTTL           time.Duration
	NoCache            bool
}
```

Add `"time"` to imports if not present.

- [ ] **Step 3: Propagate cache options into internalRequest**

Find where the `internalRequest` is constructed inside `IntegrateForDriftCheck` and add the two cache fields:

```go
req := &internalRequest{
	Logger:             r.Logger,
	DownstreamRepoPath: r.DownstreamRepoPath,
	forDriftCheck:      true,
	upstreamCommit:     r.UpstreamCommit,
	cacheTTL:           r.CacheTTL,
	noCache:            r.NoCache,
}
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/integrate/drift_check.go
git commit -m "feat(cache): DriftCheckRequest carries CacheTTL/NoCache into internalRequest"
```

---

## Task 14: Wire CheckDrift

**Files:**
- Modify: `internal/drift/check_drift.go`

- [ ] **Step 1: Find the IntegrateForDriftCheck call site**

Search for `IntegrateForDriftCheck` in `internal/drift/check_drift.go`. There will be one call inside the per-entry loop.

- [ ] **Step 2: Pass CacheTTL and NoCache from CheckDriftOptions**

Add `CacheTTL` and `NoCache` to the `integrate.DriftCheckRequest` literal at that call site:

```go
if err := integrate.IntegrateForDriftCheck(&integrate.DriftCheckRequest{
	Logger:             opts.Logger,
	DownstreamRepoPath: opts.DownstreamRepoPath,
	UpstreamURL:        entry.spec.URL,
	UpstreamSubpath:    entry.spec.Subpath,
	UpstreamToken:      entry.spec.Token,
	UpstreamCommit:     entry.commitHash,
	CacheTTL:           opts.CacheTTL,
	NoCache:            opts.NoCache,
}); err != nil {
```

- [ ] **Step 3: Verify existing tests still pass**

Run: `make test-unit`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/drift/check_drift.go
git commit -m "feat(cache): CheckDrift propagates CacheTTL/NoCache to IntegrateForDriftCheck"
```

---

## Task 15: CLI flags on `gitspork integrate`

**Files:**
- Modify: `internal/cli/integrate.go`

- [ ] **Step 1: Read the current file**

Run: `cat internal/cli/integrate.go`
Note where the existing flags are bound (search for `PersistentFlags` or `Flags()`).

- [ ] **Step 2: Add the two flag bindings**

Inside `GetCmd()`, at the end of the flag-registration section, add:

```go
cmd.PersistentFlags().Duration("cache-ttl", 0,
	"upstream mirror cache freshness threshold (e.g. 2h, 30m); if a cached upstream is younger than this, no fetch is performed. "+
		"Zero-value means 'use GITSPORK_CACHE_TTL env if set, else 2h'. Use --no-cache to bypass entirely.")
cmd.PersistentFlags().Bool("no-cache", false,
	"bypass the upstream mirror cache entirely — direct network clone on every invocation. Overrides --cache-ttl.")
```

- [ ] **Step 3: Read the flag values in RunE**

Inside the command's `RunE` closure, near the existing option construction, read the two new flags:

```go
cacheTTL, err := cmd.Flags().GetDuration("cache-ttl")
if err != nil {
	return err
}
noCache, err := cmd.Flags().GetBool("no-cache")
if err != nil {
	return err
}
```

- [ ] **Step 4: Pass them into IntegrateOptions**

Find the `sdktypes.IntegrateOptions{...}` construction and add:

```go
CacheTTL: cacheTTL,
NoCache:  noCache,
```

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: no output.

Also run: `go run ./cmd/gitspork integrate --help | grep cache`
Expected: two lines showing `--cache-ttl` and `--no-cache`.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/integrate.go
git commit -m "feat(cache): --cache-ttl and --no-cache on gitspork integrate"
```

---

## Task 16: CLI flags on `gitspork check-drift`

**Files:**
- Modify: `internal/cli/check_drift.go`

- [ ] **Step 1: Add the two flag bindings**

Inside `GetCmd()`, add (mirroring Task 15):

```go
cmd.PersistentFlags().Duration("cache-ttl", 0,
	"upstream mirror cache freshness threshold (e.g. 2h, 30m); if a cached upstream is younger than this, no fetch is performed. "+
		"Zero-value means 'use GITSPORK_CACHE_TTL env if set, else 2h'. Use --no-cache to bypass entirely.")
cmd.PersistentFlags().Bool("no-cache", false,
	"bypass the upstream mirror cache entirely — direct network clone on every invocation. Overrides --cache-ttl.")
```

- [ ] **Step 2: Read flag values in RunE and pass into CheckDriftOptions**

Same pattern as Task 15 — read via `cmd.Flags().GetDuration("cache-ttl")` and `cmd.Flags().GetBool("no-cache")`, then pass into `sdktypes.CheckDriftOptions{CacheTTL: cacheTTL, NoCache: noCache, ...}`.

- [ ] **Step 3: Verify build and help output**

Run: `go build ./... && go run ./cmd/gitspork check-drift --help | grep cache`
Expected: two lines showing `--cache-ttl` and `--no-cache`.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/check_drift.go
git commit -m "feat(cache): --cache-ttl and --no-cache on gitspork check-drift"
```

---

## Task 17: `gitspork cache dir` subcommand

**Files:**
- Create: `internal/cli/cache.go`
- Create: `internal/cli/cache_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cache_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_cacheDirCommand_printsResolvedRoot(t *testing.T) {
	t.Setenv("GITSPORK_CACHE_DIR", "/tmp/test-cache-dir")

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"dir"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "/tmp/test-cache-dir\n", out.String())
}

func Test_cacheDirCommand_defaultRoot(t *testing.T) {
	t.Setenv("GITSPORK_CACHE_DIR", "")

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"dir"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())

	// Should print a path ending in gitspork/repos.
	printed := strings.TrimSpace(out.String())
	userCache, _ := os.UserCacheDir()
	assert.Equal(t, userCache+"/gitspork/repos", printed)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run Test_cacheDirCommand -v`
Expected: FAIL — `CacheSubcommand` undefined.

- [ ] **Step 3: Implement**

Create `internal/cli/cache.go`:

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// CacheSubcommand represents `gitspork cache` and its children.
type CacheSubcommand struct{}

// GetCmd returns the cobra command tree for `gitspork cache`.
func (s *CacheSubcommand) GetCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cache",
		Short: "manage the machine-scoped upstream mirror cache",
	}
	root.AddCommand(s.dirCmd())
	return root
}

// dirCmd is `gitspork cache dir` — prints the resolved cache root.
func (s *CacheSubcommand) dirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dir",
		Short: "print the resolved upstream mirror cache root",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveCacheRoot()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), root)
			return nil
		},
	}
}

// resolveCacheRoot mirrors internal/integrate/cache.go's own resolution but
// without pulling in the whole cacheConfig plumbing. Duplicated because the
// CLI package can't import internal/integrate/cache.go's unexported helpers.
func resolveCacheRoot() (string, error) {
	if root := os.Getenv("GITSPORK_CACHE_DIR"); root != "" {
		return root, nil
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache dir: %v", err)
	}
	return filepath.Join(userCache, "gitspork", "repos"), nil
}
```

- [ ] **Step 4: Register the subcommand in root.go**

Open `internal/cli/root.go` and find where other subcommands are registered (e.g., `rootCmd.AddCommand((&IntegrateSubcommand{}).GetCmd())`). Add:

```go
rootCmd.AddCommand((&CacheSubcommand{}).GetCmd())
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run Test_cacheDirCommand -v`
Expected: PASS on both subtests.

- [ ] **Step 6: Verify wired-up help**

Run: `go run ./cmd/gitspork cache dir`
Expected: prints your user cache dir + `/gitspork/repos` on a single line.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cache.go internal/cli/cache_test.go internal/cli/root.go
git commit -m "feat(cache): gitspork cache dir subcommand"
```

---

## Task 18: `gitspork cache clear` subcommand

**Files:**
- Modify: `internal/cli/cache.go`, `internal/cli/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/cache_test.go`:

```go
func Test_cacheClearCommand_wipesRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", dir)
	// Populate with a couple of fake entries.
	require.NoError(t, os.MkdirAll(dir+"/entry1", 0755))
	require.NoError(t, os.WriteFile(dir+"/entry1/HEAD", []byte("x"), 0644))
	require.NoError(t, os.WriteFile(dir+"/entry1.fetched-at", []byte("123"), 0644))
	require.NoError(t, os.WriteFile(dir+"/entry1.lock", nil, 0644))

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"clear", "--force"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())

	// Cache root is either empty or removed entirely.
	entries, err := os.ReadDir(dir)
	if err == nil {
		assert.Empty(t, entries, "cache root must be empty after clear --force")
	}
}

func Test_cacheClearCommand_wipesSingleURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", dir)

	// Two entries. Only the first should be wiped.
	url := "file:///some/upstream"
	// The CLI package can import internal/integrate, so we call the exported
	// wrapper directly to compute the expected on-disk key.
	key := integrate.CacheKeyForURL(url)
	require.NoError(t, os.MkdirAll(dir+"/"+key, 0755))
	require.NoError(t, os.WriteFile(dir+"/"+key+".fetched-at", []byte("123"), 0644))
	require.NoError(t, os.WriteFile(dir+"/other-entry.fetched-at", []byte("456"), 0644))

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"clear", "--url", url, "--force"})
	require.NoError(t, cmd.Execute())

	assert.NoFileExists(t, dir+"/"+key)
	assert.NoFileExists(t, dir+"/"+key+".fetched-at")
	assert.FileExists(t, dir+"/other-entry.fetched-at", "unrelated entry must survive")
}

func Test_cacheClearCommand_nonTTYWithoutForce_fails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", dir)
	require.NoError(t, os.WriteFile(dir+"/entry.fetched-at", []byte("123"), 0644))

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"clear"}) // no --force
	// SetIn to a bytes.Buffer so isatty reports false.
	cmd.SetIn(&bytes.Buffer{})
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force", "error must direct the user to --force")
	assert.FileExists(t, dir+"/entry.fetched-at", "entry must NOT be wiped without confirmation/force")
}

// sha256HexOfNormalizedURL is a test-local reimplementation of the cache-key
// derivation so this test file can compute the expected on-disk path
// without importing internal/integrate.
func sha256HexOfNormalizedURL(url string) string {
	// The CLI test uses this to predict a cache path; the actual normalization
	// happens in internal/integrate.NormalizeUpstreamURL. For test purposes
	// with a file:// URL that has no trailing .git, the normalization is
	// close to identity — but for correctness this helper needs to match.
	// Import path: github.com/rockholla/gitspork/v2/internal/integrate.
	return integrateCacheKey(url)
}
```

Also add these imports to the test file if not already present:

```go
import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/integrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 2: Export a small `CacheKeyForURL` helper for the CLI test**

In `internal/integrate/cache.go`, add a small exported wrapper so `internal/cli` can compute cache keys without duplicating the algorithm:

```go
// CacheKeyForURL is the exported name of cacheKey, provided so the CLI
// package can compute cache-entry paths in tests without duplicating the
// hashing logic. Not part of the public SDK — the wrapper lives in
// internal/ so external consumers do not see it.
func CacheKeyForURL(url string) string {
	return cacheKey(url)
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run Test_cacheClearCommand -v`
Expected: FAIL — clear subcommand not implemented.

- [ ] **Step 4: Add `golang.org/x/term` for the TTY check**

Run: `go get golang.org/x/term`
Expected: `go.mod` picks up `golang.org/x/term`.

- [ ] **Step 5: Implement `clearCmd` and helpers**

Append to `internal/cli/cache.go`:

```go
import (
	// existing imports
	"bufio"
	"io"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/rockholla/gitspork/v2/internal/integrate"
	"golang.org/x/term"
)

// isTerminalFn is a variable so tests can override it. Production callers
// go through golang.org/x/term.IsTerminal.
var isTerminalFn = term.IsTerminal

func (s *CacheSubcommand) clearCmd() *cobra.Command {
	var urlFlag string
	var forceFlag bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "wipe cached upstream entries from disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveCacheRoot()
			if err != nil {
				return err
			}

			targets, err := resolveClearTargets(root, urlFlag)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				// Nothing to do.
				return nil
			}

			// TTY check: if not a TTY and --force wasn't passed, fail loud.
			if !forceFlag && !isTTY(cmd.InOrStdin()) {
				return fmt.Errorf("cannot clear cache non-interactively without --force; add --force to confirm")
			}
			if !forceFlag {
				// Interactive prompt.
				fmt.Fprintf(cmd.OutOrStdout(), "About to wipe %d cache entry(ies) under %s:\n", len(targets), root)
				for _, t := range targets {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", t.dir)
				}
				fmt.Fprint(cmd.OutOrStdout(), "Proceed? [y/N] ")
				reader := bufio.NewReader(cmd.InOrStdin())
				line, _ := reader.ReadString('\n')
				if strings.ToLower(strings.TrimSpace(line)) != "y" {
					fmt.Fprintln(cmd.OutOrStdout(), "aborted")
					return nil
				}
			}

			for _, t := range targets {
				fl := flock.New(t.lockFile)
				if err := fl.Lock(); err != nil {
					return fmt.Errorf("acquiring lock for %s: %v", t.dir, err)
				}
				removeErr := os.RemoveAll(t.dir)
				_ = os.Remove(t.tsFile)
				_ = fl.Unlock()
				_ = os.Remove(t.lockFile)
				if removeErr != nil {
					return fmt.Errorf("removing %s: %v", t.dir, removeErr)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "clear only the entry matching this upstream URL")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "skip the interactive confirmation prompt (required in non-TTY runs)")
	return cmd
}

type clearTarget struct {
	dir, tsFile, lockFile string
}

// resolveClearTargets enumerates cache entries to wipe. If urlFlag is set,
// resolves to at most one target (may be empty if no entry exists for that
// URL). Otherwise enumerates every entry under root.
func resolveClearTargets(root, urlFlag string) ([]clearTarget, error) {
	if urlFlag != "" {
		key := integrate.CacheKeyForURL(urlFlag)
		dir := filepath.Join(root, key)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
		return []clearTarget{{
			dir:      dir,
			tsFile:   filepath.Join(root, key+".fetched-at"),
			lockFile: filepath.Join(root, key+".lock"),
		}}, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var targets []clearTarget
	for _, e := range entries {
		if !e.IsDir() {
			continue // skip sidecar files; they get removed alongside their dirs
		}
		key := e.Name()
		targets = append(targets, clearTarget{
			dir:      filepath.Join(root, key),
			tsFile:   filepath.Join(root, key+".fetched-at"),
			lockFile: filepath.Join(root, key+".lock"),
		})
	}
	return targets, nil
}

// isTTY reports whether r appears to be an interactive terminal. bytes.Buffer
// (test input) does not implement Fd() and therefore reports false, which is
// exactly what the "non-TTY without --force fails" test wants.
func isTTY(r io.Reader) bool {
	type fdHaver interface{ Fd() uintptr }
	f, ok := r.(fdHaver)
	if !ok {
		return false
	}
	return isTerminalFn(int(f.Fd()))
}
```

Also register the clear subcommand — modify `GetCmd()` to add it:

```go
func (s *CacheSubcommand) GetCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cache",
		Short: "manage the machine-scoped upstream mirror cache",
	}
	root.AddCommand(s.dirCmd())
	root.AddCommand(s.clearCmd())
	return root
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run Test_cacheClearCommand -v`
Expected: PASS on all 3 subtests.

- [ ] **Step 7: Verify integration**

Run: `go run ./cmd/gitspork cache clear --help`
Expected: help output shows `--url` and `--force` flags.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cache.go internal/cli/cache_test.go internal/integrate/cache.go go.mod go.sum
git commit -m "feat(cache): gitspork cache clear subcommand with --url and --force"
```

---

## Task 19: Functional test — cache hit within TTL

**Files:**
- Create: `test/functional/cache_test.go`

- [ ] **Step 1: Write the failing test**

Create `test/functional/cache_test.go`:

```go
//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrate_cache_populatesAndHitsWithinTTL locks the primary cache
// contract: first integrate populates the cache; a second integrate within
// TTL performs NO network operation.
func TestIntegrate_cache_populatesAndHitsWithinTTL(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR; not plumbed through DockerRunner")
	}

	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// First integrate — populates the cache.
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "first integrate exited non-zero:\n%s", out)
	assert.Contains(t, out, "populating upstream cache",
		"first integrate must populate the cache")

	// Second integrate — cache is fresh (default 2h TTL), no network fetch.
	// Commit downstream so tree stays clean for the second integrate.
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-first-integrate")
	prepDownstreamWithInputData(t, downstreamDir)

	out, code = runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "second integrate exited non-zero:\n%s", out)
	assert.Contains(t, out, "upstream cache hit",
		"second integrate within TTL must hit the cache")
	assert.NotContains(t, out, "populating upstream cache",
		"second integrate must NOT re-populate")
	assert.NotContains(t, out, "refreshing upstream cache",
		"second integrate must NOT refresh (cache is fresh)")
}
```

- [ ] **Step 2: Run the test to verify it fails first, then passes**

Run: `go test -tags functional,testharness ./test/functional/ -run TestIntegrate_cache_populatesAndHitsWithinTTL -v`
Expected before Tasks 15/17 have fully landed: may fail because CLI hasn't been wired. After all prior tasks are in, expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/functional/cache_test.go
git commit -m "test(cache): functional — first-run populates, second within TTL hits"
```

---

## Task 20: Functional test — stale + --no-cache

**Files:**
- Modify: `test/functional/cache_test.go`

- [ ] **Step 1: Write the tests**

Append to `test/functional/cache_test.go`:

```go
func TestIntegrate_cache_staleTTL_refreshes(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR")
	}
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// First integrate populates.
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-first-integrate")
	prepDownstreamWithInputData(t, downstreamDir)

	// Second integrate with --cache-ttl 1ns: cache is instantly stale, refresh.
	args := append(integrateArgs(upstreamDir, downstreamDir), "--cache-ttl", "1ns")
	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "second integrate with tiny TTL failed:\n%s", out)
	assert.Contains(t, out, "refreshing upstream cache",
		"tiny TTL must trigger a refresh")
}

func TestIntegrate_cache_noCache_bypassesEntirely(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR")
	}
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	args := append(integrateArgs(upstreamDir, downstreamDir), "--no-cache")
	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "integrate --no-cache failed:\n%s", out)

	// None of the three cache log lines appear.
	assert.NotContains(t, out, "populating upstream cache")
	assert.NotContains(t, out, "refreshing upstream cache")
	assert.NotContains(t, out, "upstream cache hit")

	// And the cache dir is empty.
	entries, err := os.ReadDir(cacheDir)
	if err == nil {
		assert.Empty(t, entries, "no-cache run must leave the cache dir empty")
	}
}
```

Add `"os"` to the import block if it's not already there.

- [ ] **Step 2: Run the tests**

Run: `go test -tags functional,testharness ./test/functional/ -run 'TestIntegrate_cache_(staleTTL|noCache)' -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/functional/cache_test.go
git commit -m "test(cache): functional — --cache-ttl 1ns refreshes, --no-cache bypasses"
```

---

## Task 21: Functional test — cross-process fan-out

**Files:**
- Modify: `test/functional/cache_test.go`

- [ ] **Step 1: Write the test**

Append to `test/functional/cache_test.go`:

```go
func TestIntegrate_cache_crossProcess_singlePopulate(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR")
	}
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir := buildSimpleUpstream(t)

	// 4 concurrent gitspork subprocesses, each with its own downstream but
	// all against the same upstream URL. Locks in the cross-process flock
	// contract: exactly one populates the cache, the other three either
	// hit or wait-then-hit.
	const n = 4
	type outcome struct {
		stdout string
		code   int
	}
	results := make(chan outcome, n)

	for i := 0; i < n; i++ {
		go func() {
			downstreamDir := NewDownstreamRepo(t)
			prepDownstreamWithInputData(t, downstreamDir)
			runner := resolveRunner(t, upstreamDir, downstreamDir)
			out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
			results <- outcome{stdout: out, code: code}
		}()
	}

	populates := 0
	for i := 0; i < n; i++ {
		o := <-results
		require.Equal(t, 0, o.code, "goroutine %d failed:\n%s", i, o.stdout)
		if strings.Contains(o.stdout, "populating upstream cache") {
			populates++
		}
	}
	assert.Equal(t, 1, populates,
		"exactly one subprocess must have populated the cache; %d did", populates)
}
```

Add `"strings"` to imports if not present.

- [ ] **Step 2: Run the test**

Run: `go test -tags functional,testharness ./test/functional/ -run TestIntegrate_cache_crossProcess -v`
Expected: PASS. Populates count is exactly 1.

- [ ] **Step 3: Commit**

```bash
git add test/functional/cache_test.go
git commit -m "test(cache): functional — cross-process fan-out serialises on flock, one populate"
```

---

## Task 22: Functional test — cache subcommands

**Files:**
- Modify: `test/functional/cache_test.go`

- [ ] **Step 1: Write the tests**

Append to `test/functional/cache_test.go`:

```go
func TestCache_dirSubcommand_printsResolvedRoot(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR")
	}
	dir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", dir)

	runner := resolveRunner(t, "", "")
	out, code := runner.Run(t, []string{"cache", "dir"}, dir)
	require.Equal(t, 0, code, "cache dir exited non-zero:\n%s", out)
	assert.Equal(t, dir+"\n", out)
}

func TestCache_clearSubcommand_wipesAll(t *testing.T) {
	if isDockerBuild {
		t.Skip("cache tests require host-side GITSPORK_CACHE_DIR")
	}
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	// Populate by running one integrate.
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	_, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code)

	// Verify cache is non-empty before clearing.
	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "cache must be populated before clear test")

	// Clear.
	out, code := runner.Run(t, []string{"cache", "clear", "--force"}, cacheDir)
	require.Equal(t, 0, code, "cache clear --force exited non-zero:\n%s", out)

	// Cache is now empty (or the root dir may be absent).
	entries, err = os.ReadDir(cacheDir)
	if err == nil {
		assert.Empty(t, entries, "cache root must be empty after clear --force")
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test -tags functional,testharness ./test/functional/ -run TestCache_ -v`
Expected: PASS on both subtests.

- [ ] **Step 3: Commit**

```bash
git add test/functional/cache_test.go
git commit -m "test(cache): functional — gitspork cache dir + clear --force"
```

---

## Task 23: SDK tests for CacheTTL / NoCache

**Files:**
- Modify: `test/sdk/sdk_test.go`

- [ ] **Step 1: Write the tests**

Append to `test/sdk/sdk_test.go`:

```go
func TestIntegrate_cache_SDK_CacheTTL_honored(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	captured := &captureLogger{}
	// First integrate — populates.
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             captured,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)

	// Second integrate with tiny CacheTTL — refreshes.
	writeAndCommit(t, downstreamDir, ".gitspork/marker", "baseline")
	captured2 := &captureLogger{}
	_, err = gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             captured2,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
		CacheTTL:           1 * time.Nanosecond,
	})
	require.NoError(t, err)

	// Sanity: the SDK plumbs CacheTTL through to the cache orchestrator.
	assert.True(t, hasLog(captured2, "refreshing upstream cache"),
		"CacheTTL=1ns on the second integrate must trigger a refresh log line")
}

func TestIntegrate_cache_SDK_NoCache_bypasses(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", cacheDir)

	upstreamDir, _ := minimalUpstream(t)
	downstreamDir := emptyDownstream(t)

	captured := &captureLogger{}
	_, err := gitspork.Integrate(&gitspork.IntegrateOptions{
		Logger:             captured,
		Upstreams:          []gitspork.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
		NoCache:            true,
	})
	require.NoError(t, err)

	// None of the three cache log lines appear.
	assert.False(t, hasLog(captured, "populating upstream cache"))
	assert.False(t, hasLog(captured, "refreshing upstream cache"))
	assert.False(t, hasLog(captured, "upstream cache hit"))

	// Cache dir is empty.
	entries, err := os.ReadDir(cacheDir)
	if err == nil {
		assert.Empty(t, entries)
	}
}

// hasLog reports whether any captured entry contains substr.
func hasLog(c *captureLogger, substr string) bool {
	for _, e := range c.entries {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}
```

Add `"time"`, `"strings"`, `"os"` to imports if not present.

- [ ] **Step 2: Run the tests**

Run: `make test-sdk`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add test/sdk/sdk_test.go
git commit -m "test(cache): SDK — CacheTTL triggers refresh, NoCache bypasses"
```

---

## Task 24: Docs updates

**Files:**
- Modify: `README.md`
- Modify: `docs/README.md`
- Modify: `docs/examples/multi-upstream/README.md`

- [ ] **Step 1: Add machine-cache paragraph to top-level README under Features**

Open `README.md`. Find the Features list (bullets starting with `* **Upstream-Owned Resources**:` etc.). Add a new bullet:

```markdown
* **Machine-Level Upstream Cache**: subsequent `integrate` and `check-drift` invocations reuse a bare-mirror cache under your OS user cache directory (`os.UserCacheDir()`), only fetching from remote when the entry is older than the configured TTL (default: 2h). Purpose-built for coordinator scenarios that fan out across hundreds of downstreams against a small set of shared upstreams from one machine. Per-URL cross-process locking via `flock`. Opt-out via `--no-cache` or `GITSPORK_NO_CACHE`.
```

- [ ] **Step 2: Update `docs/README.md`**

Open `docs/README.md`. Find the CLI-flag table for `integrate` and add two rows:

```markdown
| `--cache-ttl` | Duration | `2h` | Upstream mirror cache freshness threshold. Cached entries younger than this are used as-is; older triggers `git fetch`. Set to `1ns` to force refresh; use `--no-cache` for full bypass. Env: `GITSPORK_CACHE_TTL`. |
| `--no-cache` | Bool | (unset) | Bypass the upstream mirror cache entirely. Env: `GITSPORK_NO_CACHE`. |
```

Add the same two rows to the `check-drift` CLI-flag table.

Add a new short section at the appropriate place (near the CLI docs or under a new "Cache management" heading):

```markdown
## Cache management

The machine-scoped upstream mirror cache lives under `os.UserCacheDir()` + `gitspork/repos/` by default. Override with `GITSPORK_CACHE_DIR`. Two subcommands manage it:

```bash
gitspork cache dir                             # print the resolved cache root
gitspork cache clear                           # interactive: prompt, then wipe all entries
gitspork cache clear --force                   # non-interactive: wipe all
gitspork cache clear --url <url> --force       # wipe one URL's entry
```

Non-TTY invocations of `clear` must pass `--force` (fail-loud rather than silently wipe in scripts).
```

- [ ] **Step 3: Add one sentence to the multi-upstream example README**

Open `docs/examples/multi-upstream/README.md`. In the "Real-world mapping" section, append one sentence to the paragraph about coordinator use:

```markdown
The machine-level upstream mirror cache (`~/.cache/gitspork/repos/` by default, TTL 2h) is what makes coordinator fan-out efficient — a coordinator running many `gitspork integrate` invocations against the same upstream only fetches from remote once per TTL window, not once per downstream.
```

- [ ] **Step 4: Verify no broken links or malformed tables**

Run: `grep -n "cache-ttl\|no-cache\|cache dir\|cache clear" README.md docs/README.md docs/examples/multi-upstream/README.md`
Expected: matches in the three files, no other files affected.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/README.md docs/examples/multi-upstream/README.md
git commit -m "docs: upstream mirror cache — feature paragraph, CLI flag table, cache-management subsection"
```

---

## Final verification

- [ ] **Run all test suites**

```bash
go clean -testcache
make test-unit
make test-functional
make test-sdk
make test-examples
```

Expected: all four suites pass.

- [ ] **Verify no leftover diagnostic warnings**

```bash
go vet -tags testharness ./...
```

Expected: no output.

- [ ] **Verify the release binary builds cleanly (no cache-related surface leaks into `cmd/`)**

```bash
go build -o /tmp/gitspork-check ./cmd/gitspork
```

Expected: clean build, binary produced.
