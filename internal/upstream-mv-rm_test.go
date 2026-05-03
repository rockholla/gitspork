package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func Test_globNonWildcardPrefix(t *testing.T) {
	assert.Equal(t, "docs/cloud-native", globNonWildcardPrefix("docs/cloud-native/**"))
	assert.Equal(t, "docs/cloud-native", globNonWildcardPrefix("docs/cloud-native/*.md"))
	assert.Equal(t, "", globNonWildcardPrefix("**/cloud-native/*.md"))
	assert.Equal(t, "exact/path.md", globNonWildcardPrefix("exact/path.md"))
}

func Test_upstreamMv(t *testing.T) {
	t.Run("exact upstream_owned entry is replaced", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/old.md"},
		})
		warnings, err := upstreamMv(cfg, dir, "docs/old.md", "docs/new.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/new.md"}, result.UpstreamOwned)
	})

	t.Run("glob with matching prefix is rewritten", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/**"},
		})
		warnings, err := upstreamMv(cfg, dir, "docs/cloud-native", "docs/cloud")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/cloud/**"}, result.UpstreamOwned)
	})

	t.Run("glob with wildcard before moved segment emits warning and is unchanged", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"**/cloud-native/*.md"},
		})
		warnings, err := upstreamMv(cfg, dir, "cloud-native", "cloud")
		require.NoError(t, err)
		assert.Len(t, warnings, 1)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"**/cloud-native/*.md"}, result.UpstreamOwned)
	})

	t.Run("templated template field updated", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/old.tmpl", Destination: "out/file.txt"},
			},
		})
		warnings, err := upstreamMv(cfg, dir, "templates/old.tmpl", "templates/new.tmpl")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "templates/new.tmpl", result.Templated[0].Template)
		assert.Equal(t, "out/file.txt", result.Templated[0].Destination)
	})

	t.Run("templated destination field updated", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/old.txt"},
			},
		})
		warnings, err := upstreamMv(cfg, dir, "out/old.txt", "out/new.txt")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "out/new.txt", result.Templated[0].Destination)
	})

	t.Run("glob with matching sub-prefix is rewritten", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/sub/**"},
		})
		warnings, err := upstreamMv(cfg, dir, "docs/cloud-native", "docs/cloud")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/cloud/sub/**"}, result.UpstreamOwned)
	})

	t.Run("downstream_owned entry is rewritten", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			DownstreamOwned: []string{"docs/old.md"},
		})
		warnings, err := upstreamMv(cfg, dir, "docs/old.md", "docs/new.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/new.md"}, result.DownstreamOwned)
	})
}

func makeConfigFile(t *testing.T, config *GitSporkConfig) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gitspork-mv-rm-test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfgPath := filepath.Join(dir, gitSporkConfigFileName)
	b, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, b, 0644))
	return dir, cfgPath
}

func loadConfigFile(t *testing.T, cfgPath string) *GitSporkConfig {
	t.Helper()
	cfg, err := ParseGitSporkConfig(cfgPath)
	require.NoError(t, err)
	return cfg
}

func Test_upstreamRm(t *testing.T) {
	t.Run("exact entry removed", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/guide.md", "docs/other.md"},
		})
		warnings, err := upstreamRm(cfg, dir, "docs/guide.md", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("recursive: child exact paths removed", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/file.md", "docs/other.md"},
		})
		warnings, err := upstreamRm(cfg, dir, "docs/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("recursive: glob with matching prefix removed", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/**", "docs/other.md"},
		})
		warnings, err := upstreamRm(cfg, dir, "docs/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("glob with leading wildcard emits warning and is unchanged", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"**/cloud-native/*.md"},
		})
		warnings, err := upstreamRm(cfg, dir, "cloud-native", true)
		require.NoError(t, err)
		assert.Len(t, warnings, 1)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"**/cloud-native/*.md"}, result.UpstreamOwned)
	})

	t.Run("templated entry removed when template matches", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := upstreamRm(cfg, dir, "templates/foo.tmpl", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})

	t.Run("recursive: templated entry removed when template is child of removed path", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/cloud-native/foo.tmpl", Destination: "out/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := upstreamRm(cfg, dir, "templates/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})
}
