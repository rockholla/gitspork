package internal

import (
	"fmt"
	"path/filepath"
)

// IntegratorUpstreamOwned will process a list of files to be managed as owned by the upstream gitspork repo
type IntegratorUpstreamOwned struct{}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorUpstreamOwned) Integrate(configuredGlobPatterns []string, upstreamRepoPath string, downstreamRepoPath string, logger *Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamRepoPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamRepoPath, configuredGlobPatterns, err)
	}
	for _, integrateFile := range integrateFiles {
		logger.Log("➡️ copying/overwriting %s to downstream", integrateFile)
		if err := syncFile(filepath.Join(upstreamRepoPath, integrateFile), filepath.Join(downstreamRepoPath, integrateFile)); err != nil {
			return err
		}
	}
	return nil
}
