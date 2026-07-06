//go:build functional || functional_docker

package functional

import (
	"os/exec"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/rockholla/gitspork/v2/test/testharness"
)

type Runner interface {
	Run(t *testing.T, args []string, dir string) (output string, exitCode int)
}

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

func NewUpstreamRepo(t *testing.T, files map[string]string, gitsporkYML string) string {
	return testharness.NewUpstreamRepo(t, files, gitsporkYML)
}
func NewDownstreamRepo(t *testing.T) string { return testharness.NewDownstreamRepo(t) }
func WriteFiles(t *testing.T, dir string, files map[string]string) {
	testharness.WriteFiles(t, dir, files)
}
func CommitAll(t *testing.T, repo *gogit.Repository, dir, message string) {
	testharness.CommitAll(t, repo, dir, message)
}
func OpenRepo(t *testing.T, dir string) *gogit.Repository { return testharness.OpenRepo(t, dir) }
func ReadFile(t *testing.T, dir, rel string) string       { return testharness.ReadFile(t, dir, rel) }
func AssertFileAbsent(t *testing.T, dir, rel string)      { testharness.AssertFileAbsent(t, dir, rel) }
func AssertFileContains(t *testing.T, dir, rel, substr string) {
	testharness.AssertFileContains(t, dir, rel, substr)
}
