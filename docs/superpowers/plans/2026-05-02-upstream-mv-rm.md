# Upstream `gitspork mv` / `gitspork rm` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `gitspork mv` and `gitspork rm` subcommands that wrap `git mv`/`git rm` and automatically update `.gitspork.yml` to match, including rewriting glob patterns whose non-wildcard prefix matches the moved/removed path.

**Architecture:** Internal logic lives in `internal/upstream-mv-rm.go` — two functions (`upstreamMv`, `upstreamRm`) that edit `.gitspork.yml` in-place, plus a helper to extract and rewrite glob prefixes. Cobra wiring in `cmd/mv.go` and `cmd/rm.go`. Both commands shell out to `git mv` / `git rm` for the actual file operation, then call the internal functions for config editing.

**Tech Stack:** Go, cobra, go-git v6, gobwas/glob, gopkg.in/yaml.v2

**Prerequisite:** Plan 1 (delta propagation) should be complete first, as this plan shares the same branch and some context.

---

## File Map

- **Create:** `internal/upstream-mv-rm.go` — `upstreamMv`, `upstreamRm`, `globNonWildcardPrefix`, `rewriteGlobPrefix`
- **Create:** `internal/upstream-mv-rm_test.go` — `Test_upstreamMv`, `Test_upstreamRm`
- **Create:** `cmd/mv.go` — `MvSubcommand`
- **Create:** `cmd/rm.go` — `RmSubcommand`
- **Modify:** `cmd/root.go` — register new subcommands

---

### Task 1: `globNonWildcardPrefix` helper

**Files:**
- Create: `internal/upstream-mv-rm.go`
- Create: `internal/upstream-mv-rm_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/upstream-mv-rm_test.go
package internal

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func Test_globNonWildcardPrefix(t *testing.T) {
	assert.Equal(t, "docs/cloud-native", globNonWildcardPrefix("docs/cloud-native/**"))
	assert.Equal(t, "docs/cloud-native", globNonWildcardPrefix("docs/cloud-native/*.md"))
	assert.Equal(t, "", globNonWildcardPrefix("**/cloud-native/*.md"))
	assert.Equal(t, "exact/path.md", globNonWildcardPrefix("exact/path.md"))
}
```

- [ ] **Step 2: Run to confirm failure**

```
go test ./internal/... -run Test_globNonWildcardPrefix -v
```
Expected: FAIL — undefined

- [ ] **Step 3: Implement**

```go
// internal/upstream-mv-rm.go
package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

// globNonWildcardPrefix returns the portion of a glob pattern before the first wildcard character.
// Returns empty string if the pattern begins with a wildcard.
func globNonWildcardPrefix(pattern string) string {
	for i, ch := range pattern {
		if ch == '*' || ch == '?' || ch == '[' {
			// trim trailing slash separator if present
			return strings.TrimSuffix(pattern[:i], "/")
		}
	}
	return pattern
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/... -run Test_globNonWildcardPrefix -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/upstream-mv-rm.go internal/upstream-mv-rm_test.go
git commit -m "feat: add globNonWildcardPrefix helper for upstream mv/rm config editing"
```

---

### Task 2: `upstreamMv` — config rewriting for moves

**Files:**
- Modify: `internal/upstream-mv-rm.go`
- Modify: `internal/upstream-mv-rm_test.go`

- [ ] **Step 1: Write failing tests**

Add `Test_upstreamMv` to `internal/upstream-mv-rm_test.go`:

