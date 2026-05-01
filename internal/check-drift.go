package internal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

	if err := checkCleanWorkingTree(opts.DownstreamRepoPath); err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", gitSpork+"-drift")
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if err := copyDir(opts.DownstreamRepoPath, tempDir); err != nil {
		return fmt.Errorf("error copying downstream to temp dir: %v", err)
	}

	if err := initGitBaseline(tempDir); err != nil {
		return fmt.Errorf("error initialising git baseline in temp dir: %v", err)
	}

	upstreamURL := opts.UpstreamRepoURL
	if upstreamURL == "" {
		upstreamURL = state.LastUpstreamRepoURL
	}
	if upstreamURL == "" {
		return fmt.Errorf("no upstream repo URL found in state — re-run 'gitspork integrate' or pass --upstream-repo-url")
	}

	opts.Logger.Log("re-integrating at upstream commit %s to check for drift", state.LastUpstreamCommitHash)
	if err := Integrate(&IntegrateOptions{
		Logger:              opts.Logger,
		UpstreamRepoURL:     upstreamURL,
		UpstreamRepoCommit:  state.LastUpstreamCommitHash,
		UpstreamRepoSubpath: state.LastUpstreamRepoSubpath,
		UpstreamRepoToken:   opts.UpstreamRepoToken,
		DownstreamRepoPath:  tempDir,
	}); err != nil {
		return fmt.Errorf("error running integration for drift check: %v", err)
	}

	diffOutput, err := runGitDiff(tempDir)
	if err != nil {
		return fmt.Errorf("error running git diff in temp dir: %v", err)
	}

	if diffOutput == "" {
		opts.Logger.Log("no drift detected")
		return nil
	}

	changedFiles, err := runGitDiffNameOnly(tempDir)
	if err != nil {
		return fmt.Errorf("error getting changed file list: %v", err)
	}
	files := strings.Split(strings.TrimSpace(changedFiles), "\n")
	opts.Logger.Log("drift detected: %d file(s) changed", len(files))
	for _, f := range files {
		opts.Logger.Log("  %s", f)
	}
	if opts.Verbose {
		fmt.Println(diffOutput)
	}

	return ErrDriftDetected
}

func checkCleanWorkingTree(repoPath string) error {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("error checking working tree status: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("working tree is not clean — commit or stash changes before running check-drift")
	}
	return nil
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return syncFile(path, target)
	})
}

func initGitBaseline(dir string) error {
	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "gitspork@localhost"},
		{"git", "config", "user.name", "gitspork"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", "baseline"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("error running %v: %v\n%s", args, err, string(out))
		}
	}
	return nil
}

func runGitDiff(dir string) (string, error) {
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func runGitDiffNameOnly(dir string) (string, error) {
	cmd := exec.Command("git", "diff", "HEAD", "--name-only")
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
