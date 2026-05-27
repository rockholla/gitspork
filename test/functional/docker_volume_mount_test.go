//go:build functional_docker

package functional

// Tests in this file exercise the real-world Docker volume-mount usage pattern:
//
//   docker run --rm -v "$(pwd)":/downstream --workdir /downstream \
//     -v <ssh-socket>:<ssh-socket> -e SSH_AUTH_SOCK=<ssh-socket> \
//     -v <ssh-dir>:/root/.ssh -e SSH_KNOWN_HOSTS=/root/.ssh/known_hosts \
//     gitspork <command>
//
// Critically, these tests do NOT pass --user, so the container runs as root
// while the mounted files are owned by the host user. This is the scenario
// that triggered the safe.directory and SSH_KNOWN_HOSTS bugs. The runner in
// harness_docker.go always passes --user and injects GIT_CONFIG_* env vars,
// so it cannot catch these cases — hence the direct docker invocations here.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runAsRoot executes a gitspork command inside the test Docker image with:
//   - the upstream dir volume-mounted at /upstream (when non-empty)
//   - the downstream dir volume-mounted at /downstream (when non-empty)
//   - a synthetic known_hosts file volume-mounted at /root/.ssh/known_hosts
//   - SSH_KNOWN_HOSTS=/root/.ssh/known_hosts
//   - NO --user flag (container runs as root; host files are non-root-owned)
//   - NO GIT_CONFIG_* env var workaround
//
// workdir is the container path to set as working directory.
func runAsRoot(t *testing.T, upstreamDir, downstreamDir, workdir string, args []string) (string, int) {
	t.Helper()
	return runAsRootWithSSH(t, upstreamDir, downstreamDir, workdir, args, true)
}

// runAsRootNoKnownHosts is like runAsRoot but sets SSH_KNOWN_HOSTS to a
// nonexistent path, simulating the case where the caller sets the env var
// but skips the ssh-keyscan step (e.g. when a token is used instead of SSH).
func runAsRootNoKnownHosts(t *testing.T, upstreamDir, downstreamDir, workdir string, args []string) (string, int) {
	t.Helper()
	return runAsRootWithSSH(t, upstreamDir, downstreamDir, workdir, args, false)
}

func runAsRootWithSSH(t *testing.T, upstreamDir, downstreamDir, workdir string, args []string, createKnownHosts bool) (string, int) {
	t.Helper()

	sshDir := t.TempDir()
	if createKnownHosts {
		knownHostsPath := filepath.Join(sshDir, "known_hosts")
		require.NoError(t, os.WriteFile(knownHostsPath, []byte(""), 0600))
	}

	dockerArgs := []string{"run", "--rm"}

	// Volume mounts and path rewrites for args.
	rewrite := func(a string) string {
		if upstreamDir != "" {
			a = strings.ReplaceAll(a, upstreamDir, "/upstream")
			a = strings.ReplaceAll(a, "file://"+upstreamDir, "file:///upstream")
		}
		if downstreamDir != "" {
			a = strings.ReplaceAll(a, downstreamDir, "/downstream")
		}
		return a
	}

	if upstreamDir != "" {
		dockerArgs = append(dockerArgs, "-v", upstreamDir+":/upstream")
	}
	if downstreamDir != "" {
		dockerArgs = append(dockerArgs, "-v", downstreamDir+":/downstream")
	}
	dockerArgs = append(dockerArgs,
		"-v", sshDir+":/root/.ssh",
		"-e", "SSH_KNOWN_HOSTS=/root/.ssh/known_hosts",
		"-w", workdir,
	)
	dockerArgs = append(dockerArgs, dockerImageTag)
	for _, a := range args {
		dockerArgs = append(dockerArgs, rewrite(a))
	}

	// Restore ownership of any volume-mounted dirs to the current user before
	// t.TempDir cleanup runs — the container writes as root, leaving files the
	// host process cannot remove.
	uid := os.Getuid()
	gid := os.Getgid()
	for _, dir := range []string{upstreamDir, downstreamDir, sshDir} {
		if dir == "" {
			continue
		}
		d := dir
		t.Cleanup(func() {
			_ = exec.Command("docker", "run", "--rm",
				"-v", d+":/mnt",
				"docker.io/library/alpine:3.23.4",
				"chown", "-R", fmt.Sprintf("%d:%d", uid, gid), "/mnt",
			).Run()
		})
	}

	cmd := exec.Command("docker", dockerArgs...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("docker runAsRoot: failed to launch docker: %v\noutput: %s", err, out)
		}
	}
	return string(out), code
}

