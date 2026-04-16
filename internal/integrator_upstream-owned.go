package internal

import (
	"fmt"
	"path/filepath"
	"strings"
)

// IntegratorUpstreamOwned will process a list of files to be managed as owned by the upstream gitspork repo
type IntegratorUpstreamOwned struct{}

// parseUpstreamOwnedEntry parses an upstream_owned entry which can be either:
// - "file.txt" (simple pattern)
// - "source.txt:destination.txt" (rename syntax)
func parseUpstreamOwnedEntry(entry string) (pattern string, destPattern string) {
	if strings.Contains(entry, ":") {
		parts := strings.SplitN(entry, ":", 2)
		return parts[0], parts[1]
	}
	return entry, entry
}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorUpstreamOwned) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger *Logger) error {
	// Build a map of source patterns to destination patterns
	patternMap := make(map[string]string)
	sourcePatterns := make([]string, 0, len(configuredGlobPatterns))

	for _, entry := range configuredGlobPatterns {
		sourcePattern, destPattern := parseUpstreamOwnedEntry(entry)
		sourcePatterns = append(sourcePatterns, sourcePattern)
		patternMap[sourcePattern] = destPattern
	}

	integrateFiles, err := getIntegrateFiles(upstreamPath, sourcePatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamPath, sourcePatterns, err)
	}

	for _, integrateFile := range integrateFiles {
		// Find which pattern matched this file and get its destination pattern
		destFile := integrateFile
		for sourcePattern, destPattern := range patternMap {
			matched, _ := filepath.Match(sourcePattern, integrateFile)
			if matched && sourcePattern != destPattern {
				destFile = destPattern
				break
			}
		}

		if destFile != integrateFile {
			logger.Log("➡️ copying/overwriting %s to downstream as %s", integrateFile, destFile)
		} else {
			logger.Log("➡️ copying/overwriting %s to downstream", integrateFile)
		}

		if err := syncFile(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, destFile)); err != nil {
			return err
		}
	}
	return nil
}
