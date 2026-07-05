package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rockholla/gitspork/internal/config"
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
			configPath, err := config.FindGitSporkConfig(".")
			if err != nil {
				return fmt.Errorf("not in a gitspork upstream repo: %v", err)
			}
			repoPath := filepath.Dir(configPath)

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

			// Compute all config changes before touching the index — bail early on any error.
			// Chain rewrites through the in-memory config so multi-source moves are consistent.
			cfg, warnings, err := config.ComputeUpstreamMv(configPath, srcs[0], mvDest(srcs, dest, 0))
			if err != nil {
				return fmt.Errorf("error computing .gitspork.yml update: %v", err)
			}
			for i := 1; i < len(srcs); i++ {
				var w []string
				cfg, w, err = config.ComputeUpstreamMvFromConfig(cfg, srcs[i], mvDest(srcs, dest, i))
				if err != nil {
					return fmt.Errorf("error computing .gitspork.yml update: %v", err)
				}
				warnings = append(warnings, w...)
			}

			gitCmd := exec.Command("git", append([]string{"-c", "safe.directory=*", "mv"}, args...)...)
			gitCmd.Dir = repoPath
			if out, err := gitCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git mv failed: %v\n%s", err, out)
			}

			if err := config.WriteGitSporkConfig(configPath, cfg); err != nil {
				return fmt.Errorf("error writing .gitspork.yml: %v", err)
			}
			if out, err := exec.Command("git", "-c", "safe.directory=*", "add", configPath).CombinedOutput(); err != nil {
				return fmt.Errorf("git add .gitspork.yml failed: %v\n%s", err, out)
			}

			for _, w := range warnings {
				logger.Log("⚠️  %s", w)
			}
			logger.Log("✅ git mv complete and .gitspork.yml staged — ready to commit")
			return nil
		},
	}
}

// mvDest returns the effective destination path for srcs[i] → dest.
// When multiple sources are given, dest is treated as a directory.
func mvDest(srcs []string, dest string, i int) string {
	if len(srcs) > 1 {
		return filepath.Join(dest, filepath.Base(srcs[i]))
	}
	return dest
}
