package integrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStructuredPath_yamlScalar(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("name: alice\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "name")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "alice", v)
}

func TestResolveStructuredPath_ymlExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
		[]byte("name: bob\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yml"), "name")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "bob", v)
}

func TestResolveStructuredPath_jsonScalar(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"name":"charlie"}`), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.json"), "name")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "charlie", v)
}

func TestResolveStructuredPath_nestedDotPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("user:\n  profile:\n    handle: dexter\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "user.profile.handle")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "dexter", v)
}

func TestResolveStructuredPath_pathNotFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("name: alice\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "missing.key")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_fileNotFound(t *testing.T) {
	v, found, err := resolveStructuredPath("/nonexistent/dir/config.yaml", "name")
	require.NoError(t, err) // missing file is not an error — it's "not found"
	assert.False(t, found)
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_nonScalarAtPath_mapping(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("user:\n  name: alice\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "user")
	require.NoError(t, err)
	assert.False(t, found, "a mapping node at the path must be treated as not-found, not an error")
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_nonScalarAtPath_sequence(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("tags:\n  - a\n  - b\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "tags")
	require.NoError(t, err)
	assert.False(t, found, "a sequence node at the path must be treated as not-found, not an error")
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_malformedYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("key: [unclosed_bracket\n"), 0644))
	_, _, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing destination file")
}

func TestResolveStructuredPath_malformedJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"key": "unterminated`), 0644))
	_, _, err := resolveStructuredPath(filepath.Join(dir, "config.json"), "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing destination file")
}

func TestResolveStructuredPath_unsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte(`name = "alice"`+"\n"), 0644))
	_, _, err := resolveStructuredPath(filepath.Join(dir, "config.toml"), "name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file extension")
}
