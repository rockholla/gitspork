# Drift Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `gitspork check-drift` — a command that detects whether a downstream repo has drifted from its last integrated upstream state, using a safe isolated re-integration workflow.

**Architecture:** `integrate` saves upstream URL, subpath, and resolved commit hash to `.gitspork/downstream-state.json` after each successful run. `check-drift` reads that state, copies the downstream to a temp dir, re-runs the full integration at the stored commit, diffs the result, and reports. URL rewriting (SSH ↔ HTTPS) is applied inside `cloneUpstreamForIntegrate` so it benefits both `integrate` and `check-drift`.

**Tech Stack:** Go 1.26, `github.com/go-git/go-git/v6`, `github.com/spf13/cobra`, `github.com/stretchr/testify`, `os/exec` for git CLI calls, standard library `os` and `path/filepath`.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/gitspork.go` | Modify | Add three new fields to `GitSporkDownstreamState`; add `CheckDriftOptions` type |
| `internal/integrate.go` | Modify | Resolve and save upstream metadata to state after successful clone; add `resolveUpstreamURL` helper called inside `cloneUpstreamForIntegrate` |
| `internal/check-drift.go` | Create | `CheckDrift` function — clean-tree check, temp dir copy, git init, re-integrate, diff, report |
| `internal/check_drift_test.go` | Create | Unit tests for `CheckDrift` |
| `internal/integrate_test.go` | Create | Unit tests for `resolveUpstreamURL` |
| `cmd/check-drift.go` | Create | Cobra subcommand wiring for `gitspork check-drift` |
| `cmd/root.go` | Modify | Register `CheckDriftSubcommand` |

---

## Task 1: Extend `GitSporkDownstreamState` and add `CheckDriftOptions`

**Files:**
- Modify: `internal/gitspork.go`

- [ ] **Step 1: Add three fields to `GitSporkDownstreamState`**

In `internal/gitspork.go`, replace the existing `GitSporkDownstreamState` struct:

```go
// GitSporkDownstreamState represents state stored in the downstream repo to track integrations, etc.
type GitSporkDownstreamState struct {
	MigrationsComplete     []string `json:"migrations_complete" comment:"list of migration IDs that have completed successfully against the downstream repo"`
	LastUpstreamRepoURL    string   `json:"last_upstream_repo_url,omitempty"`
	LastUpstreamRepoSubpath string  `json:"last_upstream_repo_subpath,omitempty"`
	LastUpstreamCommitHash string   `json:"last_upstream_commit_hash,omitempty"`
}
```

- [ ] **Step 2: Add `CheckDriftOptions` type**

In `internal/gitspork.go`, after the `IntegrateLocalOptions` struct, add:

```go
// CheckDriftOptions are options for the CheckDrift method
type CheckDriftOptions struct {
	Logger             *Logger
	DownstreamRepoPath string
	UpstreamRepoURL    string
	UpstreamRepoToken  string
	Verbose            bool
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/gitspork.go
git commit -m "feat: extend downstream state with upstream metadata fields; add CheckDriftOptions"
```

---

## Task 2: Add `resolveUpstreamURL` and wire it into `cloneUpstreamForIntegrate`

**Files:**
- Modify: `internal/integrate.go`
- Create: `internal/integrate_test.go`

- [ ] **Step 1: Write failing tests for `resolveUpstreamURL`**

Create `internal/integrate_test.go`:

```go
package internal

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveUpstreamURL(t *testing.T) {
	t.Run("SSH agent present, no token, HTTPS url -> rewrite to SSH", func(t *testing.T) {
		os.Setenv("SSH_AUTH_SOCK", "/tmp/fake.sock")
		defer os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("https://github.com/org/repo.git", "")
		assert.Equal(t, "git@github.com:org/repo.git", result)
	})

	t.Run("no SSH agent, token provided, SSH url -> rewrite to HTTPS", func(t *testing.T) {
		os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("git@github.com:org/repo.git", "mytoken")
		assert.Equal(t, "https://github.com/org/repo.git", result)
	})

	t.Run("SSH agent present with token, SSH url -> no rewrite", func(t *testing.T) {
		os.Setenv("SSH_AUTH_SOCK", "/tmp/fake.sock")
		defer os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("git@github.com:org/repo.git", "mytoken")
		assert.Equal(t, "git@github.com:org/repo.git", result)
	})

	t.Run("no SSH agent, no token, SSH url -> no rewrite", func(t *testing.T) {
		os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("git@github.com:org/repo.git", "")
		assert.Equal(t, "git@github.com:org/repo.git", result)
	})

	t.Run("no SSH agent, no token, HTTPS url -> no rewrite", func(t *testing.T) {
		os.Unsetenv("SSH_AUTH_SOCK")
		result := resolveUpstreamURL("https://github.com/org/repo.git", "")
		assert.Equal(t, "https://github.com/org/repo.git", result)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /path/to/gitspork && go test ./internal/... -run TestResolveUpstreamURL -v
```

Expected: FAIL with "undefined: resolveUpstreamURL"

- [ ] **Step 3: Add `resolveUpstreamURL` to `internal/integrate.go`**

Add this function anywhere before `cloneUpstreamForIntegrate` in `internal/integrate.go`. The function takes two parameters: the URL to potentially rewrite and the token. The caller (`CheckDrift`) is responsible for choosing which URL to pass (override or stored state) before calling this — this function only handles SSH↔HTTPS rewriting based on the environment:

```go
func resolveUpstreamURL(url string, token string) string {
	sshAgentAvailable := os.Getenv("SSH_AUTH_SOCK") != ""
	isHTTPS, _ := regexp.MatchString(`^https://`, url)
	isSSH, _ := regexp.MatchString(`^git@`, url)
	if sshAgentAvailable && token == "" && isHTTPS {
		re := regexp.MustCompile(`^https://([^/]+)/(.+)$`)
		return re.ReplaceAllString(url, "git@$1:$2")
	}
	if !sshAgentAvailable && token != "" && isSSH {
		re := regexp.MustCompile(`^git@([^:]+):(.+)$`)
		return re.ReplaceAllString(url, "https://$1/$2")
	}
	return url
}
```

- [ ] **Step 4: Wire `resolveUpstreamURL` into `cloneUpstreamForIntegrate`**

In `internal/integrate.go`, at the top of `cloneUpstreamForIntegrate`, add the URL resolution as the first line before the `authMethod` block:

```go
func cloneUpstreamForIntegrate(cloneDir string, opts *IntegrateOptions) (string, error) {
	opts.UpstreamRepoURL = resolveUpstreamURL(opts.UpstreamRepoURL, opts.UpstreamRepoToken)
	var err error
	// ... rest of function — see Task 3 for the full updated signature
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/... -run TestResolveUpstreamURL -v
```

Expected: PASS for all 6 cases.

- [ ] **Step 6: Commit**

```bash
git add internal/integrate.go internal/integrate_test.go
git commit -m "feat: add resolveUpstreamURL helper, apply in cloneUpstreamForIntegrate"
```

---

## Task 3: Add `UpstreamRepoCommit` to `IntegrateOptions`, update `cloneUpstreamForIntegrate`, save state

**Files:**
- Modify: `internal/gitspork.go`
- Modify: `internal/integrate.go`

`check-drift` needs to clone at a specific commit hash, not a branch or tag. `cloneUpstreamForIntegrate` currently only supports `UpstreamRepoVersion` (branch/tag ref). We add `UpstreamRepoCommit` to `IntegrateOptions`: when set, clone the default branch then checkout that commit. We also change `cloneUpstreamForIntegrate` to return the resolved commit hash so `Integrate` can save it to state.

- [ ] **Step 1: Add `UpstreamRepoCommit`, `ForDriftCheck`, and `PrevUpstreamCommitHash` to `IntegrateOptions` in `internal/gitspork.go`**

Replace the existing `IntegrateOptions` struct:

```go
// IntegrateOptions are options for the Integrate method
type IntegrateOptions struct {
	Logger                 *Logger
	UpstreamRepoURL        string
	UpstreamRepoVersion    string
	UpstreamRepoCommit     string
	UpstreamRepoSubpath    string
	UpstreamRepoToken      string
	DownstreamRepoPath     string
	ForceRePrompt          bool
	ForDriftCheck          bool
	PrevUpstreamCommitHash string
}
```

- [ ] **Step 2: Update `cloneUpstreamForIntegrate` to return `(string, error)` and support commit checkout**

Replace the full `cloneUpstreamForIntegrate` function in `internal/integrate.go`:

```go
func cloneUpstreamForIntegrate(cloneDir string, opts *IntegrateOptions) (string, error) {
	opts.UpstreamRepoURL = resolveUpstreamURL(opts.UpstreamRepoURL, opts.UpstreamRepoToken)
	var err error
	var authMethod transport.AuthMethod
	isHTTPsUpstreamURL, _ := regexp.MatchString("^https://.*$", opts.UpstreamRepoURL)
	if isHTTPsUpstreamURL && opts.UpstreamRepoToken != "" {
		authMethod = &http.BasicAuth{
			Username: gitSpork,
			Password: opts.UpstreamRepoToken,
		}
	} else if !isHTTPsUpstreamURL && os.Getenv("SSH_AUTH_SOCK") != "" {
		authMethod, err = ssh.NewSSHAgentAuth(gitSSHUsername)
		if err != nil {
			return "", fmt.Errorf("error setting up SSH auth method for git: %v", err)
		}
	}
	cloneOptions := &git.CloneOptions{
		URL:          opts.UpstreamRepoURL,
		SingleBranch: true,
		Progress:     os.Stdout,
	}
	if authMethod != nil {
		cloneOptions.Auth = authMethod
	}
	if opts.UpstreamRepoVersion != "" {
		refName := fmt.Sprintf("refs/heads/%s", opts.UpstreamRepoVersion)
		isTag, err := regexp.MatchString("^tags\\/", opts.UpstreamRepoVersion)
		if err != nil {
			return "", fmt.Errorf("error processing upstream gitspork repo version to use: %v", err)
		}
		if isTag {
			refName = fmt.Sprintf("refs/%s", opts.UpstreamRepoVersion)
		}
		cloneOptions.ReferenceName = plumbing.ReferenceName(refName)
	}
	if opts.UpstreamRepoCommit != "" || opts.PrevUpstreamCommitHash != "" {
		// need full history to checkout a specific commit or to walk history for delta
		cloneOptions.SingleBranch = false
	}
	repo, err := git.PlainClone(cloneDir, cloneOptions)
	if err != nil {
		return "", fmt.Errorf("error cloning upstream gitspork repo: %v", err)
	}
	if opts.UpstreamRepoCommit != "" {
		worktree, err := repo.Worktree()
		if err != nil {
			return "", fmt.Errorf("error getting worktree for commit checkout: %v", err)
		}
		if err := worktree.Checkout(&git.CheckoutOptions{
			Hash: plumbing.NewHash(opts.UpstreamRepoCommit),
		}); err != nil {
			return "", fmt.Errorf("error checking out commit %s: %v", opts.UpstreamRepoCommit, err)
		}
		return opts.UpstreamRepoCommit, nil
	}
	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("error resolving HEAD commit from upstream clone: %v", err)
	}
	return ref.Hash().String(), nil
}
```

- [ ] **Step 3: Update `Integrate` to capture the hash and save metadata to state**

Replace the full `Integrate` function in `internal/integrate.go`:

```go
func Integrate(opts *IntegrateOptions) error {
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

	cloneDir, err := os.MkdirTemp("", gitSpork)
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(cloneDir)
	// Capture original URL before cloneUpstreamForIntegrate may rewrite it via resolveUpstreamURL
	originalUpstreamURL := opts.UpstreamRepoURL
	opts.Logger.Log("cloning gitspork upstream repo %s", opts.UpstreamRepoURL)
	commitHash, err := cloneUpstreamForIntegrate(cloneDir, opts)
	if err != nil {
		return err
	}

	upstreamRootPath := filepath.Join(cloneDir, opts.UpstreamRepoSubpath)
	opts.Logger.Log("parsing the gitspork config file in the upstream repo clone at %s or %s", gitSporkConfigFileName, gitSporkConfigFileNameAlt)
	gitSporkConfig, err := getGitSporkConfig(upstreamRootPath)
	if err != nil {
		return err
	}

	if err := integrate(gitSporkConfig, upstreamRootPath, opts.DownstreamRepoPath, opts.ForceRePrompt, opts.ForDriftCheck, opts.Logger); err != nil {
		return err
	}

	// only persist upstream metadata on a real integrate (not on a drift-check re-integrate)
	// Store originalUpstreamURL — captured before cloneUpstreamForIntegrate rewrites opts.UpstreamRepoURL
	if !opts.ForDriftCheck {
		state, err := loadDownstreamState(opts.DownstreamRepoPath)
		if err != nil {
			return fmt.Errorf("error loading downstream state to save upstream metadata: %v", err)
		}
		state.LastUpstreamRepoURL = originalUpstreamURL
		state.LastUpstreamRepoSubpath = opts.UpstreamRepoSubpath
		state.LastUpstreamCommitHash = commitHash
		if err := saveDownstreamState(opts.DownstreamRepoPath, state); err != nil {
			return fmt.Errorf("error saving upstream metadata to downstream state: %v", err)
		}
	}

	return nil
}
```

- [ ] **Step 4: Run the existing integration test to verify nothing is broken**

```bash
go test ./internal/... -run TestIntegrate -v
```

Expected: PASS. Requires network access; skip with `-short` if offline.

- [ ] **Step 5: Commit**

```bash
git add internal/gitspork.go internal/integrate.go
git commit -m "feat: add UpstreamRepoCommit support; save upstream metadata to state after integrate"
```

---

## Task 4: Implement `CheckDrift`

**Files:**
- Create: `internal/check-drift.go`
- Create: `internal/check_drift_test.go`

- [ ] **Step 1: Write a failing test for the no-previous-integration error case**

Create `internal/check_drift_test.go`:

```go
package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckDrift(t *testing.T) {
	t.Run("returns error when no previous integration in state", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test-downstream")
		assert.Nil(t, err)
		defer os.RemoveAll(dir)

		err = CheckDrift(&CheckDriftOptions{
			Logger:             NewLogger(),
			DownstreamRepoPath: dir,
		})
		assert.ErrorContains(t, err, "no previous integration found")
	})

	t.Run("returns error when working tree is dirty", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "gitspork-test-downstream")
		assert.Nil(t, err)
		defer os.RemoveAll(dir)

		// init git repo and create an uncommitted file
		runCmd(t, dir, "git", "init")
		runCmd(t, dir, "git", "commit", "--allow-empty", "-m", "init")
		err = os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirty"), 0644)
		assert.Nil(t, err)

		// write state with a fake hash so we get past the first check
		state := &GitSporkDownstreamState{
			LastUpstreamRepoURL:    "https://github.com/rockholla/gitspork.git",
			LastUpstreamRepoSubpath: "docs/examples/simple/upstream",
			LastUpstreamCommitHash: "abc123",
		}
		err = saveDownstreamState(dir, state)
		assert.Nil(t, err)

		err = CheckDrift(&CheckDriftOptions{
			Logger:             NewLogger(),
			DownstreamRepoPath: dir,
		})
		assert.ErrorContains(t, err, "working tree is not clean")
	})
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	assert.Nil(t, err, "command failed: %s %v\n%s", name, args, string(out))
}
```

Add `"os/exec"` to the imports.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/... -run TestCheckDrift -v
```

Expected: FAIL with "undefined: CheckDrift"

- [ ] **Step 3: Implement `CheckDrift` in a new file**

Create `internal/check-drift.go`:

```go
package internal

import (
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

	opts.Logger.Log("re-integrating at upstream commit %s to check for drift", state.LastUpstreamCommitHash)
	if err := Integrate(&IntegrateOptions{
		Logger:             opts.Logger,
		UpstreamRepoURL:    upstreamURL,
		UpstreamRepoCommit: state.LastUpstreamCommitHash,
		UpstreamRepoSubpath: state.LastUpstreamRepoSubpath,
		UpstreamRepoToken:  opts.UpstreamRepoToken,
		DownstreamRepoPath: tempDir,
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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/... -run TestCheckDrift -v
```

Expected: PASS for both cases.

- [ ] **Step 5: Commit**

```bash
git add internal/check-drift.go internal/check_drift_test.go
git commit -m "feat: implement CheckDrift with isolation, diff, and exit-on-drift"
```

---

## Task 5: Wire `check-drift` into the CLI

**Files:**
- Create: `cmd/check-drift.go`
- Modify: `cmd/root.go`

- [ ] **Step 1: Create the cobra subcommand**

Create `cmd/check-drift.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rockholla/gitspork/internal"
)

const (
	checkDriftHelpShort string = "check if a downstream repo has drifted from its last integrated upstream state"
	checkDriftHelpLong  string = `check-drift re-runs the integration at the exact upstream commit hash used in the last
integrate run, against an isolated copy of the downstream repo, and reports any differences.

Exit codes:
  0 - no drift detected
  1 - drift detected
  non-zero (other) - error (missing state, unclean working tree, clone failure, etc.)

See https://github.com/rockholla/gitspork/docs for more info.`
)

