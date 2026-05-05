//go:build examples

package examples

import (
	"testing"

	"github.com/rockholla/gitspork/internal/testharness"
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
