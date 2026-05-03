package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_computeUpstreamDelta(t *testing.T) {
	t.Run("returns empty delta when prevHash is empty", func(t *testing.T) {
		repo, err := gogit.Init(memory.NewStorage(), nil)
		require.NoError(t, err)
		delta, err := computeUpstreamDelta(repo, "", "abc123", &GitSporkConfig{}, "")
		require.NoError(t, err)
		assert.Empty(t, delta.Deletions)
		assert.Empty(t, delta.Renames)
	})

	t.Run("upstream_owned file deleted appears in Deletions", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "docs/guide.md")
		config := &GitSporkConfig{UpstreamOwned: []string{"docs/**"}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, config, "")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "docs/guide.md")
		assert.Empty(t, delta.Renames)
	})

	t.Run("shared_ownership file renamed appears in Renames", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithRenamedFile(t, dir, "config/old.yml", "config/new.yml")
		config := &GitSporkConfig{
			SharedOwnership: GitSporkConfigSharedOwnership{
				Merged: []string{"config/*.yml"},
			},
		}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, config, "")
		require.NoError(t, err)
		assert.Empty(t, delta.Deletions)
		require.Len(t, delta.Renames, 1)
		assert.Equal(t, "config/old.yml", delta.Renames[0].OldPath)
		assert.Equal(t, "config/new.yml", delta.Renames[0].NewPath)
	})

	t.Run("downstream_owned file deleted does not appear in delta", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "docs/guide.md")
		config := &GitSporkConfig{DownstreamOwned: []string{"docs/**"}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, config, "")
		require.NoError(t, err)
		assert.Empty(t, delta.Deletions)
		assert.Empty(t, delta.Renames)
	})
}

func makeUpstreamWithDeletedFile(t *testing.T, dir, filePath string) (*gogit.Repository, string, string) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)

	fullPath := filepath.Join(dir, filePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
	require.NoError(t, os.WriteFile(fullPath, []byte("content"), 0644))
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: gitSpork, Email: gitSpork + "@localhost", When: time.Now()}
	prev, err := wt.Commit("add file", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	require.NoError(t, os.Remove(fullPath))
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	next, err := wt.Commit("delete file", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	return repo, prev.String(), next.String()
}

func makeUpstreamWithRenamedFile(t *testing.T, dir, oldPath, newPath string) (*gogit.Repository, string, string) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)

	fullOld := filepath.Join(dir, oldPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullOld), 0755))
	require.NoError(t, os.WriteFile(fullOld, []byte("content"), 0644))
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: gitSpork, Email: gitSpork + "@localhost", When: time.Now()}
	prev, err := wt.Commit("add file", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	fullNew := filepath.Join(dir, newPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullNew), 0755))
	require.NoError(t, os.Rename(fullOld, fullNew))
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	next, err := wt.Commit("rename file", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	return repo, prev.String(), next.String()
}

func Test_applyUpstreamDelta(t *testing.T) {
	t.Run("deletes existing file", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-apply-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		target := filepath.Join(dir, "docs/guide.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
		require.NoError(t, os.WriteFile(target, []byte("x"), 0644))

		delta := &upstreamDelta{Deletions: []string{"docs/guide.md"}}
		require.NoError(t, applyUpstreamDelta(delta, dir, NewLogger()))
		_, err = os.Stat(target)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("missing delete target does not error", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-apply-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		delta := &upstreamDelta{Deletions: []string{"docs/guide.md"}}
		assert.NoError(t, applyUpstreamDelta(delta, dir, NewLogger()))
	})

	t.Run("renames existing file to new path", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-apply-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		oldPath := filepath.Join(dir, "config/old.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(oldPath), 0755))
		require.NoError(t, os.WriteFile(oldPath, []byte("content"), 0644))

		delta := &upstreamDelta{Renames: []upstreamRename{{OldPath: "config/old.yml", NewPath: "config/new.yml"}}}
		require.NoError(t, applyUpstreamDelta(delta, dir, NewLogger()))

		_, err = os.Stat(oldPath)
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(dir, "config/new.yml"))
		assert.NoError(t, err)
	})

	t.Run("rename target already exists skips move without error", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-apply-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		oldPath := filepath.Join(dir, "config/old.yml")
		newPath := filepath.Join(dir, "config/new.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(oldPath), 0755))
		require.NoError(t, os.WriteFile(oldPath, []byte("old"), 0644))
		require.NoError(t, os.WriteFile(newPath, []byte("existing"), 0644))

		delta := &upstreamDelta{Renames: []upstreamRename{{OldPath: "config/old.yml", NewPath: "config/new.yml"}}}
		require.NoError(t, applyUpstreamDelta(delta, dir, NewLogger()))

		contents, err := os.ReadFile(newPath)
		require.NoError(t, err)
		assert.Equal(t, "existing", string(contents))
	})

	t.Run("rename source absent skips move without error", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-apply-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		delta := &upstreamDelta{Renames: []upstreamRename{{OldPath: "config/old.yml", NewPath: "config/new.yml"}}}
		require.NoError(t, applyUpstreamDelta(delta, dir, NewLogger()))

		_, err = os.Stat(filepath.Join(dir, "config/new.yml"))
		assert.True(t, os.IsNotExist(err))
	})
}
