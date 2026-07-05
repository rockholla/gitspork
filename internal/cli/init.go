package cli

import (
	"fmt"

	"github.com/rockholla/gitspork/internal/config"
	"github.com/rockholla/gitspork/internal/logutil"
	"github.com/spf13/cobra"
)

const (
	initHelpShort        string = "initializes a local repo clone directory for use as a gitspork upstream"
	initHelpLongTemplate string = `initialization will do the following:

 * create a .gitspork.yml config file at your desired path with the structure prepped for you to fill in per these docs:
 
The main .gitspork.yml config schema:
---------------------------------------------
%s

And the migrations yml config schema:
---------------------------------------------
%s

For more info on the structure and how to configure, see https://github.com/rockholla/gitspork`
)

// InitSubcommand represents the subcommand and all related functionality for `gitspork init“
type InitSubcommand struct{}

// GetCmd will return the native cobra command for the init subcommand
func (isc *InitSubcommand) GetCmd() *cobra.Command {
	var initPath string

	configSchema, migrationsSchema, _ := config.GetGitSporkConfigSchema()
	var cmd = &cobra.Command{
		Use:   "init",
		Short: initHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", initHelpShort, fmt.Sprintf(initHelpLongTemplate, logutil.ColorizeYAML(configSchema), logutil.ColorizeYAML(migrationsSchema))),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.Init(initPath, logger)
		},
	}

	cmd.PersistentFlags().StringVarP(&initPath, "path", "p", "",
		"the local path to init, by default uses the current working directory")

	return cmd
}
