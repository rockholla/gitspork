//go:build examples

package examples

import (
	"testing"

	"github.com/rockholla/gitspork/internal/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandardsLibraryExample(t *testing.T) {
	upstreamDir := initExampleRepo(t, "standards-library")
	downstreamDir := testharness.NewDownstreamRepo(t)

	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"auth-service"}`,
	})

	out, code := runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate failed:\n%s", out)
	assert.Contains(t, out, "migration 0001: policy init complete", "post-integrate migration should have run")

	// upstream-owned files land
	testharness.AssertFileContains(t, downstreamDir, ".golangci.yml", "errcheck")
	testharness.AssertFileContains(t, downstreamDir, "policies/data-handling.md", "Data Handling Policy")
	testharness.AssertFileContains(t, downstreamDir, "policies/access-control.md", "Access Control Policy")

	// .env.example merged — upstream block present
	testharness.AssertFileContains(t, downstreamDir, ".env.example", "::gitspork::begin-upstream-owned-block")
	testharness.AssertFileContains(t, downstreamDir, ".env.example", "DATABASE_URL")

	// security-policy.yaml prefer_upstream values present
	testharness.AssertFileContains(t, downstreamDir, "security-policy.yaml", "require_mfa: true")

	// service-info.txt rendered via json_data_path
	testharness.AssertFileContains(t, downstreamDir, "service-info.txt", "auth-service")

	// security-summary.md rendered via previous_input
	testharness.AssertFileContains(t, downstreamDir, "security-summary.md", "auth-service")

	// commit baseline
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// check-drift exits 0 (uses cached template inputs)
	out, code = runGitspork(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "check-drift expected no drift:\n%s", out)

	// downstream tries to override security-policy.yaml; prefer_upstream wins on re-integrate
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"security-policy.yaml": "require_mfa: false\n",
	})
	testharness.CommitAll(t, testharness.OpenRepo(t, downstreamDir), downstreamDir, "downstream tries to override security policy")
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"service-input-data.json": `{"service_name":"auth-service"}`,
	})

	out, code = runGitspork(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	policy := testharness.ReadFile(t, downstreamDir, "security-policy.yaml")
	assert.Contains(t, policy, "require_mfa: true", "security-policy.yaml prefer_upstream value should win")
}
