package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrate(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		upstreamDir, err := os.MkdirTemp("", "gitspork-test-upstream")
		require.NoError(t, err)
		defer os.RemoveAll(upstreamDir)

		downstreamDir, err := os.MkdirTemp("", "gitspork-test-downstream")
		require.NoError(t, err)
		defer os.RemoveAll(downstreamDir)

		makeUpstreamRepo(t, upstreamDir)

		err = Integrate(&IntegrateOptions{
			Logger:              NewLogger(),
			UpstreamRepoURL:     upstreamDir,
			UpstreamRepoVersion: "master",
			DownstreamRepoPath:  downstreamDir,
		})
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(downstreamDir, "upstream-owned", "sub", "sub", "sub-sub.txt"))
		assert.NoError(t, err)
	})
}

// makeUpstreamRepo initialises a local git repo with the minimal upstream structure needed for integration tests.
func makeUpstreamRepo(t *testing.T, dir string) {
	t.Helper()

	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("master")),
	)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "upstream-owned", "sub", "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "upstream-owned", "sub", "sub", "sub-sub.txt"), []byte("content"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitspork.yml"), []byte("version: dev\nupstream_owned:\n- upstream-owned/**/*\n"), 0644))

	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: gitSpork, Email: gitSpork + "@localhost", When: time.Now()}
	_, err = wt.Commit("initial", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
}
