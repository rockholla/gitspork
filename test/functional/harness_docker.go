//go:build functional_docker

package functional

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// DockerRunner runs gitspork commands inside a Docker image specified by ImageTag.
// It mounts UpstreamDir -> /upstream and DownstreamDir -> /downstream.
type DockerRunner struct {
	ImageTag      string
	UpstreamDir   string
	DownstreamDir string
}

func (r *DockerRunner) Run(t *testing.T, args []string, dir string) (string, int) {
	t.Helper()
	if r.ImageTag == "" {
		t.Fatal("DockerRunner: ImageTag must be set")
	}

	// Rewrite host path args to container paths, handling both bare paths and
	// file:// URLs (e.g. --upstream-repo-url file:///host/tmp/... becomes
	// file:///upstream since that path is volume-mounted at /upstream).
	rewritten := make([]string, len(args))
	for i, a := range args {
		switch {
		case r.UpstreamDir != "" && strings.HasPrefix(a, r.UpstreamDir):
			a = "/upstream" + strings.TrimPrefix(a, r.UpstreamDir)
		case r.UpstreamDir != "" && strings.HasPrefix(a, "file://"+r.UpstreamDir):
			a = "file:///upstream" + strings.TrimPrefix(a, "file://"+r.UpstreamDir)
		case r.DownstreamDir != "" && strings.HasPrefix(a, r.DownstreamDir):
			a = "/downstream" + strings.TrimPrefix(a, r.DownstreamDir)
		case r.DownstreamDir != "" && strings.HasPrefix(a, "file://"+r.DownstreamDir):
			a = "file:///downstream" + strings.TrimPrefix(a, "file://"+r.DownstreamDir)
		}
		rewritten[i] = a
	}

	// Determine working dir inside container.
	containerDir := "/"
	if r.UpstreamDir != "" && strings.HasPrefix(dir, r.UpstreamDir) {
		containerDir = "/upstream" + strings.TrimPrefix(dir, r.UpstreamDir)
	} else if r.DownstreamDir != "" && strings.HasPrefix(dir, r.DownstreamDir) {
		containerDir = "/downstream" + strings.TrimPrefix(dir, r.DownstreamDir)
	}

	dockerArgs := []string{"run", "--rm"}
	// Run as the current host user so files written by the container into mounted
	// volumes are owned by the same uid/gid as the test process. Without this,
	// root-owned files are left in t.TempDir() and cleanup fails with "permission denied".
	dockerArgs = append(dockerArgs,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
	)
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
			t.Fatalf("docker runner: failed to launch docker: %v\noutput: %s", err, out)
		}
	}
	return string(out), code
}

// isDockerBuild is read by TestMain (main_test.go) to decide whether to build
// the Docker image instead of the native binary.
const isDockerBuild = true
