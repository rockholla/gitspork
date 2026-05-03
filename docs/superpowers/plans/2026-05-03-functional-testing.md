# Functional Testing Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a hermetic, scenario-driven functional test suite that compiles the binary and runs it against constructed git repos, with a parallel Docker variant that runs identical scenarios against the containerized image.

**Architecture:** A `test/functional/` package gated behind build tags (`functional` / `functional_docker`). A `Runner` interface abstracts binary-vs-container execution; `TestMain` builds the artifact once and stores it in a package-level var; each test constructs fresh upstream/downstream repos using go-git, runs the binary via `exec.Command` or `docker run`, and asserts on filesystem state and exit codes.

**Tech Stack:** Go 1.25, `go-git/go-git/v6`, `stretchr/testify`, `exec.Command`, Docker CLI (for container variant), build tags `functional` and `functional_docker`.

---

## File Structure

| File | Role |
|------|------|
| `test/functional/harness.go` | `Runner` interface, `BinaryRunner`, shared repo-construction helpers, git helpers |
| `test/functional/harness_docker.go` | `DockerRunner` — mounts host dirs as `/upstream` `/downstream`, rewrites path args |
| `test/functional/main_test.go` | `TestMain`: build binary or docker image once, store in pkg var |
| `test/functional/integrate_test.go` | integrate scenarios (fresh, re-integrate, delta rename/delete, templated) |
| `test/functional/check_drift_test.go` | check-drift scenarios (no drift, drift detected) |
| `test/functional/mv_rm_test.go` | mv and rm scenarios |
| `test/functional/init_test.go` | init scenario |
| `Makefile` | add `test-functional` and `test-functional-docker` targets |

---

### Task 1: Harness — Runner interface and BinaryRunner

**Files:**
- Create: `test/functional/harness.go`

- [ ] **Step 1: Create `test/functional/harness.go`**

```go
//go:build functional || functional_docker

package functional

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

// Runner abstracts how a gitspork command is executed (native binary vs container).
type Runner interface {
	// Run executes gitspork with the given args. dir is the host working directory
	// (used for mv/rm which run from within the repo). Returns stdout+stderr combined,
	// and the exit code.
	Run(t *testing.T, args []string, dir string) (output string, exitCode int)
}

// Result holds the output and exit code from a Runner.Run call.
type Result struct {
	Output   string
	ExitCode int
}

// BinaryRunner runs the locally compiled gitspork binary.
type BinaryRunner struct {
	BinaryPath string
}

func (r *BinaryRunner) Run(t *testing.T, args []string, dir string) (string, int) {
	t.Helper()
	cmd := exec.Command(r.BinaryPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Logf("runner exec error: %v", err)
			code = -1
		}
	}
	return string(out), code
}

// --- repo construction helpers ---

var testSig = &object.Signature{
	Name:  "gitspork-test",
	Email: "gitspork-test@localhost",
	When:  time.Now(),
}

// NewUpstreamRepo creates a temp git repo populated with files and an optional
// .gitspork.yml, commits everything, and returns the repo path.
// files maps relative path -> content. If gitsporkYML is non-empty it is written
// as .gitspork.yml before the initial commit.
func NewUpstreamRepo(t *testing.T, files map[string]string, gitsporkYML string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	if gitsporkYML != "" {
		files[".gitspork.yml"] = gitsporkYML
	}
	WriteFiles(t, dir, files)
	CommitAll(t, repo, dir, "initial upstream commit")
	return dir
}

// NewDownstreamRepo creates a temp dir with git init and returns its path.
func NewDownstreamRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false,
		gogit.WithDefaultBranch(plumbing.NewBranchReferenceName("main")),
	)
	require.NoError(t, err)
	return dir
}

// WriteFiles writes a map of relative-path -> content into dir, creating subdirectories as needed.
func WriteFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}
}

// CommitAll stages everything in dir and creates a commit on repo.
func CommitAll(t *testing.T, repo *gogit.Repository, dir, message string) {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&gogit.AddOptions{All: true}))
	_, err = wt.Commit(message, &gogit.CommitOptions{Author: testSig})
	require.NoError(t, err)
}

// OpenRepo opens an existing git repo at dir.
func OpenRepo(t *testing.T, dir string) *gogit.Repository {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	return repo
}

// ReadFile reads a file inside dir and returns its content. Fails the test if absent.
func ReadFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	require.NoError(t, err, "expected file %s to exist in %s", rel, dir)
	return string(b)
}

// AssertFileAbsent fails the test if the file exists.
func AssertFileAbsent(t *testing.T, dir, rel string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, rel))
	require.True(t, os.IsNotExist(err), "expected file %s to be absent in %s", rel, dir)
}

// AssertFileContains fails if the file doesn't contain substr.
func AssertFileContains(t *testing.T, dir, rel, substr string) {
	t.Helper()
	content := ReadFile(t, dir, rel)
	require.True(t, strings.Contains(content, substr),
		"expected %s to contain %q, got:\n%s", rel, substr, content)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build -tags functional ./test/functional/...
```

