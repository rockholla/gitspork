package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rockholla/gitspork/internal"
	"github.com/spf13/cobra"
)

const (
	mvHelpShort string = "move/rename a file in an upstream gitspork repo and update .gitspork.yml"
	mvHelpLong  string = `Run from within an upstream gitspork repo. Wraps 'git mv' and updates
all entries in .gitspork.yml that reference the old path, including rewriting
glob patterns whose non-wildcard prefix matches the moved path.

All arguments are passed through directly to 'git mv'.`
)

type MvSubcommand struct{}

func (s *MvSubcommand) GetCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "mv [git mv flags] <old-path> <new-path>",
		Short:              mvHelpShort,
		Long:               fmt.Sprintf("%s\n\n%s", mvHelpShort, mvHelpLong),
		DisableFlagParsing: true,
		Args:               cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoPath, err := internal.FindGitSporkConfigDir(".")
			if err != nil {
				return fmt.Errorf("not in a gitspork upstream repo: %v", err)
			}

			gitCmd := exec.Command("git", append([]string{"mv"}, args...)...)
			gitCmd.Dir = repoPath
			if out, err := gitCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git mv failed: %v\n%s", err, out)
			}

			configPath, err := internal.FindGitSporkConfigFile(repoPath)
			if err != nil {
				return err
			}

			// Strip flags; remaining args are: [src...] dest
			var paths []string
			for _, a := range args {
				if !strings.HasPrefix(a, "-") {
					paths = append(paths, a)
				}
			}
			if len(paths) < 2 {
				return fmt.Errorf("expected at least one source and a destination")
			}
			dest := paths[len(paths)-1]
			srcs := paths[:len(paths)-1]

			var allWarnings []string
			for _, src := range srcs {
				newPath := dest
				// If dest is a directory target, the new path is dest/basename(src)
				if len(srcs) > 1 {
					newPath = filepath.Join(dest, filepath.Base(src))
				}
				warnings, err := internal.UpstreamMv(configPath, src, newPath)
				if err != nil {
					return fmt.Errorf("error updating .gitspork.yml: %v", err)
				}
				allWarnings = append(allWarnings, warnings...)
			}
			for _, w := range allWarnings {
				logger.Log("⚠️  %s", w)
			}
			logger.Log("✅ git mv complete and .gitspork.yml updated — remember to commit")
			return nil
		},
	}
}
