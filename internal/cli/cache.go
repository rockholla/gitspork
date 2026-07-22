package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rockholla/gitspork/v2/internal/integrate"
)

// isTerminalFn is a variable so tests can override it. Production callers
// go through golang.org/x/term.IsTerminal.
var isTerminalFn = term.IsTerminal

// CacheSubcommand represents `gitspork cache` and its children.
type CacheSubcommand struct{}

// GetCmd returns the cobra command tree for `gitspork cache`.
func (s *CacheSubcommand) GetCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cache",
		Short: "manage the machine-scoped upstream mirror cache",
	}
	root.AddCommand(s.dirCmd())
	root.AddCommand(s.clearCmd())
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

// clearCmd is `gitspork cache clear` — wipes cache entries.
func (s *CacheSubcommand) clearCmd() *cobra.Command {
	var urlFlag string
	var forceFlag bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "wipe cached upstream entries from disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveCacheRoot()
			if err != nil {
				return err
			}

			if !forceFlag && !isTTY(cmd.InOrStdin()) {
				return fmt.Errorf("cannot clear cache non-interactively without --force; add --force to confirm")
			}

			targets, err := resolveClearTargets(root, urlFlag)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				return nil
			}
			if !forceFlag {
				fmt.Fprintf(cmd.OutOrStdout(), "About to wipe %d cache entry(ies) under %s:\n", len(targets), root)
				for _, t := range targets {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", t.dir)
				}
				fmt.Fprint(cmd.OutOrStdout(), "Proceed? [y/N] ")
				reader := bufio.NewReader(cmd.InOrStdin())
				line, _ := reader.ReadString('\n')
				if strings.ToLower(strings.TrimSpace(line)) != "y" {
					fmt.Fprintln(cmd.OutOrStdout(), "aborted")
					return nil
				}
			}

			for _, t := range targets {
				fl := flock.New(t.lockFile)
				if err := fl.Lock(); err != nil {
					return fmt.Errorf("acquiring lock for %s: %w", t.dir, err)
				}
				removeErr := os.RemoveAll(t.dir)
				_ = os.Remove(t.tsFile)
				_ = fl.Unlock()
				_ = os.Remove(t.lockFile)
				if removeErr != nil {
					return fmt.Errorf("removing %s: %w", t.dir, removeErr)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "clear only the entry matching this upstream URL")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "skip the interactive confirmation prompt (required in non-TTY runs)")
	return cmd
}

type clearTarget struct {
	dir, tsFile, lockFile string
}

// resolveClearTargets enumerates cache entries to wipe. If urlFlag is set,
// resolves to at most one target. Otherwise enumerates every entry under root.
func resolveClearTargets(root, urlFlag string) ([]clearTarget, error) {
	if urlFlag != "" {
		key := integrate.CacheKeyForURL(urlFlag)
		dir := filepath.Join(root, key)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
		return []clearTarget{{
			dir:      dir,
			tsFile:   filepath.Join(root, key+".fetched-at"),
			lockFile: filepath.Join(root, key+".lock"),
		}}, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var targets []clearTarget
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		key := e.Name()
		targets = append(targets, clearTarget{
			dir:      filepath.Join(root, key),
			tsFile:   filepath.Join(root, key+".fetched-at"),
			lockFile: filepath.Join(root, key+".lock"),
		})
	}
	return targets, nil
}

// isTTY reports whether r appears to be an interactive terminal. bytes.Buffer
// (test input) does not implement Fd() and therefore reports false.
func isTTY(r io.Reader) bool {
	type fdHaver interface{ Fd() uintptr }
	f, ok := r.(fdHaver)
	if !ok {
		return false
	}
	return isTerminalFn(int(f.Fd()))
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
