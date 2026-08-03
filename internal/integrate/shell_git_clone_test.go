package integrate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/rockholla/gitspork/v2/test/testharness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShellGitClone_LocalHappyPath exercises the primary fast-path flag
// combination used by cloneUpstreamForIntegrate: Local=true from a file://
// URL. Locks in that the resulting directory contains the source's tracked
// files and that shell git's progress lines are forwarded to the caller-
// supplied writer.
func TestShellGitClone_LocalHappyPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	upstream, _ := testharness.MinimalUpstream(t)

	dest := filepath.Join(t.TempDir(), "clone")
	var progress bytes.Buffer
	err := shellGitClone(
		context.Background(),
		"file://"+upstream,
		dest,
		shellGitCloneOptions{Local: true},
		&progress,
		sdktypes.NoopLogger(),
	)
	require.NoError(t, err)

	// Clone dir must exist and contain the tracked files from the upstream.
	info, err := os.Stat(dest)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// testharness.MinimalUpstream lays down .gitspork.yml at the root of the
	// upstream. Confirm the file made it into the destination checkout.
	if _, err := os.Stat(filepath.Join(dest, ".gitspork.yml")); err != nil {
		t.Fatalf("expected .gitspork.yml in fresh clone at %s: %v", dest, err)
	}

	// Shell git emits at least a "Cloning into '<dest>'..." line to stderr.
	// The writer must have received it.
	assert.NotEmpty(t, progress.String(), "expected git clone progress output on stderr")
}

// TestShellGitClone_LocalWithReferenceNameTag confirms that a refs/tags/<v>
// ReferenceName maps to `--branch <v>` (short name) and lands the clone on
// the tag's commit.
func TestShellGitClone_LocalWithReferenceNameTag(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	// Build a tiny upstream with a tag.
	const tagName = "v1.2.3"
	upstream, headHash := testharness.MinimalUpstreamWithTag(t, tagName)

	dest := filepath.Join(t.TempDir(), "clone")
	var progress bytes.Buffer
	err := shellGitClone(
		context.Background(),
		"file://"+upstream,
		dest,
		shellGitCloneOptions{
			Local:         true,
			SingleBranch:  true,
			ReferenceName: "refs/tags/" + tagName,
		},
		&progress,
		sdktypes.NoopLogger(),
	)
	require.NoError(t, err)

	// Confirm the clone lands on the tagged commit.
	cloned, err := gogit.PlainOpen(dest)
	require.NoError(t, err)
	head, err := cloned.Head()
	require.NoError(t, err)
	assert.Equal(t, headHash, head.Hash(),
		"clone at tag %q should land on the tag's commit", tagName)
}

// TestShellGitClone_LocalWithReferenceNameBranch confirms the refs/heads/
// prefix is stripped correctly when mapping to --branch.
func TestShellGitClone_LocalWithReferenceNameBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	upstream, headHash := testharness.MinimalUpstream(t)

	dest := filepath.Join(t.TempDir(), "clone")
	var progress bytes.Buffer
	err := shellGitClone(
		context.Background(),
		"file://"+upstream,
		dest,
		shellGitCloneOptions{
			Local:         true,
			SingleBranch:  true,
			ReferenceName: string(plumbing.NewBranchReferenceName("main")),
		},
		&progress,
		sdktypes.NoopLogger(),
	)
	require.NoError(t, err)

	cloned, err := gogit.PlainOpen(dest)
	require.NoError(t, err)
	head, err := cloned.Head()
	require.NoError(t, err)
	assert.Equal(t, headHash, head.Hash())
}
