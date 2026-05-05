//go:build functional || functional_docker

package functional

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mvRmGitsporkYML = `upstream_owned:
- docs/old.md
- docs/keep.md
`

const globDirGitsporkYML = `upstream_owned:
- docs/**
`

const mvMultiSourceGitsporkYML = `upstream_owned:
- docs/first.md
- docs/second.md
- docs/keep.md
`

func TestMv_updates_config_and_stages(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)

	runner := resolveRunner(t, upstreamDir, "")

	out, code := runner.Run(t, []string{"mv", "docs/old.md", "docs/new.md"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork mv failed:\n%s", out)

	AssertFileAbsent(t, upstreamDir, "docs/old.md")
	AssertFileContains(t, upstreamDir, "docs/new.md", "old doc")

	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.Contains(t, cfg, "docs/new.md")
	assert.NotContains(t, cfg, "docs/old.md")

	// Verify both files are in the git index
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = upstreamDir
	staged, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(staged), ".gitspork.yml")
	assert.Contains(t, string(staged), "docs/new.md")
	assert.NotContains(t, string(staged), "docs/old.md")
}

func TestMv_multi_source(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/first.md":  "# first\n",
		"docs/second.md": "# second\n",
		"docs/keep.md":   "# keep\n",
	}, mvMultiSourceGitsporkYML)

	// Create destination directory so git mv can move multiple files into it
	require.NoError(t, os.MkdirAll(filepath.Join(upstreamDir, "docs/archive"), 0755))

	runner := resolveRunner(t, upstreamDir, "")

	out, code := runner.Run(t, []string{"mv", "docs/first.md", "docs/second.md", "docs/archive/"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork mv multi-source failed:\n%s", out)

	AssertFileAbsent(t, upstreamDir, "docs/first.md")
	AssertFileAbsent(t, upstreamDir, "docs/second.md")
	AssertFileContains(t, upstreamDir, "docs/archive/first.md", "first")
	AssertFileContains(t, upstreamDir, "docs/archive/second.md", "second")

	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.Contains(t, cfg, "docs/archive/first.md")
	assert.Contains(t, cfg, "docs/archive/second.md")
	assert.NotContains(t, cfg, "docs/first.md")
	assert.NotContains(t, cfg, "docs/second.md")
	assert.Contains(t, cfg, "docs/keep.md")

	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = upstreamDir
	staged, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(staged), ".gitspork.yml")
	assert.Contains(t, string(staged), "docs/archive/first.md")
	assert.Contains(t, string(staged), "docs/archive/second.md")
}

func TestRm_updates_config_and_stages(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)

	runner := resolveRunner(t, upstreamDir, "")

	out, code := runner.Run(t, []string{"rm", "docs/old.md"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork rm failed:\n%s", out)

	AssertFileAbsent(t, upstreamDir, "docs/old.md")

	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.NotContains(t, cfg, "docs/old.md")
	assert.Contains(t, cfg, "docs/keep.md")

	// Verify .gitspork.yml is staged
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = upstreamDir
	staged, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(staged), ".gitspork.yml")
	assert.Contains(t, string(staged), "docs/old.md")
}

// TestRm_downstream_exact runs gitspork rm on an exact path, commits the upstream
// change, then integrates into a downstream and verifies the file is gone there too.
func TestRm_downstream_exact(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	AssertFileContains(t, downstreamDir, "docs/old.md", "old doc")

	// Run gitspork rm in upstream, then commit the result.
	upstreamRunner := resolveRunner(t, upstreamDir, "")
	out, code = upstreamRunner.Run(t, []string{"rm", "docs/old.md"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork rm failed:\n%s", out)
	CommitAll(t, OpenRepo(t, upstreamDir), upstreamDir, "remove docs/old.md")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after rm failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "docs/old.md")
	AssertFileContains(t, downstreamDir, "docs/keep.md", "keep")
}

// TestRm_downstream_glob runs gitspork rm -r on a glob-covered directory, commits,
// then integrates and verifies all matching files are removed from downstream.
func TestRm_downstream_glob(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/alpha.md": "# alpha\n",
		"docs/beta.md":  "# beta\n",
	}, globDirGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	AssertFileContains(t, downstreamDir, "docs/alpha.md", "alpha")
	AssertFileContains(t, downstreamDir, "docs/beta.md", "beta")

	// Remove the entire docs/ directory from upstream.
	upstreamRunner := resolveRunner(t, upstreamDir, "")
	out, code = upstreamRunner.Run(t, []string{"rm", "-r", "docs/"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork rm -r failed:\n%s", out)
	CommitAll(t, OpenRepo(t, upstreamDir), upstreamDir, "remove docs/ directory")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after glob rm failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "docs/alpha.md")
	AssertFileAbsent(t, downstreamDir, "docs/beta.md")
}

// TestMv_downstream_exact runs gitspork mv on an exact path, commits, then integrates
// and verifies the file moved in downstream.
func TestMv_downstream_exact(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	AssertFileContains(t, downstreamDir, "docs/old.md", "old doc")

	// Run gitspork mv in upstream, then commit the result.
	upstreamRunner := resolveRunner(t, upstreamDir, "")
	out, code = upstreamRunner.Run(t, []string{"mv", "docs/old.md", "docs/new.md"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork mv failed:\n%s", out)
	CommitAll(t, OpenRepo(t, upstreamDir), upstreamDir, "rename docs/old.md to docs/new.md")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after mv failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "docs/old.md")
	AssertFileContains(t, downstreamDir, "docs/new.md", "old doc")
}

// TestMv_downstream_glob runs gitspork mv on a directory covered by a glob entry,
// commits, then integrates and verifies all files moved in downstream.
func TestMv_downstream_glob(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/alpha.md": "# alpha\n",
		"docs/beta.md":  "# beta\n",
	}, globDirGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	AssertFileContains(t, downstreamDir, "docs/alpha.md", "alpha")
	AssertFileContains(t, downstreamDir, "docs/beta.md", "beta")

	// Move the entire docs/ directory to guides/ in upstream.
	upstreamRunner := resolveRunner(t, upstreamDir, "")
	out, code = upstreamRunner.Run(t, []string{"mv", "docs/", "guides/"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork mv glob dir failed:\n%s", out)
	CommitAll(t, OpenRepo(t, upstreamDir), upstreamDir, "rename docs/ to guides/")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after glob mv failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "docs/alpha.md")
	AssertFileAbsent(t, downstreamDir, "docs/beta.md")
	AssertFileContains(t, downstreamDir, "guides/alpha.md", "alpha")
	AssertFileContains(t, downstreamDir, "guides/beta.md", "beta")
}
