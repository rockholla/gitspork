package integrate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	gogitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/rockholla/gitspork/internal/config"
	"github.com/rockholla/gitspork/internal/logutil"
	"github.com/rockholla/gitspork/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_applySSHKnownHosts(t *testing.T) {
	t.Run("no-ops when SSH_KNOWN_HOSTS points to nonexistent file", func(t *testing.T) {
		t.Setenv("SSH_KNOWN_HOSTS", "/nonexistent/known_hosts")
		auth := &gogitssh.PublicKeysCallback{}
		require.NoError(t, applySSHKnownHosts(auth))
		assert.Nil(t, auth.HostKeyCallback)
	})

	t.Run("no-ops when SSH_KNOWN_HOSTS is not set", func(t *testing.T) {
		t.Setenv("SSH_KNOWN_HOSTS", "")
		auth := &gogitssh.PublicKeysCallback{}
		require.NoError(t, applySSHKnownHosts(auth))
		assert.Nil(t, auth.HostKeyCallback)
	})

	t.Run("sets HostKeyCallback when SSH_KNOWN_HOSTS points to a valid file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "known_hosts")
		require.NoError(t, os.WriteFile(f, []byte(""), 0600))
		t.Setenv("SSH_KNOWN_HOSTS", f)
		auth := &gogitssh.PublicKeysCallback{}
		require.NoError(t, applySSHKnownHosts(auth))
		assert.NotNil(t, auth.HostKeyCallback)
	})
}

func Test_resolveUpstreamURL(t *testing.T) {
	t.Run("no token, HTTPS url -> rewrite to SSH", func(t *testing.T) {
		result := resolveUpstreamURL("https://github.com/org/repo.git", "")
		assert.Equal(t, "git@github.com:org/repo.git", result)
	})

	t.Run("token provided, SSH url -> rewrite to HTTPS", func(t *testing.T) {
		result := resolveUpstreamURL("git@github.com:org/repo.git", "mytoken")
		assert.Equal(t, "https://github.com/org/repo.git", result)
	})

	t.Run("token provided, HTTPS url -> no rewrite", func(t *testing.T) {
		result := resolveUpstreamURL("https://github.com/org/repo.git", "mytoken")
		assert.Equal(t, "https://github.com/org/repo.git", result)
	})

	t.Run("no token, SSH url -> no rewrite", func(t *testing.T) {
		result := resolveUpstreamURL("git@github.com:org/repo.git", "")
		assert.Equal(t, "git@github.com:org/repo.git", result)
	})
}

func Test_ParseUpstreamFlag(t *testing.T) {
	t.Run("url only", func(t *testing.T) {
		spec, err := ParseUpstreamFlag("url=git@github.com:org/repo.git")
		require.NoError(t, err)
		assert.Equal(t, "git@github.com:org/repo.git", spec.URL)
		assert.Equal(t, "", spec.Version)
		assert.Equal(t, "", spec.Subpath)
		assert.Equal(t, "", spec.Token)
	})
	t.Run("all keys", func(t *testing.T) {
		spec, err := ParseUpstreamFlag("url=https://github.com/org/repo.git,version=main,subpath=infra,token=tok")
		require.NoError(t, err)
		assert.Equal(t, "https://github.com/org/repo.git", spec.URL)
		assert.Equal(t, "main", spec.Version)
		assert.Equal(t, "infra", spec.Subpath)
		assert.Equal(t, "tok", spec.Token)
	})
	t.Run("missing url returns error", func(t *testing.T) {
		_, err := ParseUpstreamFlag("version=main")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url")
	})
	t.Run("unknown key returns error", func(t *testing.T) {
		_, err := ParseUpstreamFlag("url=git@github.com:org/repo.git,branch=main")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "branch")
	})
}

func Test_normalizeUpstreamURL(t *testing.T) {
	t.Run("SSH and HTTPS same repo match", func(t *testing.T) {
		assert.Equal(t,
			NormalizeUpstreamURL("git@github.com:org/repo.git", ""),
			NormalizeUpstreamURL("https://github.com/org/repo.git", ""))
	})
	t.Run("subpath included in key", func(t *testing.T) {
		assert.NotEqual(t,
			NormalizeUpstreamURL("git@github.com:org/repo.git", "infra"),
			NormalizeUpstreamURL("git@github.com:org/repo.git", ""))
	})
	t.Run("trailing .git stripped", func(t *testing.T) {
		assert.Equal(t,
			NormalizeUpstreamURL("https://github.com/org/repo.git", ""),
			NormalizeUpstreamURL("https://github.com/org/repo", ""))
	})
}

