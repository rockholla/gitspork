# Upstream Mirror Cache Design

**Date:** 2026-07-22
**Motivation:** Coverage-audit follow-up — CheckDrift and Integrate re-clone the same upstream on every invocation. For fleet operators running a coordinator that fans out drift-checks and integrates across hundreds of downstreams against 1–4 shared upstreams from a single machine, this is enormous repeated bandwidth waste.
**Branch:** `feat/upstream-mirror-cache`

## Summary

Introduce a machine-scoped bare-mirror cache of upstream repositories, sitting between `cloneUpstreamForIntegrate` and the network. First access to an upstream URL clones a bare mirror into `os.UserCacheDir()`; subsequent accesses either use the cached mirror as-is (fresh within TTL) or run `git fetch` to refresh it (stale). Both `integrate` and `check-drift` route through the cache. Cross-process safety via per-URL `flock` — the coordinator scenario is inherently cross-process. Composes with PR #90's `Depth: 1` shallow-clone optimisation: the machine cache is a full mirror; the working clone from cache is shallow.

Default TTL: 2 hours. Opt-out: `--no-cache` flag or `GITSPORK_NO_CACHE=1` env var. Corruption recovery: on any cache-side error, wipe the URL's cache entry and retry once — surface on second failure.

---

## Section 1: Architecture

### Cache root

