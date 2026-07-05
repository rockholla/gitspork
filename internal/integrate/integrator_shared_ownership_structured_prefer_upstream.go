package integrate

import (
	"fmt"
	"path/filepath"

	"dario.cat/mergo"
	"github.com/rockholla/gitspork/internal/types"
)

// IntegratorSharedOwnershipStructuredPreferUpstream will process a list of structured data files to be co-owned by upstream and downstream, merged with preference/precdence in favor of upstream
type IntegratorSharedOwnershipStructuredPreferUpstream struct{}

var _ Integrator[string] = (*IntegratorSharedOwnershipStructuredPreferUpstream)(nil)

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipStructuredPreferUpstream) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger types.Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamPath, configuredGlobPatterns, err)
	}
	for _, integrateFile := range integrateFiles {
		logger.Log("📝 gathering structured data for %s", integrateFile)
		upstreamStructuredData, downstreamStructuredData, structuredDataType, err := getStructuredData(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, integrateFile))
		if err != nil {
			return err
		}
		logger.Log("🔧 merging upstream and downstream data, prefering upstream data")
		if err := mergo.Merge(downstreamStructuredData, *upstreamStructuredData, mergo.WithOverride); err != nil {
			return fmt.Errorf("error merging structured data from %s to downstream: %v", integrateFile, err)
		}
		if err := writeStructuredData(upstreamStructuredData, structuredDataType, filepath.Join(downstreamPath, integrateFile)); err != nil {
			return fmt.Errorf("error writing merged structured data: %v", err)
		}
	}
	return nil
}
