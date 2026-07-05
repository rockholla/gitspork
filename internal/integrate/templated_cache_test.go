package integrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTemplatedInputs_returnsEmptyMapWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	got, err := loadTemplatedInputs(dir)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestLoadTemplatedInputs_parsesExistingCache(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".gitspork")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(cacheDir, templatedInputsCacheFileName),
		[]byte(`{"docs/api.md":{"name":"alice"},"Makefile":{"key":"value"}}`),
		0644,
	))

	got, err := loadTemplatedInputs(dir)
	require.NoError(t, err)
	assert.Equal(t, "alice", got["docs/api.md"]["name"])
	assert.Equal(t, "value", got["Makefile"]["key"])
}

func TestSaveTemplatedInputs_writesFileAndCreatesDir(t *testing.T) {
	dir := t.TempDir()
	inputs := map[string]map[string]string{
		"docs/api.md": {"name": "alice"},
	}
	require.NoError(t, saveTemplatedInputs(dir, inputs))

	got, err := os.ReadFile(filepath.Join(dir, ".gitspork", templatedInputsCacheFileName))
	require.NoError(t, err)
	assert.Contains(t, string(got), "alice")
	assert.Contains(t, string(got), "docs/api.md")
}

func TestSaveTemplatedInputs_deterministicKeyOrder(t *testing.T) {
	dir := t.TempDir()
	inputs := map[string]map[string]string{
		"z-last":   {"z-key": "z-val", "a-key": "a-val"},
		"a-first":  {"z-key": "z-val", "a-key": "a-val"},
		"m-middle": {"z-key": "z-val", "a-key": "a-val"},
	}
	require.NoError(t, saveTemplatedInputs(dir, inputs))

	got, err := os.ReadFile(filepath.Join(dir, ".gitspork", templatedInputsCacheFileName))
	require.NoError(t, err)
	// Go's json.Marshal sorts map keys alphabetically — both at the destination level and
	// within each destination's inputs. Confirm we get that stable order.
	aIdx := indexOf(string(got), "a-first")
	mIdx := indexOf(string(got), "m-middle")
	zIdx := indexOf(string(got), "z-last")
	assert.True(t, aIdx < mIdx && mIdx < zIdx, "destination keys must be sorted alphabetically for stable diffs")
	akIdx := indexOf(string(got), "a-key")
	zkIdx := indexOf(string(got), "z-key")
	assert.True(t, akIdx < zkIdx, "input keys within each destination must be sorted alphabetically")
}

func TestTemplatedInputs_roundTrip(t *testing.T) {
	dir := t.TempDir()
	original := map[string]map[string]string{
		"docs/api.md": {"name": "alice", "role": "admin"},
		"Makefile":    {"key": "value"},
	}
	require.NoError(t, saveTemplatedInputs(dir, original))

	loaded, err := loadTemplatedInputs(dir)
	require.NoError(t, err)
	assert.Equal(t, original, loaded)
}

func TestSaveTemplatedInputs_emptyMapWritesEmptyObject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, saveTemplatedInputs(dir, map[string]map[string]string{}))

	got, err := os.ReadFile(filepath.Join(dir, ".gitspork", templatedInputsCacheFileName))
	require.NoError(t, err)
	assert.Equal(t, "{}", string(got))
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
