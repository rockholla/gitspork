package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitSporkConfig_upstreamOnly(t *testing.T) {
	dir := t.TempDir()
	content := "upstream_only:\n- cli/**\n- internal/generated/**\n"
	f := filepath.Join(dir, ".gitspork.yml")
	require.NoError(t, os.WriteFile(f, []byte(content), 0644))

	cfg, err := ParseGitSporkConfig(f)
	require.NoError(t, err)
	assert.Equal(t, []string{"cli/**", "internal/generated/**"}, cfg.UpstreamOnly)
}

func TestParseGitSporkConfig_upstreamOnly_absentMeansNil(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".gitspork.yml")
	require.NoError(t, os.WriteFile(f, []byte("upstream_owned:\n- foo/**\n"), 0644))

	cfg, err := ParseGitSporkConfig(f)
	require.NoError(t, err)
	assert.Nil(t, cfg.UpstreamOnly)
}
