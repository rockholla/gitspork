package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rockholla/gitspork/v2/internal/integrate"
)

func Test_cacheDirCommand_printsResolvedRoot(t *testing.T) {
	t.Setenv("GITSPORK_CACHE_DIR", "/tmp/test-cache-dir")

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"dir"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "/tmp/test-cache-dir\n", out.String())
}

func Test_cacheDirCommand_defaultRoot(t *testing.T) {
	t.Setenv("GITSPORK_CACHE_DIR", "")

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"dir"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())

	printed := strings.TrimSpace(out.String())
	userCache, _ := os.UserCacheDir()
	assert.Equal(t, userCache+"/gitspork/repos", printed)
}

func Test_cacheClearCommand_wipesRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", dir)
	require.NoError(t, os.MkdirAll(dir+"/entry1", 0755))
	require.NoError(t, os.WriteFile(dir+"/entry1/HEAD", []byte("x"), 0644))
	require.NoError(t, os.WriteFile(dir+"/entry1.fetched-at", []byte("123"), 0644))
	require.NoError(t, os.WriteFile(dir+"/entry1.lock", nil, 0644))

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"clear", "--force"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())

	entries, err := os.ReadDir(dir)
	if err == nil {
		assert.Empty(t, entries, "cache root must be empty after clear --force")
	}
}

func Test_cacheClearCommand_wipesSingleURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", dir)

	url := "file:///some/upstream"
	key := integrate.CacheKeyForURL(url)
	require.NoError(t, os.MkdirAll(dir+"/"+key, 0755))
	require.NoError(t, os.WriteFile(dir+"/"+key+".fetched-at", []byte("123"), 0644))
	require.NoError(t, os.WriteFile(dir+"/other-entry.fetched-at", []byte("456"), 0644))

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"clear", "--url", url, "--force"})
	require.NoError(t, cmd.Execute())

	assert.NoFileExists(t, dir+"/"+key)
	assert.NoFileExists(t, dir+"/"+key+".fetched-at")
	assert.FileExists(t, dir+"/other-entry.fetched-at", "unrelated entry must survive")
}

func Test_cacheClearCommand_nonTTYWithoutForce_fails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITSPORK_CACHE_DIR", dir)
	require.NoError(t, os.WriteFile(dir+"/entry.fetched-at", []byte("123"), 0644))

	cmd := (&CacheSubcommand{}).GetCmd()
	cmd.SetArgs([]string{"clear"})
	cmd.SetIn(&bytes.Buffer{})
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force", "error must direct the user to --force")
	assert.FileExists(t, dir+"/entry.fetched-at", "entry must NOT be wiped without confirmation/force")
}
