package internal

import (
	"os"
	"path/filepath"
)

// IntegratorDownstreamOwned will process a list of files to be managed as owned by the downstream gitspork repo, just initially bootstrapped by the upstream
type IntegratorDownstreamOwned struct{}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorDownstreamOwned) Integrate(filePaths []string, upstreamRepoClonePath string, downstreamRepoClonePath string, logger *Logger) error {
	for _, integratePath := range filePaths {
		destination := filepath.Join(downstreamRepoClonePath, integratePath)
		if _, err := os.Stat(destination); os.IsNotExist(err) {
			logger.Log("➡️ copying %s one time to downstream", integratePath)
			if err := copyFile(filepath.Join(upstreamRepoClonePath, integratePath), destination); err != nil {
				return err
			}
		} else {
			logger.Log("🔒 downstream-owned file %s exists, not doing anything", integratePath)
		}

	}
	return nil
}
