package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/goccy/go-yaml"
)

func Test_globNonWildcardPrefix(t *testing.T) {
	assert.Equal(t, "docs/cloud-native", globNonWildcardPrefix("docs/cloud-native/**"))
	assert.Equal(t, "docs/cloud-native", globNonWildcardPrefix("docs/cloud-native/*.md"))
	assert.Equal(t, "", globNonWildcardPrefix("**/cloud-native/*.md"))
	assert.Equal(t, "exact/path.md", globNonWildcardPrefix("exact/path.md"))
}

func Test_UpstreamMv(t *testing.T) {
	t.Run("exact upstream_owned entry is replaced", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/old.md"},
		})
		warnings, err := UpstreamMv(cfg, "docs/old.md", "docs/new.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/new.md"}, result.UpstreamOwned)
	})

	t.Run("glob with matching prefix is rewritten", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/**"},
		})
		warnings, err := UpstreamMv(cfg, "docs/cloud-native", "docs/cloud")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/cloud/**"}, result.UpstreamOwned)
	})

	t.Run("glob with wildcard before moved segment emits warning and is unchanged", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"**/cloud-native/*.md"},
		})
		warnings, err := UpstreamMv(cfg, "cloud-native", "cloud")
		require.NoError(t, err)
		assert.Len(t, warnings, 1)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"**/cloud-native/*.md"}, result.UpstreamOwned)
	})

	t.Run("templated template field updated on exact match", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/old.tmpl", Destination: "out/file.txt"},
			},
		})
		warnings, err := UpstreamMv(cfg, "templates/old.tmpl", "templates/new.tmpl")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "templates/new.tmpl", result.Templated[0].Template)
		assert.Equal(t, "out/file.txt", result.Templated[0].Destination)
	})

	t.Run("templated destination field updated on exact match", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/old.txt"},
			},
		})
		warnings, err := UpstreamMv(cfg, "out/old.txt", "out/new.txt")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "out/new.txt", result.Templated[0].Destination)
	})

	t.Run("templated template field updated when parent directory moved", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/cloud/foo.tmpl", Destination: "out/foo.txt"},
			},
		})
		warnings, err := UpstreamMv(cfg, "templates/cloud", "templates/cloud-v2")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "templates/cloud-v2/foo.tmpl", result.Templated[0].Template)
	})

	t.Run("templated destination field updated when parent directory moved", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/cloud/foo.txt"},
			},
		})
		warnings, err := UpstreamMv(cfg, "out/cloud", "out/cloud-v2")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "out/cloud-v2/foo.txt", result.Templated[0].Destination)
	})

	t.Run("glob with matching sub-prefix is rewritten", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/sub/**"},
		})
		warnings, err := UpstreamMv(cfg, "docs/cloud-native", "docs/cloud")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/cloud/sub/**"}, result.UpstreamOwned)
	})

	t.Run("downstream_owned entry is rewritten", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			DownstreamOwned: []string{"docs/old.md"},
		})
		warnings, err := UpstreamMv(cfg, "docs/old.md", "docs/new.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/new.md"}, result.DownstreamOwned)
	})
}

func Test_FindGitSporkConfig(t *testing.T) {
	t.Run("finds .gitspork.yml in start dir", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, gitSporkConfigFileName)
		require.NoError(t, os.WriteFile(p, []byte(""), 0644))
		got, err := FindGitSporkConfig(dir)
		require.NoError(t, err)
		assert.Equal(t, p, got)
	})

	t.Run("falls back to .gitspork.yaml", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, gitSporkConfigFileNameAlt)
		require.NoError(t, os.WriteFile(p, []byte(""), 0644))
		got, err := FindGitSporkConfig(dir)
		require.NoError(t, err)
		assert.Equal(t, p, got)
	})

	t.Run("finds config in parent dir", func(t *testing.T) {
		parent := t.TempDir()
		child := filepath.Join(parent, "subdir")
		require.NoError(t, os.Mkdir(child, 0755))
		p := filepath.Join(parent, gitSporkConfigFileName)
		require.NoError(t, os.WriteFile(p, []byte(""), 0644))
		got, err := FindGitSporkConfig(child)
		require.NoError(t, err)
		assert.Equal(t, p, got)
	})

	t.Run("returns error when not found", func(t *testing.T) {
		dir := t.TempDir()
		_, err := FindGitSporkConfig(dir)
		require.Error(t, err)
	})
}

func makeConfigFile(t *testing.T, config *GitSporkConfig) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, gitSporkConfigFileName)
	b, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, b, 0644))
	return cfgPath
}

func loadConfigFile(t *testing.T, cfgPath string) *GitSporkConfig {
	t.Helper()
	cfg, err := ParseGitSporkConfig(cfgPath)
	require.NoError(t, err)
	return cfg
}

func Test_WriteGitSporkConfig_preservesComments(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, gitSporkConfigFileName)
	raw := "# top-level comment\nupstream_owned:\n  # entry comment\n  - docs/guide.md\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(raw), 0644))

	cfg, err := ParseGitSporkConfig(cfgPath)
	require.NoError(t, err)

	cfg.UpstreamOwned = append(cfg.UpstreamOwned, "docs/new.md")
	require.NoError(t, WriteGitSporkConfig(cfgPath, cfg))

	result, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(result), "# top-level comment")
	assert.Contains(t, string(result), "docs/guide.md")
	assert.Contains(t, string(result), "docs/new.md")
}

func Test_UpstreamRm(t *testing.T) {
	t.Run("exact entry removed", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/guide.md", "docs/other.md"},
		})
		warnings, err := UpstreamRm(cfg, "docs/guide.md", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("recursive: child exact paths removed", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/file.md", "docs/other.md"},
		})
		warnings, err := UpstreamRm(cfg, "docs/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("recursive: glob with matching prefix removed", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/**", "docs/other.md"},
		})
		warnings, err := UpstreamRm(cfg, "docs/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("glob with leading wildcard emits warning and is unchanged", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"**/cloud-native/*.md"},
		})
		warnings, err := UpstreamRm(cfg, "cloud-native", true)
		require.NoError(t, err)
		assert.Len(t, warnings, 1)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"**/cloud-native/*.md"}, result.UpstreamOwned)
	})

	t.Run("templated entry removed when template matches", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := UpstreamRm(cfg, "templates/foo.tmpl", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})

	t.Run("templated entry removed when destination matches", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := UpstreamRm(cfg, "out/foo.txt", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})

	t.Run("recursive: templated entry removed when template is child of removed path", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/cloud-native/foo.tmpl", Destination: "out/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := UpstreamRm(cfg, "templates/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})

	t.Run("recursive: templated entry removed when destination is child of removed path", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/cloud-native/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := UpstreamRm(cfg, "out/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})
}
