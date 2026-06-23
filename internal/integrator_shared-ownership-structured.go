package internal

import (
	"fmt"
	"path/filepath"

	"dario.cat/mergo"
)

// IntegratorSharedOwnershipStructured processes structured data files (json/yaml)
// co-owned by upstream and downstream, merging them with a configurable
// preference for which side wins on key collisions. PreferUpstream selects which
// side takes precedence; the merged union is always what gets written downstream.
type IntegratorSharedOwnershipStructured struct {
	PreferUpstream bool
}

var _ Integrator[string] = (*IntegratorSharedOwnershipStructured)(nil)

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipStructured) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger *Logger) error {
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
		// base holds the merged union (mergo merges the preferred side into it with
		// override, so the preferred values win); base is then what we write out.
		var base, preferred *map[string]any
		if i.PreferUpstream {
			logger.Log("🔧 merging upstream and downstream data, prefering upstream data")
			base, preferred = downstreamStructuredData, upstreamStructuredData
		} else {
			logger.Log("🔧 merging upstream and downstream data, prefering downstream data")
			base, preferred = upstreamStructuredData, downstreamStructuredData
		}
		if err := mergo.Merge(base, *preferred, mergo.WithOverride); err != nil {
			return fmt.Errorf("error merging structured data from %s to downstream: %v", integrateFile, err)
		}
		if err := writeStructuredData(base, structuredDataType, filepath.Join(downstreamPath, integrateFile)); err != nil {
			return fmt.Errorf("error writing merged structured data: %v", err)
		}
	}
	return nil
}
