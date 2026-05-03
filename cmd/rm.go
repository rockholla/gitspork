package cmd

import (
	"fmt"
	"os/exec"

	"github.com/rockholla/gitspork/internal"
	"github.com/spf13/cobra"
)

const (
	rmHelpShort string = "remove a file from an upstream gitspork repo and update .gitspork.yml"
	rmHelpLong  string = `Run from within an upstream gitspork repo. Wraps 'git rm' and updates
all entries in .gitspork.yml that reference the removed path. Use -R for
recursive directory removal.`
)

type RmSubcommand struct{}

func (s *RmSubcommand) GetCmd() *cobra.Command {
	var repoPath string
	var recursive bool

	cmd := &cobra.Command{
		Use:   "rm <path>",
		Short: rmHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", rmHelpShort, rmHelpLong),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]

			if repoPath == "" {
				var err error
				repoPath, err = internal.FindGitSporkConfigDir(".")
				if err != nil {
					return fmt.Errorf("not in a gitspork upstream repo: %v", err)
				}
			}

			gitArgs := []string{"rm"}
			if recursive {
				gitArgs = append(gitArgs, "-r")
			}
			gitArgs = append(gitArgs, path)
			gitCmd := exec.Command("git", gitArgs...)
			gitCmd.Dir = repoPath
			if out, err := gitCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git rm failed: %v\n%s", err, out)
			}

			configPath, err := internal.FindGitSporkConfigFile(repoPath)
			if err != nil {
				return err
			}

			warnings, err := internal.UpstreamRm(configPath, repoPath, path, recursive)
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

	cmd.PersistentFlags().StringVarP(&repoPath, "repo-path", "r", "", "path to the upstream gitspork repo root, defaults to current directory")
	cmd.PersistentFlags().BoolVarP(&recursive, "recursive", "R", false, "recursively remove directory and update .gitspork.yml entries under that path")
	return cmd
}