```go
func Test_upstreamMv(t *testing.T) {
	t.Run("exact upstream_owned entry is replaced", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/old.md"},
		})
		warnings, err := upstreamMv(cfg, dir, "docs/old.md", "docs/new.md")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/new.md"}, result.UpstreamOwned)
	})

	t.Run("glob with matching prefix is rewritten", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/**"},
		})
		warnings, err := upstreamMv(cfg, dir, "docs/cloud-native", "docs/cloud")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/cloud/**"}, result.UpstreamOwned)
	})

	t.Run("glob with wildcard before moved segment emits warning and is unchanged", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"**/cloud-native/*.md"},
		})
		warnings, err := upstreamMv(cfg, dir, "cloud-native", "cloud")
		require.NoError(t, err)
		assert.Len(t, warnings, 1)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"**/cloud-native/*.md"}, result.UpstreamOwned)
	})

	t.Run("templated template field updated", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/old.tmpl", Destination: "out/file.txt"},
			},
		})
		warnings, err := upstreamMv(cfg, dir, "templates/old.tmpl", "templates/new.tmpl")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "templates/new.tmpl", result.Templated[0].Template)
		assert.Equal(t, "out/file.txt", result.Templated[0].Destination)
	})

	t.Run("templated destination field updated", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/old.txt"},
			},
		})
		warnings, err := upstreamMv(cfg, dir, "out/old.txt", "out/new.txt")
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, "out/new.txt", result.Templated[0].Destination)
	})
}

// helpers
func makeConfigFile(t *testing.T, config *GitSporkConfig) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gitspork-mv-rm-test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfgPath := filepath.Join(dir, gitSporkConfigFileName)
	b, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, b, 0644))
	return dir, cfgPath
}

func loadConfigFile(t *testing.T, cfgPath string) *GitSporkConfig {
	t.Helper()
	cfg, err := ParseGitSporkConfig(cfgPath)
	require.NoError(t, err)
	return cfg
}
```

- [ ] **Step 2: Run to confirm failure**

```
go test ./internal/... -run Test_upstreamMv -v
```
Expected: FAIL — `upstreamMv` undefined

- [ ] **Step 3: Implement `upstreamMv`**

Add to `internal/upstream-mv-rm.go`:

```go
// upstreamMv updates .gitspork.yml at configPath to reflect a move from oldPath to newPath.
// Returns a list of warning strings for patterns that could not be automatically updated.
func upstreamMv(configPath, repoDir, oldPath, newPath string) ([]string, error) {
	config, err := ParseGitSporkConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("error reading config: %v", err)
	}
	var warnings []string

	rewritePatterns := func(patterns []string) []string {
		result := make([]string, len(patterns))
		for i, p := range patterns {
			prefix := globNonWildcardPrefix(p)
			if prefix == "" {
				warnings = append(warnings, fmt.Sprintf("pattern %q has a leading wildcard — update manually", p))
				result[i] = p
				continue
			}
			if p == oldPath {
				result[i] = newPath
			} else if prefix == oldPath {
				result[i] = newPath + p[len(oldPath):]
			} else if strings.HasPrefix(prefix, oldPath+"/") {
				result[i] = newPath + p[len(oldPath):]
			} else {
				result[i] = p
			}
		}
		return result
	}

	config.UpstreamOwned = rewritePatterns(config.UpstreamOwned)
	config.SharedOwnership.Merged = rewritePatterns(config.SharedOwnership.Merged)
	config.SharedOwnership.Structured.PreferUpstream = rewritePatterns(config.SharedOwnership.Structured.PreferUpstream)
	config.SharedOwnership.Structured.PreferDownstream = rewritePatterns(config.SharedOwnership.Structured.PreferDownstream)

	for i, t := range config.Templated {
		if t.Template == oldPath {
			config.Templated[i].Template = newPath
		}
		if t.Destination == oldPath {
			config.Templated[i].Destination = newPath
		}
	}

	return warnings, writeConfigFile(configPath, config)
}

func writeConfigFile(configPath string, config *GitSporkConfig) error {
	b, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshalling config: %v", err)
	}
	return os.WriteFile(configPath, b, 0644)
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/... -run Test_upstreamMv -v
```
Expected: PASS all five cases

- [ ] **Step 5: Commit**

```bash
git add internal/upstream-mv-rm.go internal/upstream-mv-rm_test.go
git commit -m "feat: implement upstreamMv config rewriting with glob prefix support"
```

---

### Task 3: `upstreamRm` — config rewriting for removals

**Files:**
- Modify: `internal/upstream-mv-rm.go`
- Modify: `internal/upstream-mv-rm_test.go`

