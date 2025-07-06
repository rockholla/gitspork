package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rockholla/gitspork/internal"
)

const (
	integrateHelpShort string = "integrate/re-integrate w/ a gitspork upstream"
	integrateHelpLong  string = `integration is the way in which downstreams will continually stay up-to-date w/ their upstreams.

This command is the key orchestration of gitspork, ensuring that specific and advanced integration and merging happen between the
upstream and downstream. See https://github.com/rockholla/gitspork/docs for more info.`
)

// IntegrateSubcommand represents the subcommand and all related functionality for `gitspork integrate`
type IntegrateSubcommand struct{}

// GetCmd will return the native cobra command for the integrate subcommand
func (isc *IntegrateSubcommand) GetCmd() *cobra.Command {
	var upstreamRepoURL string
	var upstreamRepoVersion string
	var upstreamRepoSubpath string
	var upstreamRepoToken string
	var downstreamRepoPath string

	var cmd = &cobra.Command{
		Use:   "integrate",
		Short: integrateHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", integrateHelpShort, integrateHelpLong),
		RunE: func(cmd *cobra.Command, args []string) error {
			return internal.Integrate(&internal.IntegrateOptions{
				Logger:              logger,
				UpstreamRepoURL:     upstreamRepoURL,
				UpstreamRepoVersion: upstreamRepoVersion,
				UpstreamRepoSubpath: upstreamRepoSubpath,
				UpstreamRepoToken:   upstreamRepoToken,
				DownstreamRepoPath:  downstreamRepoPath,
			})
		},
	}

	cmd.PersistentFlags().StringVarP(&upstreamRepoURL, "upstream-repo-url", "u", "",
		"upstream gitspork repo to integrate/re-integrate with")
	cmd.PersistentFlags().StringVarP(&upstreamRepoVersion, "upstream-repo-version", "v", "",
		"upstream gitspork repo version to use while integrating/re-integrating, defaults to the repo's default branch")
	cmd.PersistentFlags().StringVarP(&upstreamRepoSubpath, "upstream-repo-subpath", "p", "",
		"upstream gitspork repo subpath where the gitspork source exists, defaults to the root of the repo")
	cmd.PersistentFlags().StringVarP(&upstreamRepoToken, "upstream-repo-token", "t", "",
		"if using an HTTPs git repo URL for the upstream, this is the token to auth, otherwise SSH and agent auth assumed")
	cmd.PersistentFlags().StringVarP(&downstreamRepoPath, "downstream-repo-path", "d", "",
		"local path to the downstream repo clone to integrate/re-integrate, defaults to the present working directory")

	return cmd
}
