package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// CacheSubcommand represents `gitspork cache` and its children.
type CacheSubcommand struct{}

// GetCmd returns the cobra command tree for `gitspork cache`.
func (s *CacheSubcommand) GetCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cache",
		Short: "manage the machine-scoped upstream mirror cache",
	}
	root.AddCommand(s.dirCmd())
	return root
}

// dirCmd is `gitspork cache dir` — prints the resolved cache root.
func (s *CacheSubcommand) dirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dir",
		Short: "print the resolved upstream mirror cache root",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveCacheRoot()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), root)
			return nil
		},
	}
}

// resolveCacheRoot mirrors internal/integrate/cache.go's own resolution but
// without pulling in the whole cacheConfig plumbing. Duplicated because the
// CLI package can't (and shouldn't) import internal/integrate/cache.go's
// unexported helpers.
func resolveCacheRoot() (string, error) {
	if root := os.Getenv("GITSPORK_CACHE_DIR"); root != "" {
		return root, nil
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache dir: %w", err)
	}
	return filepath.Join(userCache, "gitspork", "repos"), nil
}
