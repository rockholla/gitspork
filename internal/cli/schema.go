package cli

import (
	"fmt"

	"github.com/rockholla/gitspork/internal/config"
	"github.com/rockholla/gitspork/internal/logutil"
	"github.com/spf13/cobra"
)

const (
	schemaHelpShort string = "print the .gitspork.yml and migration YAML schemas"
	schemaHelpLong  string = `Prints the annotated schema for both .gitspork.yml and the migration YAML format.

Output is syntax-highlighted when writing to a terminal.`
)

// SchemaSubcommand represents the subcommand and all related functionality for 'gitspork schema'
type SchemaSubcommand struct{}

// GetCmd will return the native cobra command for the schema subcommand
func (s *SchemaSubcommand) GetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: schemaHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", schemaHelpShort, schemaHelpLong),
		RunE: func(cmd *cobra.Command, args []string) error {
			configSchema, migrationsSchema, err := config.GetGitSporkConfigSchema()
			if err != nil {
				return err
			}
			fmt.Printf("Main .gitspork.yml schema:\n---------------------------------------------\n%s\n\nMigration YAML schema:\n---------------------------------------------\n%s\n",
				logutil.ColorizeYAML(configSchema),
				logutil.ColorizeYAML(migrationsSchema),
			)
			return nil
		},
	}
}