func Test_UpsertUpstreamState_newEntry(t *testing.T) {
	state := &types.GitSporkDownstreamState{}
	UpsertUpstreamState(state, types.GitSporkUpstreamState{URL: "https://github.com/org/repo.git", CommitHash: "abc"})
	require.Len(t, state.Upstreams, 1)
	assert.Equal(t, "https://github.com/org/repo.git", state.Upstreams[0].URL)
	assert.Equal(t, "abc", state.Upstreams[0].CommitHash)
}

func Test_UpsertUpstreamState_updateExisting(t *testing.T) {
	state := &types.GitSporkDownstreamState{Upstreams: []types.GitSporkUpstreamState{
		{URL: "git@github.com:org/repo.git", CommitHash: "old"},
	}}
	// SSH and HTTPS forms of same repo — should match and update in place
	UpsertUpstreamState(state, types.GitSporkUpstreamState{URL: "https://github.com/org/repo.git", CommitHash: "new"})
	require.Len(t, state.Upstreams, 1)
	assert.Equal(t, "new", state.Upstreams[0].CommitHash)
}

func Test_UpsertUpstreamState_orderPreserved(t *testing.T) {
	state := &types.GitSporkDownstreamState{Upstreams: []types.GitSporkUpstreamState{
		{URL: "https://github.com/org/base.git", CommitHash: "b1"},
		{URL: "https://github.com/org/platform.git", CommitHash: "p1"},
	}}
	UpsertUpstreamState(state, types.GitSporkUpstreamState{URL: "https://github.com/org/base.git", CommitHash: "b2"})
	require.Len(t, state.Upstreams, 2)
	assert.Equal(t, "b2", state.Upstreams[0].CommitHash)
	assert.Equal(t, "p1", state.Upstreams[1].CommitHash)
}

func Test_LoadDownstreamState_migration(t *testing.T) {
	dir := t.TempDir()
	metaDir := filepath.Join(dir, ".gitspork")
	require.NoError(t, os.MkdirAll(metaDir, 0755))
	oldState := `{"migrations_complete":["m1"],"last_upstream_repo_url":"git@github.com:org/repo.git","last_upstream_repo_subpath":"infra","last_upstream_commit_hash":"abc123"}`
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "downstream-state.json"), []byte(oldState), 0644))

	state, err := LoadDownstreamState(dir)
	require.NoError(t, err)
	require.Len(t, state.Upstreams, 1)
	assert.Equal(t, "git@github.com:org/repo.git", state.Upstreams[0].URL)
	assert.Equal(t, "infra", state.Upstreams[0].Subpath)
	assert.Equal(t, "abc123", state.Upstreams[0].CommitHash)
	// deprecated fields cleared
	assert.Equal(t, "", state.LastUpstreamRepoURL)
	assert.Equal(t, "", state.LastUpstreamCommitHash)
	assert.Equal(t, "", state.LastUpstreamRepoSubpath)
}

// testMinimalUpstream initialises a local upstream git repo with a minimal
// .gitspork.yml (upstream_owned only, no templated block) and one file. Returns
// the temp dir and the initial commit hash.
func testMinimalUpstream(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "upstream-owned"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "upstream-owned", "file.txt"), []byte("upstream content\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitspork.yml"), []byte("upstream_owned:\n- upstream-owned/**\n"), 0644))
	hash := testCommitAll(t, repo, "initial")
	return dir, hash
}

// testEmptyDownstream initialises a bare local downstream git repo ready for
// Integrate to write into.
func testEmptyDownstream(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	return dir
}

// testCommitAll stages and commits all changes in repo, returning the commit hash.
func testCommitAll(t *testing.T, repo *gogit.Repository, message string) plumbing.Hash {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: "gitspork-test", Email: "gitspork-test@localhost", When: time.Now()}
	hash, err := wt.Commit(message, &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
	return hash
}