// CheckDriftSubcommand represents the subcommand and all related functionality for 'gitspork check-drift'
type CheckDriftSubcommand struct{}

// GetCmd will return the native cobra command for the check-drift subcommand
func (cds *CheckDriftSubcommand) GetCmd() *cobra.Command {
	var downstreamRepoPath string
	var upstreamRepoURL string
	var upstreamRepoToken string
	var verbose bool

	var cmd = &cobra.Command{
		Use:   "check-drift",
		Short: checkDriftHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", checkDriftHelpShort, checkDriftHelpLong),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := internal.CheckDrift(&internal.CheckDriftOptions{
				Logger:             logger,
				DownstreamRepoPath: downstreamRepoPath,
				UpstreamRepoURL:    upstreamRepoURL,
				UpstreamRepoToken:  upstreamRepoToken,
				Verbose:            verbose,
			})
			if errors.Is(err, internal.ErrDriftDetected) {
				os.Exit(1)
			}
			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&downstreamRepoPath, "downstream-repo-path", "d", "",
		"local path to the downstream repo to check, defaults to the present working directory")
	cmd.PersistentFlags().StringVarP(&upstreamRepoURL, "upstream-repo-url", "u", "",
		"override the upstream repo URL stored in state (useful when SSH/HTTPS auto-rewrite is insufficient)")
	cmd.PersistentFlags().StringVarP(&upstreamRepoToken, "upstream-repo-token", "t", "",
		"if using an HTTPS git repo URL for the upstream, this is the token to auth")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false,
		"print full git diff output when drift is detected")

	return cmd
}
```

- [ ] **Step 2: Register the subcommand in `cmd/root.go`**

In `cmd/root.go`, inside the `init()` function, add:

```go
func init() {
	logger = internal.NewLogger()

	integrateSubcommand := &IntegrateSubcommand{}
	integrateLocalSubcommand := &IntegrateLocalSubcommand{}
	InitSubcommand := &InitSubcommand{}
	checkDriftSubcommand := &CheckDriftSubcommand{}

	rootCmd.AddCommand(integrateSubcommand.GetCmd())
	rootCmd.AddCommand(integrateLocalSubcommand.GetCmd())
	rootCmd.AddCommand(InitSubcommand.GetCmd())
	rootCmd.AddCommand(checkDriftSubcommand.GetCmd())
}
```

- [ ] **Step 3: Build and verify the command appears**

```bash
go build ./... && go run main.go check-drift --help
```

Expected output includes the short description and all four flags.

- [ ] **Step 4: Commit**

```bash
git add cmd/check-drift.go cmd/root.go
git commit -m "feat: add check-drift CLI subcommand"
```

---

## Task 6: Manual smoke test

- [ ] **Step 1: Run a fresh integration to populate state**

```bash
make ensure-local-test-downstream
make dev-test-integrate
```

Expected: integration completes, `.gitspork/downstream-state.json` in `/tmp/gitspork-downstream` now contains `last_upstream_repo_url`, `last_upstream_repo_subpath`, and `last_upstream_commit_hash`.

```bash
cat /tmp/gitspork-downstream/.gitspork/downstream-state.json
```

- [ ] **Step 2: Run `check-drift` — expect no drift**

```bash
cd /tmp/gitspork-downstream && git init && git add -A && git commit -m "init" 2>/dev/null || true
go run /path/to/gitspork/main.go check-drift --downstream-repo-path /tmp/gitspork-downstream
echo "exit code: $?"
```

Expected: "no drift detected", exit code 0.

- [ ] **Step 3: Introduce drift and run `check-drift` — expect drift detected**

```bash
# Modify an upstream-owned file in the downstream
echo "drift" >> /tmp/gitspork-downstream/upstream-owned.txt
cd /tmp/gitspork-downstream && git add -A && git commit -m "introduce drift"
go run /path/to/gitspork/main.go check-drift --downstream-repo-path /tmp/gitspork-downstream --verbose
echo "exit code: $?"
```

Expected: summary listing changed file(s), full diff printed (verbose), exit code 1.

- [ ] **Step 4: Run full test suite**

```bash
go test ./... -v
```

Expected: all tests pass.

- [ ] **Step 5: Final commit if any fixups were needed**

```bash
git add -A
git commit -m "fix: smoke test fixups for check-drift"
```

Only commit if there were actual changes. Skip if clean.
