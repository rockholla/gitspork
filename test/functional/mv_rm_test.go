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

const templatedNoMergedGitsporkYML = `templated:
- template: .gitspork-templates/one.txt.go.tmpl
  destination: one.txt
  inputs:
  - name: value
    prompt: enter a value
`

const templatedTwoNoMergedGitsporkYML = `templated:
- template: .gitspork-templates/one.txt.go.tmpl
  destination: one.txt
  inputs:
  - name: value
    prompt: enter a value
- template: .gitspork-templates/two.txt.go.tmpl
  destination: two.txt
  inputs:
  - name: value
    prompt: enter a value
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

func TestMv_dry_run_leaves_config_and_tree_untouched(t *testing.T) {
	// git mv -n only reports what would happen; gitspork mv must mirror that
	// by leaving both the working tree AND .gitspork.yml unchanged (and
	// unstaged), otherwise "--dry-run" silently rewrites and stages the config.
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)

	runner := resolveRunner(t, upstreamDir, "")

	origCfg := ReadFile(t, upstreamDir, ".gitspork.yml")

	out, code := runner.Run(t, []string{"mv", "-n", "docs/old.md", "docs/new.md"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork mv -n failed:\n%s", out)

	AssertFileContains(t, upstreamDir, "docs/old.md", "old doc")
	AssertFileAbsent(t, upstreamDir, "docs/new.md")

	postCfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.Equal(t, origCfg, postCfg, ".gitspork.yml must be byte-identical after dry-run")

	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = upstreamDir
	staged, err := cmd.Output()
	require.NoError(t, err)
	assert.NotContains(t, string(staged), ".gitspork.yml",
		"dry-run must not stage .gitspork.yml, got staged files:\n%s", staged)
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

func TestRm_dry_run_leaves_config_and_tree_untouched(t *testing.T) {
	// git rm -n only reports what would happen; gitspork rm must mirror that
	// by leaving both the working tree AND .gitspork.yml unchanged (and
	// unstaged), otherwise "--dry-run" silently rewrites and stages the config.
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)

	runner := resolveRunner(t, upstreamDir, "")

	origCfg := ReadFile(t, upstreamDir, ".gitspork.yml")

	out, code := runner.Run(t, []string{"rm", "-n", "docs/old.md"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork rm -n failed:\n%s", out)

	AssertFileContains(t, upstreamDir, "docs/old.md", "old doc")

	postCfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.Equal(t, origCfg, postCfg, ".gitspork.yml must be byte-identical after dry-run")

	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = upstreamDir
	staged, err := cmd.Output()
	require.NoError(t, err)
	assert.NotContains(t, string(staged), ".gitspork.yml",
		"dry-run must not stage .gitspork.yml, got staged files:\n%s", staged)
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

// TestMv_templated_without_merged_omits_merged_key ensures that when a templated
// entry has no `merged` field, `gitspork mv` rewrites .gitspork.yml without introducing
// a `merged: null` (or similar) line for that entry.
func TestMv_templated_without_merged_omits_merged_key(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".gitspork-templates/one.txt.go.tmpl": "value={{ index .Inputs \"value\" }}\n",
	}, templatedNoMergedGitsporkYML)

	runner := resolveRunner(t, upstreamDir, "")

	out, code := runner.Run(t, []string{"mv",
		".gitspork-templates/one.txt.go.tmpl",
		".gitspork-templates/renamed.txt.go.tmpl",
	}, upstreamDir)
	require.Equal(t, 0, code, "gitspork mv failed:\n%s", out)

	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.Contains(t, cfg, ".gitspork-templates/renamed.txt.go.tmpl")
	assert.NotContains(t, cfg, ".gitspork-templates/one.txt.go.tmpl")
	assert.NotContains(t, cfg, "merged: null",
		"rewritten templated entry should omit the merged key entirely, got:\n%s", cfg)
}

// TestRm_templated_without_merged_omits_merged_key ensures that when a templated
// entry has no `merged` field, `gitspork rm` rewrites .gitspork.yml without introducing
// a `merged: null` (or similar) line for the surviving entry.
func TestRm_templated_without_merged_omits_merged_key(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".gitspork-templates/one.txt.go.tmpl": "value={{ index .Inputs \"value\" }}\n",
		".gitspork-templates/two.txt.go.tmpl": "value={{ index .Inputs \"value\" }}\n",
	}, templatedTwoNoMergedGitsporkYML)

	runner := resolveRunner(t, upstreamDir, "")

	out, code := runner.Run(t, []string{"rm",
		".gitspork-templates/two.txt.go.tmpl",
	}, upstreamDir)
	require.Equal(t, 0, code, "gitspork rm failed:\n%s", out)

	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.Contains(t, cfg, ".gitspork-templates/one.txt.go.tmpl")
	assert.NotContains(t, cfg, ".gitspork-templates/two.txt.go.tmpl")
	assert.NotContains(t, cfg, "merged: null",
		"rewritten templated entry should omit the merged key entirely, got:\n%s", cfg)
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
