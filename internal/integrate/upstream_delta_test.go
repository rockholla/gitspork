package integrate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/goccy/go-yaml"
	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/logutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_computeUpstreamDelta(t *testing.T) {
	t.Run("returns empty delta when prevHash is empty", func(t *testing.T) {
		repo, err := gogit.Init(memory.NewStorage(), nil)
		require.NoError(t, err)
		delta, err := computeUpstreamDelta(repo, "", "abc123", &config.GitSporkConfig{}, "")
		require.NoError(t, err)
		assert.Empty(t, delta.Deletions)
		assert.Empty(t, delta.Renames)
	})

	t.Run("upstream_owned file deleted appears in Deletions", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "docs/guide.md")
		assert.Empty(t, delta.Renames)
	})

	t.Run("shared_ownership file renamed appears in Renames", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithRenamedFile(t, dir, "config/old.yml", "config/new.yml")
		cfg := &config.GitSporkConfig{
			SharedOwnership: config.GitSporkConfigSharedOwnership{
				Merged: []string{"config/*.yml"},
			},
		}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "")
		require.NoError(t, err)
		assert.Empty(t, delta.Deletions)
		require.Len(t, delta.Renames, 1)
		assert.Equal(t, "config/old.yml", delta.Renames[0].OldPath)
		assert.Equal(t, "config/new.yml", delta.Renames[0].NewPath)
	})

	t.Run("upstream_owned rename entry: deleted source maps to destination in Deletions", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "configs/app.yml")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{From: "configs/**", To: ".configs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, ".configs/app.yml")
		assert.NotContains(t, delta.Deletions, "configs/app.yml")
		assert.Empty(t, delta.Renames)
	})

	t.Run("downstream_owned file deleted does not appear in delta", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "docs/guide.md")
		cfg := &config.GitSporkConfig{DownstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "")
		require.NoError(t, err)
		assert.Empty(t, delta.Deletions)
		assert.Empty(t, delta.Renames)
	})

	t.Run("prevHash not in repo returns empty delta without error", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, _, newHash := makeUpstreamWithDeletedFile(t, dir, "docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, "0000000000000000000000000000000000000000", newHash, cfg, "")
		require.NoError(t, err)
		assert.Empty(t, delta.Deletions)
		assert.Empty(t, delta.Renames)
	})

	t.Run("upstreamSubpath prefix is stripped from delta paths", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		// file lives at upstream/docs/guide.md in the repo tree
		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "upstream/docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "upstream")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "docs/guide.md")
	})

	t.Run("templated destination removed appears in Deletions", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		prevCfg := &config.GitSporkConfig{
			Templated: []config.GitSporkConfigTemplated{
				{Template: "tmpl/foo.go.tmpl", Destination: "out/foo.txt"},
			},
		}
		newCfg := &config.GitSporkConfig{}
		repo, prevHash, newHash := makeUpstreamWithTemplatedConfigChange(t, dir, prevCfg, newCfg)

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, newCfg, "")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "out/foo.txt")
	})

	t.Run("templated destination changed appears in Renames", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		prevCfg := &config.GitSporkConfig{
			Templated: []config.GitSporkConfigTemplated{
				{Template: "tmpl/foo.go.tmpl", Destination: "out/old.txt"},
			},
		}
		newCfg := &config.GitSporkConfig{
			Templated: []config.GitSporkConfigTemplated{
				{Template: "tmpl/foo.go.tmpl", Destination: "out/new.txt"},
			},
		}
		repo, prevHash, newHash := makeUpstreamWithTemplatedConfigChange(t, dir, prevCfg, newCfg)

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, newCfg, "")
		require.NoError(t, err)
		require.Len(t, delta.Renames, 1)
		assert.Equal(t, "out/old.txt", delta.Renames[0].OldPath)
		assert.Equal(t, "out/new.txt", delta.Renames[0].NewPath)
	})
}

func Test_buildManagedMatchers_resolvesRenameDest(t *testing.T) {
	cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{
		{From: "configs/**", To: ".configs/**"},
		{Pattern: "docs/**"},
	}}
	matchers, err := buildManagedMatchers(cfg)
	require.NoError(t, err)

	dest, ok := resolveManagedDest("configs/app.yml", matchers)
	require.True(t, ok)
	assert.Equal(t, ".configs/app.yml", dest)

	dest, ok = resolveManagedDest("docs/x.md", matchers)
	require.True(t, ok)
	assert.Equal(t, "docs/x.md", dest)

	_, ok = resolveManagedDest("unmanaged.txt", matchers)
	assert.False(t, ok)
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
	sig := &object.Signature{Name: config.GitSpork, Email: config.GitSpork + "@localhost", When: time.Now()}
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
	sig := &object.Signature{Name: config.GitSpork, Email: config.GitSpork + "@localhost", When: time.Now()}
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
		require.NoError(t, applyUpstreamDelta(delta, dir, logutil.New()))
		_, err = os.Stat(target)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("missing delete target does not error", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-apply-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		delta := &upstreamDelta{Deletions: []string{"docs/guide.md"}}
		assert.NoError(t, applyUpstreamDelta(delta, dir, logutil.New()))
	})

	t.Run("renames existing file to new path", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-apply-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		oldPath := filepath.Join(dir, "config/old.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(oldPath), 0755))
		require.NoError(t, os.WriteFile(oldPath, []byte("content"), 0644))

		delta := &upstreamDelta{Renames: []upstreamRename{{OldPath: "config/old.yml", NewPath: "config/new.yml"}}}
		require.NoError(t, applyUpstreamDelta(delta, dir, logutil.New()))

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
		require.NoError(t, applyUpstreamDelta(delta, dir, logutil.New()))

		contents, err := os.ReadFile(newPath)
		require.NoError(t, err)
		assert.Equal(t, "existing", string(contents))
	})

	t.Run("rename source absent skips move without error", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-apply-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		delta := &upstreamDelta{Renames: []upstreamRename{{OldPath: "config/old.yml", NewPath: "config/new.yml"}}}
		require.NoError(t, applyUpstreamDelta(delta, dir, logutil.New()))

		_, err = os.Stat(filepath.Join(dir, "config/new.yml"))
		assert.True(t, os.IsNotExist(err))
	})
}

func makeUpstreamWithTemplatedConfigChange(t *testing.T, dir string, prevCfg, newCfg *config.GitSporkConfig) (*gogit.Repository, string, string) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	sig := &object.Signature{Name: config.GitSpork, Email: config.GitSpork + "@localhost", When: time.Now()}

	// write prevCfg as .gitspork.yml and commit
	b, err := yaml.Marshal(prevCfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.GitSporkConfigFileName), b, 0644))
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	prev, err := wt.Commit("add config", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	// overwrite with newCfg and commit
	b, err = yaml.Marshal(newCfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.GitSporkConfigFileName), b, 0644))
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	next, err := wt.Commit("update config", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	return repo, prev.String(), next.String()
}
