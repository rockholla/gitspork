package internal

import (
	"strings"
)

// globNonWildcardPrefix returns the portion of a glob pattern before the first wildcard character.
// Returns empty string if the pattern begins with a wildcard.
func globNonWildcardPrefix(pattern string) string {
	for i, ch := range pattern {
		if ch == '*' || ch == '?' || ch == '[' {
			return strings.TrimSuffix(pattern[:i], "/")
		}
	}
	return pattern
}
