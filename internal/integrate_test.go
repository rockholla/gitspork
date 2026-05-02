package internal

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_resolveUpstreamURL(t *testing.T) {
	t.Run("SSH agent present, no token, HTTPS url -> rewrite to SSH", func(t *testing.T) {
		os.Setenv("SSH_AUTH_SOCK", "/tmp/fake.sock")
		defer os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("https://github.com/org/repo.git", "")
		assert.Equal(t, "git@github.com:org/repo.git", result)
	})

	t.Run("no SSH agent, token provided, SSH url -> rewrite to HTTPS", func(t *testing.T) {
		os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("git@github.com:org/repo.git", "mytoken")
		assert.Equal(t, "https://github.com/org/repo.git", result)
	})

	t.Run("SSH agent present with token, SSH url -> no rewrite", func(t *testing.T) {
		os.Setenv("SSH_AUTH_SOCK", "/tmp/fake.sock")
		defer os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("git@github.com:org/repo.git", "mytoken")
		assert.Equal(t, "git@github.com:org/repo.git", result)
	})

	t.Run("no SSH agent, no token, SSH url -> no rewrite", func(t *testing.T) {
		os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("git@github.com:org/repo.git", "")
		assert.Equal(t, "git@github.com:org/repo.git", result)
	})

	t.Run("no SSH agent, no token, HTTPS url -> no rewrite", func(t *testing.T) {
		os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("https://github.com/org/repo.git", "")
		assert.Equal(t, "https://github.com/org/repo.git", result)
	})
}
