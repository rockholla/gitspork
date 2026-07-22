package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
