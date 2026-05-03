package cmd

import (
	"fmt"
	"os/exec"
	"slices"

	"github.com/rockholla/gitspork/internal"
	"github.com/spf13/cobra"
)

const (
	rmHelpShort string = "remove a file from an upstream gitspork repo and update .gitspork.yml"
	rmHelpLong  string = `Run from within an upstream gitspork repo. Wraps 'git rm' and updates
all entries in .gitspork.yml that reference the removed path.

All arguments are passed through directly to 'git rm'.`
)

type RmSubcommand struct{}

func (s *RmSubcommand) GetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "rm [git rm flags] <path>",
		Short:              rmHelpShort,
		Long:               fmt.Sprintf("%s\n\n%s", rmHelpShort, rmHelpLong),
		DisableFlagParsing: true,
		Args:               cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Last arg is the path; -r flag means recursive config cleanup
			path := args[len(args)-1]
			recursive := slices.Contains(args, "-r")

			repoPath, err := internal.FindGitSporkConfigDir(".")
			if err != nil {
				return fmt.Errorf("not in a gitspork upstream repo: %v", err)
			}

			gitCmd := exec.Command("git", append([]string{"rm"}, args...)...)
			gitCmd.Dir = repoPath
			if out, err := gitCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git rm failed: %v\n%s", err, out)
			}

			configPath, err := internal.FindGitSporkConfigFile(repoPath)
			if err != nil {
				return err
			}

			warnings, err := internal.UpstreamRm(configPath, path, recursive)
			if err != nil {
				return fmt.Errorf("error updating .gitspork.yml: %v", err)
			}
			for _, w := range warnings {
				logger.Log("⚠️  %s", w)
			}
			logger.Log("✅ git rm complete and .gitspork.yml updated — remember to commit")
			return nil
		},
	}

	return cmd
}
