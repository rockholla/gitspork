package internal

import (
	"path/filepath"
)

// IntegratorUpstreamOwned will process a list of files to be managed as owned by the upstream gitspork repo
type IntegratorUpstreamOwned struct{}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorUpstreamOwned) Integrate(filePaths []string, upstreamRepoClonePath string, downstreamRepoClonePath string, logger *Logger) error {
	for _, integratePath := range filePaths {
		logger.Log("➡️ copying/overwriting %s to downstream", integratePath)
		if err := copyFile(filepath.Join(upstreamRepoClonePath, integratePath), filepath.Join(downstreamRepoClonePath, integratePath)); err != nil {
			return err
		}
	}
	return nil
}
