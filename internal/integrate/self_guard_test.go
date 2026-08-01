package integrate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
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

func TestEnsureNotSelfIntegration_urlCheck(t *testing.T) {
	t.Run("empty upstreamURL skips URL check", func(t *testing.T) {
		downstream := t.TempDir()
		err := EnsureNotSelfIntegration(downstream, "", "")
		assert.NoError(t, err)
	})

	t.Run("downstream not a git repo — URL check skipped", func(t *testing.T) {
		downstream := t.TempDir() // no git init
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		assert.NoError(t, err)
	})

	t.Run("downstream has no origin — URL check skipped", func(t *testing.T) {
		downstream := initGitRepo(t) // helper defined below; no remotes added
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		assert.NoError(t, err)
	})

	t.Run("origin exactly matches upstream URL", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemote(t, downstream, "origin", "git@github.com:acme/foo.git")
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
		assert.Contains(t, err.Error(), "self-integration blocked")
		assert.Contains(t, err.Error(), "origin remote")
	})

	t.Run("origin SSH matches upstream HTTPS same repo", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemote(t, downstream, "origin", "git@github.com:acme/foo.git")
		err := EnsureNotSelfIntegration(downstream, "https://github.com/acme/foo", "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})

	t.Run("origin differs from upstream — no error", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemote(t, downstream, "origin", "git@github.com:acme/downstream.git")
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/upstream.git", "")
		assert.NoError(t, err)
	})

	t.Run("non-origin remote matches — no error (origin-only policy)", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemote(t, downstream, "origin", "git@github.com:acme/downstream.git")
		addRemote(t, downstream, "upstream", "git@github.com:acme/foo.git")
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		assert.NoError(t, err)
	})

	t.Run("origin has multiple URLs, one matches", func(t *testing.T) {
		downstream := initGitRepo(t)
		addRemoteMultiURL(t, downstream, "origin", []string{
			"git@github.com:acme/downstream.git",
			"https://github.com/acme/foo",
		})
		err := EnsureNotSelfIntegration(downstream, "git@github.com:acme/foo.git", "")
		require.Error(t, err)
		assert.True(t, errors.Is(err, sdktypes.ErrSelfIntegration))
	})
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)
	return dir
}

func addRemote(t *testing.T, dir, name, url string) {
	t.Helper()
	addRemoteMultiURL(t, dir, name, []string{url})
}

func addRemoteMultiURL(t *testing.T, dir, name string, urls []string) {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{Name: name, URLs: urls})
	require.NoError(t, err)
}
