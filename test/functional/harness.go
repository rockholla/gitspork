//go:build functional || functional_docker

package functional

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

// Runner abstracts how a gitspork command is executed (native binary vs container).
type Runner interface {
	// Run executes gitspork with the given args. dir is the host working directory
	// (used for mv/rm which run from within the repo). Returns stdout+stderr combined,
	// and the exit code.
	Run(t *testing.T, args []string, dir string) (output string, exitCode int)
}

// BinaryRunner runs the locally compiled gitspork binary.
type BinaryRunner struct {
	BinaryPath string
}

func (r *BinaryRunner) Run(t *testing.T, args []string, dir string) (string, int) {
	t.Helper()
	cmd := exec.Command(r.BinaryPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("runner: failed to launch binary: %v\noutput: %s", err, out)
		}
	}
	return string(out), code
}

// --- repo construction helpers ---

// NewUpstreamRepo creates a temp git repo populated with files and an optional
// .gitspork.yml, commits everything, and returns the repo path.
// files maps relative path -> content. If gitsporkYML is non-empty it is written
// as .gitspork.yml before the initial commit.
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

// NewDownstreamRepo creates a temp dir with git init and returns its path.
func NewDownstreamRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	return dir
}

// WriteFiles writes a map of relative-path -> content into dir, creating subdirectories as needed.
func WriteFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}
}

// CommitAll stages everything in dir and creates a commit on repo.
func CommitAll(t *testing.T, repo *gogit.Repository, dir, message string) {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{
		Name:  "gitspork-test",
		Email: "gitspork-test@localhost",
		When:  time.Now(),
	}
	_, err = wt.Commit(message, &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)
}

// OpenRepo opens an existing git repo at dir.
func OpenRepo(t *testing.T, dir string) *gogit.Repository {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	return repo
}

// ReadFile reads a file inside dir and returns its content. Fails the test if absent.
func ReadFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	require.NoError(t, err, "expected file %s to exist in %s", rel, dir)
	return string(b)
}

// AssertFileAbsent fails the test if the file exists.
func AssertFileAbsent(t *testing.T, dir, rel string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, rel))
	require.ErrorIs(t, err, fs.ErrNotExist, "expected file %s to be absent in %s", rel, dir)
}

// AssertFileContains fails if the file doesn't contain substr.
func AssertFileContains(t *testing.T, dir, rel, substr string) {
	t.Helper()
	content := ReadFile(t, dir, rel)
	require.Contains(t, content, substr, "file %s does not contain %q", rel, substr)
}

