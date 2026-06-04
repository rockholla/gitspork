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
