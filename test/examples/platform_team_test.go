//go:build examples

package examples

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/rockholla/gitspork/v2/test/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformTeamExample(t *testing.T) {
	upstreamDir := initExampleRepo(t, "platform-team")
	downstreamDir := testharness.NewDownstreamRepo(t)

	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"payments-service","team_name":"platform"}`,
	})

	out, code := runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate failed:\n%s", out)
	assert.Contains(t, out, "migration 0001: post-integrate init complete", "post-integrate migration should have run")

	// upstream-owned CI files land
	testharness.AssertFileContains(t, downstreamDir, "ci/build.yml", "Build")
	testharness.AssertFileContains(t, downstreamDir, "ci/deploy.yml", "Deploy")
	testharness.AssertFileContains(t, downstreamDir, "scripts/shared-bootstrap.sh", "bootstrapping")

	// downstream-owned README seeded
	testharness.AssertFileContains(t, downstreamDir, "README.md", "Seeded by gitspork")

	// Makefile merged — upstream block present
	testharness.AssertFileContains(t, downstreamDir, "Makefile", "::gitspork::begin-upstream-owned-block")
	testharness.AssertFileContains(t, downstreamDir, "Makefile", "Platform targets")

	// deploy-config.yaml prefer_upstream value present
	testharness.AssertFileContains(t, downstreamDir, "deploy-config.yaml", "us-east-1")

	// template rendered
	testharness.AssertFileContains(t, downstreamDir, "service-manifest.yml", "payments-service")
	testharness.AssertFileContains(t, downstreamDir, "service-manifest.yml", "platform")

	// commit baseline, check-drift exits 0
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"payments-service","team_name":"platform"}`,
	})
	out, code = runGitspork(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "check-drift expected no drift:\n%s", out)

	// downstream-owned README not overwritten after customization
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"README.md": "# payments-service\n\nCustom readme.\n",
	})
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "customize README")
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"payments-service","team_name":"platform"}`,
	})
	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)
	content := testharness.ReadFile(t, downstreamDir, "README.md")
	assert.Contains(t, content, "Custom readme", "README.md should not be overwritten")

	// deploy-config.yaml prefer_upstream still wins after re-integrate
	testharness.AssertFileContains(t, downstreamDir, "deploy-config.yaml", "us-east-1")
}

// TestPlatformTeamExample_mergedFile_roundTrip pins the two defining
// invariants of shared_ownership.merged that the main platform-team test
// doesn't reach:
//
//  1. Downstream edits OUTSIDE the upstream-owned block must survive a
//     re-integrate (the whole point of "merged" vs "upstream_owned").
//  2. Upstream edits INSIDE the upstream-owned block must land in the
//     downstream on re-integrate.
//
// A regression in either direction would silently corrupt users' Makefiles
// on their next re-integrate, so the round-trip is worth guarding at the
// examples tier where the primitive is actually declared.
func TestPlatformTeamExample_mergedFile_roundTrip(t *testing.T) {
	upstreamDir := initExampleRepo(t, "platform-team")
	downstreamDir := testharness.NewDownstreamRepo(t)

	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"payments-service","team_name":"platform"}`,
	})

	// First integrate — Makefile lands with just the upstream-owned block.
	out, code := runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	testharness.AssertFileContains(t, downstreamDir, "Makefile", "# Platform targets")

	// Simulate a downstream engineer adding lines BEFORE and AFTER the
	// upstream-owned block. These are the "downstream-owned" portions of
	// the merged file — merged ownership means the downstream can have
	// their own targets alongside the platform team's.
	const downstreamHeader = "# Downstream project makefile — customized targets below.\n"
	const downstreamFooter = `
# Downstream-only targets.
.PHONY: dev deploy-dev
dev:
	@echo "spinning up local dev..."
deploy-dev:
	@echo "deploying to dev..."
`
	original := testharness.ReadFile(t, downstreamDir, "Makefile")
	// Sanity: the upstream-owned block markers are where we expect.
	require.Contains(t, original, "::gitspork::begin-upstream-owned-block")
	require.Contains(t, original, "::gitspork::end-upstream-owned-block")

	require.NoError(t, os.WriteFile(
		filepath.Join(downstreamDir, "Makefile"),
		[]byte(downstreamHeader+original+downstreamFooter),
		0644,
	))
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "customize Makefile with downstream targets")

	// Simulate the platform team updating the upstream-owned block —
	// tweak the lint target and add a new "test" target. This tests that
	// upstream changes propagate into the block on re-integrate.
	upstreamMakefile := filepath.Join(upstreamDir, "Makefile")
	newUpstreamBlock := `# ::gitspork::begin-upstream-owned-block
# Platform targets — managed by platform team via gitspork.
.PHONY: build deploy lint test
build:
	@echo "building service..."
deploy:
	@echo "deploying service..."
lint:
	@echo "linting v2..."
test:
	@echo "running tests..."
# ::gitspork::end-upstream-owned-block
`
	require.NoError(t, os.WriteFile(upstreamMakefile, []byte(newUpstreamBlock), 0644))
	// Commit the upstream change so re-integrate picks it up on default branch.
	upstreamRepo, err := gogit.PlainOpen(upstreamDir)
	require.NoError(t, err)
	wt, err := upstreamRepo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	sig := &object.Signature{Name: "platform-team", Email: "platform@localhost", When: time.Now()}
	_, err = wt.Commit("platform team: add test target and bump lint", &gogit.CommitOptions{Author: sig})
	require.NoError(t, err)

	// Re-integrate. Downstream engineer's inputs still needed by the
	// templated step.
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"payments-service","team_name":"platform"}`,
	})
	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	// Round-trip assertions.
	final := testharness.ReadFile(t, downstreamDir, "Makefile")

	// 1. Downstream customizations survived the re-integrate.
	assert.Contains(t, final, "# Downstream project makefile",
		"downstream header outside the upstream-owned block must survive re-integrate — this is the defining contract of shared_ownership.merged")
	assert.Contains(t, final, "deploy-dev:",
		"downstream target added below the upstream-owned block must survive re-integrate")
	assert.Contains(t, final, "@echo \"spinning up local dev...\"",
		"downstream target body must survive re-integrate")

	// 2. Upstream's block updates landed.
	assert.Contains(t, final, "test:",
		"new upstream target inside the block must appear in downstream after re-integrate")
	assert.Contains(t, final, "@echo \"linting v2...\"",
		"changed upstream content inside the block must overwrite the previous version")
	assert.NotContains(t, final, "@echo \"linting...\"",
		"previous upstream block content must be replaced, not appended")

	// 3. The upstream block ordering is preserved — downstream header comes
	// before the block, the block is intact, and the footer comes after.
	headerIdx := strings.Index(final, "# Downstream project makefile")
	blockBeginIdx := strings.Index(final, "::gitspork::begin-upstream-owned-block")
	blockEndIdx := strings.Index(final, "::gitspork::end-upstream-owned-block")
	footerIdx := strings.Index(final, "# Downstream-only targets.")
	require.NotEqual(t, -1, headerIdx)
	require.NotEqual(t, -1, blockBeginIdx)
	require.NotEqual(t, -1, blockEndIdx)
	require.NotEqual(t, -1, footerIdx)
	assert.True(t, headerIdx < blockBeginIdx && blockBeginIdx < blockEndIdx && blockEndIdx < footerIdx,
		"merged file layout must remain [downstream header, upstream block, downstream footer] after re-integrate; got positions header=%d blockBegin=%d blockEnd=%d footer=%d",
		headerIdx, blockBeginIdx, blockEndIdx, footerIdx)

	// 4. Exactly one begin/end marker pair — the merge must not duplicate
	// the block when the downstream had one and the upstream provides one.
	assert.Equal(t, 1, strings.Count(final, "::gitspork::begin-upstream-owned-block"),
		"exactly one begin-marker after re-integrate — a merge that appended rather than replaced would duplicate the block")
	assert.Equal(t, 1, strings.Count(final, "::gitspork::end-upstream-owned-block"),
		"exactly one end-marker after re-integrate")
}
