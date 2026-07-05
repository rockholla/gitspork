package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const templatedInputsCacheFileName = "templated-inputs.json"

// loadTemplatedInputs reads the consolidated templated-inputs cache at
// <downstream>/.gitspork/templated-inputs.json. Returns an empty map (not nil) and
// nil error if the file doesn't exist so callers can look up destinations without
// nil-checking.
func loadTemplatedInputs(downstreamPath string) (map[string]map[string]string, error) {
	path := filepath.Join(downstreamPath, gitSporkMetaDirName, templatedInputsCacheFileName)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	out := map[string]map[string]string{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return out, nil
}

// saveTemplatedInputs writes exactly the given map to the consolidated cache. Callers
// are responsible for including only currently-configured destinations — entries not
// in the passed map are pruned by construction. Go's json.Marshal sorts map keys
// alphabetically, so file bytes are deterministic across runs.
func saveTemplatedInputs(downstreamPath string, inputs map[string]map[string]string) error {
	dir := filepath.Join(downstreamPath, gitSporkMetaDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("ensuring %s: %w", dir, err)
	}
	b, err := json.Marshal(inputs)
	if err != nil {
		return fmt.Errorf("marshaling templated inputs: %w", err)
	}
	path := filepath.Join(dir, templatedInputsCacheFileName)
	return os.WriteFile(path, b, 0644)
}
