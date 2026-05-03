package cmd

import (
	"fmt"
	"os/exec"

	"github.com/rockholla/gitspork/internal"
	"github.com/spf13/cobra"
)

const (
	mvHelpShort string = "move/rename a file in an upstream gitspork repo and update .gitspork.yml"
	mvHelpLong  string = `Run from within an upstream gitspork repo. Wraps 'git mv' and updates
all entries in .gitspork.yml that reference the old path, including rewriting
glob patterns whose non-wildcard prefix matches the moved path.`
)

type MvSubcommand struct{}

func (s *MvSubcommand) GetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mv <old-path> <new-path>",
		Short: mvHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", mvHelpShort, mvHelpLong),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldPath, newPath := args[0], args[1]

			repoPath, err := internal.FindGitSporkConfigDir(".")
			if err != nil {
				return fmt.Errorf("not in a gitspork upstream repo: %v", err)
			}

			gitCmd := exec.Command("git", "mv", oldPath, newPath)
			gitCmd.Dir = repoPath
			if out, err := gitCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git mv failed: %v\n%s", err, out)
			}

			configPath, err := internal.FindGitSporkConfigFile(repoPath)
			if err != nil {
				return err
			}

			warnings, err := internal.UpstreamMv(configPath, oldPath, newPath)
			if err != nil {
				return fmt.Errorf("error updating .gitspork.yml: %v", err)
			}
			for _, w := range warnings {
				logger.Log("⚠️  %s", w)
			}
			logger.Log("✅ git mv complete and .gitspork.yml updated — remember to commit")
			return nil
		},
	}

	return cmd
}
