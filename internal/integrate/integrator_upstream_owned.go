package integrate

import (
	"fmt"
	"path/filepath"

	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// IntegratorUpstreamOwned will process a list of files to be managed as owned by the upstream gitspork repo
type IntegratorUpstreamOwned struct {
	UpstreamOnly []string
}

var _ Integrator[config.OwnedEntry] = (*IntegratorUpstreamOwned)(nil)

// Integrate copies each upstream-owned file to the downstream, applying rename
// entries' destination resolution. Files matching any upstream_only pattern are
// skipped with a warning.
func (i *IntegratorUpstreamOwned) Integrate(entries []config.OwnedEntry, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error {
	for _, entry := range entries {
		integrateFiles, err := getIntegrateFiles(upstreamPath, []string{entry.SourcePattern()})
		if err != nil {
			return fmt.Errorf("error determining the list of files to integrate in %s from %q: %v", upstreamPath, entry.SourcePattern(), err)
		}
		integrateFiles, err = filterUpstreamOnly(integrateFiles, i.UpstreamOnly, logger)
		if err != nil {
			return err
		}
		for _, integrateFile := range integrateFiles {
			dest := entry.ResolveDest(integrateFile)
			if dest == integrateFile {
				logger.Log("➡️ copying/overwriting %s to downstream", integrateFile)
			} else {
				logger.Log("➡️ copying/overwriting %s to downstream as %s", integrateFile, dest)
			}
			if err := syncFile(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, dest)); err != nil {
				return err
			}
		}
	}
	return nil
}
