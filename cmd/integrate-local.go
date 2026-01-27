package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rockholla/gitspork/internal"
)

const (
	integrateLocalHelpShort string = "integrate/re-integrate w/ a gitspork local upstream template"
	integrateLocalHelpLong  string = `integration is the way in which local downstream directories can integrate/re-integrate w/ standards set forth by another local directory upstream template.

This command supports all of the same configuration the normal integrate command does, but just operates against two local directories.
See https://github.com/rockholla/gitspork/docs for more info on configuration options.
`
)

// IntegrateLocalSubcommand represents the subcommand and all related functionality for `gitspork integrate-local`
type IntegrateLocalSubcommand struct{}

// GetCmd will return the native cobra command for the integrate subcommand
func (ilsc *IntegrateLocalSubcommand) GetCmd() *cobra.Command {
	var upstreamPath string
	var downstreamPath string
	var forceRePrompt bool

	var cmd = &cobra.Command{
		Use:   "integrate-local",
		Short: integrateLocalHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", integrateLocalHelpShort, integrateLocalHelpLong),
		RunE: func(cmd *cobra.Command, args []string) error {
			return internal.IntegrateLocal(&internal.IntegrateLocalOptions{
				Logger:         logger,
				UpstreamPath:   upstreamPath,
				DownstreamPath: downstreamPath,
				ForceRePrompt:  forceRePrompt,
			})
		},
	}

	cmd.PersistentFlags().StringVarP(&upstreamPath, "upstream-path", "u", "",
		"local path that contains a template/gitspork upstream configuration")
	cmd.PersistentFlags().StringVarP(&downstreamPath, "downstream-path", "d", "",
		"local path to integrate/re-integrate w/ the standards set at the upstream-path")
	cmd.PersistentFlags().BoolVarP(&forceRePrompt, "force-re-prompt", "f", false,
		"If true, will disregard and previous prompt input value caches for templated instructions, requiring values to be re-input by the user")

	return cmd
}
