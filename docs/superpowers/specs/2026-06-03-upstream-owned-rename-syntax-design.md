# Design: rename support for `upstream_owned` and `downstream_owned` entries

## Goal

Let an upstream maintainer declare that a file should land at a *different*
path in the downstream repo than it occupies upstream. Today every
`upstream_owned` / `downstream_owned` entry is a glob whose matched files are
copied to the identical relative path downstream. We want a way to express a
rename, e.g. `.markdownlint-cli2-downstream.jsonc` upstream →
`.markdownlint-cli2.jsonc` downstream.

Rename support applies to **both** flat ownership lists, `upstream_owned` and
`downstream_owned`, which share one entry type (`OwnedEntry`). The two lists
differ only in their integration behavior (upstream overwrites every integrate;
downstream seeds once and is never overwritten) — the rename mechanics are
identical. `shared_ownership.*` (a merge operation) and `templated` (which
already has `template`/`destination`) are out of scope.

This must be fully backward compatible: existing plain-string entries behave
exactly as before.

## Syntax: structured entries (no inline delimiter)

An `upstream_owned` or `downstream_owned` entry is **either** a YAML scalar
(unchanged behavior) **or** a `{from, to}` mapping:

```yaml
upstream_owned:
  - src/**                                   # plain glob, copied to same path
  - from: .markdownlint-cli2-downstream.jsonc # rename: exact file
    to: .markdownlint-cli2.jsonc
  - from: configs/**                          # rename: glob (prefix substitution)
    to: .configs/**
```

We chose the structured-map form over an inline delimiter (`:` or `=>`)
because every ASCII punctuation character is legal in a Unix path, so any
inline delimiter carries some collision risk. The map form has **zero**
collision risk, is structurally unambiguous, and matches the precedent set by
the existing `templated` entries (a list of `template`/`destination` maps).
The cost — `UpstreamOwned` and `DownstreamOwned` are no longer plain
`[]string` — is accepted.

## Components

### 1. Config type (`internal/gitspork.go`)

Both `UpstreamOwned` and `DownstreamOwned` change from `[]string` to
`[]OwnedEntry` — one shared type for both flat ownership lists:

```go
type OwnedEntry struct {
    Pattern string // plain glob entry (no rename) — from a YAML scalar
    From    string // rename source glob/path — from a YAML map
    To      string // rename destination glob/path
}
```

`OwnedEntry` is intentionally ownership-neutral: it describes a path/rename, not
a policy. The difference between the two lists lives in their integrators, not
the entry type.

Custom YAML (un)marshaling via goccy/go-yaml interfaces:

- Unmarshal: a scalar node → `{Pattern: <value>}`; a mapping node with
  `from`/`to` → `{From, To}`.
- Marshal: plain entry → scalar; rename entry → `{from, to}` map. Round-trip
  fidelity is required because `mv`, `rm`, and `schema` all re-emit the config.

Helper methods:

- `SourcePattern() string` — `Pattern` for plain entries, `From` for renames.
  This is the glob matched against the **upstream** tree, and the value used
  everywhere the source side is needed (integration matching, delta managed
  globs, `mv`/`rm` matching).
- `IsRename() bool` — `From != ""`.
- `ResolveDest(matchedFile string) string` — the downstream path for a matched
  upstream file. Plain entries: identity (`matchedFile`). Rename entries:
  **prefix substitution** (see below).

### Prefix substitution (destination resolution)

For a rename entry, the destination of a matched file is computed by replacing
the source pattern's non-wildcard prefix with the destination pattern's
non-wildcard prefix, preserving the remainder. This reuses the existing
`globNonWildcardPrefix` helper in `upstream-mv-rm.go`.

```
From = configs/**     srcPrefix = configs
To   = .configs/**    dstPrefix = .configs

configs/app/db.yml -> .configs/app/db.yml
configs/x/y/z.txt  -> .configs/x/y/z.txt
```

Exact (wildcard-free) renames are the degenerate case: `srcPrefix == From`, the
remainder after trimming is empty, so the result is exactly `To`.

