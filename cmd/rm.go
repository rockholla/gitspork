package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rockholla/gitspork/internal/config"
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

			configPath, err := config.FindGitSporkConfig(".")
			if err != nil {
				return fmt.Errorf("not in a gitspork upstream repo: %v", err)
			}
			repoPath := filepath.Dir(configPath)

			// Strip flags; remaining args are the paths to remove.
			// Compute all config changes before touching the index — bail early on any error.
			// Chain removals through the in-memory config so multi-path removes are consistent.
			var rmPaths []string
			for _, a := range args {
				if !strings.HasPrefix(a, "-") {
					rmPaths = append(rmPaths, a)
				}
			}
			if len(rmPaths) == 0 {
				return fmt.Errorf("expected at least one path")
			}

			cfg, warnings, err := config.ComputeUpstreamRm(configPath, rmPaths[0], recursive)
			if err != nil {
				return fmt.Errorf("error computing .gitspork.yml update: %v", err)
			}
			for i := 1; i < len(rmPaths); i++ {
				var w []string
				cfg, w, err = config.ComputeUpstreamRmFromConfig(cfg, rmPaths[i], recursive)
				if err != nil {
					return fmt.Errorf("error computing .gitspork.yml update: %v", err)
				}
				warnings = append(warnings, w...)
			}

			gitCmd := exec.Command("git", append([]string{"-c", "safe.directory=*", "rm"}, args...)...)
			gitCmd.Dir = repoPath
			if out, err := gitCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git rm failed: %v\n%s", err, out)
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
			logger.Log("✅ git rm complete and .gitspork.yml staged — ready to commit")
			return nil
		},
	}
}
