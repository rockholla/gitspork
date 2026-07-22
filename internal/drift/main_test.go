package drift

import (
	"os"
	"testing"
)

// TestMain sets GITSPORK_CACHE_DIR to a per-run tempdir before any tests
// execute. This isolates end-to-end CheckDrift and Integrate calls (which go
// through ensureUpstreamCache) from the user's real machine cache. Mirrors
// internal/integrate/main_test.go.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gitspork-drift-test-cache-")
	if err != nil {
		panic("failed to create test cache dir: " + err.Error())
	}
	defer os.RemoveAll(dir)
	if err := os.Setenv("GITSPORK_CACHE_DIR", dir); err != nil {
		panic("failed to set GITSPORK_CACHE_DIR: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
