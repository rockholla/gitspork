package config

import (
	"path"
	"strings"
)

// NormalizeUpstreamPath returns a canonical, forward-slash logical path for
// user-supplied inputs like mv/rm arguments and UpstreamSpec.Subpath values.
//
// It leans on path.Clean to collapse "//" segments, resolve "." and ".."
// segments, and strip trailing slashes — all of which shell tab-completion,
// autocomplete, or human error routinely produce. It also normalizes a leading
// slash away (git tree entries and .gitspork.yml patterns never carry one) and
// maps the "no path" input to the empty string rather than path.Clean's "."
// sentinel, so callers can special-case the root/no-subpath case naturally.
//
// Examples:
//
//	""              -> ""
//	"/"             -> ""
//	"docs/foo"      -> "docs/foo"
//	"docs/foo/"     -> "docs/foo"
//	"/docs/foo"     -> "docs/foo"
//	"./docs/foo"    -> "docs/foo"
//	"docs//foo"     -> "docs/foo"
//	"docs/./foo"    -> "docs/foo"
//	"docs/bar/../foo" -> "docs/foo"
func NormalizeUpstreamPath(p string) string {
	if p == "" {
		return ""
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return ""
	}
	return strings.TrimPrefix(cleaned, "/")
}
