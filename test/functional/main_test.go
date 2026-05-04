//go:build functional || functional_docker

package functional

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// activeRunner is set in TestMain and used by resolveRunner in all tests.
var activeRunner Runner

const dockerImageTag = "gitspork:functional-test"

func TestMain(m *testing.M) {
	// This file lives at test/functional/; the repo root is two levels up.
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		panic("cannot resolve repo root: " + err.Error())
	}

	if isFunctionalDocker() {
		buildDockerImageForTests(repoRoot)
		// DockerRunner is constructed per-test via resolveRunner with scenario-specific dirs.
		activeRunner = nil
	} else {
		binaryPath := buildBinary(repoRoot)
		activeRunner = &BinaryRunner{BinaryPath: binaryPath}
	}

	os.Exit(m.Run())
}

func buildBinary(repoRoot string) string {
	// Use a unique temp dir per run to avoid collisions when multiple functional
	// test runs execute concurrently on the same machine (e.g. parallel CI jobs).
	dir, err := os.MkdirTemp("", "gitspork-functional-")
	if err != nil {
		panic("cannot create temp dir for binary: " + err.Error())
	}
	out := filepath.Join(dir, "gitspork")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = repoRoot
	if b, err := cmd.CombinedOutput(); err != nil {
		panic("go build failed:\n" + string(b))
	}
	return out
}

func buildDockerImageForTests(repoRoot string) {
	// Build the linux/amd64 binary first, then use the release Dockerfile
	// (which expects a pre-built binary named "gitspork" in the build context).
	fmt.Println("=== docker test setup: compiling linux/amd64 binary...")
	dir, err := os.MkdirTemp("", "gitspork-docker-build-")
	if err != nil {
		panic("cannot create temp dir for docker build context: " + err.Error())
	}
	binaryPath := filepath.Join(dir, "gitspork")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if b, err := buildCmd.CombinedOutput(); err != nil {
		panic("go build (linux/amd64) failed:\n" + string(b))
	}

	dockerfileSource := filepath.Join(repoRoot, "Dockerfile")
	dockerfileDest := filepath.Join(dir, "Dockerfile")
	dockerfileContent, err := os.ReadFile(dockerfileSource)
	if err != nil {
		panic("cannot read Dockerfile: " + err.Error())
	}
	if err := os.WriteFile(dockerfileDest, dockerfileContent, 0644); err != nil {
		panic("cannot copy Dockerfile to build context: " + err.Error())
	}

	fmt.Printf("=== docker test setup: building image %s...\n", dockerImageTag)
	cmd := exec.Command("docker", "build", "-t", dockerImageTag, dir)
	if b, err := cmd.CombinedOutput(); err != nil {
		panic("docker build failed:\n" + string(b))
	}
	fmt.Println("=== docker test setup: image ready, running tests...")
}

// isFunctionalDocker returns true when compiled with -tags functional_docker.
// It reads isDockerBuild which is defined in harness_docker.go (true) or harness_native.go (false).
func isFunctionalDocker() bool {
	return isDockerBuild
}

// resolveRunner returns the Runner to use for a test.
// For native builds, returns the pre-built BinaryRunner from activeRunner.
// For docker builds, constructs a DockerRunner with the scenario's dirs.
// upstreamDir and downstreamDir may be empty when a scenario only uses one dir.
func resolveRunner(t *testing.T, upstreamDir, downstreamDir string) Runner {
	t.Helper()
	if activeRunner != nil {
		return activeRunner
	}
	return &DockerRunner{
		ImageTag:      dockerImageTag,
		UpstreamDir:   upstreamDir,
		DownstreamDir: downstreamDir,
	}
}
