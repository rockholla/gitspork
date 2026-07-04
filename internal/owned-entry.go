package internal

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// OwnedEntry is a single entry in an ownership list (upstream_owned or
// downstream_owned). It is either a plain glob pattern (Pattern, from a YAML
// scalar) or a rename (From/To, from a {from, to} YAML map). The forms are
// mutually exclusive. The type is ownership-neutral: it describes a path/rename,
// not a policy — the difference between the two lists lives in their integrators.
//
// The yaml/comment struct tags are consumed ONLY by the reflection-based
// marshal.YAMLWithComments schema renderer. goccy uses the custom
// UnmarshalYAML/MarshalYAML below, which ignore tags.
type OwnedEntry struct {
	Pattern string `yaml:"pattern,omitempty" comment:"a single glob file pattern"`
	From    string `yaml:"from,omitempty" comment:"(rename) upstream source glob/path"`
	To      string `yaml:"to,omitempty" comment:"(rename) downstream destination glob/path"`
}

// IsRename reports whether the entry renames a file (From/To form).
func (e OwnedEntry) IsRename() bool { return e.From != "" }

// SourcePattern returns the glob matched against the upstream tree.
func (e OwnedEntry) SourcePattern() string {
	if e.IsRename() {
		return e.From
	}
	return e.Pattern
}

// ResolveDest returns the downstream destination path for an upstream file that
// matched this entry's SourcePattern. Plain entries map to the same path; rename
// entries swap the source pattern's non-wildcard prefix for the destination's,
// preserving the remainder (prefix substitution).
func (e OwnedEntry) ResolveDest(matchedFile string) string {
	if !e.IsRename() {
		return matchedFile
	}
	srcPrefix := globNonWildcardPrefix(e.From)
	dstPrefix := globNonWildcardPrefix(e.To)
	return dstPrefix + strings.TrimPrefix(matchedFile, srcPrefix)
}

// Validate reports a configuration error if the entry is malformed: a rename
// must set both From and To, and the two sides must agree on whether they are
// globs (both contain a wildcard or neither does). An asymmetric rename — a glob
// source with a scalar destination, or vice versa — silently produces malformed
// destination paths in ResolveDest, so it is rejected at parse time instead.
func (e OwnedEntry) Validate() error {
	if e.From == "" && e.To == "" {
		if e.Pattern == "" {
			return fmt.Errorf("ownership entry is empty: provide a pattern or a {from, to} rename")
		}
		return nil
	}
	if e.Pattern != "" {
		return fmt.Errorf("ownership entry is either a pattern or a {from, to} rename, not both (got pattern=%q from=%q to=%q)", e.Pattern, e.From, e.To)
	}
	if e.From == "" || e.To == "" {
		return fmt.Errorf("rename entry must set both 'from' and 'to' (got from=%q to=%q)", e.From, e.To)
	}
	fromGlob := globNonWildcardPrefix(e.From) != e.From
	toGlob := globNonWildcardPrefix(e.To) != e.To
	if fromGlob != toGlob {
		return fmt.Errorf("rename entry 'from' and 'to' must both be globs or both be exact paths (got from=%q to=%q)", e.From, e.To)
	}
	return nil
}

// UnmarshalYAML accepts either a scalar (plain pattern) or a {from, to} map.
func (e *OwnedEntry) UnmarshalYAML(b []byte) error {
	var s string
	if err := yaml.Unmarshal(b, &s); err == nil {
		e.Pattern = s
		return nil
	}
	var m struct {
		From string `yaml:"from"`
		To   string `yaml:"to"`
	}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return err
	}
	e.From, e.To = m.From, m.To
	return nil
}

// MarshalYAML emits a scalar for plain entries and a {from, to} map for renames.
func (e OwnedEntry) MarshalYAML() ([]byte, error) {
	if e.IsRename() {
		return yaml.Marshal(yaml.MapSlice{
			{Key: "from", Value: e.From},
			{Key: "to", Value: e.To},
		})
	}
	return yaml.Marshal(e.Pattern)
}

// collapsePlainOwnedEntries rewrites reflection-rendered `- pattern: "X"` lines
// within the upstream_owned: and downstream_owned: blocks of schema output back
// to bare scalars `- "X"`, leaving {from, to} rename entries and other sections
// untouched. Needed because marshal.YAMLWithComments is reflection-based and
// ignores OwnedEntry.MarshalYAML. The block-start check below lists both keys and
// runs before the block-end check, so each owned block is handled independently.
var ownedEntryPatternLineRE = regexp.MustCompile(`^(\s*)- pattern: (".*?"|\S+)(\s*#.*)?$`)

func collapsePlainOwnedEntries(schema string) string {
	lines := strings.Split(schema, "\n")
	inBlock := false
	for i, line := range lines {
		if strings.HasPrefix(line, "upstream_owned:") || strings.HasPrefix(line, "downstream_owned:") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		// A list item or its continuation stays in the block; a non-indented,
		// non-list, non-blank line is a new top-level key and ends the block.
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "" {
			inBlock = false
			continue
		}
		if m := ownedEntryPatternLineRE.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + "- " + m[2]
		}
	}
	return strings.Join(lines, "\n")
}