Expected: no errors (package has no test functions yet, just helpers).

- [ ] **Step 3: Commit**

```bash
git add test/functional/harness.go
git commit -m "feat(functional-tests): add Runner interface, BinaryRunner, and repo helpers"
```

---

### Task 2: Harness — DockerRunner

**Files:**
- Create: `test/functional/harness_docker.go`

The DockerRunner mounts the upstream and downstream host dirs into the container at `/upstream` and `/downstream`. It rewrites any arg that is a host path prefix to the corresponding container path. For `mv`/`rm` (which run with no path flag, from within the upstream dir), it sets the working directory to `/upstream` inside the container via `-w`.

- [ ] **Step 1: Create `test/functional/harness_docker.go`**

```go
//go:build functional_docker

package functional

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// DockerRunner runs gitspork commands inside the gitspork:functional-test Docker image.
// It mounts upstreamDir -> /upstream and downstreamDir -> /downstream.
// Path args that begin with upstreamDir or downstreamDir are rewritten to container paths.
type DockerRunner struct {
	ImageTag      string
	UpstreamDir   string
	DownstreamDir string
}

// NewDockerRunner constructs a DockerRunner for a specific test scenario.
// upstreamDir and downstreamDir are host paths that will be mounted into the container.
func NewDockerRunner(t *testing.T, imageTag, upstreamDir, downstreamDir string) *DockerRunner {
	t.Helper()
	return &DockerRunner{
		ImageTag:      imageTag,
		UpstreamDir:   upstreamDir,
		DownstreamDir: downstreamDir,
	}
}

func (r *DockerRunner) Run(t *testing.T, args []string, dir string) (string, int) {
	t.Helper()

	// Rewrite host path args to container paths.
	rewritten := make([]string, len(args))
	for i, a := range args {
		if r.UpstreamDir != "" && strings.HasPrefix(a, r.UpstreamDir) {
			a = "/upstream" + strings.TrimPrefix(a, r.UpstreamDir)
		} else if r.DownstreamDir != "" && strings.HasPrefix(a, r.DownstreamDir) {
			a = "/downstream" + strings.TrimPrefix(a, r.DownstreamDir)
		}
		rewritten[i] = a
	}

	// Determine working dir inside container.
	containerDir := "/"
	if r.UpstreamDir != "" && dir == r.UpstreamDir {
		containerDir = "/upstream"
	} else if r.DownstreamDir != "" && dir == r.DownstreamDir {
		containerDir = "/downstream"
	}

	dockerArgs := []string{"run", "--rm"}
	if r.UpstreamDir != "" {
		dockerArgs = append(dockerArgs, "-v", r.UpstreamDir+":/upstream")
	}
	if r.DownstreamDir != "" {
		dockerArgs = append(dockerArgs, "-v", r.DownstreamDir+":/downstream")
	}
	dockerArgs = append(dockerArgs, "-w", containerDir)
	dockerArgs = append(dockerArgs, r.ImageTag)
	dockerArgs = append(dockerArgs, rewritten...)

	cmd := exec.Command("docker", dockerArgs...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Logf("docker runner exec error: %v\noutput: %s", err, out)
			code = -1
		}
	}
	return string(out), code
}

// BuildDockerImage builds the gitspork Docker image with the given tag.
// Called once from TestMain when running under the functional_docker build tag.
func BuildDockerImage(t *testing.T, tag, contextDir string) {
	t.Helper()
	cmd := exec.Command("docker", "build", "-t", tag, contextDir)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "docker build failed:\n%s", string(out))
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build -tags functional_docker ./test/functional/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add test/functional/harness_docker.go
git commit -m "feat(functional-tests): add DockerRunner with volume-mount path rewriting"
```

