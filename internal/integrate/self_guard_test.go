package integrate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureNotSelfIntegration_pathCheck(t *testing.T) {
	t.Run("empty upstreamLocalPath skips path check", func(t *testing.T) {
		downstream := t.TempDir()
		err := EnsureNotSelfIntegration(downstream, "", "")
		assert.NoError(t, err)
	})

	t.Run("upstream path == downstream path errors", func(t *testing.T) {
		downstream := t.TempDir()
		err := EnsureNotSelfIntegration(downstream, "", downstream)
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
		assert.Contains(t, err.Error(), "self-integration blocked")
		assert.Contains(t, err.Error(), downstream)
	})

	t.Run("upstream inside downstream errors", func(t *testing.T) {
		downstream := t.TempDir()
		upstream := filepath.Join(downstream, "templates")
		require.NoError(t, os.MkdirAll(upstream, 0755))
		err := EnsureNotSelfIntegration(downstream, "", upstream)
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})

	t.Run("downstream inside upstream errors", func(t *testing.T) {
		upstream := t.TempDir()
		downstream := filepath.Join(upstream, "some-child")
		require.NoError(t, os.MkdirAll(downstream, 0755))
		err := EnsureNotSelfIntegration(downstream, "", upstream)
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})

	t.Run("unrelated absolute paths pass", func(t *testing.T) {
		downstream := t.TempDir()
		upstream := t.TempDir()
		err := EnsureNotSelfIntegration(downstream, "", upstream)
		assert.NoError(t, err)
	})

	t.Run("upstream path that doesn't exist yet — unrelated, passes", func(t *testing.T) {
		downstream := t.TempDir()
		// Sibling of downstream that isn't created on disk.
		upstream := filepath.Join(filepath.Dir(downstream), "not-yet-created")
		err := EnsureNotSelfIntegration(downstream, "", upstream)
		assert.NoError(t, err)
	})

	t.Run("symlink pointing into downstream triggers error after EvalSymlinks", func(t *testing.T) {
		downstream := t.TempDir()
		realDir := filepath.Join(downstream, "real")
		require.NoError(t, os.MkdirAll(realDir, 0755))
		symlinkParent := t.TempDir()
		symlink := filepath.Join(symlinkParent, "link-into-downstream")
		require.NoError(t, os.Symlink(realDir, symlink))
		err := EnsureNotSelfIntegration(downstream, "", symlink)
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})
}
