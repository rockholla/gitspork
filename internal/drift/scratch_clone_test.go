package drift

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_provisionScratchClone_happyPath(t *testing.T) {
	src := t.TempDir()
	makeBaselineRepo(t, src)

	srcHead := gitRevParseHEAD(t, src)

	scratch, cleanup, err := provisionScratchClone(src)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	// Scratch is a git repo pinned to src's HEAD hash.
	scratchHead := gitRevParseHEAD(t, scratch)
	assert.Equal(t, srcHead, scratchHead, "scratch clone HEAD should match caller HEAD")

	// Tracked files are present with the same content.
	got, err := os.ReadFile(filepath.Join(scratch, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "baseline content", string(got))
}

// gitRevParseHEAD is a small shell-git wrapper used by these tests to read
// HEAD hashes without pulling in go-git.
func gitRevParseHEAD(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-c", "safe.directory=*", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func Test_provisionScratchClone_detachedHEADSource(t *testing.T) {
	src := t.TempDir()
	makeBaselineRepo(t, src)
	srcHead := gitRevParseHEAD(t, src)

	// Detach source HEAD by checking out the commit hash directly.
	require.NoError(t, exec.Command("git", "-c", "safe.directory=*", "-C", src, "-c", "advice.detachedHead=false", "checkout", srcHead).Run())

	scratch, cleanup, err := provisionScratchClone(src)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	assert.Equal(t, srcHead, gitRevParseHEAD(t, scratch), "scratch must land on the caller's exact HEAD hash even when source is detached")
}

func Test_provisionScratchClone_cleanupIsIdempotent(t *testing.T) {
	src := t.TempDir()
	makeBaselineRepo(t, src)

	scratch, cleanup, err := provisionScratchClone(src)
	require.NoError(t, err)

	cleanup()
	// Calling cleanup a second time must not panic and must not return the dir.
	assert.NotPanics(t, cleanup)

	_, statErr := os.Stat(scratch)
	assert.True(t, os.IsNotExist(statErr), "scratch dir should be gone after cleanup")
}

func Test_provisionScratchClone_failsOnNonRepo(t *testing.T) {
	src := t.TempDir() // no git init

	_, cleanup, err := provisionScratchClone(src)
	if cleanup != nil {
		defer cleanup()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving caller HEAD hash",
		"error should identify the rev-parse step that failed")
}

func Test_ensureStateFilePresent_copiesWhenMissingInScratch(t *testing.T) {
	src := t.TempDir()
	scratch := t.TempDir()

	// Simulate the edge case: state file exists in caller but scratch was
	// provisioned without it (git clone of a repo where the file is ignored
	// and never committed).
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".gitspork"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".gitspork/downstream-state.json"), []byte(`{"upstreams":[]}`), 0644))

	require.NoError(t, ensureStateFilePresent(src, scratch))

	got, err := os.ReadFile(filepath.Join(scratch, ".gitspork/downstream-state.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"upstreams":[]}`, string(got))
}

func Test_ensureStateFilePresent_noopWhenAlreadyInScratch(t *testing.T) {
	src := t.TempDir()
	scratch := t.TempDir()

	// State present in both (normal case: git clone brought it over).
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".gitspork"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".gitspork/downstream-state.json"), []byte(`{"upstreams":["src"]}`), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(scratch, ".gitspork"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(scratch, ".gitspork/downstream-state.json"), []byte(`{"upstreams":["scratch"]}`), 0644))

	require.NoError(t, ensureStateFilePresent(src, scratch))

	// The scratch's existing file wins — this helper never overwrites.
	got, err := os.ReadFile(filepath.Join(scratch, ".gitspork/downstream-state.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"upstreams":["scratch"]}`, string(got))
}

func Test_ensureStateFilePresent_noopWhenAbsentInBoth(t *testing.T) {
	src := t.TempDir()
	scratch := t.TempDir()

	// Nothing to copy: no state file anywhere. The downstream state loader
	// will produce its own not-found error later; this helper stays quiet.
	require.NoError(t, ensureStateFilePresent(src, scratch))

	_, err := os.Stat(filepath.Join(scratch, ".gitspork/downstream-state.json"))
	assert.True(t, os.IsNotExist(err))
}
