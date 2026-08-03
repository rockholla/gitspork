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
	githttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
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

// TestShellGitClone_MirrorLocal exercises the Mirror=true flag with a
// file:// source — the exact combination populateCache uses when the git
// binary is on PATH. Confirms the destination is a bare mirror (HEAD file
// present at the root, refs/ populated, no working tree / no .git dir).
func TestShellGitClone_MirrorLocal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	upstream, upstreamHash := testharness.MinimalUpstream(t)

	dest := filepath.Join(t.TempDir(), "mirror")
	var progress bytes.Buffer
	err := shellGitClone(
		context.Background(),
		"file://"+upstream,
		dest,
		shellGitCloneOptions{Mirror: true},
		&progress,
		sdktypes.NoopLogger(),
	)
	require.NoError(t, err)

	// Bare mirror layout: HEAD file at the root, no .git subdir.
	if _, err := os.Stat(filepath.Join(dest, "HEAD")); err != nil {
		t.Fatalf("expected HEAD file in mirror at %s: %v", dest, err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Fatalf("bare mirror must NOT have a nested .git dir at %s: err=%v", dest, err)
	}
	// Branch refs may live under refs/heads/ OR be collapsed into a
	// packed-refs file by shell git — accept either shape. What matters is
	// that go-git can walk the branch iterator and see at least one.
	mirror, err := gogit.PlainOpen(dest)
	require.NoError(t, err)
	branches, err := mirror.Branches()
	require.NoError(t, err)
	var branchCount int
	require.NoError(t, branches.ForEach(func(_ *plumbing.Reference) error {
		branchCount++
		return nil
	}))
	assert.Greater(t, branchCount, 0, "mirror should carry at least one branch ref")

	// The upstream's HEAD commit hash is reachable in the mirror.
	_, err = mirror.CommitObject(upstreamHash)
	assert.NoError(t, err, "mirror must carry the upstream's HEAD commit")
}

// TestRewriteHTTPSWithToken locks the URL-rewriting contract used by both
// shellGitClone and shellGitFetch for HTTPS authentication. Table-driven so
// each edge case is easy to inspect independently.
func TestRewriteHTTPSWithToken(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		token string
		want  string
	}{
		{
			name:  "https URL + token → embedded",
			url:   "https://example.com/org/repo",
			token: "foo",
			want:  "https://x-access-token:foo@example.com/org/repo",
		},
		{
			name:  "https URL + empty token → untouched",
			url:   "https://example.com/org/repo",
			token: "",
			want:  "https://example.com/org/repo",
		},
		{
			name:  "ssh URL + token → untouched (ssh-agent handles auth)",
			url:   "git@github.com:org/repo.git",
			token: "foo",
			want:  "git@github.com:org/repo.git",
		},
		{
			name:  "file URL + token → untouched (no auth needed)",
			url:   "file:///some/path",
			token: "foo",
			want:  "file:///some/path",
		},
		{
			name:  "http (non-tls) URL + token → untouched (only https path embeds)",
			url:   "http://example.com/org/repo",
			token: "foo",
			want:  "http://example.com/org/repo",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, rewriteHTTPSWithToken(tc.url, tc.token))
		})
	}
}

// TestTokenFromAuth locks the auth → token extraction used by populateCache
// and refreshCache to keep their signatures stable. Non-BasicAuth methods
// (nil, SSH agent) must return empty so the URL passes through untouched.
func TestTokenFromAuth(t *testing.T) {
	t.Run("nil auth → empty", func(t *testing.T) {
		assert.Equal(t, "", tokenFromAuth(nil))
	})

	t.Run("BasicAuth → password", func(t *testing.T) {
		a := &githttp.BasicAuth{Username: "gitspork", Password: "the-token"}
		assert.Equal(t, "the-token", tokenFromAuth(a))
	})

	t.Run("SSH agent auth → empty (shell git uses ssh-agent natively)", func(t *testing.T) {
		// NewSSHAgentAuth would try to connect to a live agent — construct
		// the type directly to keep this test hermetic.
		agentAuth := &ssh.PublicKeysCallback{User: "git"}
		assert.Equal(t, "", tokenFromAuth(agentAuth))
	})
}