- [ ] **Step 1: Write failing tests**

Add `Test_upstreamRm` to `internal/upstream-mv-rm_test.go`:

```go
func Test_upstreamRm(t *testing.T) {
	t.Run("exact entry removed", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/guide.md", "docs/other.md"},
		})
		warnings, err := upstreamRm(cfg, dir, "docs/guide.md", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("recursive: child exact paths removed", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/file.md", "docs/other.md"},
		})
		warnings, err := upstreamRm(cfg, dir, "docs/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("recursive: glob with matching prefix removed", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"docs/cloud-native/**", "docs/other.md"},
		})
		warnings, err := upstreamRm(cfg, dir, "docs/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"docs/other.md"}, result.UpstreamOwned)
	})

	t.Run("glob with leading wildcard emits warning and is unchanged", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			UpstreamOwned: []string{"**/cloud-native/*.md"},
		})
		warnings, err := upstreamRm(cfg, dir, "cloud-native", true)
		require.NoError(t, err)
		assert.Len(t, warnings, 1)
		result := loadConfigFile(t, cfg)
		assert.Equal(t, []string{"**/cloud-native/*.md"}, result.UpstreamOwned)
	})

	t.Run("templated entry removed when template matches", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/foo.tmpl", Destination: "out/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := upstreamRm(cfg, dir, "templates/foo.tmpl", false)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})

	t.Run("recursive: templated entry removed when template is child of removed path", func(t *testing.T) {
		dir, cfg := makeConfigFile(t, &GitSporkConfig{
			Templated: []GitSporkConfigTemplated{
				{Template: "templates/cloud-native/foo.tmpl", Destination: "out/foo.txt"},
				{Template: "templates/bar.tmpl", Destination: "out/bar.txt"},
			},
		})
		warnings, err := upstreamRm(cfg, dir, "templates/cloud-native", true)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		result := loadConfigFile(t, cfg)
		require.Len(t, result.Templated, 1)
		assert.Equal(t, "templates/bar.tmpl", result.Templated[0].Template)
	})
}
```

- [ ] **Step 2: Run to confirm failure**

```
go test ./internal/... -run Test_upstreamRm -v
```
Expected: FAIL — `upstreamRm` undefined

- [ ] **Step 3: Implement `upstreamRm`**

Add to `internal/upstream-mv-rm.go`:

```go
// upstreamRm updates .gitspork.yml at configPath to remove entries matching path.
// If recursive is true, also removes entries whose non-wildcard prefix falls under path.
func upstreamRm(configPath, repoDir, path string, recursive bool) ([]string, error) {
	config, err := ParseGitSporkConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("error reading config: %v", err)
	}
	var warnings []string

	filterPatterns := func(patterns []string) []string {
		var result []string
		for _, p := range patterns {
			if p == path {
				continue // exact match — remove
			}
			if recursive {
				prefix := globNonWildcardPrefix(p)
				if prefix == "" {
					warnings = append(warnings, fmt.Sprintf("pattern %q has a leading wildcard — update manually", p))
					result = append(result, p)
					continue
				}
				if prefix == path || strings.HasPrefix(prefix, path+"/") {
					continue // prefix falls under removed path — remove
				}
				if strings.HasPrefix(p, path+"/") {
					continue // exact child path — remove
				}
			}
			result = append(result, p)
		}
		return result
	}

	config.UpstreamOwned = filterPatterns(config.UpstreamOwned)
	config.SharedOwnership.Merged = filterPatterns(config.SharedOwnership.Merged)
	config.SharedOwnership.Structured.PreferUpstream = filterPatterns(config.SharedOwnership.Structured.PreferUpstream)
	config.SharedOwnership.Structured.PreferDownstream = filterPatterns(config.SharedOwnership.Structured.PreferDownstream)

	var templated []GitSporkConfigTemplated
	for _, t := range config.Templated {
		if t.Template == path {
			continue
		}
		if recursive && strings.HasPrefix(t.Template, path+"/") {
			continue
		}
		templated = append(templated, t)
	}
	config.Templated = templated

	return warnings, writeConfigFile(configPath, config)
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/... -run Test_upstreamRm -v
```
Expected: PASS all six cases

