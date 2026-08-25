package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveStructuredPath reads filePath (JSON or YAML, detected by extension),
// navigates to the dot-delimited dotPath within it, and returns the scalar
// value as a string. Returns ("", false, nil) when the file is absent, the
// path is not found, or the node at the path is not a scalar. Returns
// ("", false, err) when the file exists but cannot be parsed.
func resolveStructuredPath(filePath, dotPath string) (string, bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	var parseFn func([]byte) (*node, error)
	switch ext {
	case ".json":
		parseFn = parseJSON
	case ".yml", ".yaml":
		parseFn = parseYAML
	default:
		return "", false, fmt.Errorf("unsupported file extension %q for from_destination_structured (must be .json, .yml, or .yaml)", ext)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("error reading destination file %s: %v", filePath, err)
	}

	root, err := parseFn(data)
	if err != nil {
		return "", false, fmt.Errorf("error parsing destination file %s: %v", filePath, err)
	}

	current := root
	for _, seg := range strings.Split(dotPath, ".") {
		if current.kind != nodeMapping {
			return "", false, nil
		}
		child, ok := current.mapping.Get(seg)
		if !ok {
			return "", false, nil
		}
		current = child
	}

	if current.kind != nodeScalar {
		return "", false, nil
	}
	return fmt.Sprint(current.scalar), true, nil
}
