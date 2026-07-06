//go:build examples

package examples

import (
	"testing"

	"github.com/rockholla/gitspork/v2/test/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenSourceTemplateExample(t *testing.T) {
	upstreamDir := initExampleRepo(t, "open-source-template")
	downstreamDir := testharness.NewDownstreamRepo(t)

	// project-meta.json is seeded by upstream on first integrate (prefer_downstream means
	// downstream's value wins on re-integrate once they customize it).
	// No separate input file needed — project-meta.json is the json_data_path.

	out, code := runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate failed:\n%s", out)

	// upstream-owned files land
	testharness.AssertFileContains(t, downstreamDir, ".github/workflows/ci.yml", "CI")
	testharness.AssertFileContains(t, downstreamDir, ".github/workflows/release.yml", "Release")
	testharness.AssertFileContains(t, downstreamDir, ".github/ISSUE_TEMPLATE.md", "Description")
	testharness.AssertFileContains(t, downstreamDir, "LICENSE", "MIT License")
	testharness.AssertFileContains(t, downstreamDir, "CONTRIBUTING.md", "Contributing")

	// downstream-owned files seeded; project-meta.json seeded from upstream
	testharness.AssertFileContains(t, downstreamDir, "README.md", "Seeded by gitspork")
	testharness.AssertFileContains(t, downstreamDir, "CHANGELOG.md", "Seeded by gitspork")
	testharness.AssertFileContains(t, downstreamDir, "project-meta.json", "my-project")

	// downstream_owned rename: upstream's canonical starter/AUTHORS.md lands
	// in the downstream at the conventional top-level AUTHORS.md path. The
	// raw upstream path must NOT appear in the downstream — the rename must
	// actually rename.
	testharness.AssertFileContains(t, downstreamDir, "AUTHORS.md", "Seeded by gitspork")
	testharness.AssertFileAbsent(t, downstreamDir, "starter/AUTHORS.md")

	// template rendered using project-meta.json
	testharness.AssertFileContains(t, downstreamDir, "CODE_OF_CONDUCT.md", "my-project")

	// commit baseline
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// check-drift exits 0
	out, code = runGitspork(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "check-drift expected no drift:\n%s", out)

	// customize README, CHANGELOG, and the renamed AUTHORS.md; re-integrate,
	// assert none are overwritten (the defining invariant of downstream_owned,
	// which applies to the rename form too — the destination path is what's
	// checked for existence, and the rename lands there once, then downstream
	// owns it).
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"README.md":    "# my-project\n\nCustom readme.\n",
		"CHANGELOG.md": "# Changelog\n\n## v1.0.0\n- initial release\n",
		"AUTHORS.md":   "# Authors\n\n- Alice <alice@example.com>\n- Bob <bob@example.com>\n",
	})
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "customize downstream-owned files")

	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	readme := testharness.ReadFile(t, downstreamDir, "README.md")
	assert.Contains(t, readme, "Custom readme", "README.md should not be overwritten")

	changelog := testharness.ReadFile(t, downstreamDir, "CHANGELOG.md")
	assert.Contains(t, changelog, "initial release", "CHANGELOG.md should not be overwritten")

	// The renamed downstream_owned entry follows the same invariant: once
	// the destination path is populated, subsequent integrates leave it alone.
	authors := testharness.ReadFile(t, downstreamDir, "AUTHORS.md")
	assert.Contains(t, authors, "Alice <alice@example.com>",
		"AUTHORS.md (rename destination) should not be overwritten on re-integrate")
	assert.NotContains(t, authors, "Seeded by gitspork",
		"the customized content must have fully replaced the seed content")
	// The upstream source path still must not be in the downstream even
	// after re-integrate — the rename should not "leak" the source path.
	testharness.AssertFileAbsent(t, downstreamDir, "starter/AUTHORS.md")

	// downstream modified project-meta.json, re-integrate: prefer_downstream value survives
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"project-meta.json": `{"project_name":"forked-project","description":"My fork."}`,
	})
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "customize project-meta.json")

	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after project-meta change failed:\n%s", out)

	meta := testharness.ReadFile(t, downstreamDir, "project-meta.json")
	assert.Contains(t, meta, "forked-project", "project-meta.json downstream value should survive (prefer_downstream)")

	coc := testharness.ReadFile(t, downstreamDir, "CODE_OF_CONDUCT.md")
	assert.Contains(t, coc, "forked-project", "CODE_OF_CONDUCT.md should re-render with downstream project_name")
}
