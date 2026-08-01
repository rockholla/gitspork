//go:build sdk

package sdk_test

import (
	"errors"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/rockholla/gitspork/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrate_selfIntegration_returnsSentinel verifies that
// gitspork.Integrate returns an error that unwraps to gitspork.ErrSelfIntegration
// when the downstream's origin remote matches the upstream URL. Guards the
// sentinel re-export from the internal sdktypes package.
func TestIntegrate_selfIntegration_returnsSentinel(t *testing.T) {
	upstreamDir, _ := minimalUpstream(t)
	upstreamURL := "file://" + upstreamDir
	downstreamDir := emptyDownstream(t)

	// Give the downstream an origin remote pointing at the upstream URL.
	repo, err := gogit.PlainOpen(downstreamDir)
	require.NoError(t, err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{upstreamURL},
	})
	require.NoError(t, err)

	_, err = gitspork.Integrate(&gitspork.IntegrateOptions{
		Upstreams:          []gitspork.UpstreamSpec{{URL: upstreamURL, Version: "main"}},
		DownstreamRepoPath: downstreamDir,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, gitspork.ErrSelfIntegration),
		"expected err to unwrap to gitspork.ErrSelfIntegration; got: %v", err)
}
