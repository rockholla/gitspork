//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


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

func TestIntegrate_structured_prefer_downstream(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	WriteFiles(t, downstreamDir, map[string]string{
		"info.json": `{"version":"downstream"}`,
	})
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "downstream modifies prefer_downstream file")

	prepDownstreamWithInputData(t, downstreamDir)
	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	content := ReadFile(t, downstreamDir, "info.json")
	assert.Contains(t, content, "downstream",
		"info.json (prefer_downstream) should retain downstream value after re-integrate")
}
