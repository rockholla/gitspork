package config

import (
	"fmt"
	"os"
	"strings"
)

// globNonWildcardPrefix returns the portion of a glob pattern before the first wildcard character.
// Returns empty string if the pattern begins with a wildcard.
func globNonWildcardPrefix(pattern string) string {
	for i, ch := range pattern {
		if ch == '*' || ch == '?' || ch == '[' {
			return strings.TrimSuffix(pattern[:i], "/")
		}
	}
	return pattern
}

// ComputeUpstreamMv returns the rewritten config and any warnings for a move from oldPath to newPath,
// without writing to disk. Use WriteGitSporkConfig to persist the result.
func ComputeUpstreamMv(configPath, oldPath, newPath string) (*GitSporkConfig, []string, error) {
	config, err := ParseGitSporkConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading config: %v", err)
	}
	return ComputeUpstreamMvFromConfig(config, oldPath, newPath)
}

// ComputeUpstreamMvFromConfig applies a move rewrite to an already-parsed config.
// Used to chain multiple source rewrites without re-reading the file between each.
func ComputeUpstreamMvFromConfig(config *GitSporkConfig, oldPath, newPath string) (*GitSporkConfig, []string, error) {
	// Shell tab-completion routinely appends "/" to directory arguments; strip it
	// so the pattern-prefix comparisons below match as the user intends.
	oldPath = strings.TrimSuffix(oldPath, "/")
	newPath = strings.TrimSuffix(newPath, "/")
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

	rewriteOwned := func(entries []OwnedEntry) []OwnedEntry {
		result := make([]OwnedEntry, len(entries))
		for i, e := range entries {
			src := e.SourcePattern()
			prefix := globNonWildcardPrefix(src)
			var newSrc string
			switch {
			case prefix == "":
				warnings = append(warnings, fmt.Sprintf("pattern %q has a leading wildcard — update manually", src))
				newSrc = src
			case src == oldPath:
				newSrc = newPath
			case prefix == oldPath:
				newSrc = newPath + src[len(oldPath):]
			case strings.HasPrefix(prefix, oldPath+"/"):
				newSrc = newPath + src[len(oldPath):]
			default:
				newSrc = src
			}
			if e.IsRename() {
				// To is a downstream landing path, not an upstream file, so mv never rewrites it.
				result[i] = OwnedEntry{From: newSrc, To: e.To}
			} else {
				result[i] = OwnedEntry{Pattern: newSrc}
			}
		}
		return result
	}

	config.UpstreamOwned = rewriteOwned(config.UpstreamOwned)
	config.DownstreamOwned = rewriteOwned(config.DownstreamOwned)
	config.SharedOwnership.Merged = rewritePatterns(config.SharedOwnership.Merged)
	config.SharedOwnership.Structured.PreferUpstream = rewritePatterns(config.SharedOwnership.Structured.PreferUpstream)
	config.SharedOwnership.Structured.PreferDownstream = rewritePatterns(config.SharedOwnership.Structured.PreferDownstream)

	rewritePath := func(p string) string {
		if p == oldPath {
			return newPath
		}
		if strings.HasPrefix(p, oldPath+"/") {
			return newPath + p[len(oldPath):]
		}
		return p
	}
	for i, t := range config.Templated {
		config.Templated[i].Template = rewritePath(t.Template)
		config.Templated[i].Destination = rewritePath(t.Destination)
	}

	return config, warnings, nil
}

// UpstreamMv updates the config at configPath to reflect a file/directory move from oldPath to newPath.
// It rewrites exact path entries, glob patterns whose non-wildcard prefix matches the old path,
// and emits warnings for patterns it can't automatically handle.
func UpstreamMv(configPath, oldPath, newPath string) ([]string, error) {
	config, warnings, err := ComputeUpstreamMv(configPath, oldPath, newPath)
	if err != nil {
		return nil, err
	}
	return warnings, WriteGitSporkConfig(configPath, config)
}

// ComputeUpstreamRm returns the rewritten config and any warnings for a removal of path,
// without writing to disk. Use WriteGitSporkConfig to persist the result.
func ComputeUpstreamRm(configPath, path string, recursive bool) (*GitSporkConfig, []string, error) {
	config, err := ParseGitSporkConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading config: %v", err)
	}
	return ComputeUpstreamRmFromConfig(config, path, recursive)
}

// ComputeUpstreamRmFromConfig applies a removal to an already-parsed config.
// Used to chain multiple path removals without re-reading the file between each.
func ComputeUpstreamRmFromConfig(config *GitSporkConfig, path string, recursive bool) (*GitSporkConfig, []string, error) {
	// Shell tab-completion routinely appends "/" to directory arguments; strip it
	// so the pattern-prefix comparisons below match as the user intends.
	path = strings.TrimSuffix(path, "/")
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
			}
			result = append(result, p)
		}
		return result
	}

	filterOwned := func(entries []OwnedEntry) []OwnedEntry {
		var result []OwnedEntry
		for _, e := range entries {
			src := e.SourcePattern()
			if src == path {
				continue
			}
			if recursive {
				prefix := globNonWildcardPrefix(src)
				if prefix == "" {
					warnings = append(warnings, fmt.Sprintf("pattern %q has a leading wildcard — update manually", src))
					result = append(result, e)
					continue
				}
				if prefix == path || strings.HasPrefix(prefix, path+"/") {
					continue
				}
			}
			result = append(result, e)
		}
		return result
	}

	config.UpstreamOwned = filterOwned(config.UpstreamOwned)
	config.DownstreamOwned = filterOwned(config.DownstreamOwned)
	config.SharedOwnership.Merged = filterPatterns(config.SharedOwnership.Merged)
	config.SharedOwnership.Structured.PreferUpstream = filterPatterns(config.SharedOwnership.Structured.PreferUpstream)
	config.SharedOwnership.Structured.PreferDownstream = filterPatterns(config.SharedOwnership.Structured.PreferDownstream)

	var templated []GitSporkConfigTemplated
	for _, t := range config.Templated {
		if t.Template == path || t.Destination == path {
			continue
		}
		if recursive && (strings.HasPrefix(t.Template, path+"/") || strings.HasPrefix(t.Destination, path+"/")) {
			continue
		}
		templated = append(templated, t)
	}
	config.Templated = templated

	return config, warnings, nil
}

// UpstreamRm updates .gitspork.yml at configPath to remove entries matching path.
// If recursive is true, also removes entries whose non-wildcard prefix falls under path.
func UpstreamRm(configPath, path string, recursive bool) ([]string, error) {
	config, warnings, err := ComputeUpstreamRm(configPath, path, recursive)
	if err != nil {
		return nil, err
	}
	return warnings, WriteGitSporkConfig(configPath, config)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
