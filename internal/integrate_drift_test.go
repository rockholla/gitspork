package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckUpstreamDrift(t *testing.T) {
	t.Run("detects drift when files differ", func(t *testing.T) {
		// Create upstream and downstream directories
		upstreamPath, err := os.MkdirTemp("", "gitspork-drift-upstream-*")
		require.NoError(t, err)
		defer os.RemoveAll(upstreamPath)

		downstreamPath, err := os.MkdirTemp("", "gitspork-drift-downstream-*")
		require.NoError(t, err)
		defer os.RemoveAll(downstreamPath)

		// Create .gitspork.yml in upstream
		gitsporkConfig := `version: v0.0.1
upstream_owned:
- test.txt
`
		err = os.WriteFile(filepath.Join(upstreamPath, ".gitspork.yml"), []byte(gitsporkConfig), 0644)
		require.NoError(t, err)

		// Create matching file in upstream
		upstreamContent := "upstream content\n"
		err = os.WriteFile(filepath.Join(upstreamPath, "test.txt"), []byte(upstreamContent), 0644)
		require.NoError(t, err)

		// Create modified file in downstream
		downstreamContent := "downstream modified content\n"
		err = os.WriteFile(filepath.Join(downstreamPath, "test.txt"), []byte(downstreamContent), 0644)
		require.NoError(t, err)

		// Parse config and detect drift
		config, err := getGitSporkConfig(upstreamPath)
		require.NoError(t, err)

		// Test drift detection logic (without prompting)
		upstreamFiles, err := getIntegrateFiles(upstreamPath, config.UpstreamOwned)
		require.NoError(t, err)
		assert.NotEmpty(t, upstreamFiles, "Should find upstream-owned files")

		// Verify drift would be detected
		foundDrift := false
		for _, file := range upstreamFiles {
			upstreamFilePath := filepath.Join(upstreamPath, file)
			downstreamFilePath := filepath.Join(downstreamPath, file)

			if _, err := os.Stat(downstreamFilePath); err == nil {
				upstreamBytes, _ := os.ReadFile(upstreamFilePath)
				downstreamBytes, _ := os.ReadFile(downstreamFilePath)

				if string(upstreamBytes) != string(downstreamBytes) {
					foundDrift = true
					break
				}
			}
		}
		assert.True(t, foundDrift, "Should detect drift in modified file")
	})

	t.Run("no drift when files match", func(t *testing.T) {
		// Create upstream and downstream directories
		upstreamPath, err := os.MkdirTemp("", "gitspork-drift-upstream-*")
		require.NoError(t, err)
		defer os.RemoveAll(upstreamPath)

		downstreamPath, err := os.MkdirTemp("", "gitspork-drift-downstream-*")
		require.NoError(t, err)
		defer os.RemoveAll(downstreamPath)

		// Create .gitspork.yml in upstream
		gitsporkConfig := `version: v0.0.1
upstream_owned:
- test.txt
`
		err = os.WriteFile(filepath.Join(upstreamPath, ".gitspork.yml"), []byte(gitsporkConfig), 0644)
		require.NoError(t, err)

		// Create matching files in both upstream and downstream
		content := "same content\n"
		err = os.WriteFile(filepath.Join(upstreamPath, "test.txt"), []byte(content), 0644)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(downstreamPath, "test.txt"), []byte(content), 0644)
		require.NoError(t, err)

		// Parse config
		config, err := getGitSporkConfig(upstreamPath)
		require.NoError(t, err)

		// Test no drift
		upstreamFiles, err := getIntegrateFiles(upstreamPath, config.UpstreamOwned)
		require.NoError(t, err)

		foundDrift := false
		for _, file := range upstreamFiles {
			upstreamFilePath := filepath.Join(upstreamPath, file)
			downstreamFilePath := filepath.Join(downstreamPath, file)

			if _, err := os.Stat(downstreamFilePath); err == nil {
				upstreamBytes, _ := os.ReadFile(upstreamFilePath)
				downstreamBytes, _ := os.ReadFile(downstreamFilePath)

				if string(upstreamBytes) != string(downstreamBytes) {
					foundDrift = true
					break
				}
			}
		}
		assert.False(t, foundDrift, "Should not detect drift when files match")
	})

	t.Run("skips files that don't exist in downstream", func(t *testing.T) {
		// Create upstream directory
		upstreamPath, err := os.MkdirTemp("", "gitspork-drift-upstream-*")
		require.NoError(t, err)
		defer os.RemoveAll(upstreamPath)

		// Create empty downstream directory
		downstreamPath, err := os.MkdirTemp("", "gitspork-drift-downstream-*")
		require.NoError(t, err)
		defer os.RemoveAll(downstreamPath)

		// Create .gitspork.yml in upstream
		gitsporkConfig := `version: v0.0.1
upstream_owned:
- test.txt
`
		err = os.WriteFile(filepath.Join(upstreamPath, ".gitspork.yml"), []byte(gitsporkConfig), 0644)
		require.NoError(t, err)

		// Create file only in upstream
		err = os.WriteFile(filepath.Join(upstreamPath, "test.txt"), []byte("content"), 0644)
		require.NoError(t, err)

		// Parse config and check drift - should not error
		config, err := getGitSporkConfig(upstreamPath)
		require.NoError(t, err)

		err = checkUpstreamDrift(config, upstreamPath, downstreamPath, NewLogger())
		assert.NoError(t, err, "Should not error when downstream files don't exist yet")
	})
}
