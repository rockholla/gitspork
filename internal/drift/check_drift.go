package drift

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	fdiff "github.com/go-git/go-git/v6/plumbing/format/diff"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/integrate"
	"github.com/rockholla/gitspork/v2/internal/logutil"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

const driftCheckBranch = "_gitspork-check-drift"

// CheckDrift detects whether the downstream has drifted from its last integrated upstream state
func CheckDrift(opts *sdktypes.CheckDriftOptions) (*sdktypes.DriftReport, error) {
	report := &sdktypes.DriftReport{}
	var err error

	if opts.Logger == nil {
		opts.Logger = sdktypes.NoopLogger()
	}

	if opts.DownstreamRepoPath == "" {
		opts.DownstreamRepoPath, err = os.Getwd()
		if err != nil {
			return report, fmt.Errorf("unable to get the present working directory: %v", err)
		}
	} else {
		opts.DownstreamRepoPath, err = filepath.Abs(opts.DownstreamRepoPath)
		if err != nil {
			return report, fmt.Errorf("unable to determine local downstream repo path: %v", err)
		}
	}

	state, err := integrate.LoadDownstreamState(opts.DownstreamRepoPath)
	if err != nil {
		return report, fmt.Errorf("error loading downstream state: %v", err)
	}

	// Resolve which upstreams to check and their recorded commit hashes.
	type upstreamCheckEntry struct {
		spec       sdktypes.UpstreamSpec
		commitHash string
	}
	var entries []upstreamCheckEntry

	if len(opts.Upstreams) > 0 {
		// Override mode: match each override to its state entry for the commit hash.
		for _, override := range opts.Upstreams {
			key := integrate.NormalizeUpstreamURL(override.URL, override.Subpath)
			found := false
			for _, su := range state.Upstreams {
				if integrate.NormalizeUpstreamURL(su.URL, su.Subpath) == key {
					entries = append(entries, upstreamCheckEntry{spec: override, commitHash: su.CommitHash})
					found = true
					break
				}
			}
			if !found {
				return report, fmt.Errorf("--upstream override %q has no matching state entry — run 'gitspork integrate' first", override.URL)
			}
		}
	} else {
		if len(state.Upstreams) == 0 {
			return report, fmt.Errorf("no previous integration found in downstream state — run 'gitspork integrate' first")
		}
		for _, su := range state.Upstreams {
			entries = append(entries, upstreamCheckEntry{
				spec:       sdktypes.UpstreamSpec{URL: su.URL, Subpath: su.Subpath},
				commitHash: su.CommitHash,
			})
		}
	}

	repo, err := gogit.PlainOpen(opts.DownstreamRepoPath)
	if err != nil {
		return report, fmt.Errorf("error opening downstream repo: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return report, fmt.Errorf("error accessing downstream worktree: %v", err)
	}

	if err := checkCleanWorkingTree(opts.DownstreamRepoPath); err != nil {
		return report, err
	}

	headRef, err := repo.Head()
	if err != nil {
		return report, fmt.Errorf("error resolving HEAD: %v", err)
	}

	// Remember how to restore HEAD once the drift check finishes. CI runners
	// (e.g. Buildkite) typically check out a specific commit, leaving a detached
	// HEAD with no branch to return to; in that case restore by hash, otherwise
	// restore the original branch.
	restore := &gogit.CheckoutOptions{Hash: headRef.Hash()}
	if headRef.Name().IsBranch() {
		restore = &gogit.CheckoutOptions{Branch: headRef.Name()}
	}

	driftBranchRef := plumbing.NewBranchReferenceName(driftCheckBranch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(driftBranchRef, headRef.Hash())); err != nil {
		return report, fmt.Errorf("error creating/resetting drift-check branch: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: driftBranchRef}); err != nil {
		return report, fmt.Errorf("error checking out drift-check branch: %v", err)
	}
	defer func() {
		_ = wt.Checkout(restore)
		_ = repo.DeleteBranch(driftCheckBranch)
	}()

	// Re-integrate each upstream; track which files each one last touched.
	// fileOwner maps relative file path -> upstream URL that last wrote it.
	fileOwner := map[string]string{}

	for _, entry := range entries {
		opts.Logger.Log("re-integrating upstream %s at commit %s", entry.spec.URL, entry.commitHash)

		beforeFiles, err := listWorktreeFiles(opts.DownstreamRepoPath)
		if err != nil {
			return report, fmt.Errorf("error listing worktree files before integrate: %v", err)
		}

		if err := integrate.IntegrateForDriftCheck(&integrate.DriftCheckRequest{
			Logger:             opts.Logger,
			DownstreamRepoPath: opts.DownstreamRepoPath,
			UpstreamURL:        entry.spec.URL,
			UpstreamSubpath:    entry.spec.Subpath,
			UpstreamToken:      entry.spec.Token,
			UpstreamCommit:     entry.commitHash,
		}); err != nil {
			return report, fmt.Errorf("error running integration for drift check: %v", err)
		}

		afterFiles, err := listWorktreeFiles(opts.DownstreamRepoPath)
		if err != nil {
			return report, fmt.Errorf("error listing worktree files after integrate: %v", err)
		}

		// Any file that appeared or changed is attributed to this upstream.
		for f, hash := range afterFiles {
			if beforeFiles[f] != hash {
				fileOwner[f] = entry.spec.URL
			}
		}
		for f := range beforeFiles {
			if _, stillPresent := afterFiles[f]; !stillPresent {
				fileOwner[f] = entry.spec.URL
			}
		}
	}

	patch, err := diffWorktreeAgainstHEAD(repo, wt)
	if err != nil {
		return report, fmt.Errorf("error diffing downstream against HEAD: %v", err)
	}
	if patch == nil {
		return report, nil
	}

	report.HasDrift = true
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		var name string
		switch {
		case to != nil:
			name = to.Path()
		case from != nil:
			name = from.Path()
		default:
			continue
		}
		diffText, err := encodeFilePatch(fp)
		if err != nil {
			return report, fmt.Errorf("error encoding per-file diff for %s: %v", name, err)
		}
		report.Files = append(report.Files, sdktypes.DriftedFile{
			Path:          name,
			AttributedURL: fileOwner[name], // empty string means unattributed
			Diff:          diffText,
			ColorizedDiff: logutil.ColorizeUnifiedDiff(diffText),
		})
	}

	return report, sdktypes.ErrDriftDetected
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

	sig := &object.Signature{Name: config.GitSpork, Email: config.GitSpork + "@localhost", When: time.Now()}
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

// singleFilePatch adapts a single fdiff.FilePatch to the fdiff.Patch interface
// so it can be run through UnifiedEncoder to produce a per-file unified diff.
type singleFilePatch struct {
	fp fdiff.FilePatch
}

var _ fdiff.Patch = (*singleFilePatch)(nil)

func (s *singleFilePatch) FilePatches() []fdiff.FilePatch { return []fdiff.FilePatch{s.fp} }
func (s *singleFilePatch) Message() string                { return "" }

// encodeFilePatch renders one file's unified diff to a string.
func encodeFilePatch(fp fdiff.FilePatch) (string, error) {
	var buf bytes.Buffer
	enc := fdiff.NewUnifiedEncoder(&buf, fdiff.DefaultContextLines)
	if err := enc.Encode(&singleFilePatch{fp: fp}); err != nil {
		return "", err
	}
	return buf.String(), nil
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

// listWorktreeFiles returns a map of relative path -> hex content hash for all
// non-.git files under dir. Used to detect which files an integrate pass touched.
func listWorktreeFiles(dir string) (map[string]string, error) {
	result := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h := fmt.Sprintf("%x", sha256.Sum256(b))
		result[rel] = h
		return nil
	})
	return result, err
}