`os.UserCacheDir()` + `gitspork/repos/` — resolves to `$XDG_CACHE_HOME/gitspork/repos/` on Linux and `~/Library/Caches/gitspork/repos/` on macOS. Overridable via `GITSPORK_CACHE_DIR` env var (test-friendly; also useful when the default location isn't writable in containers).

### Per-URL cache entry

Each upstream URL maps to a triple of paths under the cache root, keyed by `sha256(NormalizeUpstreamURL(url, ""))`:

- `<key>/` — a bare git mirror (`git clone --mirror`), holds all refs and objects reachable from remote refs.
- `<key>.fetched-at` — small sidecar file containing a Unix timestamp of the last successful fetch or clone.
- `<key>.lock` — zero-byte sentinel file used as the `flock` target for cross-process coordination.

The key uses the same `NormalizeUpstreamURL` normalisation the state file uses, so `git@github.com:foo/bar.git` and `https://github.com/foo/bar` collapse to the same cache entry.

### Data flow inside `cloneUpstreamForIntegrate`

For each call with a resolved `upstreamURL`:

1. If cache is disabled (opt-out flag/env set), fall through to today's behaviour: direct network `PlainClone` with the requested `Depth`. Return.
2. Compute the cache-entry key. Open a `*flock.Flock` at `<key>.lock` and acquire an exclusive lock (blocking).
3. Check the cache state:
   - Cache dir absent → `git clone --mirror <upstreamURL> <cache-dir>`, write `.fetched-at`.
   - Cache dir present and `(now - fetched-at) > ttl` → `git -C <cache-dir> fetch --prune`, write `.fetched-at`.
   - Cache dir present and fresh → no-op.
4. Release the flock.
5. Working clone: `git.PlainClone(cloneDir, {URL: "file://<cache-dir>", ReferenceName, SingleBranch, Depth: ...})` — a local filesystem clone against the mirror. Fast; no network.
6. Everything downstream of this step (checkout by hash, `ResolveRevision` for hex-hash Version, etc.) is unchanged.

The working clone runs OUTSIDE the flock because `git clone --local` against a bare repo is safe under concurrent readers-while-writer-fetches, matching how real git servers operate. Same-URL fetches serialise behind the flock; different-URL work runs fully in parallel; same-URL working clones don't serialise.

### In-process concurrency

`flock(2)` on POSIX systems is per-open-file-description, not per-process, so two goroutines in the same process each calling `flock.New(path).Lock()` would get separate fds and could both hold the lock. To avoid that, `internal/integrate/cache.go` maintains a package-level `map[string]*flock.Flock` guarded by a `sync.Mutex` for map access. All in-process callers for a given URL go through the same `*flock.Flock` instance, which serialises them cleanly. Cross-process callers each construct their own map entry in their own address space, and the OS flock coordinates them.

### Opt-out

`--no-cache` (CLI flag on `integrate` and `check-drift`) or `GITSPORK_NO_CACHE=1` (env). When either is set, step 1 short-circuits — no cache read, no cache write, no lock acquired. The direct-network-clone path (with `Depth: 1` when applicable per PR #90) is used. This is the pre-cache behaviour, preserved as an escape hatch.

### Log-line contract

The cache emits one of three distinct log lines per invocation, chosen so tests and users can grep to understand what happened. These strings are part of the observable contract of this feature — changes require updating tests.

- Populate: `populating upstream cache for <url> at <cache-dir>`
- Refresh: `refreshing upstream cache for <url> (last fetch: <duration> ago, ttl: <ttl>)`
- Hit:     `upstream cache hit for <url> (fetched <duration> ago, ttl: <ttl>)`

On opt-out, no cache-specific log line is emitted — the pre-cache network-clone log line is unchanged.

---

## Section 2: TTL / CLI Surface

### New flags on `integrate` and `check-drift`

| Flag | Default | Format | Meaning |
|---|---|---|---|
| `--cache-ttl` | `2h` | Go duration string (`time.ParseDuration` — `2h`, `30m`, `1h30m`) | Cache is considered fresh if `(now - fetched-at) <= ttl`. Set to a very small value (`1ns`) to force refresh on every run; use `--no-cache` for full bypass. |
| `--no-cache` | (unset) | boolean | Bypass the cache entirely for this run. Direct network clone; cache neither read nor written. Use this when you want fresh-every-run semantics. |

**On `--cache-ttl 0s`**: treated identically to "flag not set" — the SDK type is `time.Duration` (non-pointer), and its zero-value semantic is "use env var if set, else compiled default (2h)". A user wanting fetch-every-run behavior should use `--no-cache` (which touches nothing and is unambiguous) or `--cache-ttl 1ns` (which still populates the cache after fetching, so subsequent runs benefit).

### Environment-variable equivalents

CLI flag wins if both are set; env is the per-machine default.

- `GITSPORK_CACHE_TTL` — same duration format as `--cache-ttl`.
- `GITSPORK_NO_CACHE` — truthy = presence-based (any non-empty value bypasses the cache), matching the boolean semantics of the `--no-cache` flag. Simple, unambiguous, no bikeshed over what `"0"` or `"false"` means.
- `GITSPORK_CACHE_DIR` — override the cache root.

Precedence: CLI flag > env var > compiled default.

### SDK surface

`IntegrateOptions`, `IntegrateLocalOptions`, and `CheckDriftOptions` gain two new fields:

```go
CacheTTL time.Duration // zero-value = "use env var if set, else 2h"
NoCache  bool          // true = skip the cache entirely
```

`IntegrateLocal` currently uses local paths (not URLs) and doesn't call `cloneUpstreamForIntegrate`, so its `NoCache`/`CacheTTL` fields are inert — added for API symmetry and future-proofing but produce no behaviour change today. Documented as such in the field's godoc.

### Interaction rules

- `--no-cache` overrides `--cache-ttl` — no-cache wins; TTL is moot.
- `--cache-ttl 0s` and `--cache-ttl` unset are equivalent (both mean "use env or 2h default"). Users wanting force-refresh use `--no-cache` (touches nothing) or `--cache-ttl 1ns` (fetches but still updates the cache).
- On a first-ever run against an upstream (no cache dir yet), TTL is irrelevant — cache doesn't exist so we populate. TTL only gates fetches on existing entries.

---

## Section 3: Management Subcommands

New parent command `gitspork cache` with two children.

### `gitspork cache dir`

Prints the resolved cache root to stdout. Zero flags. Respects `GITSPORK_CACHE_DIR`. Enables scripting like:

```sh
cd $(gitspork cache dir)
du -sh $(gitspork cache dir)
find $(gitspork cache dir) -maxdepth 1
```

### `gitspork cache clear`

Wipes cached upstream entries from disk.

Flags:

- `--url <url>` — clear a single upstream's cache entry. Matched via `NormalizeUpstreamURL` so SSH/HTTPS variants collapse to the same lookup. Wipes `<key>/`, `<key>.fetched-at`, and `<key>.lock`.
- `--force` — skip the interactive confirmation prompt. Required for non-TTY invocations (the coordinator, CI); TTY runs prompt `y/N` before wiping.

Without `--url`, wipes everything under the cache root.

Behavior:

- Acquires the flock for each URL before wiping so it can't race with an in-progress fetch.
- Removes the lock file at the end (harmless — the OS has released the underlying advisory lock; a subsequent gitspork invocation will re-create it).
- On a TTY: prints a summary of what will be removed (one line per URL entry, plus total byte count) and prompts `y/N` before wiping. `n`/empty response = abort without touching anything.
- On non-TTY: exits non-zero without touching anything unless `--force` was passed. Error message names the `--force` flag so the user can add it if they meant to. Fail-loud rather than silently wipe in scripts.

### What is NOT included (see also Section 6)

- No `gitspork cache prune` (`git gc` inside each mirror to reclaim dead objects from force-pushes).
- No `gitspork cache list` (enumerating URL → path mappings).
- No `gitspork cache warm`.

---

## Section 4: Corruption Recovery + Concurrency

### Lock library

`github.com/gofrs/flock` — portable across Linux/macOS/Windows, POSIX-flock-backed, kernel-releases-on-process-exit. One additional dependency; the standard library doesn't ship a cross-platform equivalent.

### Lock scope

Exclusive per-URL flock, held ONLY across cache-side operations (populate, refresh, timestamp write). Released BEFORE the working-clone step so different-URL fetches and same-URL working clones can proceed in parallel.

Stale lock files after a process crash: none required. `flock(2)` releases automatically when the holding process exits. No PID files, no cleanup daemons.

### Three failure boundaries, each with retry-once

1. **Cache populate fails** (network error, disk full, killed during initial `clone --mirror`)
   - Symptom: `git clone --mirror` returns error, or a partial `.git/` is left behind.
   - Handler: `os.RemoveAll(<key>/)` + `os.Remove(<key>.fetched-at)` → retry populate once → on second failure, surface the wrapped error to the caller.

2. **Cache refresh fails** (fetch returns error against an otherwise-valid mirror)
   - Symptom: `git fetch --prune` returns error inside an existing cache dir.
   - Handler: same wipe + repopulate + retry-once as (1). Cheaper than distinguishing "transient network flake" from "mirror corruption" — a fresh mirror is the recovery for either.

3. **Working clone fails** (rare race where a concurrent fetch-prune deleted a ref this clone snapshotted)
   - Symptom: `PlainClone(cloneDir, {URL: "file://<cache-dir>", ...})` returns an error.
   - Handler: retry the working clone once, still without the lock — the offending ref-delete is one-shot per fetch, so a second attempt has fresh refs. On second failure, surface the error. NEVER wipe the cache from this path; the cache is likely healthy and the race is between concurrent consumers.

### What is NOT retried

- Errors from `NormalizeUpstreamURL`, `os.UserCacheDir()`, the flock acquire itself, or the sidecar timestamp read/write. Those are local, deterministic, and a retry cannot help.

### Retry bounding

All three retries are hard-bounded to a single retry. A genuinely broken remote or corrupt-and-uncorrectable cache must surface as an error rather than looping.

### Opt-out bypasses all of this

When `--no-cache` or `GITSPORK_NO_CACHE=1` is set, `cloneUpstreamForIntegrate` behaves exactly like today — direct `PlainClone` against the remote with `Depth: 1` if applicable. No lock acquired, no cache directory touched.

---

## Section 5: Testing

### Unit tier (`internal/integrate/cache_test.go` — new file)

- **Cache key stability:** URL variants (`git@github.com:foo/bar.git`, `https://github.com/foo/bar`, mixed-case host) all map to the same cache-entry key via `NormalizeUpstreamURL` + sha256.
- **TTL freshness predicate:** a small testable helper `isCacheFresh(fetchedAt time.Time, ttl time.Duration) bool` with a table-driven test covering fresh, exactly-at-TTL boundary, stale, zero TTL (never fresh), and negative-duration edge cases.
- **Corruption recovery on populate:** given a cache dir containing a corrupt `.git/`, the populate path wipes + retries once. Simulate corruption by pre-writing garbage into `<key>/.git/HEAD`.
- **Retry bounding:** a persistently-broken remote (bogus URL) surfaces the wrapped error after exactly one retry, not an infinite loop. Assert timing (must complete in <10s to fail-loud vs hang).
- **`GITSPORK_NO_CACHE` truthiness:** any non-empty value bypasses the cache; unset or `""` enables it.
- **`GITSPORK_CACHE_TTL` parse errors surface loudly.** A malformed env value (e.g., `"lol"`) returns a wrapped error naming the env var and the parse failure — no silent fallback to the compiled default. Matches the CLI's own behavior when `--cache-ttl garbage` is passed. User-supplied config that's syntactically wrong is always surfaced.
- **In-process serialisation:** two goroutines in the same process against the same URL each go through the shared `*flock.Flock` instance and serialise cleanly (exactly one populate, not two).

### Functional tier (`test/functional/cache_test.go` — new file, `//go:build functional || functional_docker`)

Every test scopes `GITSPORK_CACHE_DIR` to a `t.TempDir()` so the real user cache is never touched.

The tests grep for the stable prefixes of the log lines defined in Section 1's **Log-line contract** (`populating upstream cache`, `refreshing upstream cache`, `upstream cache hit`).

- **Cache hit within TTL:** first `integrate` populates cache and emits the `populating upstream cache` line; second `integrate` within TTL emits `upstream cache hit` and performs NO network operation (asserted by grepping the log output).
- **Cache stale beyond TTL:** run with `--cache-ttl 1ns` on second invocation — emits `refreshing upstream cache` and performs a fetch.
- **`--cache-ttl 1ns`:** effectively forces fetch on every run (cache is considered stale on second use) — emits `refreshing upstream cache`.
- **`--cache-ttl 0s` is equivalent to unset:** cache uses env/default (2h). Populates on first run, hits cache on second within TTL.
- **`--no-cache`:** bypasses entirely. No cache dir created, none of the three cache log lines emitted.
- **Cross-process fan-out:** N=4 concurrent gitspork subprocesses via the functional runner, all against the same upstream URL with distinct downstream tempdirs. All succeed; exactly one populates the cache (assert by counting `populating upstream cache` occurrences across the four stdout captures — expect exactly one). Locks in the cross-process flock contract.
- **`gitspork cache dir` output** is the resolved cache root.
- **`gitspork cache clear`:** populate, clear all, verify cache root is empty. Then populate, clear `--url <specific>`, verify only that URL's entry is gone.
- **`gitspork cache clear` non-TTY:** without `--force`, exits non-zero and doesn't wipe.

### SDK tier (`test/sdk/sdk_test.go` — extend existing suite)

- **`IntegrateOptions.CacheTTL` and `NoCache` honored:** parallel to CLI, exercised via the Go SDK directly.
- **`CacheTTL` zero-value falls back:** with `GITSPORK_CACHE_TTL=1ns` in env, zero-value option triggers a fetch. Without env var, zero-value option uses the compiled 2h default.
- **`NoCache=true` on `IntegrateLocalOptions`:** confirmed inert (documented behaviour) — no cache dir touched, no error.

---

## Section 6: Out of Scope for v2.1

Documented here so future us knows what deliberate deferrals looked like at design time.

- **Automatic `git gc` inside cache mirrors** to reclaim disk from force-pushed dead objects. Users can `gitspork cache clear --url <url>` and let the next run re-populate. Future: a `gitspork cache prune` subcommand.
- **Cache warming** (`gitspork cache warm --url ...`): the first `integrate`/`check-drift` invocation populates naturally; a warm command adds surface without meaningful value.
- **Reader-writer flock** (multiple concurrent readers, one writer): if working-clone-from-cache serializing behind fetch becomes a bottleneck under real fan-out, upgrade the exclusive flock to an RWMutex-shaped scheme. Prediction: not needed at the coordinator scale described in the motivation.
- **Size caps / LRU eviction:** bounded upstream count in the target scenario. Not worth the complexity unless real usage shows the cache growing pathologically.
- **Cross-user shared cache** on a multi-user machine (`/var/cache/gitspork` or similar): possible today by setting `GITSPORK_CACHE_DIR` to a group-owned path with permissive modes. No first-class support planned.
- **Time-based auto-eviction of entire URL entries** (delete if not accessed in N days): different semantic from the per-run TTL refresh policy. Adds a maintenance sweep. Users who want this can run `gitspork cache clear` themselves on a cron.

---

## Section 7: Docs Updates

- `README.md` — one paragraph under **Features** describing the machine cache, its opt-out, and the coordinator-scale use case it targets.
- `docs/README.md` — expand the CLI-flag documentation with `--cache-ttl` and `--no-cache` on both `integrate` and `check-drift`. Add a short "Cache management" subsection with `gitspork cache dir` and `gitspork cache clear`.
- `docs/CONTRIBUTING.md` — no changes required; the layout section under `internal/integrate/` already covers where the cache implementation will live.
- `docs/examples/multi-upstream/README.md` — one sentence in the "Real-world mapping" section noting the machine cache as the mechanism that makes coordinator fan-out efficient.
