# Security Scanning (CodeQL, fail-on-findings) — Design

**Date:** 2026-06-04
**Branch:** `feat/security-scanning`
**Status:** Approved

## Goal

Add OSS security scanning to CI for the `gitspork` repo using **CodeQL**, gate
pull requests and the default branch on findings (the workflow fails when a
qualifying finding exists), and surface the current security status of the
default branch via a single status badge in the root `README.md`.

## Decisions (locked)

| Decision | Choice |
|---|---|
| Scanner | CodeQL only (Go) |
| Gating | Block on findings — the workflow run fails on qualifying findings |
| Triggers | `pull_request` → main, `push` → main, weekly `schedule`, `workflow_dispatch` |
| Badge | Single dedicated `security` workflow badge in root README |
| Approach | **B** — self-contained fail-on-findings: CodeQL writes SARIF locally; a gate step parses it and fails the run |

### Why Approach B

CodeQL's standard action uploads findings to the GitHub Security tab but the
*workflow run* succeeds even when alerts exist. A workflow status badge reflects
the *run* status, and "block on findings" is normally enforced through repo
code-scanning settings + branch protection (not YAML). The user wants both
blocking **and** a badge that reflects security status, so we make the workflow
itself fail when findings exist. The SARIF is produced locally by the `analyze`
step (`output:`), so the gate is deterministic — no polling of the asynchronous
code-scanning alerts API.

## Architecture

Three units, each independently understandable:

1. **`.github/workflows/security.yml`** — the CodeQL workflow. Owns triggers,
   permissions, the CodeQL init/autobuild/analyze sequence, and invokes the gate
   script. It still uploads to the Security tab (`upload: true`) for triage.
2. **`scripts/ci/security-gate.sh`** — pure gate logic, isolated from YAML so it
   is readable and unit-testable. Input: a SARIF file path (and an optional
   severity threshold). Output: a step summary of offending rules; exit code 0
   (clean) or non-zero (findings).
3. **Root `README.md` badge** — a one-line status badge pinned to `main`.

### Workflow detail — `.github/workflows/security.yml`

```yaml
name: Security

on:
  pull_request:
    branches: [ main ]
  push:
    branches: [ main ]
  schedule:
    - cron: '0 0 * * 1'   # weekly, Mondays 00:00 UTC
  workflow_dispatch:

concurrency:
  group: security-${{ github.ref }}
  cancel-in-progress: true

jobs:
  codeql:
    name: CodeQL
    runs-on: ubuntu-latest
    permissions:
      security-events: write
      contents: read
      actions: read
    steps:
      - uses: actions/checkout@v4
      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'
      - name: Initialize CodeQL
        uses: github/codeql-action/init@v3
        with:
          languages: go
          build-mode: autobuild
      - name: Analyze
        uses: github/codeql-action/analyze@v3
        with:
          category: "/language:go"
          upload: true
          output: sarif-results
      - name: Security gate
        run: ./scripts/ci/security-gate.sh sarif-results/go.sarif
```

Notes:
- `actions/setup-go@v5` pins Go 1.26 from `go.mod` so CodeQL `autobuild` uses the
  correct toolchain.
- The default CodeQL query suite (high-precision security queries) is used to
  minimize false-positive blocking. No `queries:` override.
- `analyze` writes `sarif-results/go.sarif` (CodeQL names the file `<language>.sarif`).

### Gate detail — `scripts/ci/security-gate.sh`

Behavior:
- Reads the SARIF file passed as `$1`. Errors clearly if it is missing or `jq`
  is unavailable.
- Counts results whose `level` is `error` or `warning` (informational `note`
  results do not block). The blocking levels are documented in the script and
  overridable via a second argument, but default to `error,warning`.
- Writes a human-readable summary (count + offending rule IDs) to
  `$GITHUB_STEP_SUMMARY` when that variable is set, and echoes the same to stdout
  so local runs are useful.
- Exits `0` when no qualifying findings exist, non-zero otherwise.

Pseudocode:

```sh
#!/usr/bin/env bash
set -euo pipefail
sarif="${1:?usage: security-gate.sh <sarif-file> [levels]}"
levels="${2:-error,warning}"
# jq filter: select results whose .level is in the requested set
count=$(jq --arg levels "$levels" '
  ($levels | split(",")) as $want
  | [ .runs[].results[] | select(.level as $l | $want | index($l)) ] | length
' "$sarif")
# summary of rule ids ...
if [ "$count" -gt 0 ]; then exit 1; fi
```

(The implementation plan provides the complete, final script.)

### README badge

Add immediately after the existing `ci` badge line:

```
[![security](https://github.com/rockholla/gitspork/actions/workflows/security.yml/badge.svg?branch=main)](https://github.com/rockholla/gitspork/actions/workflows/security.yml)
```

`?branch=main` pins the badge to the default branch. Because the gate runs on
`push` and `schedule` against `main`, the badge turns red whenever a qualifying
finding exists on the default branch.

## Testing

- **Gate unit test (deterministic, local):** two fixture SARIF files committed
  under a test directory — one containing a `warning`/`error` result, one clean.
  A shell test asserts the script exits non-zero for the dirty fixture and zero
  for the clean fixture. Wired as `make test-security-gate`, mirroring the
  existing `make test-*` targets.
- **YAML lint:** run `actionlint` on `security.yml` if available locally.
- **End-to-end:** validated by pushing the branch and observing the GitHub
  Actions run (CodeQL analysis + gate + badge). Not done automatically — requires
  an explicit push.

## Out of scope (YAGNI)

- Additional scanners: govulncheck, gosec, gitleaks, Trivy.
- Branch-protection / required-status-check configuration (a repo Settings
  concern; noted for the maintainer but not automated here).
- Per-scanner badges.

## Risks / notes

- **CodeQL autobuild for Go** relies on `go build ./...` succeeding; the repo
  already builds in CI, so this is low risk. If autobuild ever fails, switch to a
  manual build step.
- **Threshold tuning:** if the default suite produces false positives that block
  PRs, the gate's blocking levels (or the CodeQL query suite) can be adjusted.
  Starting strict (block on error+warning) per the "block on findings" decision.
- **Maintainer action:** to enforce blocking on PRs, require the `Security / CodeQL`
  check in branch protection. Documented, not automated.
