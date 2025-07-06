package internal

import (
	"fmt"
	"path/filepath"

	"dario.cat/mergo"
)

// IntegratorSharedOwnershipStructuredPreferUpstream will process a list of structured data files to be co-owned by upstream and downstream, merged with preference/precdence in favor of upstream
type IntegratorSharedOwnershipStructuredPreferUpstream struct{}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipStructuredPreferUpstream) Integrate(filePaths []string, upstreamRepoClonePath string, downstreamRepoClonePath string, logger *Logger) error {
	for _, integratePath := range filePaths {
		logger.Log("📄 gathering structured data for %s", integratePath)
		upstreamStructuredData, downstreamStructuredData, structuredDataType, err := getStructuredData(filepath.Join(upstreamRepoClonePath, integratePath), filepath.Join(downstreamRepoClonePath, integratePath))
		if err != nil {
			return err
		}
		logger.Log("🔧 merging upstream and downstream data, prefering upstream data")
		if err := mergo.Merge(downstreamStructuredData, upstreamStructuredData, mergo.WithOverride); err != nil {
			return fmt.Errorf("error merging structured data from %s to downstream: %v", integratePath, err)
		}
		if err := writeStructuredData(downstreamStructuredData, structuredDataType, filepath.Join(downstreamRepoClonePath, integratePath)); err != nil {
			return fmt.Errorf("error writing merged structured data: %v", err)
		}
	}
	return nil
}
