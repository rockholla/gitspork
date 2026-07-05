package integrate

import (
	"fmt"
	"path/filepath"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// IntegratorSharedOwnershipStructuredPreferDownstream will process a list of structured data files to be co-owned by upstream and downstream, merged with preference/precdence in favor of downstream
type IntegratorSharedOwnershipStructuredPreferDownstream struct{}

var _ Integrator[string] = (*IntegratorSharedOwnershipStructuredPreferDownstream)(nil)

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipStructuredPreferDownstream) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamPath, configuredGlobPatterns, err)
	}
	for _, integrateFile := range integrateFiles {
		logger.Log("📝 gathering structured data for %s", integrateFile)
		upstreamData, downstreamData, structuredDataType, err := getStructuredData(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, integrateFile))
		if err != nil {
			return err
		}
		logger.Log("🔧 merging upstream and downstream data, prefering downstream data")
		merged := mergeNodes(upstreamData, downstreamData, true)
		if err := writeStructuredData(merged, structuredDataType, filepath.Join(downstreamPath, integrateFile)); err != nil {
			return fmt.Errorf("error writing merged structured data: %v", err)
		}
	}
	return nil
}