```
From = a.txt   To = b.txt   matched a.txt -> b.txt
```

**Documented contract / limitation:** prefix substitution assumes the
destination's wildcard structure mirrors the source's (the common case:
`prefix/**` → `prefix/**`). A mismatched pattern (e.g. wildcard `From`,
wildcard-free `To`) is a misconfiguration and produces deterministic but
likely-unintended paths. We do not add validation for this in the first cut
(YAGNI); the contract is documented in the schema/README.

### 2. Integration (`internal/integrator_upstream-owned.go`, `internal/integrator_downstream-owned.go`)

Both integrators take `[]OwnedEntry` and process **per entry** so each matched
file is associated with the entry that matched it.

`IntegratorUpstreamOwned.Integrate` — copies/overwrites every integrate:

```
for each entry:
    files = getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
    for each file:
        syncFile(upstream/file -> downstream/entry.ResolveDest(file))
```

`IntegratorDownstreamOwned.Integrate` — same matching and destination
resolution, but preserves its one-time-seed semantics: it only copies when the
**destination** path does not already exist downstream (the file is owned by the
downstream thereafter).

```
for each entry:
    files = getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
    for each file:
        dest = entry.ResolveDest(file)
        if downstream/dest does not exist:
            syncFile(upstream/file -> downstream/dest)
```

`getIntegrateFiles` already uses `gobwas/glob`, so matching is consistent with
the rest of the codebase. Using `ResolveDest` (not the literal pattern) fixes
the two integration bugs called out in review.

### 3. Delta propagation (`internal/upstream-delta.go`)

Delta propagation concerns **`upstream_owned` and `shared_ownership.*` only** —
`downstream_owned` files are owned by the downstream and have never participated
in delta (upstream deletions/renames do not propagate to them). That stays true:
the managed-matcher set is built from `UpstreamOwned` + `SharedOwnership.*`, and
`DownstreamOwned` is deliberately excluded. So adding rename support to
`downstream_owned` requires **no** delta changes.

For `upstream_owned` rename entries, downstream files live at the
**destination** path, so delta propagation must operate in
downstream-destination space.

`buildManagedGlobs` (and its callers) must use each entry's `SourcePattern()`
instead of treating the entry as a literal glob — this fixes the
"`from=>to` never matches a real file" bug. Beyond matching, the managed-glob
list is generalized into **matchers that carry the resolving entry**, so the
delta loop can map an upstream source path to its downstream destination:

- **Deletion** of upstream source `S` (matched by a managed entry under the
  prev config) → delete `entry.ResolveDest(S)` from downstream, not `S`.
- **Rename** of upstream source `S → S2` between commits →
  `oldDest = resolveDest(S)` under the **prev** config,
  `newDest = resolveDest(S2)` under the **new** config; emit a downstream move
  only when `oldDest != newDest`. A pure source-side rename whose destination
  is unchanged produces **no** downstream move.

The existing `applyTemplatedConfigDelta` path (for `templated` entries) is
unaffected.

### 4. `mv` / `rm` awareness (`internal/upstream-mv-rm.go`)

`gitspork mv` / `gitspork rm` operate on **upstream source files**, so they
match against each entry's **source side** (`Pattern` for plain, `From` for
rename); `To` is never touched (it is a downstream landing path, not an
upstream file).

Add entry-aware variants of the existing `rewritePatterns` / `filterPatterns`
(`rewriteOwned` / `filterOwned`) that apply the current exact-match and
non-wildcard-prefix logic to the source side of each entry, and run them over
**both** `UpstreamOwned` and `DownstreamOwned`:

- `mv source.txt new.txt` on `{from: source.txt, to: dest.txt}` ⇒
  `{from: new.txt, to: dest.txt}`.
- `mv configs old/new` with prefix match rewrites the `from` prefix, preserving
  `to`.
- `rm source.txt` on `{from: source.txt, to: dest.txt}` ⇒ entry removed.
- Recursive `rm` removes entries whose source-side non-wildcard prefix falls
  under the removed path.

The `shared_ownership.*` lists remain `[]string` and keep using the existing
`rewritePatterns` / `filterPatterns` closures; `templated` is unchanged.

