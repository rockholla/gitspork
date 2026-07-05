package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rockholla/gitspork/internal"
	"github.com/rockholla/gitspork/internal/types"
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
	var upstreamFlags []string
	var downstreamRepoPath string
	var forceRePrompt bool

	var cmd = &cobra.Command{
		Use:   "integrate",
		Short: integrateHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", integrateHelpShort, integrateHelpLong),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldFlagsSet := upstreamRepoURL != "" || upstreamRepoVersion != "" || upstreamRepoSubpath != "" || upstreamRepoToken != ""
			if len(upstreamFlags) > 0 && oldFlagsSet {
				return fmt.Errorf("cannot mix --upstream with --upstream-repo-url/version/subpath/token flags")
			}

			opts := &types.IntegrateOptions{
				Logger:              logger,
				UpstreamRepoURL:     upstreamRepoURL,
				UpstreamRepoVersion: upstreamRepoVersion,
				UpstreamRepoSubpath: upstreamRepoSubpath,
				UpstreamRepoToken:   upstreamRepoToken,
				DownstreamRepoPath:  downstreamRepoPath,
				ForceRePrompt:       forceRePrompt,
			}
			for _, f := range upstreamFlags {
				spec, err := internal.ParseUpstreamFlag(f)
				if err != nil {
					return err
				}
				opts.Upstreams = append(opts.Upstreams, spec)
			}
			if _, err := internal.Integrate(opts); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringArrayVar(&upstreamFlags, "upstream", nil,
		"upstream spec as comma-separated key=value pairs (url, version, subpath, token); repeatable for multiple upstreams")
	cmd.PersistentFlags().StringVarP(&upstreamRepoURL, "upstream-repo-url", "u", "",
		"upstream gitspork repo to integrate/re-integrate with (single-upstream shorthand)")
	cmd.PersistentFlags().StringVarP(&upstreamRepoVersion, "upstream-repo-version", "v", "",
		"upstream gitspork repo version (single-upstream shorthand)")
	cmd.PersistentFlags().StringVarP(&upstreamRepoSubpath, "upstream-repo-subpath", "p", "",
		"upstream gitspork repo subpath (single-upstream shorthand)")
	cmd.PersistentFlags().StringVarP(&upstreamRepoToken, "upstream-repo-token", "t", "",
		"upstream gitspork repo token (single-upstream shorthand)")
	cmd.PersistentFlags().StringVarP(&downstreamRepoPath, "downstream-repo-path", "d", "",
		"local path to the downstream repo clone to integrate/re-integrate, defaults to the present working directory")
	cmd.PersistentFlags().BoolVarP(&forceRePrompt, "force-re-prompt", "f", false,
		"If true, will disregard any previous prompt input value caches for templated instructions")

	return cmd
}