// TestDockerRootMount_integrate_missing_known_hosts verifies integrate works when
// SSH_KNOWN_HOSTS is set but points to a nonexistent file — the case where the
// caller sets the env var but skips ssh-keyscan (e.g. when using a token). The
// upstream URL here is file://, so no SSH connection is made; the test just
// ensures gitspork doesn't hard-error on the missing file.
func TestDockerRootMount_integrate_missing_known_hosts(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)

	out, code := runAsRootNoKnownHosts(t, upstreamDir, downstreamDir, "/downstream", []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", "/downstream",
	})
	require.Equal(t, 0, code, "integrate with missing known_hosts failed:\n%s", out)
	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
}

// TestDockerRootMount_integrate verifies integrate works when the container runs
// as root against a host-owned downstream volume mount (safe.directory scenario).
func TestDockerRootMount_integrate(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)

	out, code := runAsRoot(t, upstreamDir, downstreamDir, "/downstream", []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", "/downstream",
	})
	require.Equal(t, 0, code, "integrate as root failed:\n%s", out)
	AssertFileContains(t, downstreamDir, "upstream-owned/file.txt", "upstream content")
	AssertFileContains(t, downstreamDir, ".gitspork/downstream-state.json", "last_upstream_commit_hash")
}

// TestDockerRootMount_check_drift verifies check-drift works when the container
// runs as root against a host-owned downstream (safe.directory + SSH_KNOWN_HOSTS).
func TestDockerRootMount_check_drift(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)

	// Use the standard harness runner to integrate and commit first.
	stdRunner := &DockerRunner{ImageTag: dockerImageTag, UpstreamDir: upstreamDir, DownstreamDir: downstreamDir}
	out, code := stdRunner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 0, code, "integrate setup failed:\n%s", out)
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")
	prepDownstreamWithInputData(t, downstreamDir)

	out, code = runAsRoot(t, upstreamDir, downstreamDir, "/downstream", []string{
		"check-drift",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--downstream-repo-path", "/downstream",
	})
	require.Equal(t, 0, code, "check-drift as root reported unexpected result:\n%s", out)
	assert.Contains(t, out, "no drift detected")
}

// TestDockerRootMount_mv verifies gitspork mv works when the container runs as root
// against a host-owned upstream volume mount (safe.directory scenario).
func TestDockerRootMount_mv(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)

	out, code := runAsRoot(t, upstreamDir, "", "/upstream", []string{
		"mv", "docs/old.md", "docs/new.md",
	})
	require.Equal(t, 0, code, fmt.Sprintf("gitspork mv as root failed:\n%s", out))

	AssertFileAbsent(t, upstreamDir, "docs/old.md")
	AssertFileContains(t, upstreamDir, "docs/new.md", "old doc")
	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.Contains(t, cfg, "docs/new.md")
	assert.NotContains(t, cfg, "docs/old.md")
}

// TestDockerRootMount_rm verifies gitspork rm works when the container runs as root
// against a host-owned upstream volume mount (safe.directory scenario).
func TestDockerRootMount_rm(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"docs/old.md":  "# old doc\n",
		"docs/keep.md": "# keep\n",
	}, mvRmGitsporkYML)

	out, code := runAsRoot(t, upstreamDir, "", "/upstream", []string{
		"rm", "docs/old.md",
	})
	require.Equal(t, 0, code, fmt.Sprintf("gitspork rm as root failed:\n%s", out))

	AssertFileAbsent(t, upstreamDir, "docs/old.md")
	cfg := ReadFile(t, upstreamDir, ".gitspork.yml")
	assert.NotContains(t, cfg, "docs/old.md")
	assert.Contains(t, cfg, "docs/keep.md")
}
