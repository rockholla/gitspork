package integrate

import (
	"os"
	"testing"
)

// TestMain sets GITSPORK_CACHE_DIR to a per-run tempdir before any tests
// execute. This isolates end-to-end Integrate tests (which go through
// ensureUpstreamCache) from the user's real machine cache. Individual tests
// that need a specific cache dir can override via t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gitspork-test-cache-")
	if err != nil {
		panic("failed to create test cache dir: " + err.Error())
	}
	defer os.RemoveAll(dir)
	if err := os.Setenv("GITSPORK_CACHE_DIR", dir); err != nil {
		panic("failed to set GITSPORK_CACHE_DIR: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir) // idempotent, matches defer
	os.Exit(code)
}
