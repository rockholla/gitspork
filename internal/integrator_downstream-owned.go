package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

// IntegratorDownstreamOwned will process a list of files to be managed as owned by the downstream gitspork repo, just initially bootstrapped by the upstream
type IntegratorDownstreamOwned struct{}

var _ Integrator[OwnedEntry] = (*IntegratorDownstreamOwned)(nil)

// Integrate seeds each downstream-owned file from the upstream a single time,
// applying rename entries' destination resolution. A file is only copied when
// its downstream destination does not already exist — the downstream owns it
// thereafter.
func (i *IntegratorDownstreamOwned) Integrate(entries []OwnedEntry, upstreamPath string, downstreamPath string, logger *Logger) error {
	for _, entry := range entries {
		integrateFiles, err := getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
		if err != nil {
			return fmt.Errorf("error determining the list of files to integrate in %s from %q: %v", upstreamPath, entry.SourcePattern(), err)
		}
		for _, integrateFile := range integrateFiles {
			dest := entry.ResolveDest(integrateFile)
			destination := filepath.Join(downstreamPath, dest)
			if _, err := os.Stat(destination); os.IsNotExist(err) {
				if dest == integrateFile {
					logger.Log("➡️ copying %s one time to downstream", integrateFile)
				} else {
					logger.Log("➡️ copying %s one time to downstream as %s", integrateFile, dest)
				}
				if err := syncFile(filepath.Join(upstreamPath, integrateFile), destination); err != nil {
					return err
				}
			} else {
				logger.Log("🔒 downstream-owned file %s exists, not doing anything", dest)
			}
		}
	}
	return nil
}
