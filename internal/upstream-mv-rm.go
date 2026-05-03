package internal

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
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

// upstreamMv updates the config at configPath to reflect a file/directory move from oldPath to newPath.
// It rewrites exact path entries, glob patterns whose non-wildcard prefix matches the old path,
// and emits warnings for patterns it can't automatically handle.
// The repoDir parameter is included for future use (cmd layer needs it for FindGitSporkConfigFile).
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
	config.DownstreamOwned = rewritePatterns(config.DownstreamOwned)
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
			}
			result = append(result, p)
		}
		return result
	}

	config.UpstreamOwned = filterPatterns(config.UpstreamOwned)
	config.DownstreamOwned = filterPatterns(config.DownstreamOwned)
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

func writeConfigFile(configPath string, config *GitSporkConfig) error {
	b, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("error marshalling config: %v", err)
	}
	return os.WriteFile(configPath, b, 0644)
}
