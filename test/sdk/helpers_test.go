//go:build sdk

package sdk_test

import (
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"

	"github.com/rockholla/gitspork/v2/internal/testharness"
)

// minimalUpstream builds a local upstream git repo with a minimal .gitspork.yml
// (upstream_owned only) and one file. Returns the repo dir and its HEAD hash.
func minimalUpstream(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	return testharness.MinimalUpstream(t)
}

// emptyDownstream returns a fresh non-bare local downstream git repo dir.
func emptyDownstream(t *testing.T) string {
	t.Helper()
	return testharness.EmptyDownstream(t)
}

// writeAndCommit writes a file in downstreamDir and commits it, returning the
// resulting commit hash.
func writeAndCommit(t *testing.T, downstreamDir, relPath, content string) plumbing.Hash {
	t.Helper()
	full := filepath.Join(downstreamDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	return testharness.CommitAllWithMessage(t, repo, "edit: "+relPath)
}

// fileExists returns nil if the file exists at base/parts..., non-nil error otherwise.
func fileExists(base string, parts ...string) error {
	_, err := os.Stat(filepath.Join(append([]string{base}, parts...)...))
	return err
}
