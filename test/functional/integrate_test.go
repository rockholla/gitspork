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
	// state written (multi-upstream format)
	AssertFileContains(t, downstreamDir, ".gitspork/downstream-state.json", "commit_hash")
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

func TestIntegrate_multi_upstream_precedence(t *testing.T) {
	// Second upstream wins on file.txt because it comes last (left-to-right precedence).
	upstreamDir1 := buildSimpleUpstream(t)
	upstreamDir2 := buildSecondUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir1, downstreamDir)
	if isDockerBuild {
		t.Skip("multi-upstream path rewriting not supported in DockerRunner")
	}

	out, code := runner.Run(t, integrateArgsMulti(upstreamDir1, upstreamDir2, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "multi-upstream integrate failed:\n%s", out)

	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "second upstream content")
}

func TestIntegrate_multi_upstream_backward_compat_old_flags(t *testing.T) {
	// Single --upstream-repo-url flag still works (backward compat).
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "backward-compat single flag integrate failed:\n%s", out)
	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
}

func TestIntegrate_multi_upstream_flag_conflict_error(t *testing.T) {
	// Mixing --upstream and --upstream-repo-url returns exit code 1.
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, []string{
		"integrate",
		"--upstream", "url=file://" + upstreamDir + ",version=main",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 1, code, "expected error exit code when mixing flags:\n%s", out)
}

func TestIntegrate_multi_upstream_state_records_all(t *testing.T) {
	// Both upstream URLs appear in downstream-state.json after a multi-upstream integrate.
	upstreamDir1 := buildSimpleUpstream(t)
	upstreamDir2 := buildSecondUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir1, downstreamDir)
	if isDockerBuild {
		t.Skip("multi-upstream path rewriting not supported in DockerRunner")
	}

	out, code := runner.Run(t, integrateArgsMulti(upstreamDir1, upstreamDir2, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "multi-upstream integrate failed:\n%s", out)

	state := ReadFile(t, downstreamDir, ".gitspork/downstream-state.json")
	assert.Contains(t, state, `"upstreams"`)
	assert.Contains(t, state, `"commit_hash"`)
}
