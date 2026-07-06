package integrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/gobwas/glob"
	"github.com/goccy/go-yaml"
	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

type upstreamRename struct {
	OldPath string
	NewPath string
}

type upstreamDelta struct {
	Deletions []string
	Renames   []upstreamRename
}

func computeUpstreamDelta(repo *gogit.Repository, prevHash, newHash string, cfg *config.GitSporkConfig, upstreamSubpath string) (*upstreamDelta, error) {
	// Users may specify UpstreamSpec.Subpath with leading or trailing slashes
	// (e.g. "infra/"). Normalize once here so downstream helpers don't have to.
	upstreamSubpath = strings.Trim(upstreamSubpath, "/")
	delta := &upstreamDelta{}
	if prevHash == "" {
		return delta, nil
	}

	prevCommit, err := repo.CommitObject(plumbing.NewHash(prevHash))
	if err != nil {
		return delta, nil // prevHash not in history — skip delta silently
	}
	newCommit, err := repo.CommitObject(plumbing.NewHash(newHash))
	if err != nil {
		return delta, fmt.Errorf("error resolving new upstream commit %s: %v", newHash, err)
	}

	// Deletions/renames must be checked against the OLD config: a file removed by
	// "gitspork rm" is stripped from .gitspork.yml in the same commit, so the new
	// config no longer lists it. Using the prev config ensures those removals still
	// propagate to downstream repos.
	prevConfig, err := readConfigFromCommit(prevCommit, upstreamSubpath)
	if err != nil {
		prevConfig = cfg // fall back to new config if prev has none
	}
	prevMatchers, err := buildManagedMatchers(prevConfig)
	if err != nil {
		return delta, err
	}
	newMatchers, err := buildManagedMatchers(cfg)
	if err != nil {
		return delta, err
	}

	prevTree, err := prevCommit.Tree()
	if err != nil {
		return delta, fmt.Errorf("error getting prev commit tree: %v", err)
	}
	newTree, err := newCommit.Tree()
	if err != nil {
		return delta, fmt.Errorf("error getting new commit tree: %v", err)
	}

	changes, err := object.DiffTreeWithOptions(context.Background(), prevTree, newTree, object.DefaultDiffTreeOptions)
	if err != nil {
		return delta, fmt.Errorf("error computing tree diff: %v", err)
	}

	for _, change := range changes {
		action, err := change.Action()
		if err != nil {
			return delta, fmt.Errorf("error determining action for change: %v", err)
		}

		switch action {
		case merkletrie.Delete:
			fromPath := stripSubpath(change.From.Name, upstreamSubpath)
			if dest, ok := resolveManagedDest(fromPath, prevMatchers); ok {
				delta.Deletions = append(delta.Deletions, dest)
			}
		case merkletrie.Modify:
			// A Modify with different From/To names is a rename (after rename detection)
			if change.From.Name != change.To.Name {
				fromPath := stripSubpath(change.From.Name, upstreamSubpath)
				toPath := stripSubpath(change.To.Name, upstreamSubpath)
				oldDest, ok := resolveManagedDest(fromPath, prevMatchers)
				if !ok {
					break
				}
				newDest, ok := resolveManagedDest(toPath, newMatchers)
				if !ok {
					// toPath is no longer covered by any new config entry; fall back to the
					// raw upstream path so the downstream file moves rather than being orphaned.
					newDest = toPath
				}
				if oldDest != newDest {
					delta.Renames = append(delta.Renames, upstreamRename{OldPath: oldDest, NewPath: newDest})
				}
			}
		}
	}

	if err := applyTemplatedConfigDelta(prevCommit, newCommit, upstreamSubpath, delta); err != nil {
		return delta, err
	}

	return delta, nil
}

type managedMatcher struct {
	glob  glob.Glob
	entry *config.OwnedEntry // non-nil only for rename entries; nil means identity dest
}

func buildManagedMatchers(cfg *config.GitSporkConfig) ([]managedMatcher, error) {
	var matchers []managedMatcher
	for i := range cfg.UpstreamOwned {
		e := cfg.UpstreamOwned[i]
		g, err := glob.Compile(e.SourcePattern())
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q in .gitspork.yml: %v", e.SourcePattern(), err)
		}
		var ref *config.OwnedEntry
		if e.IsRename() {
			ref = &e
		}
		matchers = append(matchers, managedMatcher{glob: g, entry: ref})
	}
	var plain []string
	plain = append(plain, cfg.SharedOwnership.Merged...)
	plain = append(plain, cfg.SharedOwnership.Structured.PreferUpstream...)
	plain = append(plain, cfg.SharedOwnership.Structured.PreferDownstream...)
	for _, p := range plain {
		g, err := glob.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q in .gitspork.yml: %v", p, err)
		}
		matchers = append(matchers, managedMatcher{glob: g})
	}
	return matchers, nil
}

