# Security Scanning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add CodeQL security scanning to CI that fails the workflow on qualifying findings, and surface default-branch security status via a single README badge.

**Architecture:** A dedicated `.github/workflows/security.yml` runs CodeQL (init → autobuild → analyze), writes SARIF locally, and invokes a standalone gate script that fails the run when findings at/above a severity threshold exist. The gate logic lives in `scripts/ci/security-gate.sh` (isolated from YAML so it is unit-testable via fixture SARIF files). A `security` status badge pinned to `main` goes red when the default-branch run fails.

**Tech Stack:** GitHub Actions, `github/codeql-action@v3`, Go 1.26 (`go.mod`), Bash + `jq`.

---

## File Structure

- **Create** `scripts/ci/security-gate.sh` — parses a SARIF file, fails (non-zero exit) when results at requested `level`s exist; writes a summary. The only real logic in this feature.
- **Create** `test/security-gate/fixtures/clean.sarif` — SARIF with zero results.
- **Create** `test/security-gate/fixtures/findings.sarif` — SARIF with one `error`, one `warning`, one `note` result.
- **Create** `test/security-gate/run-tests.sh` — asserts the gate's exit codes and reported counts against the fixtures.
- **Modify** `Makefile` — add a `test-security-gate` target mirroring the existing `test-*` targets.
- **Create** `.github/workflows/security.yml` — the CodeQL workflow that invokes the gate.
- **Modify** `README.md:5` — add the `security` badge after the existing `ci` badge.

---

### Task 1: Security gate script and its unit tests

**Files:**
- Create: `test/security-gate/fixtures/clean.sarif`
- Create: `test/security-gate/fixtures/findings.sarif`
- Create: `test/security-gate/run-tests.sh`
- Create: `scripts/ci/security-gate.sh`
- Modify: `Makefile`

- [ ] **Step 1: Write the clean fixture**

Create `test/security-gate/fixtures/clean.sarif`:

```json
{
  "version": "2.1.0",
  "runs": [
    {
      "tool": { "driver": { "name": "CodeQL" } },
      "results": []
    }
  ]
}
```

- [ ] **Step 2: Write the findings fixture**

Create `test/security-gate/fixtures/findings.sarif` (one `error`, one `warning`, one non-blocking `note`):

```json
{
  "version": "2.1.0",
  "runs": [
    {
      "tool": { "driver": { "name": "CodeQL" } },
      "results": [
        { "ruleId": "go/sql-injection", "level": "error", "message": { "text": "Possible SQL injection" } },
        { "ruleId": "go/weak-crypto", "level": "warning", "message": { "text": "Weak cryptographic algorithm" } },
        { "ruleId": "go/informational-note", "level": "note", "message": { "text": "FYI only" } }
      ]
    }
  ]
}
```

- [ ] **Step 3: Write the failing test runner**

Create `test/security-gate/run-tests.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
gate="$here/../../scripts/ci/security-gate.sh"

fail() { echo "FAIL: $1" >&2; exit 1; }

# clean fixture -> exit 0
if ! "$gate" "$here/fixtures/clean.sarif" >/dev/null; then
  fail "clean fixture should pass (exit 0)"
fi

# findings fixture (default levels error,warning) -> non-zero exit
if "$gate" "$here/fixtures/findings.sarif" >/dev/null; then
  fail "findings fixture should fail (non-zero exit)"
fi

# default levels report exactly 2 blocking findings (error + warning, note excluded)
out="$("$gate" "$here/fixtures/findings.sarif" || true)"
echo "$out" | grep -q "2 finding(s)" || fail "expected 2 findings reported, got: $out"

# overriding levels to 'error' only -> exactly 1 finding
out_err="$("$gate" "$here/fixtures/findings.sarif" error || true)"
echo "$out_err" | grep -q "1 finding(s)" || fail "expected 1 error finding, got: $out_err"

echo "All security-gate tests passed."
```

Make it executable:

```bash
chmod +x test/security-gate/run-tests.sh
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `./test/security-gate/run-tests.sh`
Expected: FAIL — the script errors because `scripts/ci/security-gate.sh` does not exist yet (bash: "No such file or directory").

- [ ] **Step 5: Implement the gate script**

Create `scripts/ci/security-gate.sh`:

```bash
#!/usr/bin/env bash
# Fails (non-zero exit) when a SARIF file contains results at the requested
# severity levels. Used by .github/workflows/security.yml to block on findings.
#
# Usage: security-gate.sh <sarif-file> [comma-separated-levels]
#   levels defaults to "error,warning"; SARIF "note" results are non-blocking.
set -euo pipefail

sarif="${1:?usage: security-gate.sh <sarif-file> [comma-separated-levels]}"
levels="${2:-error,warning}"

if ! command -v jq >/dev/null 2>&1; then
  echo "security-gate: jq is required but not found" >&2
  exit 2
