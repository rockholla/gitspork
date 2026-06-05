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
	var upstreamRepoURL string
	var upstreamRepoToken string
	var verbose bool

	var cmd = &cobra.Command{
		Use:   "check-drift",
		Short: checkDriftHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", checkDriftHelpShort, checkDriftHelpLong),
		// Drift (and other failures) are operational errors, not usage errors:
		// don't dump the help/usage block, and let root's Fatal print the message
		// once instead of cobra also printing its own "Error:" line.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := internal.CheckDrift(&internal.CheckDriftOptions{
				Logger:             logger,
				DownstreamRepoPath: downstreamRepoPath,
				UpstreamRepoURL:    upstreamRepoURL,
				UpstreamRepoToken:  upstreamRepoToken,
				Verbose:            verbose,
			})
			if errors.Is(err, internal.ErrDriftDetected) {
				os.Exit(2)
			}
			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&downstreamRepoPath, "downstream-repo-path", "d", "",
		"local path to the downstream repo to check, defaults to the present working directory")
	cmd.PersistentFlags().StringVarP(&upstreamRepoURL, "upstream-repo-url", "u", "",
		"override the upstream repo URL stored in state (useful when SSH/HTTPS auto-rewrite is insufficient)")
	cmd.PersistentFlags().StringVarP(&upstreamRepoToken, "upstream-repo-token", "t", "",
		"if using an HTTPS git repo URL for the upstream, this is the token to auth")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false,
		"print full git diff output when drift is detected")

	return cmd
}
