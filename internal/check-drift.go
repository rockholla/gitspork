package internal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

const driftCheckBranch = "_gitspork-check-drift"

// ErrDriftDetected is returned by CheckDrift when drift is found in the downstream
var ErrDriftDetected = errors.New("drift detected")

// CheckDrift detects whether the downstream has drifted from its last integrated upstream state
func CheckDrift(opts *CheckDriftOptions) error {
	var err error

	if opts.DownstreamRepoPath == "" {
		opts.DownstreamRepoPath, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("unable to get the present working directory: %v", err)
		}
	} else {
		opts.DownstreamRepoPath, err = filepath.Abs(opts.DownstreamRepoPath)
		if err != nil {
			return fmt.Errorf("unable to determine local downstream repo path: %v", err)
		}
	}

	state, err := loadDownstreamState(opts.DownstreamRepoPath)
	if err != nil {
		return fmt.Errorf("error loading downstream state: %v", err)
	}
	if state.LastUpstreamCommitHash == "" {
		return fmt.Errorf("no previous integration found in downstream state — run 'gitspork integrate' first")
	}

	upstreamURL := opts.UpstreamRepoURL
	if upstreamURL == "" {
		upstreamURL = state.LastUpstreamRepoURL
	}
	if upstreamURL == "" {
		return fmt.Errorf("no upstream repo URL found in state — re-run 'gitspork integrate' or pass --upstream-repo-url")
	}

	repo, err := gogit.PlainOpen(opts.DownstreamRepoPath)
	if err != nil {
		return fmt.Errorf("error opening downstream repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("error accessing downstream worktree: %v", err)
	}

	if err := checkCleanWorkingTree(opts.DownstreamRepoPath); err != nil {
		return err
	}

	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("error resolving HEAD: %v", err)
	}
	if !headRef.Name().IsBranch() {
		return fmt.Errorf("downstream repo is in detached HEAD state — check out a branch before running check-drift")
	}
	originalBranch := headRef.Name()

	// create or reset the drift-check branch to the current HEAD
	driftBranchRef := plumbing.NewBranchReferenceName(driftCheckBranch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(driftBranchRef, headRef.Hash())); err != nil {
		return fmt.Errorf("error creating/resetting drift-check branch: %v", err)
	}

	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: driftBranchRef}); err != nil {
		return fmt.Errorf("error checking out drift-check branch: %v", err)
	}

	defer func() {
		_ = wt.Checkout(&gogit.CheckoutOptions{Branch: originalBranch})
		_ = repo.DeleteBranch(driftCheckBranch)
	}()

	opts.Logger.Log("re-integrating at upstream commit %s to check for drift", state.LastUpstreamCommitHash)
	if err := Integrate(&IntegrateOptions{
		Logger:              opts.Logger,
		UpstreamRepoURL:     upstreamURL,
		UpstreamRepoCommit:  state.LastUpstreamCommitHash,
		UpstreamRepoSubpath: state.LastUpstreamRepoSubpath,
		UpstreamRepoToken:   opts.UpstreamRepoToken,
		DownstreamRepoPath:  opts.DownstreamRepoPath,
		ForDriftCheck:       true,
	}); err != nil {
		return fmt.Errorf("error running integration for drift check: %v", err)
	}

	patch, err := diffWorktreeAgainstHEAD(repo, wt)
	if err != nil {
		return fmt.Errorf("error diffing downstream against HEAD: %v", err)
	}

	if patch == nil {
		opts.Logger.Log("no drift detected")
		return nil
	}

	stats := patch.Stats()
	opts.Logger.Log("drift detected: %d file(s) changed", len(stats))
	for _, s := range stats {
		opts.Logger.Log("  %s", s.Name)
	}
	if opts.Verbose {
		pr, pw := io.Pipe()
		go func() { pw.CloseWithError(patch.Encode(pw)) }()
		if err := opts.Logger.Diff(pr); err != nil {
			return fmt.Errorf("error encoding diff: %v", err)
		}
	}

	return ErrDriftDetected
}

// diffWorktreeAgainstHEAD stages all changes and compares against HEAD.
// Returns nil patch when there are no changes (no drift).
func diffWorktreeAgainstHEAD(repo *gogit.Repository, wt *gogit.Worktree) (*object.Patch, error) {
	headRef, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("error resolving HEAD: %v", err)
	}
	headCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("error loading HEAD commit: %v", err)
	}

	if err := wt.AddWithOptions(&gogit.AddOptions{All: true}); err != nil {
		return nil, fmt.Errorf("error staging changes: %v", err)
	}

	sig := &object.Signature{Name: gitSpork, Email: gitSpork + "@localhost", When: time.Now()}
	newHash, err := wt.Commit("drift-check", &gogit.CommitOptions{Author: sig})
	if errors.Is(err, gogit.ErrEmptyCommit) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error committing staged changes: %v", err)
	}

	newCommit, err := repo.CommitObject(newHash)
	if err != nil {
		return nil, fmt.Errorf("error loading new commit: %v", err)
	}

	// Reverse: show what drifted from clean (re-integrated) state to HEAD,
	// not what integrate would do to correct it.
	patch, err := newCommit.Patch(headCommit)
	if err != nil {
		return nil, fmt.Errorf("error computing patch: %v", err)
	}
	return patch, nil
}

func checkCleanWorkingTree(repoPath string) error {
	out, err := exec.Command("git", "-c", "safe.directory=*", "-C", repoPath, "status", "--porcelain").Output()
	if err != nil {
		return fmt.Errorf("error checking working tree status: %v", err)
	}
	if status := strings.TrimSpace(string(out)); status != "" {
		return fmt.Errorf("working tree is not clean — commit or stash changes before running check-drift:\n%s\n\n"+
			"note: this may be running in a container with different global gitignore rules than your local git environment, "+
			"which can explain differences you see versus a local `git status`. "+
			"Commit needed gitignore changes to your repo's .gitignore in these cases to ensure the repo ignores what you need it to regardless of global rules.", status)
	}
	return nil
}
