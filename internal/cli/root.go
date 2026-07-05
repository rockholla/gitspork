package cli

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/rockholla/gitspork/internal/logutil"
	"github.com/spf13/cobra"
)

const (
	rootHelpShort string = "A more-powerful upstream alternative to git forks or templates"
	rootHelpLong  string = `See https://github.com/rockholla/gitspork for more detailed info on what
purpose this tool serves and how to use it.`
)

var (
	version string
	logger  *logutil.Logger
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gitspork",
	Short: rootHelpShort,
	Long:  fmt.Sprintf("%s\n\n%s", rootHelpShort, rootHelpLong),
}

func Execute(ver string) {
	version = ver
	rootCmd.Version = version
	err := rootCmd.Execute()
	if err != nil {
		logger.Fatal("%s", err.Error())
	}
}

func init() {
	var forceColor bool
	rootCmd.PersistentFlags().BoolVar(&forceColor, "color", false, "force color output even when stdout is not a TTY (useful in Docker)")
	cobra.OnInitialize(func() {
		if forceColor {
			color.NoColor = false
		}
		logger = logutil.New()
	})

	logger = logutil.New()

	integrateSubcommand := &IntegrateSubcommand{}
	integrateLocalSubcommand := &IntegrateLocalSubcommand{}
	InitSubcommand := &InitSubcommand{}
	checkDriftSubcommand := &CheckDriftSubcommand{}

	mvSubcommand := &MvSubcommand{}
	rmSubcommand := &RmSubcommand{}
	schemaSubcommand := &SchemaSubcommand{}

	rootCmd.AddCommand(integrateSubcommand.GetCmd())
	rootCmd.AddCommand(integrateLocalSubcommand.GetCmd())
	rootCmd.AddCommand(InitSubcommand.GetCmd())
	rootCmd.AddCommand(checkDriftSubcommand.GetCmd())
	rootCmd.AddCommand(mvSubcommand.GetCmd())
	rootCmd.AddCommand(rmSubcommand.GetCmd())
	rootCmd.AddCommand(schemaSubcommand.GetCmd())
}
