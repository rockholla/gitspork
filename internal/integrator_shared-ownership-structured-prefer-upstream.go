package internal

import (
	"fmt"
	"path/filepath"

	"dario.cat/mergo"
)

// IntegratorSharedOwnershipStructuredPreferUpstream will process a list of structured data files to be co-owned by upstream and downstream, merged with preference/precdence in favor of upstream
type IntegratorSharedOwnershipStructuredPreferUpstream struct{}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipStructuredPreferUpstream) Integrate(configuredGlobPatterns []string, upstreamRepoPath string, downstreamRepoPath string, logger *Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamRepoPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamRepoPath, configuredGlobPatterns, err)
	}
	for _, integrateFile := range integrateFiles {
		logger.Log("📝 gathering structured data for %s", integrateFile)
		upstreamStructuredData, downstreamStructuredData, structuredDataType, err := getStructuredData(filepath.Join(upstreamRepoPath, integrateFile), filepath.Join(downstreamRepoPath, integrateFile))
		if err != nil {
			return err
		}
		logger.Log("🔧 merging upstream and downstream data, prefering upstream data")
		if err := mergo.Merge(downstreamStructuredData, *upstreamStructuredData, mergo.WithOverride); err != nil {
			return fmt.Errorf("error merging structured data from %s to downstream: %v", integrateFile, err)
		}
		if err := writeStructuredData(upstreamStructuredData, structuredDataType, filepath.Join(downstreamRepoPath, integrateFile)); err != nil {
			return fmt.Errorf("error writing merged structured data: %v", err)
		}
	}
	return nil
}
