package testharness

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

func NewUpstreamRepo(t *testing.T, files map[string]string, gitsporkYML string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	merged := make(map[string]string, len(files)+1)
	for k, v := range files {
		merged[k] = v
	}
	if gitsporkYML != "" {
		merged[".gitspork.yml"] = gitsporkYML
	}
	WriteFiles(t, dir, merged)
	CommitAll(t, repo, dir, "initial upstream commit")
	return dir
}

func NewDownstreamRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	return dir
}

func WriteFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}
}

func CommitAll(t *testing.T, repo *gogit.Repository, dir, message string) {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: "gitspork-test", Email: "gitspork-test@localhost", When: time.Now()}
	_, err = wt.Commit(message, &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
}

func OpenRepo(t *testing.T, dir string) *gogit.Repository {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	return repo
}

func ReadFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	require.NoError(t, err, "expected file %s to exist in %s", rel, dir)
	return string(b)
}

func AssertFileAbsent(t *testing.T, dir, rel string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, rel))
	require.ErrorIs(t, err, fs.ErrNotExist, "expected file %s to be absent in %s", rel, dir)
}

func AssertFileContains(t *testing.T, dir, rel, substr string) {
	t.Helper()
	content := ReadFile(t, dir, rel)
	require.Contains(t, content, substr, "file %s does not contain %q", rel, substr)
}

// MinimalUpstream initialises a local upstream git repo with a minimal
// .gitspork.yml (upstream_owned only, no templated block) and one file.
// Returns the temp dir and the initial commit hash.
func MinimalUpstream(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "upstream-owned"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "upstream-owned", "file.txt"), []byte("upstream content\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitspork.yml"), []byte("upstream_owned:\n- upstream-owned/**\n"), 0644))
	hash := CommitAllWithMessage(t, repo, "initial")
	return dir, hash
}

// EmptyDownstream initialises a bare local downstream git repo ready for
// Integrate to write into.
func EmptyDownstream(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	return dir
}

// CommitAllWithMessage stages and commits all changes in repo, returning the
// resulting commit hash.
func CommitAllWithMessage(t *testing.T, repo *gogit.Repository, message string) plumbing.Hash {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: "gitspork-test", Email: "gitspork-test@localhost", When: time.Now()}
	hash, err := wt.Commit(message, &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
	return hash
}