---

### Task 3: TestMain — build artifact once per test run

**Files:**
- Create: `test/functional/main_test.go`

`TestMain` compiles the binary (or builds the Docker image) once for the entire test run and stores the result in a package-level variable that all test functions read. It uses `go build -o` to a `t.TempDir()`-equivalent temp file.

- [ ] **Step 1: Create `test/functional/main_test.go`**

```go
//go:build functional || functional_docker

package functional

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// activeRunner is set in TestMain and shared by all tests.
var activeRunner Runner

const dockerImageTag = "gitspork:functional-test"

func TestMain(m *testing.M) {
	// Determine the repo root: this file lives at test/functional/, so go up two levels.
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		panic("cannot resolve repo root: " + err.Error())
	}

	if isFunctionalDocker() {
		buildDockerImageForTests(repoRoot)
		// DockerRunner is constructed per-test with scenario-specific dirs;
		// activeRunner is a sentinel here — tests that need Docker construct their own DockerRunner.
		activeRunner = nil
	} else {
		binaryPath := buildBinary(repoRoot)
		activeRunner = &BinaryRunner{BinaryPath: binaryPath}
	}

	os.Exit(m.Run())
}

func buildBinary(repoRoot string) string {
	out := filepath.Join(os.TempDir(), "gitspork-functional-test")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = repoRoot
	if b, err := cmd.CombinedOutput(); err != nil {
		panic("go build failed:\n" + string(b))
	}
	return out
}

func buildDockerImageForTests(repoRoot string) {
	cmd := exec.Command("docker", "build", "-t", dockerImageTag, repoRoot)
	if b, err := cmd.CombinedOutput(); err != nil {
		panic("docker build failed:\n" + string(b))
	}
}

// isFunctionalDocker returns true when the functional_docker build tag is active.
// We detect this via a build-tag-gated constant defined in harness_docker.go.
func isFunctionalDocker() bool {
	return isDockerBuild
}
```

Add the constant that `isFunctionalDocker` reads to `harness_docker.go` — append this to the bottom of that file:

```go
// isDockerBuild is true when compiled with -tags functional_docker.
const isDockerBuild = true
```

And add the inverse to a new file `test/functional/harness_native.go`:

```go
//go:build functional

package functional

const isDockerBuild = false
```

- [ ] **Step 2: Verify both tags compile**

```bash
go build -tags functional ./test/functional/... && \
go build -tags functional_docker ./test/functional/...
```

Expected: no errors for either tag.

- [ ] **Step 3: Commit**

```bash
git add test/functional/main_test.go test/functional/harness_native.go
git commit -m "feat(functional-tests): TestMain builds binary or Docker image once per run"
```

---

### Task 4: Makefile targets

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add targets to Makefile**

Append after the existing `test` target:

```makefile
.PHONY: test-functional
test-functional:
	@go test -tags functional -timeout 120s -v ./test/functional/...

.PHONY: test-functional-docker
test-functional-docker:
	@go test -tags functional_docker -timeout 300s -v ./test/functional/...

.PHONY: test-all
test-all: test test-functional
```

- [ ] **Step 2: Verify make targets are recognized**

```bash
make -n test-functional
make -n test-functional-docker
make -n test-all
```

