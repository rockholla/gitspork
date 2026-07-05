// Package gitbin centralizes the fail-fast check for the git binary in PATH.
// gitspork uses go-git for most git operations, but a small number of paths
// (CheckDrift's working-tree cleanliness check, `gitspork mv`, `gitspork rm`)
// still shell out to the git CLI. Callers on those paths invoke Require() at
// entry so users get a clear error before any work has been done.
package gitbin

import (
	"fmt"
	"os/exec"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// Require returns nil when the git binary is on PATH. When missing, it returns
// a wrapped sdktypes.ErrGitBinaryMissing with an actionable message. SDK
// consumers can detect the failure with errors.Is(err, gitspork.ErrGitBinaryMissing).
func Require() error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("%w: %v — install git and ensure it is on PATH", sdktypes.ErrGitBinaryMissing, err)
	}
	return nil
}
