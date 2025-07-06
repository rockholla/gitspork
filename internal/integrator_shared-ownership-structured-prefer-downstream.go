package internal

import (
	"fmt"
	"path/filepath"

	"dario.cat/mergo"
)

// IntegratorSharedOwnershipStructuredPreferDownstream will process a list of structured data files to be co-owned by upstream and downstream, merged with preference/precdence in favor of downstream
type IntegratorSharedOwnershipStructuredPreferDownstream struct{}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipStructuredPreferDownstream) Integrate(filePaths []string, upstreamRepoClonePath string, downstreamRepoClonePath string, logger *Logger) error {
	for _, integratePath := range filePaths {
		logger.Log("📄 gathering structured data for %s", integratePath)
		upstreamStructuredData, downstreamStructuredData, structuredDataType, err := getStructuredData(filepath.Join(upstreamRepoClonePath, integratePath), filepath.Join(downstreamRepoClonePath, integratePath))
		if err != nil {
			return err
		}
		logger.Log("🔧 merging upstream and downstream data, prefering downstream data")
		if err := mergo.Merge(upstreamStructuredData, *downstreamStructuredData, mergo.WithOverride); err != nil {
			return fmt.Errorf("error merging structured data from %s to downstream: %v", integratePath, err)
		}
		if err := writeStructuredData(upstreamStructuredData, structuredDataType, filepath.Join(downstreamRepoClonePath, integratePath)); err != nil {
			return fmt.Errorf("error writing merged structured data: %v", err)
		}
	}
	return nil
}
