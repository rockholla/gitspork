//go:build functional || functional_docker

package functional

import (
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
	cmd := exec.Command("docker", "build", "-t", dockerImageTag, repoRoot)
	if b, err := cmd.CombinedOutput(); err != nil {
		panic("docker build failed:\n" + string(b))
	}
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
