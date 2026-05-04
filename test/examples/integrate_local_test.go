//go:build examples

package examples

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rockholla/gitspork/internal/testharness"
	"github.com/stretchr/testify/require"
)

func TestIntegrateLocalExample(t *testing.T) {
	exDir := examplePath(t, "integrate-local")
	upstreamDir := filepath.Join(exDir, "upstream")
	exDownstreamDir := filepath.Join(exDir, "downstream")

	// Create a temp downstream seeded with the example's input-data.json.
	downstreamDir := t.TempDir()
	inputData, err := os.ReadFile(filepath.Join(exDownstreamDir, "input-data.json"))
	require.NoError(t, err)
	testharness.WriteFiles(t, downstreamDir, map[string]string{
		"input-data.json": string(inputData),
	})

	out, code := runGitspork(t, []string{
		"integrate-local",
		"--upstream-path", upstreamDir,
		"--downstream-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate-local failed:\n%s", out)

	// upstream-owned file lands
	testharness.AssertFileContains(t, downstreamDir, "app-config.yaml", "log_level: info")

	// template rendered with values from input-data.json
	testharness.AssertFileContains(t, downstreamDir, "config.yml", "my-local-app")
	testharness.AssertFileContains(t, downstreamDir, "config.yml", "development")
}