Expected: each prints the command it would run (dry-run, no error).

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat(functional-tests): add test-functional, test-functional-docker, test-all make targets"
```

---

### Task 5: init scenario

**Files:**
- Create: `test/functional/init_test.go`

This is the simplest scenario — run `gitspork init --path <dir>` and assert `.gitspork.yml` was created with the header comment.

- [ ] **Step 1: Create `test/functional/init_test.go`**

```go
//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	runner := resolveRunner(t, "", "")

	dir := t.TempDir()
	out, code := runner.Run(t, []string{"init", "--path", dir}, dir)
	require.Equal(t, 0, code, "init exited non-zero:\n%s", out)

	AssertFileContains(t, dir, ".gitspork.yml", "# For full docs")
	content := ReadFile(t, dir, ".gitspork.yml")
	assert.Contains(t, content, "upstream_owned")
}
```

Add `resolveRunner` to `harness.go` — it returns `activeRunner` for binary, and a `DockerRunner` for docker (mapping dir to the right mount). Append to `harness.go`:

```go
// resolveRunner returns the Runner to use for a test.
// upstreamDir and downstreamDir may be empty when the scenario only uses one dir.
func resolveRunner(t *testing.T, upstreamDir, downstreamDir string) Runner {
	t.Helper()
	if activeRunner != nil {
		return activeRunner
	}
	// Docker build tag: construct a DockerRunner for this scenario's dirs.
	return &DockerRunner{
		ImageTag:      dockerImageTag,
		UpstreamDir:   upstreamDir,
		DownstreamDir: downstreamDir,
	}
}
```

Note: `resolveRunner` references `activeRunner` and `dockerImageTag` from `main_test.go`. Both are package-level vars, so this compiles fine.

For `init`, there's no upstream or downstream — the dir being initialized is neither. The DockerRunner will mount it as `/upstream` since `upstreamDir` is passed as `dir`. Update `resolveRunner` call in `TestInit`:

```go
runner := resolveRunner(t, dir, "")
```

- [ ] **Step 2: Run the init test (binary)**

```bash
go test -tags functional -run TestInit -v ./test/functional/...
```

Expected: PASS.

- [ ] **Step 3: Run the init test (docker)**

```bash
go test -tags functional_docker -run TestInit -v ./test/functional/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add test/functional/init_test.go
git commit -m "test(functional): add init scenario"
```

---

### Task 6: integrate — fresh and re-integrate scenarios

**Files:**
- Create: `test/functional/integrate_test.go`

The upstream for these tests mirrors the `docs/examples/simple/upstream` layout. The `.gitspork.yml` uses `upstream_owned`, `downstream_owned`, `shared_ownership.merged`, `shared_ownership.structured`, and `templated` sections. No migrations in these scenarios (covered in Task 8).

The `json_data_path` input for the templated file must be present in the downstream before the first integrate — exactly how the Makefile `ensure-local-test-downstream` copies `templated-json-input-data.json`.

- [ ] **Step 1: Create `test/functional/integrate_test.go`** with fresh integrate and re-integrate subtests

```go
//go:build functional || functional_docker

package functional

import (
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NOTE: integrate_test.go does not import "path/filepath" — remove it from the import block.
// The file:// URL is constructed inline as "file://" + upstreamDir.

const simpleGitsporkYML = `version: dev
upstream_owned:
- upstream-owned/**
- upstream-owned.mk
downstream_owned:
- downstream-owned.md
shared_ownership:
  merged:
  - Makefile
  structured:
    prefer_upstream:
    - config.yaml
    prefer_downstream:
    - info.json
templated:
- template: .gitspork-templates/meta.txt.go.tmpl
  destination: meta.txt
  inputs:
  - name: project_name
    json_data_path: input-data.json
  - name: project_description
    json_data_path: input-data.json
`

const metaTemplate = `Project: {{ index .Inputs "project_name" }}
Description: {{ index .Inputs "project_description" }}
`

func buildSimpleUpstream(t *testing.T) string {
	t.Helper()
	return NewUpstreamRepo(t, map[string]string{
		"upstream-owned/file.txt":                  "upstream content\n",
		"upstream-owned.mk":                        "upstream mk content\n",
		"downstream-owned.md":                      "downstream seed content\n",
		"Makefile":                                 "# upstream makefile\n",
		"config.yaml":                              "key: upstream-value\n",
		"info.json":                                `{"version":"1"}`,
		".gitspork-templates/meta.txt.go.tmpl":     metaTemplate,
	}, simpleGitsporkYML)
}

func prepDownstreamWithInputData(t *testing.T, downstreamDir string) {
	t.Helper()
	WriteFiles(t, downstreamDir, map[string]string{
		"input-data.json": `{"project_name":"my-project","project_description":"my description"}`,
	})
}

func TestIntegrate_fresh(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	out, code := runner.Run(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate exited non-zero:\n%s", out)

	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
	AssertFileContains(t, downstreamDir, "upstream-owned.mk", "upstream mk content")
	AssertFileContains(t, downstreamDir, "downstream-owned.md", "downstream seed content")
	AssertFileContains(t, downstreamDir, "meta.txt", "my-project")
	AssertFileContains(t, downstreamDir, "meta.txt", "my description")
	AssertFileContains(t, downstreamDir, ".gitspork/downstream-state.json", "last_upstream_commit_hash")
}

func TestIntegrate_reintegrate_idempotent(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	runArgs := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}

	// First integrate
	out, code := runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)

	// Commit the downstream so it has a clean state
	repo := OpenRepo(t, downstreamDir)
	CommitAll(t, repo, downstreamDir, "post-integrate baseline")

	// Second integrate — must succeed with same result
	out, code = runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
	AssertFileContains(t, downstreamDir, "meta.txt", "my-project")
}

