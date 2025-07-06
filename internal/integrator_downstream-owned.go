package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

// IntegratorDownstreamOwned will process a list of files to be managed as owned by the downstream gitspork repo, just initially bootstrapped by the upstream
type IntegratorDownstreamOwned struct{}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorDownstreamOwned) Integrate(configuredGlobPatterns []string, upstreamRepoPath string, downstreamRepoPath string, logger *Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamRepoPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamRepoPath, configuredGlobPatterns, err)
	}
	for _, integrateFile := range integrateFiles {
		destination := filepath.Join(downstreamRepoPath, integrateFile)
		if _, err := os.Stat(destination); os.IsNotExist(err) {
			logger.Log("➡️ copying %s one time to downstream", integrateFile)
			if err := syncFile(filepath.Join(upstreamRepoPath, integrateFile), destination); err != nil {
				return err
			}
		} else {
			logger.Log("🔒 downstream-owned file %s exists, not doing anything", integrateFile)
		}
	}
	return nil
}
