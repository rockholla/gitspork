package internal

import (
	"fmt"
	"path/filepath"
)

// IntegratorUpstreamOwned will process a list of files to be managed as owned by the upstream gitspork repo
type IntegratorUpstreamOwned struct{}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorUpstreamOwned) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger *Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamPath, configuredGlobPatterns, err)
	}
	for _, integrateFile := range integrateFiles {
		logger.Log("➡️ copying/overwriting %s to downstream", integrateFile)
		if err := syncFile(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, integrateFile)); err != nil {
			return err
		}
	}
	return nil
}
