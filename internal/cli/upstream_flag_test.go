package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
