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

const mvRmGitsporkYML = `version: dev
upstream_owned:
- docs/old.md
- docs/keep.md
`

const mvMultiSourceGitsporkYML = `version: dev
upstream_owned:
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