// TestShellGitFetch_LocalHappyPath exercises the shell git fetch fast path
// end-to-end: mirror-clone a small upstream, advance the upstream, run
// shellGitFetch, and confirm the new commit is now reachable in the mirror.
// This is the exact loop refreshCache runs when a cache entry is stale.
func TestShellGitFetch_LocalHappyPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	upstreamDir, firstHash := testharness.MinimalUpstream(t)

	// Set up the mirror.
	mirrorDir := filepath.Join(t.TempDir(), "mirror")
	require.NoError(t, shellGitClone(
		context.Background(),
		"file://"+upstreamDir,
		mirrorDir,
		shellGitCloneOptions{Mirror: true},
		nil,
		sdktypes.NoopLogger(),
	))

	// Advance the upstream with a new commit.
	require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "added-later.txt"), []byte("later"), 0644))
	upstreamRepo, err := gogit.PlainOpen(upstreamDir)
	require.NoError(t, err)
	secondHash := testharness.CommitAllWithMessage(t, upstreamRepo, "advance upstream after mirror-clone")

	// Before fetch: mirror has firstHash but not secondHash.
	preRepo, err := gogit.PlainOpen(mirrorDir)
	require.NoError(t, err)
	_, err = preRepo.CommitObject(firstHash)
	assert.NoError(t, err, "mirror carries the original commit pre-fetch")
	_, err = preRepo.CommitObject(secondHash)
	assert.Error(t, err, "mirror must NOT carry the advance commit pre-fetch")

	// Fetch and re-open (go-git packfile index cache doesn't invalidate on
	// external writes — cache_test.go documents this).
	require.NoError(t, shellGitFetch(
		context.Background(),
		mirrorDir,
		"file://"+upstreamDir,
		shellGitFetchOptions{},
		nil,
	))

	postRepo, err := gogit.PlainOpen(mirrorDir)
	require.NoError(t, err)
	_, err = postRepo.CommitObject(secondHash)
	assert.NoError(t, err, "mirror must carry the advance commit after fetch")
	_, err = postRepo.CommitObject(firstHash)
	assert.NoError(t, err, "original commit still present after fetch")
}

// TestShellGitFetch_NoChanges_Succeeds locks the "nothing to fetch" branch:
// shell git returns 0 when the mirror is already up to date, matching
// go-git's NoErrAlreadyUpToDate as a success signal.
func TestShellGitFetch_NoChanges_Succeeds(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	upstreamDir, _ := testharness.MinimalUpstream(t)

	mirrorDir := filepath.Join(t.TempDir(), "mirror")
	require.NoError(t, shellGitClone(
		context.Background(),
		"file://"+upstreamDir,
		mirrorDir,
		shellGitCloneOptions{Mirror: true},
		nil,
		sdktypes.NoopLogger(),
	))

	// No upstream changes — fetch should succeed silently.
	require.NoError(t, shellGitFetch(
		context.Background(),
		mirrorDir,
		"file://"+upstreamDir,
		shellGitFetchOptions{},
		nil,
	))
}

// TestUseShellGitFastPath_gitOnPATH sanity-checks the wrapper. Not a very
// deep test — gitbin.Require has its own coverage — but the wrapper is what
// all four call sites gate on, so lock it in.
func TestUseShellGitFastPath_gitOnPATH(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	assert.True(t, useShellGitFastPath(), "git on PATH → shell git fast path enabled")
}

func TestUseShellGitFastPath_gitMissing(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-path-for-gitspork-tests")
	assert.False(t, useShellGitFastPath(), "git missing from PATH → fast path disabled")
}