func TestIntegrate_honors_UpstreamRepoCommit(t *testing.T) {
	// Create a local upstream repo with two commits; verify that Integrate
	// checks out the older commit (v1) when UpstreamRepoCommit is set.
	upstreamDir := t.TempDir()
	upstreamRepo, err := gogit.PlainInit(upstreamDir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)

	const gitsporkYML = `upstream_owned:
- upstream-owned/**
`

	// Commit v1: write version one content.
	require.NoError(t, os.MkdirAll(filepath.Join(upstreamDir, "upstream-owned"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "upstream-owned", "file.txt"), []byte("version one\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, ".gitspork.yml"), []byte(gitsporkYML), 0644))
	commitV1 := testCommitAll(t, upstreamRepo, "v1")

	// Commit v2: update to version two content.
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "upstream-owned", "file.txt"), []byte("version two\n"), 0644))
	testCommitAll(t, upstreamRepo, "v2")

	// Create downstream repo.
	downstreamDir := t.TempDir()
	_, err = gogit.PlainInit(downstreamDir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)

	logger := logutil.New()
	_, err = Integrate(&types.IntegrateOptions{
		Logger:             logger,
		UpstreamRepoURL:    "file://" + upstreamDir,
		UpstreamRepoCommit: commitV1.String(),
		DownstreamRepoPath: downstreamDir,
		ForDriftCheck:      true, // skip state write; we only care about file content
	})
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(downstreamDir, "upstream-owned", "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "version one\n", string(content),
		"Integrate with UpstreamRepoCommit set to v1 should produce v1 content, not HEAD (v2)")
}

func TestIntegrate_returns_result_with_upstream_url_and_hash(t *testing.T) {
	upstreamDir, upstreamHash := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)

	result, err := Integrate(&types.IntegrateOptions{
		Logger:             logutil.New(),
		Upstreams:          []types.UpstreamSpec{{URL: "file://" + upstreamDir, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Upstreams, 1)
	assert.Equal(t, "file://"+upstreamDir, result.Upstreams[0].URL)
	assert.Equal(t, upstreamHash.String(), result.Upstreams[0].CommitHash)
	assert.Equal(t, "", result.Upstreams[0].Subpath)
}

func TestIntegrateLocal_returns_result_with_upstream_paths(t *testing.T) {
	upstreamDir, _ := testMinimalUpstream(t)
	downstreamDir := testEmptyDownstream(t)

	result, err := IntegrateLocal(&types.IntegrateLocalOptions{
		Logger:         logutil.New(),
		UpstreamPaths:  []string{upstreamDir},
		DownstreamPath: downstreamDir,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Upstreams, 1)
	// IntegrateLocal has no URL — record the path in the URL slot with no scheme.
	assert.Equal(t, upstreamDir, result.Upstreams[0].URL)
	assert.Equal(t, "", result.Upstreams[0].CommitHash)
}

func TestIntegrate(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		upstreamDir, err := os.MkdirTemp("", "gitspork-test-upstream")
		require.NoError(t, err)
		defer os.RemoveAll(upstreamDir)

		downstreamDir, err := os.MkdirTemp("", "gitspork-test-downstream")
		require.NoError(t, err)
		defer os.RemoveAll(downstreamDir)

		makeUpstreamRepo(t, upstreamDir)

		_, err = Integrate(&types.IntegrateOptions{
			Logger:              logutil.New(),
			UpstreamRepoURL:     upstreamDir,
			UpstreamRepoVersion: "master",
			DownstreamRepoPath:  downstreamDir,
		})
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(downstreamDir, "upstream-owned", "sub", "sub", "sub-sub.txt"))
		assert.NoError(t, err)
	})
}

// makeUpstreamRepo initialises a local git repo with the minimal upstream structure needed for integration tests.
func makeUpstreamRepo(t *testing.T, dir string) {
	t.Helper()

	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("master")),
	)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "upstream-owned", "sub", "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "upstream-owned", "sub", "sub", "sub-sub.txt"), []byte("content"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitspork.yml"), []byte("version: dev\nupstream_owned:\n- upstream-owned/**/*\n"), 0644))

	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: config.GitSpork, Email: config.GitSpork + "@localhost", When: time.Now()}
	_, err = wt.Commit("initial", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
}
