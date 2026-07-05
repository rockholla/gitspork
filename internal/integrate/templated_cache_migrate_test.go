package integrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateLegacyTemplatedCache_noopWhenGitsporkDirMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, migrateLegacyTemplatedCache(dir))
	_, err := os.Stat(filepath.Join(dir, ".gitspork"))
	assert.True(t, os.IsNotExist(err), ".gitspork dir must not be created if there was nothing to migrate")
}

func TestMigrateLegacyTemplatedCache_noopWhenOnlyProtectedFiles(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".gitspork")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "downstream-state.json"), []byte(`{"upstreams":[]}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, templatedInputsCacheFileName), []byte(`{"docs/api.md":{"k":"v"}}`), 0644))

	require.NoError(t, migrateLegacyTemplatedCache(dir))

	// both protected files must survive untouched
	stateBytes, err := os.ReadFile(filepath.Join(cacheDir, "downstream-state.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"upstreams":[]}`, string(stateBytes))
	consBytes, err := os.ReadFile(filepath.Join(cacheDir, templatedInputsCacheFileName))
	require.NoError(t, err)
	assert.Equal(t, `{"docs/api.md":{"k":"v"}}`, string(consBytes))
}

func TestMigrateLegacyTemplatedCache_migratesTopLevelFile(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".gitspork")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	legacyPath := filepath.Join(cacheDir, "Makefile.json")
	require.NoError(t, os.WriteFile(legacyPath, []byte(`{"inputs":{"name":"alice"}}`), 0644))

	require.NoError(t, migrateLegacyTemplatedCache(dir))

	// legacy file removed
	_, err := os.Stat(legacyPath)
	assert.True(t, os.IsNotExist(err), "legacy file must be deleted after migration")

	// consolidated cache has the entry keyed by destination
	loaded, err := loadTemplatedInputs(dir)
	require.NoError(t, err)
	assert.Equal(t, "alice", loaded["Makefile"]["name"])
}

func TestMigrateLegacyTemplatedCache_migratesNestedFile(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".gitspork", "docs")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	legacyPath := filepath.Join(cacheDir, "api.md.json")
	require.NoError(t, os.WriteFile(legacyPath, []byte(`{"inputs":{"title":"API"}}`), 0644))

	require.NoError(t, migrateLegacyTemplatedCache(dir))

	_, err := os.Stat(legacyPath)
	assert.True(t, os.IsNotExist(err), "nested legacy file must be deleted")

	loaded, err := loadTemplatedInputs(dir)
	require.NoError(t, err)
	assert.Equal(t, "API", loaded["docs/api.md"]["title"], "destination key must include the nested path")
}

func TestMigrateLegacyTemplatedCache_mergesIntoExistingConsolidated(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".gitspork")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(cacheDir, templatedInputsCacheFileName),
		[]byte(`{"already/here.md":{"pre":"existing"}}`),
		0644,
	))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "new-entry.json"), []byte(`{"inputs":{"fresh":"value"}}`), 0644))

	require.NoError(t, migrateLegacyTemplatedCache(dir))

	loaded, err := loadTemplatedInputs(dir)
	require.NoError(t, err)
	assert.Equal(t, "existing", loaded["already/here.md"]["pre"], "prior consolidated entry must survive")
	assert.Equal(t, "value", loaded["new-entry"]["fresh"], "legacy entry must fold in")
}

func TestMigrateLegacyTemplatedCache_migratesMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".gitspork")
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "docs"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "Makefile.json"), []byte(`{"inputs":{"a":"1"}}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "docs", "api.md.json"), []byte(`{"inputs":{"b":"2"}}`), 0644))

	require.NoError(t, migrateLegacyTemplatedCache(dir))

	loaded, err := loadTemplatedInputs(dir)
	require.NoError(t, err)
	assert.Equal(t, "1", loaded["Makefile"]["a"])
	assert.Equal(t, "2", loaded["docs/api.md"]["b"])
}

func TestMigrateLegacyTemplatedCache_idempotentSecondRun(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".gitspork")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "Makefile.json"), []byte(`{"inputs":{"k":"v"}}`), 0644))

	require.NoError(t, migrateLegacyTemplatedCache(dir))
	firstInfo, err := os.Stat(filepath.Join(cacheDir, templatedInputsCacheFileName))
	require.NoError(t, err)

	require.NoError(t, migrateLegacyTemplatedCache(dir))
	secondInfo, err := os.Stat(filepath.Join(cacheDir, templatedInputsCacheFileName))
	require.NoError(t, err)
	assert.Equal(t, firstInfo.ModTime(), secondInfo.ModTime(), "second run must not rewrite the consolidated cache")
}
