package internal

import (
	"os"
	"path/filepath"
	"testing"

	gogitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
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
			normalizeUpstreamURL("git@github.com:org/repo.git", ""),
			normalizeUpstreamURL("https://github.com/org/repo.git", ""))
	})
	t.Run("subpath included in key", func(t *testing.T) {
		assert.NotEqual(t,
			normalizeUpstreamURL("git@github.com:org/repo.git", "infra"),
			normalizeUpstreamURL("git@github.com:org/repo.git", ""))
	})
	t.Run("trailing .git stripped", func(t *testing.T) {
		assert.Equal(t,
			normalizeUpstreamURL("https://github.com/org/repo.git", ""),
			normalizeUpstreamURL("https://github.com/org/repo", ""))
	})
}

func Test_upsertUpstreamState_newEntry(t *testing.T) {
	state := &GitSporkDownstreamState{}
	upsertUpstreamState(state, GitSporkUpstreamState{URL: "https://github.com/org/repo.git", CommitHash: "abc"})
	require.Len(t, state.Upstreams, 1)
	assert.Equal(t, "https://github.com/org/repo.git", state.Upstreams[0].URL)
	assert.Equal(t, "abc", state.Upstreams[0].CommitHash)
}

func Test_upsertUpstreamState_updateExisting(t *testing.T) {
	state := &GitSporkDownstreamState{Upstreams: []GitSporkUpstreamState{
		{URL: "git@github.com:org/repo.git", CommitHash: "old"},
	}}
	// SSH and HTTPS forms of same repo — should match and update in place
	upsertUpstreamState(state, GitSporkUpstreamState{URL: "https://github.com/org/repo.git", CommitHash: "new"})
	require.Len(t, state.Upstreams, 1)
	assert.Equal(t, "new", state.Upstreams[0].CommitHash)
}

func Test_upsertUpstreamState_orderPreserved(t *testing.T) {
	state := &GitSporkDownstreamState{Upstreams: []GitSporkUpstreamState{
		{URL: "https://github.com/org/base.git", CommitHash: "b1"},
		{URL: "https://github.com/org/platform.git", CommitHash: "p1"},
	}}
	upsertUpstreamState(state, GitSporkUpstreamState{URL: "https://github.com/org/base.git", CommitHash: "b2"})
	require.Len(t, state.Upstreams, 2)
	assert.Equal(t, "b2", state.Upstreams[0].CommitHash)
	assert.Equal(t, "p1", state.Upstreams[1].CommitHash)
}

func Test_loadDownstreamState_migration(t *testing.T) {
	dir := t.TempDir()
	metaDir := filepath.Join(dir, ".gitspork")
	require.NoError(t, os.MkdirAll(metaDir, 0755))
	oldState := `{"migrations_complete":["m1"],"last_upstream_repo_url":"git@github.com:org/repo.git","last_upstream_repo_subpath":"infra","last_upstream_commit_hash":"abc123"}`
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "downstream-state.json"), []byte(oldState), 0644))

	state, err := loadDownstreamState(dir)
	require.NoError(t, err)
	require.Len(t, state.Upstreams, 1)
	assert.Equal(t, "git@github.com:org/repo.git", state.Upstreams[0].URL)
	assert.Equal(t, "infra", state.Upstreams[0].Subpath)
	assert.Equal(t, "abc123", state.Upstreams[0].CommitHash)
	// deprecated fields cleared
	assert.Equal(t, "", state.LastUpstreamRepoURL)
	assert.Equal(t, "", state.LastUpstreamCommitHash)
}