func TestIntegrate_upstream_adds_file(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	runArgs := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}

	// First integrate
	out, code := runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	repo := OpenRepo(t, downstreamDir)
	CommitAll(t, repo, downstreamDir, "post-integrate baseline")

	// Add a new upstream-owned file and commit it upstream
	upstreamRepo := OpenRepo(t, upstreamDir)
	WriteFiles(t, upstreamDir, map[string]string{
		"upstream-owned/new-file.txt": "brand new upstream file\n",
	})
	CommitAll(t, upstreamRepo, upstreamDir, "add new upstream file")

	// Re-integrate
	out, code = runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after upstream add failed:\n%s", out)

	AssertFileContains(t, downstreamDir, "upstream-owned/new-file.txt", "brand new upstream file")
}

func TestIntegrate_downstream_owned_not_overwritten(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	runArgs := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}

	// First integrate seeds downstream-owned.md from upstream
	out, code := runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)

	// Downstream customizes the file
	WriteFiles(t, downstreamDir, map[string]string{
		"downstream-owned.md": "# downstream customization\n",
	})
	repo := OpenRepo(t, downstreamDir)
	CommitAll(t, repo, downstreamDir, "downstream customizes owned file")

	// Re-integrate must not overwrite it
	out, code = runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "re-integrate failed:\n%s", out)

	content := ReadFile(t, downstreamDir, "downstream-owned.md")
	assert.Contains(t, content, "downstream customization",
		"downstream-owned.md should not be overwritten by re-integrate")
}

func TestIntegrate_upstream_delta_rename(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	runArgs := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}

	// First integrate
	out, code := runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	repo := OpenRepo(t, downstreamDir)
	CommitAll(t, repo, downstreamDir, "post-integrate baseline")

	// Rename an upstream-owned file upstream
	upstreamRepo := OpenRepo(t, upstreamDir)
	require.NoError(t, gogit.ErrWorktreeNotProvided) // force import use
	upstreamWt, err := upstreamRepo.Worktree()
	require.NoError(t, err)
	_, err = upstreamWt.Move("upstream-owned/file.txt", "upstream-owned/renamed-file.txt")
	require.NoError(t, err)
	CommitAll(t, upstreamRepo, upstreamDir, "rename upstream file")

	// Re-integrate — delta should rename in downstream
	out, code = runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after rename failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "upstream-owned/file.txt")
	AssertFileContains(t, downstreamDir, "upstream-owned/renamed-file.txt", "upstream content")
}

func TestIntegrate_upstream_delta_delete(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	runArgs := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}

	// First integrate
	out, code := runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	repo := OpenRepo(t, downstreamDir)
	CommitAll(t, repo, downstreamDir, "post-integrate baseline")

	// Delete an upstream-owned file upstream
	upstreamRepo := OpenRepo(t, upstreamDir)
	upstreamWt, err := upstreamRepo.Worktree()
	require.NoError(t, err)
	_, err = upstreamWt.Remove("upstream-owned/file.txt")
	require.NoError(t, err)
	CommitAll(t, upstreamRepo, upstreamDir, "delete upstream file")

	// Re-integrate — delta should remove from downstream
	out, code = runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after delete failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "upstream-owned/file.txt")
}

