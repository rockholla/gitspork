package integrate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestPrepareShellGitAuth locks the credential-helper wiring used by
// shellGitClone / shellGitFetch / shellGitLsRemote for HTTPS token auth.
// The token must NOT appear in credArgs (that's the whole point — argv is
// world-readable via `ps`); it goes into the child env under
// shellGitAuthEnvVar. Non-HTTPS / empty-token cases return nil/nil so
// callers pass URLs through untouched.
func TestPrepareShellGitAuth(t *testing.T) {
	t.Run("https URL + token → cred helper args + env with token", func(t *testing.T) {
		credArgs, env := prepareShellGitAuth("https://example.com/org/repo", "the-token")
		require.NotNil(t, credArgs)
		require.NotNil(t, env)

		// Must not leak the token via argv.
		for _, a := range credArgs {
			assert.NotContains(t, a, "the-token", "token must not appear in argv (Copilot PR#1113 threat model)")
		}
		// Must install the credential.helper that reads the scoped env var.
		joined := strings.Join(credArgs, " ")
		assert.Contains(t, joined, "credential.helper=")
		assert.Contains(t, joined, "$"+shellGitAuthEnvVar)
		// Env carries the token under the scoped name.
		want := shellGitAuthEnvVar + "=the-token"
		found := false
		for _, e := range env {
			if e == want {
				found = true
				break
			}
		}
		assert.True(t, found, "expected %q in child env; got %v", want, env)
	})

	t.Run("https URL + empty token → nil/nil (trust ambient git config)", func(t *testing.T) {
		credArgs, env := prepareShellGitAuth("https://example.com/org/repo", "")
		assert.Nil(t, credArgs)
		assert.Nil(t, env)
	})

	t.Run("ssh URL + token → nil/nil (ssh-agent handles auth)", func(t *testing.T) {
		credArgs, env := prepareShellGitAuth("git@github.com:org/repo.git", "foo")
		assert.Nil(t, credArgs)
		assert.Nil(t, env)
	})

	t.Run("file URL + token → nil/nil (no auth needed)", func(t *testing.T) {
		credArgs, env := prepareShellGitAuth("file:///some/path", "foo")
		assert.Nil(t, credArgs)
		assert.Nil(t, env)
	})

	t.Run("http (non-tls) URL + token → nil/nil (only https gets the helper)", func(t *testing.T) {
		credArgs, env := prepareShellGitAuth("http://example.com/org/repo", "foo")
		assert.Nil(t, credArgs)
		assert.Nil(t, env)
	})

	t.Run("pre-existing token env var is filtered so our value is definitive", func(t *testing.T) {
		t.Setenv(shellGitAuthEnvVar, "stale-value")
		_, env := prepareShellGitAuth("https://example.com/org/repo", "fresh-value")
		var seen int
		for _, e := range env {
			if strings.HasPrefix(e, shellGitAuthEnvVar+"=") {
				seen++
				assert.Equal(t, shellGitAuthEnvVar+"=fresh-value", e, "child env must carry only the fresh value")
			}
		}
		assert.Equal(t, 1, seen, "child env must contain exactly one entry for %s", shellGitAuthEnvVar)
	})
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

// TestShellGitLsRemote_returnsAllRefs exercises the shell-git ls-remote
// fast path used by resolveUpstreamVersionRef. Builds a tiny upstream with
// both a tag and (implicitly) a default branch, then asserts both refs come
// back keyed by full refname.
func TestShellGitLsRemote_returnsAllRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	upstream, hash := testharness.MinimalUpstreamWithTag(t, "v1.2.3")

	refs, err := shellGitLsRemote(context.Background(), "file://"+upstream, "")
	require.NoError(t, err)

	// Tag should be present as refs/tags/v1.2.3 → HEAD hash.
	assert.Equal(t, hash.String(), refs["refs/tags/v1.2.3"], "tag ref should map to the tagged commit")
	// Default branch (main; MinimalUpstream initializes it) should be present too.
	assert.Equal(t, hash.String(), refs["refs/heads/main"], "default branch ref should map to HEAD")
}

// TestShellGitLsRemote_emptyURL surfaces a clear guard rather than shelling
// out to `git ls-remote` with no argument.
func TestShellGitLsRemote_emptyURL(t *testing.T) {
	_, err := shellGitLsRemote(context.Background(), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty url")
}
