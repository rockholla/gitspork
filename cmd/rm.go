package cmd

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

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
	return &cobra.Command{
		Use:                "rm [git rm flags] <path>...",
		Short:              rmHelpShort,
		Long:               fmt.Sprintf("%s\n\n%s", rmHelpShort, rmHelpLong),
		DisableFlagParsing: true,
		Args:               cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			// Strip flags; remaining args are the paths to remove
			var allWarnings []string
			for _, a := range args {
				if strings.HasPrefix(a, "-") {
					continue
				}
				warnings, err := internal.UpstreamRm(configPath, a, recursive)
				if err != nil {
					return fmt.Errorf("error updating .gitspork.yml: %v", err)
				}
				allWarnings = append(allWarnings, warnings...)
			}
			for _, w := range allWarnings {
				logger.Log("⚠️  %s", w)
			}
			logger.Log("✅ git rm complete and .gitspork.yml updated — remember to commit")
			return nil
		},
	}
}