// resolveManagedDest returns the downstream destination for an upstream source
// path if any managed matcher matches it.
func resolveManagedDest(srcPath string, matchers []managedMatcher) (string, bool) {
	for _, m := range matchers {
		if m.glob.Match(srcPath) {
			if m.entry != nil {
				return m.entry.ResolveDest(srcPath), true
			}
			return srcPath, true
		}
	}
	return "", false
}

func stripSubpath(path, subpath string) string {
	// Defense-in-depth: normalize slashes even though computeUpstreamDelta
	// already trims its input, so a future caller can't silently break the
	// prefix comparison by passing "infra/".
	subpath = strings.Trim(subpath, "/")
	if subpath == "" {
		return path
	}
	prefix := subpath + "/"
	if len(path) > len(prefix) && path[:len(prefix)] == prefix {
		return path[len(prefix):]
	}
	return path
}

func applyTemplatedConfigDelta(prevCommit, newCommit *object.Commit, upstreamSubpath string, delta *upstreamDelta) error {
	prevConfig, err := readConfigFromCommit(prevCommit, upstreamSubpath)
	if err != nil {
		// No config file in prev commit — nothing to compare
		return nil
	}
	newConfig, err := readConfigFromCommit(newCommit, upstreamSubpath)
	if err != nil {
		// No config file in new commit — treat all prev templated entries as deleted
		for _, prev := range prevConfig.Templated {
			delta.Deletions = append(delta.Deletions, prev.Destination)
		}
		return nil
	}

	newByTemplate := map[string]config.GitSporkConfigTemplated{}
	for _, t := range newConfig.Templated {
		newByTemplate[t.Template] = t
	}

	for _, prev := range prevConfig.Templated {
		next, exists := newByTemplate[prev.Template]
		if !exists {
			delta.Deletions = append(delta.Deletions, prev.Destination)
			continue
		}
		if next.Destination != prev.Destination {
			delta.Renames = append(delta.Renames, upstreamRename{OldPath: prev.Destination, NewPath: next.Destination})
		}
	}
	return nil
}

func readConfigFromCommit(commit *object.Commit, subpath string) (*config.GitSporkConfig, error) {
	// Defense-in-depth: normalize slashes so a "foo/" or "/foo" subpath doesn't
	// produce a "foo//.gitspork.yml" lookup that misses the file.
	subpath = strings.Trim(subpath, "/")
	tree, err := commit.Tree()
	if err != nil {
		return &config.GitSporkConfig{}, err
	}
	configPath := config.GitSporkConfigFileName
	if subpath != "" {
		configPath = subpath + "/" + config.GitSporkConfigFileName
	}
	f, err := tree.File(configPath)
	if err != nil {
		configPath = config.GitSporkConfigFileNameAlt
		if subpath != "" {
			configPath = subpath + "/" + config.GitSporkConfigFileNameAlt
		}
		f, err = tree.File(configPath)
		if err != nil {
			return &config.GitSporkConfig{}, fmt.Errorf("no .gitspork.yml found in commit tree")
		}
	}
	contents, err := f.Contents()
	if err != nil {
		return &config.GitSporkConfig{}, err
	}
	cfg := &config.GitSporkConfig{}
	if err := yaml.Unmarshal([]byte(contents), cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyUpstreamDelta(delta *upstreamDelta, downstreamPath string, logger sdktypes.Logger) error {
	for _, del := range delta.Deletions {
		target := filepath.Join(downstreamPath, del)
		if _, err := os.Stat(target); os.IsNotExist(err) {
			logger.Log("⚠️  delta: %s already absent in downstream, skipping removal", del)
			continue
		}
		logger.Log("🗑️  delta: removing %s from downstream", del)
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("error removing %s from downstream: %v", del, err)
		}
	}

	for _, ren := range delta.Renames {
		oldTarget := filepath.Join(downstreamPath, ren.OldPath)
		newTarget := filepath.Join(downstreamPath, ren.NewPath)
		if _, err := os.Stat(newTarget); err == nil {
			logger.Log("⚠️  delta: rename target %s already exists in downstream, skipping move", ren.NewPath)
			continue
		}
		if _, err := os.Stat(oldTarget); os.IsNotExist(err) {
			logger.Log("⚠️  delta: rename source %s absent in downstream, skipping move", ren.OldPath)
			continue
		}
		logger.Log("📦 delta: moving %s → %s in downstream", ren.OldPath, ren.NewPath)
		if err := os.MkdirAll(filepath.Dir(newTarget), 0755); err != nil {
			return fmt.Errorf("error creating directory for %s: %v", ren.NewPath, err)
		}
		if err := os.Rename(oldTarget, newTarget); err != nil {
			return fmt.Errorf("error moving %s to %s: %v", ren.OldPath, ren.NewPath, err)
		}
	}

	return nil
}
