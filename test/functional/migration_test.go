//go:build functional || functional_docker

package functional

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrationGitsporkYML wires a single migration file into the upstream config.
const migrationGitsporkYML = `upstream_owned:
- upstream-owned/**
migrations:
- .gitspork/migrations/0001/migration.yml
`

// migrationYML declares both a pre_integrate and post_integrate hook. The
// scripts each append their name plus a timestamp-like counter to a sentinel
// file in the downstream — running twice must yield exactly one appended line
// per hook (the once-only property enforced by state.MigrationsComplete).
const migrationYML = `pre_integrate:
  exec: ./.gitspork/migrations/0001/pre.sh
post_integrate:
  exec: ./.gitspork/migrations/0001/post.sh
`

const preScript = `#!/bin/sh
echo pre-ran >> migration-log.txt
`

const postScript = `#!/bin/sh
echo post-ran >> migration-log.txt
`

// TestIntegrate_migration_hooks_run_end_to_end verifies:
//   1. pre_integrate and post_integrate hooks both execute against the downstream.
//   2. On a second integrate, neither hook re-runs — the once-only guarantee
//      via .gitspork/downstream-state.json migrations_complete.
//
// Skipped on Windows because the migration scripts are POSIX shell.
func TestIntegrate_migration_hooks_run_end_to_end(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("migration hooks are POSIX shell in this test")
	}

	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"upstream-owned/file.txt":               "upstream content\n",
		".gitspork/migrations/0001/migration.yml": migrationYML,
		".gitspork/migrations/0001/pre.sh":        preScript,
		".gitspork/migrations/0001/post.sh":       postScript,
	}, migrationGitsporkYML)

	// The two scripts need executable bits — NewUpstreamRepo writes with the
	// default file mode, so chmod them here before the upstream is used.
	require.NoError(t, os.Chmod(filepath.Join(upstreamDir, ".gitspork/migrations/0001/pre.sh"), 0755))
	require.NoError(t, os.Chmod(filepath.Join(upstreamDir, ".gitspork/migrations/0001/post.sh"), 0755))
	// Re-commit so the chmod is captured by the repo.
	CommitAll(t, OpenRepo(t, upstreamDir), upstreamDir, "chmod +x migration scripts")

	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)
	args := integrateArgs(upstreamDir, downstreamDir)

	// First integrate: hooks should run.
	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "first integrate exited non-zero:\n%s", out)

	logAfterFirst := ReadFile(t, downstreamDir, "migration-log.txt")
	assert.Equal(t, "pre-ran\npost-ran\n", logAfterFirst,
		"first integrate should run pre_integrate then post_integrate exactly once each")

	// Commit the log so the tree is clean before re-integrate.
	CommitAll(t, OpenRepo(t, downstreamDir), downstreamDir, "post-integrate baseline")

	// State file should list both hooks as complete after run one.
	stateAfterFirst := ReadFile(t, downstreamDir, ".gitspork/downstream-state.json")
	assert.Contains(t, stateAfterFirst, ":pre_integrate",
		"state.migrations_complete should record the pre_integrate hook")
	assert.Contains(t, stateAfterFirst, ":post_integrate",
		"state.migrations_complete should record the post_integrate hook")

	// Second integrate: hooks must NOT run again.
	out, code = runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "second integrate exited non-zero:\n%s", out)

	logAfterSecond := ReadFile(t, downstreamDir, "migration-log.txt")
	assert.Equal(t, logAfterFirst, logAfterSecond,
		"migration-log.txt must be unchanged after re-integrate: hooks are once-only")
	// Defense in depth against future silent regressions of the once-only
	// property: count occurrences directly rather than only comparing strings.
	assert.Equal(t, 1, strings.Count(logAfterSecond, "pre-ran"),
		"pre_integrate must run exactly once across two integrates")
	assert.Equal(t, 1, strings.Count(logAfterSecond, "post-ran"),
		"post_integrate must run exactly once across two integrates")
}
