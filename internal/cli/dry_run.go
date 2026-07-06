package cli

import "strings"

// isDryRun reports whether args carries a git-style dry-run flag: the long
// form `--dry-run`, a bare `-n`, or `-n` bundled into a short-flag group
// like `-rn`, `-nf`, or `-rfn`. Everything after a `--` separator is treated
// as positional and not scanned.
func isDryRun(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if len(a) < 2 || a[0] != '-' {
			continue
		}
		if a == "--dry-run" {
			return true
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		if strings.ContainsRune(a[1:], 'n') {
			return true
		}
	}
	return false
}
