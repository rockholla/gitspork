package config

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
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/old.md"}},
		})
		warnings, err := UpstreamMv(cfg, "docs/old.md", "docs/new.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/new.md"}}, result.UpstreamOwned)
	})

	t.Run("glob with matching prefix is rewritten", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/cloud-native/**"}},
		})
		warnings, err := UpstreamMv(cfg, "docs/cloud-native", "docs/cloud")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/cloud/**"}}, result.UpstreamOwned)
	})

	t.Run("glob with wildcard before moved segment emits warning and is unchanged", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{Pattern: "**/cloud-native/*.md"}},
		})
		warnings, err := UpstreamMv(cfg, "cloud-native", "cloud")
		require.NoError(t, err)
		assert.Len(t, warnings, 1)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "**/cloud-native/*.md"}}, result.UpstreamOwned)
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
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/cloud-native/sub/**"}},
		})
		warnings, err := UpstreamMv(cfg, "docs/cloud-native", "docs/cloud")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/cloud/sub/**"}}, result.UpstreamOwned)
	})

	t.Run("downstream_owned entry is rewritten", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			DownstreamOwned: []OwnedEntry{{Pattern: "docs/old.md"}},
		})
		warnings, err := UpstreamMv(cfg, "docs/old.md", "docs/new.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/new.md"}}, result.DownstreamOwned)
	})

	t.Run("upstream rename entry: mv rewrites source side, leaves destination", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{From: "source.txt", To: "dest.txt"}},
		})
		warnings, err := UpstreamMv(cfg, "source.txt", "new-source.txt")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{From: "new-source.txt", To: "dest.txt"}}, result.UpstreamOwned)
	})

	t.Run("downstream rename entry: mv rewrites source side, leaves destination", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			DownstreamOwned: []OwnedEntry{{From: "seed-from.md", To: "seed-to.md"}},
		})
		warnings, err := UpstreamMv(cfg, "seed-from.md", "renamed-seed.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{From: "renamed-seed.md", To: "seed-to.md"}}, result.DownstreamOwned)
	})

	// Shell tab-completion routinely appends "/" to directory arguments.
	// mv/rm must treat "docs/cloud-native/" the same as "docs/cloud-native",
	// otherwise the git operation succeeds but no config entries are rewritten
	// and the resulting commit is silently broken.
	t.Run("trailing slash on oldPath still rewrites matching glob", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/cloud-native/**"}},
		})
		warnings, err := UpstreamMv(cfg, "docs/cloud-native/", "docs/cloud")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/cloud/**"}}, result.UpstreamOwned)
	})

	t.Run("trailing slash on newPath does not corrupt rewrite", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/cloud-native/**"}},
		})
		warnings, err := UpstreamMv(cfg, "docs/cloud-native", "docs/cloud/")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/cloud/**"}}, result.UpstreamOwned)
	})

	t.Run("trailing slashes on both paths rewrite templated destination", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/cloud/foo.txt"},
			},
		})
		warnings, err := UpstreamMv(cfg, "out/cloud/", "out/cloud-v2/")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "out/cloud-v2/foo.txt", result.Templated[0].Destination)
	})
}

func Test_FindGitSporkConfig(t *testing.T) {
	t.Run("finds .gitspork.yml in start dir", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, GitSporkConfigFileName)
		require.NoError(t, os.WriteFile(p, []byte(""), 0644))
		got, err := FindGitSporkConfig(dir)
		require.NoError(t, err)
		assert.Equal(t, p, got)
	})

	t.Run("falls back to .gitspork.yaml", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, GitSporkConfigFileNameAlt)
		require.NoError(t, os.WriteFile(p, []byte(""), 0644))
		got, err := FindGitSporkConfig(dir)
		require.NoError(t, err)
		assert.Equal(t, p, got)
	})

	t.Run("finds config in parent dir", func(t *testing.T) {
		parent := t.TempDir()
		child := filepath.Join(parent, "subdir")
		require.NoError(t, os.Mkdir(child, 0755))
		p := filepath.Join(parent, GitSporkConfigFileName)
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
	cfgPath := filepath.Join(dir, GitSporkConfigFileName)
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
	cfgPath := filepath.Join(dir, GitSporkConfigFileName)
	raw := "# top-level comment\nupstream_owned:\n  # entry comment\n  - docs/guide.md\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(raw), 0644))

	cfg, err := ParseGitSporkConfig(cfgPath)
	require.NoError(t, err)

	cfg.UpstreamOwned = append(cfg.UpstreamOwned, OwnedEntry{Pattern: "docs/new.md"})
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
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/guide.md"}, {Pattern: "docs/other.md"}},
		})
		warnings, err := UpstreamRm(cfg, "docs/guide.md", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/other.md"}}, result.UpstreamOwned)
	})

	t.Run("recursive: child exact paths removed", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/cloud-native/file.md"}, {Pattern: "docs/other.md"}},
		})
		warnings, err := UpstreamRm(cfg, "docs/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/other.md"}}, result.UpstreamOwned)
	})

	t.Run("recursive: glob with matching prefix removed", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/cloud-native/**"}, {Pattern: "docs/other.md"}},
		})
		warnings, err := UpstreamRm(cfg, "docs/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/other.md"}}, result.UpstreamOwned)
	})

	t.Run("glob with leading wildcard emits warning and is unchanged", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{Pattern: "**/cloud-native/*.md"}},
		})
		warnings, err := UpstreamRm(cfg, "cloud-native", true)
		require.NoError(t, err)
		assert.Len(t, warnings, 1)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "**/cloud-native/*.md"}}, result.UpstreamOwned)
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

	t.Run("upstream rename entry: rm matches source side and removes entry", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{
				{From: "source.txt", To: "dest.txt"},
				{Pattern: "keep.txt"},
			},
		})
		warnings, err := UpstreamRm(cfg, "source.txt", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "keep.txt"}}, result.UpstreamOwned)
	})

	t.Run("downstream rename entry: rm matches source side and removes entry", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			DownstreamOwned: []OwnedEntry{
				{From: "seed-from.md", To: "seed-to.md"},
				{Pattern: "keep.md"},
			},
		})
		warnings, err := UpstreamRm(cfg, "seed-from.md", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "keep.md"}}, result.DownstreamOwned)
	})

	// Shell tab-completion routinely appends "/" to directory arguments; rm must
	// treat "docs/cloud-native/" the same as "docs/cloud-native".
	t.Run("trailing slash on path still removes matching glob (recursive)", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []OwnedEntry{{Pattern: "docs/cloud-native/**"}, {Pattern: "docs/other.md"}},
		})
		warnings, err := UpstreamRm(cfg, "docs/cloud-native/", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []OwnedEntry{{Pattern: "docs/other.md"}}, result.UpstreamOwned)
	})

	t.Run("trailing slash on path still removes matching templated entry (recursive)", func(t *testing.T) {
		cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/cloud-native/foo.tmpl", Destination: "out/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := UpstreamRm(cfg, "templates/cloud-native/", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})
}
