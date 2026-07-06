//go:build functional || functional_docker

package functional

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/rockholla/gitspork/v2/test/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildSubpathUpstream wraps testharness.MinimalUpstreamInSubpath for the
// functional tier. The upstream has:
//   - README.md at the repo root (should NOT be integrated)
//   - infra/.gitspork.yml + infra/upstream-owned/file.txt (should be integrated)
func buildSubpathUpstream(t *testing.T, subpath string) (string, plumbing.Hash) {
	t.Helper()
	return testharness.MinimalUpstreamInSubpath(t, subpath)
}

// TestIntegrate_subpath_via_cli_flag runs `gitspork integrate` with the
// `--upstream-repo-subpath` scalar flag against a monorepo-shaped upstream.
// Locks the subpath-scoped integration behaviour at the compiled-binary tier.
func TestIntegrate_subpath_via_cli_flag(t *testing.T) {
	upstreamDir, upstreamHash := buildSubpathUpstream(t, "infra")
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	args := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--upstream-repo-subpath", "infra",
		"--downstream-repo-path", downstreamDir,
	}
	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "integrate exited non-zero:\n%s", out)

	// File scoped by subpath landed in downstream with the prefix stripped.
	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")

	// Repo-root file MUST NOT have been picked up.
	AssertFileAbsent(t, downstreamDir, "README.md")

	// State records the subpath alongside URL and commit hash so subsequent
	// integrates (and check-drift) can find this upstream.
	stateRaw := ReadFile(t, downstreamDir, ".gitspork/downstream-state.json")
	var state struct {
		Upstreams []struct {
			URL        string `json:"url"`
			Subpath    string `json:"subpath"`
			CommitHash string `json:"commit_hash"`
		} `json:"upstreams"`
	}
	require.NoError(t, json.Unmarshal([]byte(stateRaw), &state))
	require.Len(t, state.Upstreams, 1)
	assert.Equal(t, "infra", state.Upstreams[0].Subpath)
	assert.Equal(t, upstreamHash.String(), state.Upstreams[0].CommitHash)
}

// TestIntegrate_subpath_via_upstream_kv_flag runs `gitspork integrate` with
// the repeatable `--upstream url=...,subpath=...` KV flag. This is the
// multi-upstream flag path — subpath must flow through ParseUpstreamFlag
// into the integration in the same shape as the legacy scalar flag.
func TestIntegrate_subpath_via_upstream_kv_flag(t *testing.T) {
	upstreamDir, _ := buildSubpathUpstream(t, "infra")
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	args := []string{
		"integrate",
		"--upstream", "url=file://" + upstreamDir + ",version=main,subpath=infra",
		"--downstream-repo-path", downstreamDir,
	}
	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "integrate exited non-zero:\n%s", out)

	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
	AssertFileAbsent(t, downstreamDir, "README.md")
}

// TestIntegrate_subpath_trailing_slash_still_matches_state_entry verifies the
// PR #62/#64 fix at the CLI tier: a user who ran `subpath=infra` once and then
// `subpath=infra/` (tab-completion appended slash) must not produce two state
// entries. The delta-propagation path relies on this — a duplicate entry
// causes upstream-remove-file cases to silently miss.
func TestIntegrate_subpath_trailing_slash_still_matches_state_entry(t *testing.T) {
	upstreamDir, _ := buildSubpathUpstream(t, "infra")
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	firstArgs := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--upstream-repo-subpath", "infra",
		"--downstream-repo-path", downstreamDir,
	}
	out, code := runner.Run(t, firstArgs, downstreamDir)
	require.Equal(t, 0, code, "first integrate exited non-zero:\n%s", out)

	// Commit the downstream so tree is clean before re-integrate.
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// Second run: user (or shell tab-completion) sneaks a trailing slash in.
	secondArgs := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--upstream-repo-subpath", "infra/",
		"--downstream-repo-path", downstreamDir,
	}
	out, code = runner.Run(t, secondArgs, downstreamDir)
	require.Equal(t, 0, code, "second integrate (with trailing slash on subpath) exited non-zero:\n%s", out)

	// Exactly one state entry — normalization collapsed the shape variants.
	stateRaw := ReadFile(t, downstreamDir, ".gitspork/downstream-state.json")
	var state struct {
		Upstreams []struct {
			URL     string `json:"url"`
			Subpath string `json:"subpath"`
		} `json:"upstreams"`
	}
	require.NoError(t, json.Unmarshal([]byte(stateRaw), &state))
	assert.Len(t, state.Upstreams, 1,
		"subpath shape variants must resolve to one state entry — locks the PR #62/#64 fix at the CLI boundary")
	// The persisted subpath is the canonical form (no trailing slash).
	assert.Equal(t, "infra", state.Upstreams[0].Subpath)
}

// Sanity: make sure the harness helper produces the expected tree shape.
// If MinimalUpstreamInSubpath changes, the assertions above need to be
// revisited — this test guards against silent drift in the fixture itself.
func TestSubpathUpstreamHarness_tree_shape(t *testing.T) {
	upstreamDir, _ := buildSubpathUpstream(t, "infra")

	_, err := os.Stat(filepath.Join(upstreamDir, "README.md"))
	assert.NoError(t, err, "harness should drop an unrelated README at repo root")

	_, err = os.Stat(filepath.Join(upstreamDir, "infra", ".gitspork.yml"))
	assert.NoError(t, err, "harness should place .gitspork.yml under the subpath dir")

	_, err = os.Stat(filepath.Join(upstreamDir, "infra", "upstream-owned", "file.txt"))
	assert.NoError(t, err, "harness should place upstream-owned content under the subpath dir")

	// And nothing at the repo root that looks like a gitspork target.
	_, err = os.Stat(filepath.Join(upstreamDir, ".gitspork.yml"))
	assert.Error(t, err, "harness must NOT drop .gitspork.yml at the repo root when subpath is set")

	// Sanity that the harness actually made a git repo (some helpers just build a directory).
	_, err = gogit.PlainOpen(upstreamDir)
	assert.NoError(t, err)
}
