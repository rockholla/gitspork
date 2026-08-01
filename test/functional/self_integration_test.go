//go:build functional || functional_docker

package functional

import (
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/stretchr/testify/require"
)

// TestIntegrate_selfIntegrationBlocked_byOrigin verifies that when the
// downstream has its origin remote pointing at the upstream URL, `gitspork
// integrate` refuses to proceed and exits with code 3.
func TestIntegrate_selfIntegrationBlocked_byOrigin(t *testing.T) {
	upstreamDir := buildSimpleUpstream(t)
	upstreamURL := "file://" + upstreamDir
	downstreamDir := NewDownstreamRepo(t)
	prepDownstreamWithInputData(t, downstreamDir)

	// Point downstream's origin at the upstream.
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{upstreamURL},
	})
	require.NoError(t, err)

	runner := resolveRunner(t, upstreamDir, downstreamDir)
	out, code := runner.Run(t, integrateArgs(upstreamDir, downstreamDir), downstreamDir)
	require.Equal(t, 3, code, "expected exit code 3 on self-integration; got %d\nout:\n%s", code, out)
	require.True(t, strings.Contains(out, "self-integration blocked"),
		"expected self-integration message in output:\n%s", out)
}

// TestIntegrateLocal_selfIntegrationBlocked_samePath verifies that
// `gitspork integrate-local --upstream-path X --downstream-path X` refuses
// to proceed and exits with code 3.
func TestIntegrateLocal_selfIntegrationBlocked_samePath(t *testing.T) {
	// Use the simple upstream as both upstream and downstream: the upstream
	// directory is a valid gitspork upstream, so the guard is the only thing
	// that should stop integration.
	dir := buildSimpleUpstream(t)
	runner := resolveRunner(t, dir, dir)

	out, code := runner.Run(t, []string{
		"integrate-local",
		"--upstream-path", dir,
		"--downstream-path", dir,
	}, dir)
	require.Equal(t, 3, code, "expected exit code 3 on self-integration; got %d\nout:\n%s", code, out)
	require.True(t, strings.Contains(out, "self-integration blocked"),
		"expected self-integration message in output:\n%s", out)
}
