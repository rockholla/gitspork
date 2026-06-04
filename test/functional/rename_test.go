//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const upstreamRenameGitsporkYML = `upstream_owned:
- from: .markdownlint-cli2-downstream.jsonc
  to: .markdownlint-cli2.jsonc
- from: configs/**
  to: .configs/**
`

const downstreamRenameGitsporkYML = `downstream_owned:
- from: seed-from.md
  to: seed-to.md
`

func TestIntegrate_upstream_rename_exact_and_glob(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".markdownlint-cli2-downstream.jsonc": "{\"config\":true}\n",
		"configs/app.yml":                     "app: true\n",
		"configs/nested/db.yml":               "db: true\n",
	}, upstreamRenameGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "integrate exited non-zero:\n%s", out)

	// exact rename landed at destination, absent at source
	AssertFileContains(t, downstreamDir, ".markdownlint-cli2.jsonc", "\"config\":true")
	AssertFileAbsent(t, downstreamDir, ".markdownlint-cli2-downstream.jsonc")

	// glob rename: prefix-substituted destinations, absent at source prefix
	AssertFileContains(t, downstreamDir, ".configs/app.yml", "app: true")
	AssertFileContains(t, downstreamDir, ".configs/nested/db.yml", "db: true")
	AssertFileAbsent(t, downstreamDir, "configs/app.yml")
	AssertFileAbsent(t, downstreamDir, "configs/nested/db.yml")
}

func TestIntegrate_upstream_rename_delete_propagates_to_destination(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		".markdownlint-cli2-downstream.jsonc": "{\"config\":true}\n",
		"configs/app.yml":                     "app: true\n",
	}, upstreamRenameGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	AssertFileContains(t, downstreamDir, ".markdownlint-cli2.jsonc", "\"config\":true")
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// Delete the upstream source file via go-git worktree (mirrors delta-test pattern).
	upstreamRepo := OpenRepo(t, upstreamDir)
	upstreamWt, err := upstreamRepo.Worktree()
	require.NoError(t, err)
	_, err = upstreamWt.Remove(".markdownlint-cli2-downstream.jsonc")
	require.NoError(t, err)
	CommitAll(t, upstreamRepo, upstreamDir, "remove renamed source upstream")

	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after upstream delete failed:\n%s", out)

	// Deletion must propagate to the DESTINATION path downstream.
	AssertFileAbsent(t, downstreamDir, ".markdownlint-cli2.jsonc")
}

func TestIntegrate_downstream_rename_seeds_and_survives_reintegrate(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"seed-from.md": "seed content\n",
	}, downstreamRenameGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)

	// seeded at destination, absent at source
	AssertFileContains(t, downstreamDir, "seed-to.md", "seed content")
	AssertFileAbsent(t, downstreamDir, "seed-from.md")

	// downstream customizes the seeded destination file, commits
	WriteFiles(t, downstreamDir, map[string]string{"seed-to.md": "downstream edit\n"})
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "customize seeded file")

	// re-integrate must NOT overwrite the downstream-owned destination
	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)
	content := ReadFile(t, downstreamDir, "seed-to.md")
	require.Contains(t, content, "downstream edit",
		"downstream-owned rename destination should survive re-integrate")
}
