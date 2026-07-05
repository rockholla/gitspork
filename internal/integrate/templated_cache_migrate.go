package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migrateLegacyTemplatedCache folds any per-destination JSON files under
// <downstream>/.gitspork/ (a shape used by prior gitspork versions) into the
// consolidated templated-inputs.json cache and deletes the originals. Idempotent —
// no work is done if no legacy files exist.
//
// The two current-schema files (downstream-state.json at the .gitspork root and
// templated-inputs.json) are left alone. Any other *.json under .gitspork/ (at any
// depth) is treated as legacy: the file path relative to .gitspork/ minus the .json
// suffix becomes the destination key.
func migrateLegacyTemplatedCache(downstreamPath string) error {
	cacheDir := filepath.Join(downstreamPath, gitSporkMetaDirName)
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil
	}

	legacyFiles, err := findLegacyTemplatedCacheFiles(cacheDir)
	if err != nil {
		return err
	}
	if len(legacyFiles) == 0 {
		return nil
	}

	consolidated, err := loadTemplatedInputs(downstreamPath)
	if err != nil {
		return err
	}

	for _, legacyPath := range legacyFiles {
		rel, err := filepath.Rel(cacheDir, legacyPath)
		if err != nil {
			return fmt.Errorf("resolving legacy cache path %s: %w", legacyPath, err)
		}
		destination := strings.TrimSuffix(filepath.ToSlash(rel), ".json")

		b, err := os.ReadFile(legacyPath)
		if err != nil {
			return fmt.Errorf("reading legacy cache %s: %w", legacyPath, err)
		}
		var legacy struct {
			Inputs map[string]string `json:"inputs"`
		}
		if err := json.Unmarshal(b, &legacy); err != nil {
			return fmt.Errorf("parsing legacy cache %s: %w", legacyPath, err)
		}
		if legacy.Inputs == nil {
			legacy.Inputs = map[string]string{}
		}
		consolidated[destination] = legacy.Inputs
	}

	if err := saveTemplatedInputs(downstreamPath, consolidated); err != nil {
		return err
	}
	for _, legacyPath := range legacyFiles {
		if err := os.Remove(legacyPath); err != nil {
			return fmt.Errorf("removing legacy cache %s: %w", legacyPath, err)
		}
	}
	return nil
}

func findLegacyTemplatedCacheFiles(cacheDir string) ([]string, error) {
	var out []string
	err := filepath.Walk(cacheDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		rel, err := filepath.Rel(cacheDir, path)
		if err != nil {
			return err
		}
		if rel == downstreamStateFileName || rel == templatedInputsCacheFileName {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", cacheDir, err)
	}
	return out, nil
}