// (end of integrate_test.go)
```

Note: the `gogit.ErrWorktreeNotProvided` import trick will cause a compile error — remove that line. The correct import is just using `upstreamWt.Move`. The `resolveFileURL` helper at the bottom is used for the file:// URL so paths work on all platforms.

- [ ] **Step 2: Fix the spurious `ErrWorktreeNotProvided` reference** — remove this line from `TestIntegrate_upstream_delta_rename`:

```go
require.NoError(t, gogit.ErrWorktreeNotProvided) // force import use
```

The `gogit` import is used by `gogit.PlainInit` in `harness.go`; within this file the import is needed for `(*gogit.Worktree).Move`. The corrected test:

```go
func TestIntegrate_upstream_delta_rename(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	runArgs := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}

	out, code := runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "first integrate failed:\n%s", out)
	repo := OpenRepo(t, downstreamDir)
	CommitAll(t, repo, downstreamDir, "post-integrate baseline")

	upstreamRepo := OpenRepo(t, upstreamDir)
	upstreamWt, err := upstreamRepo.Worktree()
	require.NoError(t, err)
	_, err = upstreamWt.Move("upstream-owned/file.txt", "upstream-owned/renamed-file.txt")
	require.NoError(t, err)
	CommitAll(t, upstreamRepo, upstreamDir, "rename upstream file")

	out, code = runner.Run(t, runArgs, downstreamDir)
	require.Equal(t, 0, code, "re-integrate after rename failed:\n%s", out)

	AssertFileAbsent(t, downstreamDir, "upstream-owned/file.txt")
	AssertFileContains(t, downstreamDir, "upstream-owned/renamed-file.txt", "upstream content")
}
```

- [ ] **Step 3: Verify compile**

```bash
go build -tags functional ./test/functional/...
```

Expected: no errors.

- [ ] **Step 4: Run integrate tests (binary)**

```bash
go test -tags functional -run TestIntegrate -v ./test/functional/...
```

Expected: all PASS.

- [ ] **Step 5: Run integrate tests (docker)**

```bash
go test -tags functional_docker -run TestIntegrate -v ./test/functional/...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add test/functional/integrate_test.go
git commit -m "test(functional): add integrate scenarios (fresh, re-integrate, delta rename/delete, downstream-owned)"
```

---

### Task 7: check-drift scenarios

**Files:**
- Create: `test/functional/check_drift_test.go`

Both scenarios require a post-integrate downstream that has been committed (clean working tree).

- [ ] **Step 1: Create `test/functional/check_drift_test.go`**

```go
//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func integrateForDrift(t *testing.T, runner Runner, upstreamDir, downstreamDir string) {
	t.Helper()
	out, code := runner.Run(t, []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "integrate for drift setup failed:\n%s", out)

	repo := OpenRepo(t, downstreamDir)
	CommitAll(t, repo, downstreamDir, "post-integrate baseline")
}

func TestCheckDrift_no_drift(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	integrateForDrift(t, runner, upstreamDir, downstreamDir)

	// Re-copy input data (drift check re-runs integrate which needs it)
	prepDownstreamWithInputData(t, downstreamDir)

	out, code := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
		"--upstream-repo-url", "file://" + upstreamDir,
	}, downstreamDir)
	require.Equal(t, 0, code, "expected no drift (exit 0):\n%s", out)
}

func TestCheckDrift_drift_detected(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	integrateForDrift(t, runner, upstreamDir, downstreamDir)

	// Modify an upstream-owned file in the downstream to introduce drift
	WriteFiles(t, downstreamDir, map[string]string{
		"upstream-owned/file.txt": "drifted content\n",
	})
	repo := OpenRepo(t, downstreamDir)
	CommitAll(t, repo, downstreamDir, "introduce drift")

	// Re-copy input data before check-drift
	prepDownstreamWithInputData(t, downstreamDir)

	out, code := runner.Run(t, []string{
		"check-drift",
		"--downstream-repo-path", downstreamDir,
		"--upstream-repo-url", "file://" + upstreamDir,
		"--verbose",
	}, downstreamDir)
	require.Equal(t, 1, code, "expected drift detected (exit 1):\n%s", out)
}
```

- [ ] **Step 2: Run check-drift tests (binary)**

```bash
go test -tags functional -run TestCheckDrift -v ./test/functional/...
```

Expected: both PASS.

- [ ] **Step 3: Run check-drift tests (docker)**

```bash
go test -tags functional_docker -run TestCheckDrift -v ./test/functional/...
```

Expected: both PASS.

- [ ] **Step 4: Commit**

```bash
git add test/functional/check_drift_test.go
git commit -m "test(functional): add check-drift scenarios (no drift, drift detected)"
```

---

### Task 8: mv and rm scenarios

**Files:**
- Create: `test/functional/mv_rm_test.go`

`mv` and `rm` run from inside the upstream repo (no path flags). For Docker, the working dir is `/upstream`. The test constructs a git-init'd upstream repo with a `.gitspork.yml`, runs the command, then reads `.gitspork.yml` to verify it was updated and checks the git index to confirm staging.

- [ ] **Step 1: Create `test/functional/mv_rm_test.go`**

```go
//go:build functional || functional_docker

