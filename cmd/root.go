package cmd

import (
	"fmt"

	"github.com/rockholla/gitspork/internal"
	"github.com/spf13/cobra"
)

const (
	rootHelpShort string = "A more-powerful upstream alternative to git forks or templates"
	rootHelpLong  string = `See https://github.com/rockholla/gitspork for more detailed info on what
purpose this tool serves and how to use it.`
)

var (
	version string
	logger  *internal.Logger
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gitspork",
	Short: rootHelpShort,
	Long:  fmt.Sprintf("%s\n\n%s", rootHelpShort, rootHelpLong),
}

func Execute(ver string) {
	version = ver
	err := rootCmd.Execute()
	if err != nil {
		logger.Fatal(err.Error())
	}
}

func init() {
	logger = internal.NewLogger()

	integrateSubcommand := &IntegrateSubcommand{}
	InitSubcommand := &InitSubcommand{}

	rootCmd.AddCommand(integrateSubcommand.GetCmd())
	rootCmd.AddCommand(InitSubcommand.GetCmd())
}
