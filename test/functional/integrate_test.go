//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const simpleGitsporkYML = `version: dev
upstream_owned:
- upstream-owned/**
- upstream-owned.mk
downstream_owned:
- downstream-owned.md
shared_ownership:
  merged:
  - Makefile
  structured:
    prefer_upstream:
    - config.yaml
    prefer_downstream:
    - info.json
templated:
- template: .gitspork-templates/meta.txt.go.tmpl
  destination: meta.txt
  inputs:
  - name: project_name
    json_data_path: input-data.json
  - name: project_description
    json_data_path: input-data.json
`

const metaTemplate = `Project: {{ index .Inputs "project_name" }}
Description: {{ index .Inputs "project_description" }}
`

func buildSimpleUpstream(t *testing.T) string {
	t.Helper()
	return NewUpstreamRepo(t, map[string]string{
		"upstream-owned/file.txt":              "upstream content\n",
		"upstream-owned.mk":                    "upstream mk content\n",
		"downstream-owned.md":                  "downstream seed content\n",
		"Makefile":                             "# upstream makefile\n",
		"config.yaml":                          "key: upstream-value\n",
		"info.json":                            `{"version":"1"}`,
		".gitspork-templates/meta.txt.go.tmpl": metaTemplate,
	}, simpleGitsporkYML)
}

func prepDownstreamWithInputData(t *testing.T, downstreamDir string) {
	t.Helper()
	WriteFiles(t, downstreamDir, map[string]string{
		"input-data.json": `{"project_name":"my-project","project_description":"my description"}`,
	})
}

func integrateArgs(upstreamDir, downstreamDir string) []string {
	return []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}
}

func TestIntegrate_fresh(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "integrate exited non-zero:\n%s", out)

	// upstream-owned files
	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
	AssertFileContains(t, downstreamDir, "upstream-owned.mk", "upstream mk content")
	// downstream-owned file (seeded from upstream on first integrate)
	AssertFileContains(t, downstreamDir, "downstream-owned.md", "downstream seed content")
	// templated output
	AssertFileContains(t, downstreamDir, "meta.txt", "my-project")
	AssertFileContains(t, downstreamDir, "meta.txt", "my description")
	// shared-ownership: merged file present, structured prefer_upstream value wins
	AssertFileContains(t, downstreamDir, "Makefile", "upstream makefile")
	AssertFileContains(t, downstreamDir, "config.yaml", "upstream-value")
	// state written
	AssertFileContains(t, downstreamDir, ".gitspork/downstream-state.json", "last_upstream_commit_hash")
}

func TestIntegrate_reintegrate_idempotent(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
	AssertFileContains(t, downstreamDir, "meta.txt", "my-project")
}

func TestIntegrate_upstream_adds_file(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	WriteFiles(t, upstreamDir, map[string]string{
		"upstream-owned/new-file.txt": "brand new upstream file\n",
	})
	CommitAll(t, OpenRepo(t, upstreamDir), upstreamDir, "add new upstream file")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after upstream add failed:\n%s", out)

	AssertFileContains(t, downstreamDir, "upstream-owned/new-file.txt", "brand new upstream file")
}

func TestIntegrate_downstream_owned_not_overwritten(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	WriteFiles(t, downstreamDir, map[string]string{
		"downstream-owned.md": "# downstream customization\n",
	})
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "downstream customizes owned file")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	content := ReadFile(t, downstreamDir, "downstream-owned.md")
	assert.Contains(t, content, "downstream customization",
		"downstream-owned.md should not be overwritten by re-integrate")
}

func TestIntegrate_upstream_delta_rename(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	upstreamRepo := OpenRepo(t, upstreamDir)
	upstreamWt, err := upstreamRepo.Worktree()
	require.NoError(t, err)
	_, err = upstreamWt.Move("upstream-owned/file.txt", "upstream-owned/renamed-file.txt")
	require.NoError(t, err)
	CommitAll(t, upstreamRepo, upstreamDir, "rename upstream file")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after rename failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "upstream-owned/file.txt")
	AssertFileContains(t, downstreamDir, "upstream-owned/renamed-file.txt", "upstream content")
}

func TestIntegrate_upstream_delta_delete(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	upstreamRepo := OpenRepo(t, upstreamDir)
	upstreamWt, err := upstreamRepo.Worktree()
	require.NoError(t, err)
	_, err = upstreamWt.Remove("upstream-owned/file.txt")
	require.NoError(t, err)
	CommitAll(t, upstreamRepo, upstreamDir, "delete upstream file")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after delete failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "upstream-owned/file.txt")
}