package functional

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mvRmGitsporkYML = `version: dev
upstream_owned:
- docs/old.md
- docs/keep.md
`

// gitIndexContains returns true if path appears in the git index of dir.
func gitIndexContains(t *testing.T, dir, path string) bool {
	t.Helper()
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	return assert.ObjectsExportedFieldsAreEqual != nil && // dummy to use assert import
		containsLine(string(out), path)
}

func containsLine(output, target string) bool {
	for _, line := range splitLines(output) {
		if line == target {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	cur := ""
	for _, c := range s {
		if c == '\n' {
			if cur != "" {
				lines = append(lines, cur)
			}
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func TestMv_updates_config_and_stages(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)

	runner := resolveRunner(t, upstreamDir, "")

	out, code := runner.Run(t, []string{"mv", "docs/old.md", "docs/new.md"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork mv failed:\n%s", out)

	// File was moved
	AssertFileAbsent(t, upstreamDir, "docs/old.md")
	AssertFileContains(t, upstreamDir, "docs/new.md", "old doc")

	// .gitspork.yml was updated
	AssertFileContains(t, upstreamDir, ".gitspork.yml", "docs/new.md")
	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.NotContains(t, cfg, "docs/old.md")

	// Both docs/new.md and .gitspork.yml are staged
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = upstreamDir
	staged, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(staged), ".gitspork.yml")
	assert.Contains(t, string(staged), "docs/new.md")
}

func TestRm_updates_config_and_stages(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)

	runner := resolveRunner(t, upstreamDir, "")

	out, code := runner.Run(t, []string{"rm", "docs/old.md"}, upstreamDir)
	require.Equal(t, 0, code, "gitspork rm failed:\n%s", out)

	// File was removed
	AssertFileAbsent(t, upstreamDir, "docs/old.md")

	// .gitspork.yml was updated
	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.NotContains(t, cfg, "docs/old.md")
	assert.Contains(t, cfg, "docs/keep.md")

	// .gitspork.yml is staged
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = upstreamDir
	staged, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, string(staged), ".gitspork.yml")
}
```

Note: `gitIndexContains` is defined but the final implementation just uses `exec.Command("git", "diff", "--cached", ...)` inline in the tests for clarity. Remove the `gitIndexContains` function and the `assert.ObjectsExportedFieldsAreEqual != nil` dummy expression — it's a placeholder that won't compile. Use the inline approach shown in each test instead. Final `mv_rm_test.go` should not contain `gitIndexContains`.

- [ ] **Step 2: Remove the dead helpers** — the file as written above has a non-compiling dummy expression. The clean version omits `gitIndexContains`, `containsLine`, and `splitLines` entirely (they were pre-draft scaffolding). The two test functions `TestMv_updates_config_and_stages` and `TestRm_updates_config_and_stages` are correct and self-contained using inline `exec.Command`.

- [ ] **Step 3: Run mv/rm tests (binary)**

```bash
go test -tags functional -run "TestMv|TestRm" -v ./test/functional/...
```

Expected: both PASS.

- [ ] **Step 4: Run mv/rm tests (docker)**

```bash
go test -tags functional_docker -run "TestMv|TestRm" -v ./test/functional/...
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add test/functional/mv_rm_test.go
git commit -m "test(functional): add mv and rm scenarios"
```

---

### Task 9: Full suite smoke-run and Makefile validation

- [ ] **Step 1: Run full functional suite (binary)**

```bash
make test-functional
```

Expected: all tests PASS, output shows scenario names.

- [ ] **Step 2: Run full functional suite (docker)**

```bash
make test-functional-docker
```

Expected: all tests PASS.

- [ ] **Step 3: Run combined target**

```bash
make test-all
```

Expected: unit tests and functional tests all PASS.

- [ ] **Step 4: Commit if any fixups were needed during smoke-run, otherwise done**

```bash
git add -p
git commit -m "fix(functional-tests): smoke-run fixups"
```
