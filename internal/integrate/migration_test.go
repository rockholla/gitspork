package integrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_runMigration(t *testing.T) {
	t.Run("empty Exec is a no-op", func(t *testing.T) {
		err := runMigration(&config.GitSporkConfigMigrationInstructions{Exec: ""}, t.TempDir(), t.TempDir(), sdktypes.NoopLogger())
		assert.NoError(t, err)
	})

	t.Run("whitespace-only Exec returns zero-token error", func(t *testing.T) {
		err := runMigration(&config.GitSporkConfigMigrationInstructions{Exec: "   \t  "}, t.TempDir(), t.TempDir(), sdktypes.NoopLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolved to zero tokens")
	})

	if runtime.GOOS == "windows" {
		t.Skip("remaining subtests use POSIX shell scripts")
		return
	}

	t.Run("upstream script is resolved to absolute upstream path and executes from downstream cwd", func(t *testing.T) {
		upstreamDir := t.TempDir()
		downstreamDir := t.TempDir()

		scriptRel := "migrate.sh"
		// pwd -P forces physical-path resolution so the sentinel matches
		// filepath.EvalSymlinks on the Go side (macOS's /var/folders is a
		// symlink to /private/var/folders and `pwd` alone would keep the
		// logical form the Go stdlib used to chdir).
		scriptContents := "#!/bin/sh\npwd -P > cwd.txt\necho migration ran > ran.txt\n"
		require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, scriptRel), []byte(scriptContents), 0755))

		err := runMigration(&config.GitSporkConfigMigrationInstructions{Exec: "./" + scriptRel}, upstreamDir, downstreamDir, sdktypes.NoopLogger())
		require.NoError(t, err)

		// Script wrote to cwd — asserts it ran with cmd.Dir == downstreamDir,
		// NOT upstream (since runMigration should scope side effects to the
		// downstream tree).
		gotCwd, readErr := os.ReadFile(filepath.Join(downstreamDir, "cwd.txt"))
		require.NoError(t, readErr, "expected downstream cwd sentinel; runMigration should execute with cmd.Dir=downstreamDir")

		// macOS resolves /tmp -> /private/tmp; compare via EvalSymlinks.
		resolvedDownstream, err := filepath.EvalSymlinks(downstreamDir)
		require.NoError(t, err)
		assert.Equal(t, resolvedDownstream, strings.TrimSpace(string(gotCwd)),
			"pwd inside the migration script must match downstream repo path")

		gotMarker, err := os.ReadFile(filepath.Join(downstreamDir, "ran.txt"))
		require.NoError(t, err)
		assert.Equal(t, "migration ran\n", string(gotMarker))

		// Sanity: the upstream directory should be untouched.
		_, err = os.Stat(filepath.Join(upstreamDir, "cwd.txt"))
		assert.True(t, os.IsNotExist(err), "script should not have written into the upstream tree")
	})

	t.Run("command not present in upstream falls through to PATH", func(t *testing.T) {
		upstreamDir := t.TempDir()
		downstreamDir := t.TempDir()
		// "true" is not a file at upstreamDir/true; runMigration should
		// leave execParts[0] alone and exec.LookPath resolves it from $PATH.
		err := runMigration(&config.GitSporkConfigMigrationInstructions{Exec: "true"}, upstreamDir, downstreamDir, sdktypes.NoopLogger())
		assert.NoError(t, err)
	})

	t.Run("non-zero exit propagates as error", func(t *testing.T) {
		upstreamDir := t.TempDir()
		downstreamDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "fail.sh"), []byte("#!/bin/sh\nexit 3\n"), 0755))
		err := runMigration(&config.GitSporkConfigMigrationInstructions{Exec: "./fail.sh"}, upstreamDir, downstreamDir, sdktypes.NoopLogger())
		require.Error(t, err, "subprocess non-zero exit must propagate")
		assert.Contains(t, err.Error(), "exit status 3")
	})

	t.Run("tab-separated and double-spaced arguments tokenize via strings.Fields", func(t *testing.T) {
		upstreamDir := t.TempDir()
		downstreamDir := t.TempDir()
		// The script writes its argv, so we can verify tokens flowed through
		// intact regardless of the whitespace shape the user wrote.
		scriptContents := "#!/bin/sh\nprintf %s \"$1|$2|$3\" > argv.txt\n"
		require.NoError(t, os.WriteFile(filepath.Join(upstreamDir, "args.sh"), []byte(scriptContents), 0755))

		// Two tabs, three spaces, mixed — should still yield three arguments.
		err := runMigration(&config.GitSporkConfigMigrationInstructions{Exec: "./args.sh\talpha  beta \tgamma"}, upstreamDir, downstreamDir, sdktypes.NoopLogger())
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(downstreamDir, "argv.txt"))
		require.NoError(t, err)
		assert.Equal(t, "alpha|beta|gamma", string(got))
	})
}

func Test_migrationCompletedInDownstream(t *testing.T) {
	t.Run("returns true when ID is present in state", func(t *testing.T) {
		dir := t.TempDir()
		state := &sdktypes.DownstreamState{MigrationsComplete: []string{"m/one:pre_integrate", "m/two:post_integrate"}}
		require.NoError(t, SaveDownstreamState(dir, state))

		got, err := migrationCompletedInDownstream("m/one:pre_integrate", dir)
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("returns false when ID is not in state", func(t *testing.T) {
		dir := t.TempDir()
		state := &sdktypes.DownstreamState{MigrationsComplete: []string{"m/one:pre_integrate"}}
		require.NoError(t, SaveDownstreamState(dir, state))

		got, err := migrationCompletedInDownstream("m/absent:post_integrate", dir)
		require.NoError(t, err)
		assert.False(t, got)
	})

	t.Run("returns false against a fresh downstream with no state file", func(t *testing.T) {
		got, err := migrationCompletedInDownstream("anything", t.TempDir())
		require.NoError(t, err)
		assert.False(t, got)
	})
}

func Test_recordCompleteMigration(t *testing.T) {
	t.Run("creates fresh state and records the ID", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, recordCompleteMigration("m/one:pre_integrate", dir))

		state, err := LoadDownstreamState(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"m/one:pre_integrate"}, state.MigrationsComplete)
	})

	t.Run("recording the same ID twice does not duplicate the entry", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, recordCompleteMigration("m/one:pre_integrate", dir))
		require.NoError(t, recordCompleteMigration("m/one:pre_integrate", dir))

		state, err := LoadDownstreamState(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"m/one:pre_integrate"}, state.MigrationsComplete,
			"recordCompleteMigration must be idempotent — this guarantees migrations run at most once")
	})

	t.Run("recording distinct IDs preserves order and grows the list", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, recordCompleteMigration("m/one:pre_integrate", dir))
		require.NoError(t, recordCompleteMigration("m/two:post_integrate", dir))
		require.NoError(t, recordCompleteMigration("m/three:pre_integrate", dir))

		state, err := LoadDownstreamState(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"m/one:pre_integrate", "m/two:post_integrate", "m/three:pre_integrate"}, state.MigrationsComplete)
	})
}
