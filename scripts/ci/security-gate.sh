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
