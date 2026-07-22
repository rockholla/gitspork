//go:build sdk

package sdk_test

import (
	"os"
	"testing"
)

// TestMain sets GITSPORK_NO_CACHE=1 as the default for the SDK test suite so
// that SDK-level Integrate calls that advance the upstream between two
// invocations don't hit stale-but-fresh cache entries. Cache-specific SDK
// tests (TestIntegrate_cache_SDK_*) override with t.Setenv to re-enable the
// cache for their scope. Also isolates any code path that DOES touch the
// cache from the user's real machine cache by pinning GITSPORK_CACHE_DIR to
// a per-run tempdir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gitspork-sdk-cache-")
	if err != nil {
		panic("failed to create test cache dir: " + err.Error())
	}
	defer os.RemoveAll(dir)
	if err := os.Setenv("GITSPORK_CACHE_DIR", dir); err != nil {
		panic("failed to set GITSPORK_CACHE_DIR: " + err.Error())
	}
	if err := os.Setenv("GITSPORK_NO_CACHE", "1"); err != nil {
		panic("failed to set GITSPORK_NO_CACHE: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
