//go:build functional || functional_docker

package functional

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oldFormatState represents the pre-multi-upstream state file schema — a
// single upstream captured directly at the top level via the now-deprecated
// last_upstream_* fields. LoadDownstreamState migrates instances of this
// shape into the Upstreams slice on first read (see integrate.go:711-720).
// A functional test seeds this shape into .gitspork/downstream-state.json
// so the migration is exercised on the exact upgrade path real v1.x → v2.x
// users hit.
type oldFormatState struct {
	MigrationsComplete      []string `json:"migrations_complete"`
	LastUpstreamRepoURL     string   `json:"last_upstream_repo_url"`
	LastUpstreamRepoSubpath string   `json:"last_upstream_repo_subpath,omitempty"`
	LastUpstreamCommitHash  string   `json:"last_upstream_commit_hash"`
}

// stateAfterIntegrate is decoded post-run to inspect what shape landed on
// disk. The deprecated fields are declared here explicitly (not via the
// public gitspork.DownstreamState) so a genuine schema regression that
// dropped omitempty would be visible as a non-empty value here.
type stateAfterIntegrate struct {
	MigrationsComplete []string `json:"migrations_complete"`
	Upstreams          []struct {
		URL        string `json:"url"`
		Subpath    string `json:"subpath,omitempty"`
		CommitHash string `json:"commit_hash"`
	} `json:"upstreams"`
	LastUpstreamRepoURL     string `json:"last_upstream_repo_url,omitempty"`
	LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
	LastUpstreamCommitHash  string `json:"last_upstream_commit_hash,omitempty"`
}

// seedOldFormatState writes an old-format downstream-state.json into the
// downstream repo's .gitspork/ directory. Simulates a repo that was
// integrated with a pre-multi-upstream gitspork release.
func seedOldFormatState(t *testing.T, downstreamDir string, state oldFormatState) {
	t.Helper()
	metaDir := filepath.Join(downstreamDir, ".gitspork")
	require.NoError(t, os.MkdirAll(metaDir, 0755))
	b, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "downstream-state.json"), b, 0644))
}

// TestStateMigration_seedMatchesFreshIntegrate is the common upgrade path: a
// v1.x user has been integrating against a single upstream; after upgrading
// they run `gitspork integrate` against that same upstream. The migrated
// entry must be recognised by UpsertUpstreamState and updated in place with
// the fresh commit hash — NOT duplicated.
func TestStateMigration_seedMatchesFreshIntegrate(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)

	// Seed old-format state with the same URL we'll be integrating against.
	// The commit_hash is intentionally bogus — Integrate will replace it with
	// the real one from the upstream repo. The migrations_complete list is
	// carried forward to prove the upgrade doesn't lose downstream progress.
	seedOldFormatState(t, downstreamDir, oldFormatState{
		MigrationsComplete:     []string{"legacy/0001:pre_integrate", "legacy/0002:post_integrate"},
		LastUpstreamRepoURL:    "file://" + upstreamDir,
		LastUpstreamCommitHash: "0000000000000000000000000000000000000000",
	})

	runner := resolveRunner(t, upstreamDir, downstreamDir)
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "integrate against old-format state exited non-zero:\n%s", out)

	state := readStateAfterIntegrate(t, downstreamDir)

	// Exactly one upstream entry — the migration recognised the seeded URL and
	// UpsertUpstreamState updated it in place with the fresh commit hash.
	require.Len(t, state.Upstreams, 1,
		"seeded old-format entry with matching URL must be migrated AND updated in place, not duplicated")
	assert.Equal(t, "file://"+upstreamDir, state.Upstreams[0].URL)
	assert.NotEqual(t, "0000000000000000000000000000000000000000", state.Upstreams[0].CommitHash,
		"CommitHash must have been replaced with the real one from the current integrate")
	assert.NotEmpty(t, state.Upstreams[0].CommitHash)

	// Legacy migrations_complete must be preserved so downstream hooks don't
	// re-run after the upgrade.
	assert.Equal(t, []string{"legacy/0001:pre_integrate", "legacy/0002:post_integrate"}, state.MigrationsComplete)

	// Deprecated fields cleared. With omitempty they should not appear in the
	// serialised JSON at all — assert on the decoded struct for defence in
	// depth, and separately on the raw bytes below.
	assert.Empty(t, state.LastUpstreamRepoURL)
	assert.Empty(t, state.LastUpstreamCommitHash)
	assert.Empty(t, state.LastUpstreamRepoSubpath)

	// Raw-bytes check: no field key called "last_upstream_*" should survive.
	raw := ReadFile(t, downstreamDir, ".gitspork/downstream-state.json")
	assert.NotContains(t, raw, "last_upstream_repo_url",
		"deprecated key must be absent from written state (omitempty should elide it)")
	assert.NotContains(t, raw, "last_upstream_commit_hash")
	assert.NotContains(t, raw, "last_upstream_repo_subpath")
}

