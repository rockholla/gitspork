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

func TestResolveStructuredPath_sequenceAtPath_serializedAsJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("tags:\n  - a\n  - b\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "tags")
	require.NoError(t, err)
	assert.True(t, found, "a sequence node at the path must be serialized as a JSON array string")
	assert.Equal(t, `["a","b"]`, v)
}

func TestResolveStructuredPath_sequenceAtPath_singleElement(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("refs:\n  - account_default\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "refs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, `["account_default"]`, v)
}

func TestResolveStructuredPath_sequenceAtPath_jsonFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"items":["x","y","z"]}`), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.json"), "items")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, `["x","y","z"]`, v)
}

func TestResolveStructuredPath_bracketIndex_midPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("users:\n  - name: alice\n  - name: bob\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "users[1].name")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "bob", v)
}

func TestResolveStructuredPath_bracketIndex_terminalScalar(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("tags:\n  - alpha\n  - beta\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "tags[0]")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "alpha", v)
}

func TestResolveStructuredPath_bracketIndex_outOfRange(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("tags:\n  - alpha\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "tags[5]")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_bracketIndex_onNonSequence(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("name: alice\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "name[0]")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_bracketIndex_malformedSegment(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("tags:\n  - a\n"), 0644))
	_, _, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "tags[notanumber]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid path segment")
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

func TestResolveStructuredPath_nullValue_yaml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("name: null\n"), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.yaml"), "name")
	require.NoError(t, err)
	assert.False(t, found, "an explicit null YAML value must be treated as not-found, not returned as \"<nil>\"")
	assert.Equal(t, "", v)
}

func TestResolveStructuredPath_nullValue_json(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"name":null}`), 0644))
	v, found, err := resolveStructuredPath(filepath.Join(dir, "config.json"), "name")
	require.NoError(t, err)
	assert.False(t, found, "an explicit null JSON value must be treated as not-found, not returned as \"<nil>\"")
	assert.Equal(t, "", v)
}
