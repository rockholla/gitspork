package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/gobwas/glob"
	"github.com/goccy/go-yaml"
)

type upstreamRename struct {
	OldPath string
	NewPath string
}

type upstreamDelta struct {
	Deletions []string
	Renames   []upstreamRename
}

func computeUpstreamDelta(repo *gogit.Repository, prevHash, newHash string, config *GitSporkConfig, upstreamSubpath string) (*upstreamDelta, error) {
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
		prevConfig = config // fall back to new config if prev has none
	}
	prevManagedGlobs, err := buildManagedGlobs(prevConfig)
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
			if matchesAnyGlob(fromPath, prevManagedGlobs) {
				delta.Deletions = append(delta.Deletions, fromPath)
			}
		case merkletrie.Modify:
			// A Modify with different From/To names is a rename (after rename detection)
			if change.From.Name != change.To.Name {
				fromPath := stripSubpath(change.From.Name, upstreamSubpath)
				toPath := stripSubpath(change.To.Name, upstreamSubpath)
				if matchesAnyGlob(fromPath, prevManagedGlobs) {
					delta.Renames = append(delta.Renames, upstreamRename{OldPath: fromPath, NewPath: toPath})
				}
			}
		}
	}

	if err := applyTemplatedConfigDelta(prevCommit, newCommit, upstreamSubpath, delta); err != nil {
		return delta, err
	}

	return delta, nil
}

func buildManagedGlobs(config *GitSporkConfig) ([]glob.Glob, error) {
	var patterns []string
	patterns = append(patterns, config.UpstreamOwned...)
	patterns = append(patterns, config.SharedOwnership.Merged...)
	patterns = append(patterns, config.SharedOwnership.Structured.PreferUpstream...)
	patterns = append(patterns, config.SharedOwnership.Structured.PreferDownstream...)
	var compiled []glob.Glob
	for _, p := range patterns {
		g, err := glob.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q in .gitspork.yml: %v", p, err)
		}
		compiled = append(compiled, g)
	}
	return compiled, nil
}

func matchesAnyGlob(path string, globs []glob.Glob) bool {
	for _, g := range globs {
		if g.Match(path) {
			return true
		}
	}
	return false
}

func stripSubpath(path, subpath string) string {
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

	newByTemplate := map[string]GitSporkConfigTemplated{}
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

func readConfigFromCommit(commit *object.Commit, subpath string) (*GitSporkConfig, error) {
	tree, err := commit.Tree()
	if err != nil {
		return &GitSporkConfig{}, err
	}
	configPath := gitSporkConfigFileName
	if subpath != "" {
		configPath = subpath + "/" + gitSporkConfigFileName
	}
	f, err := tree.File(configPath)
	if err != nil {
		configPath = gitSporkConfigFileNameAlt
		if subpath != "" {
			configPath = subpath + "/" + gitSporkConfigFileNameAlt
		}
		f, err = tree.File(configPath)
		if err != nil {
			return &GitSporkConfig{}, fmt.Errorf("no .gitspork.yml found in commit tree")
		}
	}
	contents, err := f.Contents()
	if err != nil {
		return &GitSporkConfig{}, err
	}
	cfg := &GitSporkConfig{}
	if err := yaml.Unmarshal([]byte(contents), cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyUpstreamDelta(delta *upstreamDelta, downstreamPath string, logger *Logger) error {
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