// TestStateMigration_seedForDifferentUpstream: seed the old state with a
// URL for a legacy upstream, then integrate against a new upstream. Both
// entries should end up in the Upstreams slice — the migrated legacy one
// AND the fresh one — so downstream repos don't lose track of upstreams
// they were previously integrated against.
func TestStateMigration_seedForDifferentUpstream(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)

	// A legacy URL that DOESN'T match the current integrate. The commit_hash
	// is a real-looking 40-char hex string so it looks like a valid stored
	// state entry.
	const legacyURL = "file:///legacy/nonexistent-upstream"
	const legacyCommit = "1111111111111111111111111111111111111111"
	const legacySubpath = "some/subdir"
	seedOldFormatState(t, downstreamDir, oldFormatState{
		LastUpstreamRepoURL:     legacyURL,
		LastUpstreamRepoSubpath: legacySubpath,
		LastUpstreamCommitHash:  legacyCommit,
	})

	runner := resolveRunner(t, upstreamDir, downstreamDir)
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "integrate against differing-URL old-format state exited non-zero:\n%s", out)

	state := readStateAfterIntegrate(t, downstreamDir)

	// Two entries — the migrated legacy one and the freshly integrated one.
	require.Len(t, state.Upstreams, 2,
		"differing legacy URL must produce a distinct migrated entry alongside the fresh one")

	// Find each by URL so ordering isn't part of the contract we test.
	byURL := map[string]struct {
		Subpath    string
		CommitHash string
	}{}
	for _, u := range state.Upstreams {
		byURL[u.URL] = struct {
			Subpath    string
			CommitHash string
		}{u.Subpath, u.CommitHash}
	}

	legacy, ok := byURL[legacyURL]
	require.True(t, ok, "migrated legacy URL entry missing")
	assert.Equal(t, legacySubpath, legacy.Subpath, "migrated legacy subpath must be preserved")
	assert.Equal(t, legacyCommit, legacy.CommitHash, "migrated legacy commit_hash must be preserved verbatim")

	fresh, ok := byURL["file://"+upstreamDir]
	require.True(t, ok, "fresh integrate URL entry missing")
	assert.NotEqual(t, legacyCommit, fresh.CommitHash, "fresh entry must carry the real current-integrate commit hash")

	// Deprecated fields cleared as before.
	assert.Empty(t, state.LastUpstreamRepoURL)
	assert.Empty(t, state.LastUpstreamCommitHash)
	assert.Empty(t, state.LastUpstreamRepoSubpath)
}

func readStateAfterIntegrate(t *testing.T, downstreamDir string) stateAfterIntegrate {
	t.Helper()
	raw := ReadFile(t, downstreamDir, ".gitspork/downstream-state.json")
	var state stateAfterIntegrate
	require.NoError(t, json.Unmarshal([]byte(raw), &state))
	return state
}
