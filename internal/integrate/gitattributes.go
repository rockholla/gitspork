package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	gitAttributesFileName = ".gitattributes"
	gitsporkAttrMarker    = "# gitspork-managed: cache files under .gitspork/ are auto-generated"
	gitsporkAttrPattern   = ".gitspork/**/*.json"
	gitsporkAttrFlags     = "linguist-generated=true -diff merge=binary"
)

// ensureGitsporkAttributes writes or updates a .gitattributes file at atDir so that
// gitspork's cache files (.gitspork/**/*.json) are marked as generated for git and
// GitHub tooling. The operation is idempotent: if the file already contains our
// exact managed block, it is left untouched.
//
// If a user maintains their own .gitattributes rules, they are preserved. If a prior
// gitspork version wrote a different attribute set, the entry is replaced in place
// so downstreams upgrade cleanly without duplicate lines.
func ensureGitsporkAttributes(atDir string) error {
	path := filepath.Join(atDir, gitAttributesFileName)

	var existing []byte
	if b, err := os.ReadFile(path); err == nil {
		existing = b
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	filtered := filterGitsporkAttributeLines(string(existing))
	block := gitsporkAttrMarker + "\n" + gitsporkAttrPattern + " " + gitsporkAttrFlags + "\n"

	var next string
	switch {
	case filtered == "":
		next = block
	case strings.HasSuffix(filtered, "\n"):
		next = filtered + block
	default:
		next = filtered + "\n" + block
	}

	if next == string(existing) {
		return nil
	}
	return os.WriteFile(path, []byte(next), 0644)
}

// filterGitsporkAttributeLines returns content with any gitspork-managed marker
// comment or pattern-owned line removed, so ensureGitsporkAttributes can re-append
// a single fresh block regardless of the prior state.
func filterGitsporkAttributeLines(content string) string {
	if content == "" {
		return ""
	}
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == gitsporkAttrMarker {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == gitsporkAttrPattern {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return ""
	}
	out := strings.Join(kept, "\n")
	if trailingNewline {
		out += "\n"
	}
	return out
}