fi
if [ ! -f "$sarif" ]; then
  echo "security-gate: SARIF file not found: $sarif" >&2
  exit 2
fi

# One "<level>\t<ruleId>" line per result whose level is in the requested set.
# Missing .level defaults to "warning" (CodeQL security results are at least
# warnings); the ? operators tolerate runs/results keys being absent.
findings="$(jq -r --arg levels "$levels" '
  ($levels | split(",")) as $want
  | .runs[]?.results[]?
  | (.level // "warning") as $lvl
  | select($want | index($lvl))
  | "\($lvl)\t\(.ruleId // "unknown")"
' "$sarif")"

count=0
if [ -n "$findings" ]; then
  count=$(printf '%s\n' "$findings" | grep -c '.')
fi

emit() {
  echo "$1"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    echo "$1" >> "$GITHUB_STEP_SUMMARY"
  fi
}

if [ "$count" -eq 0 ]; then
  emit "✅ Security gate: no findings at levels [$levels]."
  exit 0
fi

emit "❌ Security gate: $count finding(s) at levels [$levels]:"
printf '%s\n' "$findings" | while IFS=$'\t' read -r lvl rule; do
  emit "- [$lvl] $rule"
done
exit 1
```

Make it executable:

```bash
chmod +x scripts/ci/security-gate.sh
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `./test/security-gate/run-tests.sh`
Expected: PASS — final line `All security-gate tests passed.`

- [ ] **Step 7: Add the Makefile target**

In `Makefile`, add after the `test-examples` target (after line 24) and before `test-all`:

```makefile
.PHONY: test-security-gate
test-security-gate: ## Run unit tests for the CI security gate script
	@./test/security-gate/run-tests.sh
```

- [ ] **Step 8: Verify the Makefile target works**

Run: `make test-security-gate`
Expected: PASS — prints `All security-gate tests passed.`

- [ ] **Step 9: Commit**

```bash
git add scripts/ci/security-gate.sh test/security-gate/ Makefile
git commit -m "feat: add CI security gate script with SARIF fixture tests"
```

---

### Task 2: CodeQL security workflow

**Files:**
- Create: `.github/workflows/security.yml`

- [ ] **Step 1: Write the workflow**

Create `.github/workflows/security.yml`:

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

- [ ] **Step 2: Validate the workflow YAML**

Prefer `actionlint` if installed:

Run: `command -v actionlint >/dev/null && actionlint .github/workflows/security.yml && echo "actionlint OK" || echo "actionlint not installed — falling back to YAML parse"`
Expected: `actionlint OK`, or the fallback message.

Fallback well-formedness check (always run this):

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/security.yml')); print('YAML OK')"`
Expected: `YAML OK`

- [ ] **Step 3: Confirm the gate path referenced by the workflow exists and is executable**

Run: `test -x scripts/ci/security-gate.sh && echo "gate present & executable"`
Expected: `gate present & executable`
(If this fails, Task 1 was not completed — stop and complete Task 1 first.)

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/security.yml
git commit -m "feat: add CodeQL security scanning workflow with fail-on-findings gate"
```

---

### Task 3: README security badge

**Files:**
- Modify: `README.md:5`

- [ ] **Step 1: Confirm the current badge line**

Run: `sed -n '5p' README.md`
Expected output (exact):

```
[![ci](https://github.com/rockholla/gitspork/actions/workflows/main.yml/badge.svg)](https://github.com/rockholla/gitspork/actions/workflows/main.yml)
```

- [ ] **Step 2: Add the security badge**

In `README.md`, replace line 5:

```
[![ci](https://github.com/rockholla/gitspork/actions/workflows/main.yml/badge.svg)](https://github.com/rockholla/gitspork/actions/workflows/main.yml)
```

with these two lines (ci badge kept, security badge added immediately after):

```
[![ci](https://github.com/rockholla/gitspork/actions/workflows/main.yml/badge.svg)](https://github.com/rockholla/gitspork/actions/workflows/main.yml)
[![security](https://github.com/rockholla/gitspork/actions/workflows/security.yml/badge.svg?branch=main)](https://github.com/rockholla/gitspork/actions/workflows/security.yml)
```

- [ ] **Step 3: Verify the badge was added**

Run: `grep -F 'workflows/security.yml/badge.svg?branch=main' README.md && echo "badge present"`
Expected: the matched line, then `badge present`.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: add security status badge to README"
```

---

## Post-implementation notes (not tasks)

- **End-to-end validation** requires pushing `feat/security-scanning` and watching the GitHub Actions "Security" workflow run (CodeQL analysis → gate → badge). Do not push without explicit user confirmation.
- **Enforcing PR blocking** additionally requires requiring the `CodeQL` check in the repo's branch-protection settings — a maintainer Settings action, intentionally not automated here.
- If CodeQL `autobuild` ever fails, replace the `init` `build-mode: autobuild` with a manual build step (`run: go build ./...`).
