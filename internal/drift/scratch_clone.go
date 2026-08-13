package drift

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// provisionScratchClone clones callerRepoPath into a temporary directory and
// checks it out at the caller's exact HEAD hash. Returns the scratch path plus
// a cleanup func the caller must defer.
//
// The scratch clone is a fully functional git repo with its own object store,
// so any writes gitspork's drift-check flow performs (temporary branch,
// staging, commit) land in the scratch and are removed when cleanup runs.
// This keeps the caller's working tree strictly untouched by CheckDrift.
func provisionScratchClone(callerRepoPath string) (string, func(), error) {
	scratchPath, err := os.MkdirTemp("", "gitspork-drift-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("error creating scratch temp dir: %v", err)
	}
	cleanup := func() { _ = os.RemoveAll(scratchPath) }

	callerHead, err := shellGitOutput(callerRepoPath, "rev-parse", "HEAD")
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("error resolving caller HEAD hash: %v", err)
	}

	// --local hardlinks the object store (cheap on time and disk). --no-checkout
	// skips the initial checkout so we can pin explicitly to the caller's HEAD
	// hash below — necessary when the caller is on a detached HEAD (e.g. CI).
	if _, err := shellGitOutput("", "clone", "--local", "--no-checkout", callerRepoPath, scratchPath); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("error cloning caller repo to scratch: %v", err)
	}
	if _, err := shellGitOutput(scratchPath, "-c", "advice.detachedHead=false", "checkout", callerHead); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("error checking out caller HEAD in scratch: %v", err)
	}

	return scratchPath, cleanup, nil
}

// shellGitOutput runs `git <args...>` (with -c safe.directory=* prepended) and
// returns trimmed stdout, wrapping stderr into the error on failure so callers
// see the real git message.
//
// dir="" runs from the current working directory (used for `clone` where -C is
// meaningless).
func shellGitOutput(dir string, args ...string) (string, error) {
	full := []string{"-c", "safe.directory=*"}
	if dir != "" {
		full = append(full, "-C", dir)
	}
	full = append(full, args...)

	cmd := exec.Command("git", full...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
