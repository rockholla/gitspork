package internal

import (
	"context"
	"fmt"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/gobwas/glob"
	"gopkg.in/yaml.v2"
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

	managedGlobs := buildManagedGlobs(config)

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
			continue
		}

		switch action {
		case merkletrie.Delete:
			fromPath := stripSubpath(change.From.Name, upstreamSubpath)
			if matchesAnyGlob(fromPath, managedGlobs) {
				delta.Deletions = append(delta.Deletions, fromPath)
			}
		case merkletrie.Modify:
			// A Modify with different From/To names is a rename (after rename detection)
			if change.From.Name != change.To.Name {
				fromPath := stripSubpath(change.From.Name, upstreamSubpath)
				toPath := stripSubpath(change.To.Name, upstreamSubpath)
				if matchesAnyGlob(fromPath, managedGlobs) {
					delta.Renames = append(delta.Renames, upstreamRename{OldPath: fromPath, NewPath: toPath})
				}
			}
		}
	}

	if err := applyTemplatedConfigDelta(repo, prevCommit, newCommit, upstreamSubpath, delta); err != nil {
		return delta, err
	}

	return delta, nil
}

func buildManagedGlobs(config *GitSporkConfig) []string {
	var patterns []string
	patterns = append(patterns, config.UpstreamOwned...)
	patterns = append(patterns, config.SharedOwnership.Merged...)
	patterns = append(patterns, config.SharedOwnership.Structured.PreferUpstream...)
	patterns = append(patterns, config.SharedOwnership.Structured.PreferDownstream...)
	return patterns
}

func matchesAnyGlob(path string, patterns []string) bool {
	for _, pattern := range patterns {
		g, err := glob.Compile(pattern)
		if err != nil {
			continue
		}
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

func applyTemplatedConfigDelta(repo *gogit.Repository, prevCommit, newCommit *object.Commit, upstreamSubpath string, delta *upstreamDelta) error {
	prevConfig, err := readConfigFromCommit(repo, prevCommit, upstreamSubpath)
	if err != nil {
		// No config file in prev commit — nothing to compare
		return nil
	}
	newConfig, err := readConfigFromCommit(repo, newCommit, upstreamSubpath)
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

func readConfigFromCommit(repo *gogit.Repository, commit *object.Commit, subpath string) (*GitSporkConfig, error) {
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
	return nil
}
