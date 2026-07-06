//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchema_printsAnnotatedScaffolds locks the shape of `gitspork schema`
// stdout. This isn't a regression against a specific past bug — it's a
// snapshot of the two annotated YAML sections the command emits so an
// accidental change (deleted key, dropped section, rewritten header) is
// caught at CI time. schema is user-facing documentation of the
// .gitspork.yml surface, so silent shape drift is a real risk.
func TestSchema_printsAnnotatedScaffolds(t *testing.T) {
	// schema doesn't need any repo state — run it from a fresh temp dir.
	dir := t.TempDir()
	runner := resolveRunner(t, dir, "")
	out, code := runner.Run(t, []string{"schema"}, dir)
	require.Equal(t, 0, code, "schema exited non-zero:\n%s", out)

	t.Run("section headers present", func(t *testing.T) {
		assert.Contains(t, out, "Main .gitspork.yml schema:",
			"main schema header should introduce the .gitspork.yml section")
		assert.Contains(t, out, "Migration YAML schema:",
			"migration schema header should introduce the migration section")
	})

	t.Run("main schema exposes every top-level ownership primitive", func(t *testing.T) {
		// These keys are the public schema surface of .gitspork.yml. If any
		// key is dropped from the scaffold, users lose in-tree documentation
		// of that primitive — worth catching at CI time.
		for _, key := range []string{
			"upstream_owned:",
			"downstream_owned:",
			"shared_ownership:",
			"templated:",
			"migrations:",
		} {
			assert.Contains(t, out, key, "top-level key %q missing from main schema", key)
		}
	})

	t.Run("main schema exposes shared_ownership sub-primitives", func(t *testing.T) {
		for _, key := range []string{
			"merged:",
			"structured:",
			"prefer_upstream:",
			"prefer_downstream:",
		} {
			assert.Contains(t, out, key, "shared_ownership sub-key %q missing from main schema", key)
		}
	})

	t.Run("main schema demonstrates rename ({from, to}) entries", func(t *testing.T) {
		// The scaffold intentionally shows a rename entry so users see the
		// {from, to} form is valid. Losing that example would degrade the
		// self-documenting utility of the schema.
		assert.Contains(t, out, "from:",
			"scaffold should demonstrate the rename {from, to} form")
		assert.Contains(t, out, "to:",
			"scaffold should demonstrate the rename {from, to} form")
	})

	t.Run("main schema demonstrates all three templated input types", func(t *testing.T) {
		// prompt / json_data_path / previous_input are the three ways a
		// templated input can be sourced. The scaffold exercises all three
		// so users see the full menu.
		for _, key := range []string{
			"prompt:",
			"json_data_path:",
			"previous_input:",
		} {
			assert.Contains(t, out, key, "templated input source %q missing from main schema", key)
		}
	})

	t.Run("migration schema exposes both hook slots", func(t *testing.T) {
		assert.Contains(t, out, "pre_integrate:",
			"migration schema should show the pre_integrate hook")
		assert.Contains(t, out, "post_integrate:",
			"migration schema should show the post_integrate hook")
		assert.Contains(t, out, "exec:",
			"migration schema should show the exec instruction key")
	})
}
