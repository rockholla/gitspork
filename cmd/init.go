package cmd

import (
	"fmt"

	"github.com/rockholla/gitspork/internal"
	"github.com/spf13/cobra"
)

const (
	initHelpShort string = "initializes a local repo clone directory for use as a gitspork upstream"
	initHelpLong  string = `initialization will do the following:

 * create a .gitspork.yml config file at your desired path with the structure prepped for you to fill in
 
For more info on the structure and how to configure, see https://github.com/rockholla/gitspork`
)

// InitSubcommand represents the subcommand and all related functionality for `gitspork init“
type InitSubcommand struct{}

// GetCmd will return the native cobra command for the init subcommand
func (isc *InitSubcommand) GetCmd() *cobra.Command {
	var initPath string

	var cmd = &cobra.Command{
		Use:   "init",
		Short: initHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", initHelpShort, initHelpLong),
		RunE: func(cmd *cobra.Command, args []string) error {
			return internal.Init(initPath, version, logger)
		},
	}

	cmd.PersistentFlags().StringVarP(&initPath, "path", "p", "",
		"the local path to init, by default uses the current working directory")

	return cmd
}