### 5. Schema / docs

- Update the `init` scaffold and `schema` command output so the annotated
  schema documents the structured rename form.
- Update `docs/README.md` with a usage example.

**Marshaling approach (verified by the de-risking spike, now `internal/owned_entry_test.go`).**
The spike confirmed how the two marshaling paths behave (it was written against
fixture types; implementation points it at the real `OwnedEntry`):

- **Config read/write path (goccy/go-yaml).** A custom `BytesUnmarshaler`
  (`UnmarshalYAML([]byte) error`) + `BytesMarshaler` (`MarshalYAML() ([]byte, error)`)
  on `OwnedEntry` works correctly. Unmarshal accepts a mixed list of
  scalars and `{from,to}` maps; marshal round-trips plain entries to **bare
  scalars** and renames to `{from,to}` maps. The comment-preserving write path
  (`MarshalWithOptions` + `CommentMap`, used by `WriteGitSporkConfig`) preserves
  user comments and renders scalar/map forms correctly. The entry's `from`/`to`
  (and the plain `pattern`) fields carry `,omitempty` yaml tags.

- **Schema-doc path (go-lib `marshal.YAMLWithComments`).** This renderer is
  reflection-based and **ignores** custom `MarshalYAML`, so it emits plain
  entries in verbose `- pattern: "x"` map form. Chosen fix: a targeted
  **post-processing pass** (`collapsePlainOwnedEntries`) that rewrites
  `- pattern: "x"` lines within **both** the `upstream_owned:` and
  `downstream_owned:` blocks back to bare scalars `- "x"`, leaving `{from,to}`
  rename entries and other sections untouched. (The block-start check lists both
  keys; because it runs before the block-end check, each owned block is handled
  independently.) This pass lives next to `GetGitSporkConfigSchema` and is
  applied to its output. The spike confirmed the transform produces the intended
  schema and does not bleed into adjacent sections.

## Testing

### Unit (`internal/`)

- `OwnedEntry` unmarshal: scalar form and map form.
- `OwnedEntry` marshal + round-trip: scalar stays scalar, map stays map.
  (Already started in the spike, which currently uses temporary fixture types;
  during implementation these tests are pointed at the real `OwnedEntry`, the
  file is renamed to `internal/owned_entry_test.go`, and the schema
  `collapsePlainOwnedEntries` helper moves into non-test code.)
- `ResolveDest`: plain identity; exact rename; glob prefix substitution.
- `collapsePlainOwnedEntries`: collapses plain entries in both the
  `upstream_owned:` and `downstream_owned:` blocks; preserves rename entries.
- `buildManagedGlobs` / delta matchers use the source side (a rename entry's
  `from` glob matches a real upstream file); `downstream_owned` is not in the
  managed set.
- `ComputeUpstreamMvFromConfig` / `ComputeUpstreamRmFromConfig` on rename
  entries in **both** lists: source-side match rewrites/removes; `to` untouched.

### Functional (`test/functional/`, build tag `functional`)

- End-to-end `integrate` with an **exact** `upstream_owned` rename entry: assert
  the file lands at the destination path downstream and is absent at the source
  path.
- End-to-end `integrate` with a **glob** `upstream_owned` rename entry: assert
  matched files land under the destination prefix.
- Delta propagation: integrate at commit A, delete the upstream source between
  A and B, integrate at B, assert the downstream **destination** file is
  removed.
- End-to-end `integrate` with a `downstream_owned` rename entry: assert the file
  is seeded at the destination path; a downstream edit at that destination
  survives re-integrate (one-time-seed semantics hold for renames).

## Out of scope

- Inline-delimiter syntax (`:`, `=>`) — superseded by structured entries.
- Full positional wildcard capture/substitution — prefix substitution covers
  the intended cases; full capture is unnecessary complexity (YAGNI).
- Validation/rejection of mismatched source/destination wildcard structures —
  documented as a contract, not enforced.
- Rename support for `shared_ownership.*` (a merge operation, where rename
  semantics are murkier) and `templated` (already has `template`/`destination`).
