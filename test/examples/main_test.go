//go:build examples

package examples

import (
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

var binaryPath string

func TestMain(m *testing.M) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		panic("cannot resolve repo root: " + err.Error())
	}
	binaryPath = buildBinary(repoRoot)
	os.Exit(m.Run())
}

func buildBinary(repoRoot string) string {
	dir, err := os.MkdirTemp("", "gitspork-examples-")
	if err != nil {
		panic("cannot create temp dir for binary: " + err.Error())
	}
	out := filepath.Join(dir, "gitspork")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/gitspork")
	cmd.Dir = repoRoot
	if b, err := cmd.CombinedOutput(); err != nil {
		panic("go build failed:\n" + string(b))
	}
	return out
}

func runGitspork(t *testing.T, args []string, dir string) (string, int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run gitspork: %v\noutput: %s", err, out)
		}
	}
	return string(out), code
}

func exampleUpstreamPath(t *testing.T, name string) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("cannot resolve repo root: %v", err)
	}
	return filepath.Join(repoRoot, "docs", "examples", name, "upstream")
}

func examplePath(t *testing.T, name string) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("cannot resolve repo root: %v", err)
	}
	return filepath.Join(repoRoot, "docs", "examples", name)
}

// initExampleRepo copies a docs/examples/<name>/upstream dir into a fresh temp git repo
// and commits everything, making it cloneable via file:// URL.
func initExampleRepo(t *testing.T, name string) string {
	t.Helper()
	srcDir := exampleUpstreamPath(t, name)
	dstDir := t.TempDir()

	repo, err := gogit.PlainInit(dstDir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)

	err = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dstDir, rel), 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dstDir, rel), b, info.Mode())
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: "gitspork-test", Email: "gitspork-test@localhost", When: time.Now()}
	_, err = wt.Commit("initial example commit", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	return dstDir
}
