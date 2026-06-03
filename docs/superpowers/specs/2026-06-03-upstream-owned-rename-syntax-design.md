# Design: rename support for `upstream_owned` entries

## Goal

Let an upstream maintainer declare that a file should land at a *different*
path in the downstream repo than it occupies upstream. Today every
`upstream_owned` entry is a glob whose matched files are copied to the
identical relative path downstream. We want a way to express a rename, e.g.
`.markdownlint-cli2-downstream.jsonc` upstream → `.markdownlint-cli2.jsonc`
downstream.

This must be fully backward compatible: existing plain-string entries behave
exactly as before.

## Syntax: structured entries (no inline delimiter)

An `upstream_owned` entry is **either** a YAML scalar (unchanged behavior)
**or** a `{from, to}` mapping:

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
The cost — `UpstreamOwned` is no longer a plain `[]string` — is accepted.

## Components

### 1. Config type (`internal/gitspork.go`)

`UpstreamOwned` changes from `[]string` to `[]UpstreamOwnedEntry`:

```go
type UpstreamOwnedEntry struct {
    Pattern string // plain glob entry (no rename) — from a YAML scalar
    From    string // rename source glob/path — from a YAML map
    To      string // rename destination glob/path
}
```

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

### 2. Integration (`internal/integrator_upstream-owned.go`)

`IntegratorUpstreamOwned.Integrate` takes `[]UpstreamOwnedEntry`. It processes
**per entry** so each matched file is associated with the entry that matched it:

```
for each entry:
    files = getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
    for each file:
        syncFile(upstream/file -> downstream/entry.ResolveDest(file))
```

`getIntegrateFiles` already uses `gobwas/glob`, so matching is consistent with
the rest of the codebase. Using `ResolveDest` (not the literal pattern) fixes
the two integration bugs called out in review.

### 3. Delta propagation (`internal/upstream-delta.go`)

Downstream files for rename entries live at the **destination** path, so delta
propagation must operate in downstream-destination space.

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
that apply the current exact-match and non-wildcard-prefix logic to the
source side of each entry:

- `mv source.txt new.txt` on `{from: source.txt, to: dest.txt}` ⇒
  `{from: new.txt, to: dest.txt}`.
- `mv configs old/new` with prefix match rewrites the `from` prefix, preserving
  `to`.
- `rm source.txt` on `{from: source.txt, to: dest.txt}` ⇒ entry removed.
- Recursive `rm` removes entries whose source-side non-wildcard prefix falls
  under the removed path.

The other ownership lists (`downstream_owned`, `shared_ownership.*`,
`templated`) are unchanged.

### 5. Schema / docs

- Update the `init` scaffold and `schema` command output so the annotated
  schema documents the structured rename form.
- Update `docs/README.md` with a usage example.

**Marshaling approach (verified by `internal/upstream_owned_marshal_test.go`).**
A de-risking spike confirmed how the two marshaling paths behave:

- **Config read/write path (goccy/go-yaml).** A custom `BytesUnmarshaler`
  (`UnmarshalYAML([]byte) error`) + `BytesMarshaler` (`MarshalYAML() ([]byte, error)`)
  on `UpstreamOwnedEntry` works correctly. Unmarshal accepts a mixed list of
  scalars and `{from,to}` maps; marshal round-trips plain entries to **bare
  scalars** and renames to `{from,to}` maps. The comment-preserving write path
  (`MarshalWithOptions` + `CommentMap`, used by `WriteGitSporkConfig`) preserves
  user comments and renders scalar/map forms correctly. The entry's `from`/`to`
  (and the plain `pattern`) fields carry `,omitempty` yaml tags.

- **Schema-doc path (go-lib `marshal.YAMLWithComments`).** This renderer is
  reflection-based and **ignores** custom `MarshalYAML`, so it emits plain
  entries in verbose `- pattern: "x"` map form. Chosen fix: a targeted
  **post-processing pass** (`collapsePlainUpstreamOwned`) that rewrites
  `- pattern: "x"` lines within the `upstream_owned:` block back to bare scalars
  `- "x"`, leaving `{from,to}` rename entries and following sections untouched.
  This pass lives next to `GetGitSporkConfigSchema` and is applied to its output.
  The spike confirmed the transform produces the intended schema and does not
  bleed into adjacent sections.

## Testing

### Unit (`internal/`)

- `UpstreamOwnedEntry` unmarshal: scalar form and map form.
- `UpstreamOwnedEntry` marshal + round-trip: scalar stays scalar, map stays map.
  (Already started in `internal/upstream_owned_marshal_test.go`, which currently
  uses temporary fixture types; during implementation these tests are pointed at
  the real `UpstreamOwnedEntry` and the schema `collapsePlainUpstreamOwned`
  helper moves into non-test code.)
- `ResolveDest`: plain identity; exact rename; glob prefix substitution.
- `buildManagedGlobs` / delta matchers use the source side (a rename entry's
  `from` glob matches a real upstream file).
- `ComputeUpstreamMvFromConfig` / `ComputeUpstreamRmFromConfig` on rename
  entries: source-side match rewrites/removes; `to` untouched.

### Functional (`test/functional/`, build tag `functional`)

- End-to-end `integrate` with an **exact** rename entry: assert the file lands
  at the destination path downstream and is absent at the source path.
- End-to-end `integrate` with a **glob** rename entry: assert matched files
  land under the destination prefix.
- Delta propagation: integrate at commit A, delete the upstream source between
  A and B, integrate at B, assert the downstream **destination** file is
  removed.

## Out of scope

- Inline-delimiter syntax (`:`, `=>`) — superseded by structured entries.
- Full positional wildcard capture/substitution — prefix substitution covers
  the intended cases; full capture is unnecessary complexity (YAGNI).
- Validation/rejection of mismatched source/destination wildcard structures —
  documented as a contract, not enforced.
- Rename support for ownership types other than `upstream_owned`.
