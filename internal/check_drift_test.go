package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckDrift(t *testing.T) {
	t.Run("returns error when no previous integration in state", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test-downstream")
		assert.Nil(t, err)
		defer os.RemoveAll(dir)

		err = CheckDrift(&CheckDriftOptions{
			Logger:             NewLogger(),
			DownstreamRepoPath: dir,
		})
		assert.ErrorContains(t, err, "no previous integration found")
	})

	t.Run("returns error when working tree is dirty", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test-downstream")
		assert.Nil(t, err)
		defer os.RemoveAll(dir)

		runCmd(t, dir, "git", "init")
		runCmd(t, dir, "git", "commit", "--allow-empty", "-m", "init")
		err = os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirty"), 0644)
		assert.Nil(t, err)

		state := &GitSporkDownstreamState{
			LastUpstreamRepoURL:     "https://github.com/rockholla/gitspork.git",
			LastUpstreamRepoSubpath: "docs/examples/simple/upstream",
			LastUpstreamCommitHash:  "abc123",
		}
		err = saveDownstreamState(dir, state)
		assert.Nil(t, err)

		err = CheckDrift(&CheckDriftOptions{
			Logger:             NewLogger(),
			DownstreamRepoPath: dir,
		})
		assert.ErrorContains(t, err, "working tree is not clean")
	})
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	assert.Nil(t, err, "command failed: %s %v\n%s", name, args, string(out))
}
