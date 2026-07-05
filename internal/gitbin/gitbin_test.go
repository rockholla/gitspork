package gitbin

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

func TestRequire_present(t *testing.T) {
	// Sanity check: the test host should have git installed.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed on this host; cannot test the happy path")
	}
	require.NoError(t, Require())
}

func TestRequire_missing(t *testing.T) {
	// Scrub PATH so exec.LookPath cannot find git.
	t.Setenv("PATH", "/nonexistent-path-for-gitspork-tests")

	err := Require()
	require.Error(t, err)
	assert.True(t, errors.Is(err, sdktypes.ErrGitBinaryMissing),
		"error should wrap sdktypes.ErrGitBinaryMissing so callers can errors.Is against it")
	assert.Contains(t, err.Error(), "install git",
		"error message should point users at the fix")
}
