package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ParseMigrationConfig(t *testing.T) {
	writeYAML := func(t *testing.T, contents string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "migration.yml")
		require.NoError(t, os.WriteFile(p, []byte(contents), 0644))
		return p
	}

	t.Run("only pre_integrate present", func(t *testing.T) {
		p := writeYAML(t, "pre_integrate:\n  exec: ./setup.sh --v2\n")
		m, err := ParseMigrationConfig(p)
		require.NoError(t, err)
		require.NotNil(t, m.PreIntegrate)
		assert.Equal(t, "./setup.sh --v2", m.PreIntegrate.Exec)
		assert.Nil(t, m.PostIntegrate)
	})

	t.Run("only post_integrate present", func(t *testing.T) {
		p := writeYAML(t, "post_integrate:\n  exec: ./cleanup.sh\n")
		m, err := ParseMigrationConfig(p)
		require.NoError(t, err)
		assert.Nil(t, m.PreIntegrate)
		require.NotNil(t, m.PostIntegrate)
		assert.Equal(t, "./cleanup.sh", m.PostIntegrate.Exec)
	})

	t.Run("both present", func(t *testing.T) {
		p := writeYAML(t, "pre_integrate:\n  exec: pre.sh\npost_integrate:\n  exec: post.sh\n")
		m, err := ParseMigrationConfig(p)
		require.NoError(t, err)
		require.NotNil(t, m.PreIntegrate)
		require.NotNil(t, m.PostIntegrate)
		assert.Equal(t, "pre.sh", m.PreIntegrate.Exec)
		assert.Equal(t, "post.sh", m.PostIntegrate.Exec)
	})

	t.Run("empty file yields both-nil migration", func(t *testing.T) {
		p := writeYAML(t, "")
		m, err := ParseMigrationConfig(p)
		require.NoError(t, err)
		assert.Nil(t, m.PreIntegrate)
		assert.Nil(t, m.PostIntegrate)
	})

	t.Run("missing file returns wrapped read error", func(t *testing.T) {
		_, err := ParseMigrationConfig(filepath.Join(t.TempDir(), "nonexistent.yml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error reading gitspork migration config file")
	})

	t.Run("malformed YAML returns wrapped parse error", func(t *testing.T) {
		p := writeYAML(t, "pre_integrate: [invalid: mapping\n  under: sequence]\n")
		_, err := ParseMigrationConfig(p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error parsing gitspork migration config file")
	})
}
