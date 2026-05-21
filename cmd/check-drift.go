package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rockholla/gitspork/internal"
)

const (
	checkDriftHelpShort string = "check if a downstream repo has drifted from its last integrated upstream state"
	checkDriftHelpLong  string = `check-drift re-runs the integration at the exact upstream commit hash used in the last
integrate run, against an isolated copy of the downstream repo, and reports any differences.

Exit codes:
  0 - no drift detected
  1 - error (missing state, unclean working tree, clone failure, etc.)
  2 - drift detected

See https://github.com/rockholla/gitspork/docs for more info.`
)

// CheckDriftSubcommand represents the subcommand and all related functionality for 'gitspork check-drift'
type CheckDriftSubcommand struct{}

// GetCmd will return the native cobra command for the check-drift subcommand
func (cds *CheckDriftSubcommand) GetCmd() *cobra.Command {
	var downstreamRepoPath string
	var upstreamFlags []string
	var verbose bool

	var cmd = &cobra.Command{
		Use:   "check-drift",
		Short: checkDriftHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", checkDriftHelpShort, checkDriftHelpLong),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := &internal.CheckDriftOptions{
				Logger:             logger,
				DownstreamRepoPath: downstreamRepoPath,
				Verbose:            verbose,
			}
			for _, f := range upstreamFlags {
				spec, err := internal.ParseUpstreamFlag(f)
				if err != nil {
					return err
				}
				opts.Upstreams = append(opts.Upstreams, spec)
			}
			err := internal.CheckDrift(opts)
			if errors.Is(err, internal.ErrDriftDetected) {
				os.Exit(2)
			}
			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&downstreamRepoPath, "downstream-repo-path", "d", "",
		"local path to the downstream repo to check, defaults to the present working directory")
	cmd.PersistentFlags().StringArrayVar(&upstreamFlags, "upstream", nil,
		"override upstream(s) as comma-separated key=value pairs (url, version, subpath, token); repeatable")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false,
		"print full git diff output when drift is detected")

	return cmd
}