- [ ] **Step 5: Commit**

```bash
git add internal/upstream-mv-rm.go internal/upstream-mv-rm_test.go
git commit -m "feat: implement upstreamRm config rewriting with recursive and glob prefix support"
```

---

### Task 4: `cmd/mv.go` and `cmd/rm.go` cobra wiring

**Files:**
- Create: `cmd/mv.go`
- Create: `cmd/rm.go`
- Modify: `cmd/root.go`

- [ ] **Step 1: Create `cmd/mv.go`**

```go
// cmd/mv.go
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
	var repoPath string

	cmd := &cobra.Command{
		Use:   "mv <old-path> <new-path>",
		Short: mvHelpShort,
		Long:  fmt.Sprintf("%s\n\n%s", mvHelpShort, mvHelpLong),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldPath, newPath := args[0], args[1]

			if repoPath == "" {
				var err error
				repoPath, err = internal.FindGitSporkConfigDir(".")
				if err != nil {
					return fmt.Errorf("not in a gitspork upstream repo: %v", err)
				}
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

			warnings, err := internal.UpstreamMv(configPath, repoPath, oldPath, newPath)
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

	cmd.PersistentFlags().StringVarP(&repoPath, "repo-path", "r", "", "path to the upstream gitspork repo root, defaults to current directory")
	return cmd
}
```

- [ ] **Step 2: Create `cmd/rm.go`**

```go
// cmd/rm.go
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
all entries in .gitspork.yml that reference the removed path. Use -r for
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
```

- [ ] **Step 3: Export `upstreamMv` and `upstreamRm` and add `FindGitSporkConfigDir` / `FindGitSporkConfigFile` helpers**

In `internal/upstream-mv-rm.go`, rename `upstreamMv` → `UpstreamMv` and `upstreamRm` → `UpstreamRm` (exported).

In `internal/upstream-mv-rm_test.go`, update all call sites:
- `upstreamMv(cfg, dir, ...)` → `UpstreamMv(cfg, dir, ...)`
- `upstreamRm(cfg, dir, ...)` → `UpstreamRm(cfg, dir, ...)`

Add to `internal/upstream-mv-rm.go`:

```go
// FindGitSporkConfigDir walks up from startDir to find a directory containing .gitspork.yml or .gitspork.yaml.
func FindGitSporkConfigDir(startDir string) (string, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if _, err := os.Stat(filepath.Join(dir, gitSporkConfigFileName)); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, gitSporkConfigFileNameAlt)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .gitspork.yml found in %s or any parent directory", startDir)
		}
		dir = parent
	}
}

// FindGitSporkConfigFile returns the path to .gitspork.yml (or .yaml) in repoDir.
func FindGitSporkConfigFile(repoDir string) (string, error) {
	p := filepath.Join(repoDir, gitSporkConfigFileName)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	p = filepath.Join(repoDir, gitSporkConfigFileNameAlt)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no .gitspork.yml found in %s", repoDir)
}
```

- [ ] **Step 4: Register subcommands in `cmd/root.go`**

Add to the `init()` function in `cmd/root.go`:

```go
mvSubcommand := &MvSubcommand{}
rmSubcommand := &RmSubcommand{}
rootCmd.AddCommand(mvSubcommand.GetCmd())
rootCmd.AddCommand(rmSubcommand.GetCmd())
```

- [ ] **Step 5: Build**

```
go build ./...
```
Expected: clean build

- [ ] **Step 6: Run all tests**

```
go test ./... -v
```
Expected: all tests pass

- [ ] **Step 7: Commit**

```bash
git add cmd/mv.go cmd/rm.go cmd/root.go internal/upstream-mv-rm.go internal/upstream-mv-rm_test.go
git commit -m "feat: add gitspork mv and gitspork rm upstream helper subcommands"
```
