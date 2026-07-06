package integrate

import (
	"os"
	"path/filepath"
	"runtime"
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

	// UpstreamSpec.Subpath may be supplied by users with trailing or leading
	// slashes (e.g. "upstream/"). Pre-normalization such inputs made
	// stripSubpath build a "upstream//" prefix that matched nothing, silently
	// dropping every delta from the propagation.
	t.Run("upstreamSubpath with trailing slash still strips prefix", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "upstream/docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "upstream/")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "docs/guide.md",
			"trailing-slash subpath must still strip the prefix so deletions propagate")
	})

	t.Run("upstreamSubpath with leading slash still strips prefix", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "upstream/docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "/upstream")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "docs/guide.md",
			"leading-slash subpath must still strip the prefix so deletions propagate")
	})

	t.Run("upstreamSubpath with trailing slash still finds nested .gitspork.yml for templated delta", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		prevCfg := &config.GitSporkConfig{
			Templated: []config.GitSporkConfigTemplated{
				{Template: "tmpl/foo.go.tmpl", Destination: "out/foo.txt"},
			},
		}
		newCfg := &config.GitSporkConfig{}
		// Config lives at upstream/.gitspork.yml; readConfigFromCommit must not
		// paste an extra "/" when the caller passed "upstream/" as the subpath.
		repo, prevHash, newHash := makeUpstreamWithTemplatedConfigChangeInSubpath(t, dir, "upstream", prevCfg, newCfg)

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, newCfg, "upstream/")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "out/foo.txt",
			"trailing-slash subpath must not prevent nested .gitspork.yml discovery")
	})

	// path.Clean-backed normalization catches more than a naive TrimSuffix would;
	// lock these adjacent shapes down as regressions too.
	t.Run("upstreamSubpath with ./ prefix still strips", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "upstream/docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "./upstream")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "docs/guide.md",
			"./ prefix on subpath must normalize away")
	})

	t.Run("upstreamSubpath with doubled slashes still strips", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "up/stream/docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "up//stream")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "docs/guide.md",
			"doubled slashes in subpath must be collapsed")
	})

	t.Run("upstreamSubpath with interior .. resolves", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "upstream/docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "sibling/../upstream")
		require.NoError(t, err)
		assert.Contains(t, delta.Deletions, "docs/guide.md",
			"interior .. in subpath must be resolved before prefix comparison")
	})

	// The prevHash lookup previously swallowed *every* CommitObject error as
	// "not in history — skip delta silently". That masked I/O failures on the
	// upstream clone (corrupted objects, permission-denied on .git/objects,
	// etc.) — users saw a normal integrate but with no delta propagation.
	// After the fix, only plumbing.ErrObjectNotFound is silent; everything
	// else must surface.
	t.Run("prevHash lookup I/O failure surfaces as error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("permission-denied semantics differ on Windows")
		}
		if os.Getuid() == 0 {
			t.Skip("root bypasses filesystem permission checks")
		}
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		// Real repo with real commits so prevHash is valid.
		repo, prevHash, newHash := makeUpstreamWithDeletedFile(t, dir, "docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		// Sanity: the happy path yields a real deletion delta first.
		delta, err := computeUpstreamDelta(repo, prevHash, newHash, cfg, "")
		require.NoError(t, err)
		require.Contains(t, delta.Deletions, "docs/guide.md")

		// Now strip read permission on .git/objects so any CommitObject read
		// returns an I/O error (permission denied) rather than ErrObjectNotFound.
		objectsDir := filepath.Join(dir, ".git", "objects")
		require.NoError(t, os.Chmod(objectsDir, 0000))
		t.Cleanup(func() { _ = os.Chmod(objectsDir, 0755) })

		delta, err = computeUpstreamDelta(repo, prevHash, newHash, cfg, "")
		require.Error(t, err, "I/O failure on prevHash lookup must surface, not be swallowed as silent no-op")
		assert.Contains(t, err.Error(), prevHash,
			"error should identify which upstream commit failed to load")
		assert.Empty(t, delta.Deletions, "no partial delta should be produced when an error is surfaced")
	})

	t.Run("prevHash genuinely missing still returns empty delta silently", func(t *testing.T) {
		// Belt-and-suspenders next to "prevHash not in repo returns empty delta
		// without error": guards that the fix distinguishes ErrObjectNotFound
		// (silent) from other errors.
		dir, err := os.MkdirTemp("", "gitspork-delta-test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		repo, _, newHash := makeUpstreamWithDeletedFile(t, dir, "docs/guide.md")
		cfg := &config.GitSporkConfig{UpstreamOwned: []config.OwnedEntry{{Pattern: "docs/**"}}}

		// Zero-hash never resolves to a real object; storer reports not-found.
		delta, err := computeUpstreamDelta(repo, plumbing.ZeroHash.String(), newHash, cfg, "")
		require.NoError(t, err, "ErrObjectNotFound must still be treated as silent skip")
		assert.Empty(t, delta.Deletions)
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

// makeUpstreamWithTemplatedConfigChangeInSubpath is a variant that places
// .gitspork.yml under <dir>/<subpath>/.gitspork.yml instead of at the repo root.
func makeUpstreamWithTemplatedConfigChangeInSubpath(t *testing.T, dir, subpath string, prevCfg, newCfg *config.GitSporkConfig) (*gogit.Repository, string, string) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	sig := &object.Signature{Name: config.GitSpork, Email: config.GitSpork + "@localhost", When: time.Now()}

	require.NoError(t, os.MkdirAll(filepath.Join(dir, subpath), 0755))
	configPath := filepath.Join(dir, subpath, config.GitSporkConfigFileName)

	b, err := yaml.Marshal(prevCfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, b, 0644))
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	prev, err := wt.Commit("add subpath config", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	b, err = yaml.Marshal(newCfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, b, 0644))
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	next, err := wt.Commit("update subpath config", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	return repo, prev.String(), next.String()
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
