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
