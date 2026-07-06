package integrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDownstreamOwnedFixture creates an upstream + downstream pair and seeds
// upstream files. Downstream files are seeded via a second call so tests can
// distinguish "downstream absent" from "downstream pre-populated" scenarios.
func setupDownstreamOwnedFixture(t *testing.T, upstreamFiles map[string]string) (string, string) {
	t.Helper()
	upstreamDir := t.TempDir()
	downstreamDir := t.TempDir()
	for relPath, content := range upstreamFiles {
		full := filepath.Join(upstreamDir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}
	return upstreamDir, downstreamDir
}

func writeDownstreamFile(t *testing.T, downstreamDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(downstreamDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0644))
}

func TestIntegratorDownstreamOwned_seedsPlainFileWhenDownstreamMissing(t *testing.T) {
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"downstream-owned.md": "upstream seed content\n",
	})
	entries := []config.OwnedEntry{{Pattern: "downstream-owned.md"}}

	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	got, err := os.ReadFile(filepath.Join(downstreamDir, "downstream-owned.md"))
	require.NoError(t, err)
	assert.Equal(t, "upstream seed content\n", string(got))
}

// TestIntegratorDownstreamOwned_doesNotOverwriteExistingDownstreamFile is the
// DEFINING invariant of downstream-owned: after the initial seed, the
// downstream owns the file thereafter. A subsequent Integrate must NOT
// overwrite downstream-modified content, even if upstream diverges.
func TestIntegratorDownstreamOwned_doesNotOverwriteExistingDownstreamFile(t *testing.T) {
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"downstream-owned.md": "upstream content — should NOT land in downstream\n",
	})
	// Downstream already has divergent content.
	writeDownstreamFile(t, downstreamDir, "downstream-owned.md", "downstream custom content\n")

	entries := []config.OwnedEntry{{Pattern: "downstream-owned.md"}}
	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	got, err := os.ReadFile(filepath.Join(downstreamDir, "downstream-owned.md"))
	require.NoError(t, err)
	assert.Equal(t, "downstream custom content\n", string(got),
		"downstream-owned files must not be overwritten on subsequent integrates — this is the defining invariant of the ownership category")
}

func TestIntegratorDownstreamOwned_globPatternSeedsMultipleFiles(t *testing.T) {
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"configs/one.yml":   "one",
		"configs/two.yml":   "two",
		"configs/three.yml": "three",
		"unrelated.txt":     "should not be copied",
	})
	entries := []config.OwnedEntry{{Pattern: "configs/**"}}

	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	for _, name := range []string{"one.yml", "two.yml", "three.yml"} {
		got, err := os.ReadFile(filepath.Join(downstreamDir, "configs", name))
		require.NoError(t, err, "expected configs/%s to be seeded", name)
		assert.NotEmpty(t, string(got))
	}
	_, err := os.Stat(filepath.Join(downstreamDir, "unrelated.txt"))
	assert.True(t, os.IsNotExist(err), "files not covered by the glob must not land in downstream")
}

func TestIntegratorDownstreamOwned_renameEntryPlainSeedsAtDestination(t *testing.T) {
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"seed-src.md": "seed content from upstream source path\n",
	})
	// {from, to} rename form: seed at a different downstream path.
	entries := []config.OwnedEntry{{From: "seed-src.md", To: "seed-dest.md"}}

	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	got, err := os.ReadFile(filepath.Join(downstreamDir, "seed-dest.md"))
	require.NoError(t, err)
	assert.Equal(t, "seed content from upstream source path\n", string(got))

	// Source path must NOT also land in downstream — only the resolved destination.
	_, err = os.Stat(filepath.Join(downstreamDir, "seed-src.md"))
	assert.True(t, os.IsNotExist(err), "rename entry should not leave the source path in the downstream")
}

func TestIntegratorDownstreamOwned_renameEntryGlobPreservesRemainder(t *testing.T) {
	// Prefix-substitution rename: configs/** → .configs/** re-lands each file
	// under the destination prefix while preserving the remainder of its path.
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"configs/app/settings.yml": "app settings\n",
		"configs/db/pool.yml":      "db pool\n",
	})
	entries := []config.OwnedEntry{{From: "configs/**", To: ".configs/**"}}

	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	got, err := os.ReadFile(filepath.Join(downstreamDir, ".configs/app/settings.yml"))
	require.NoError(t, err)
	assert.Equal(t, "app settings\n", string(got))

	got, err = os.ReadFile(filepath.Join(downstreamDir, ".configs/db/pool.yml"))
	require.NoError(t, err)
	assert.Equal(t, "db pool\n", string(got))

	// Original path must not appear in downstream.
	_, err = os.Stat(filepath.Join(downstreamDir, "configs"))
	assert.True(t, os.IsNotExist(err))
}

func TestIntegratorDownstreamOwned_renameEntryDoesNotOverwriteExistingDestination(t *testing.T) {
	// Same "downstream owns thereafter" invariant, but for the rename form:
	// if the resolved destination path already exists downstream, don't touch it.
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"seed-src.md": "upstream content — should NOT overwrite\n",
	})
	writeDownstreamFile(t, downstreamDir, "seed-dest.md", "downstream custom\n")

	entries := []config.OwnedEntry{{From: "seed-src.md", To: "seed-dest.md"}}
	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	got, err := os.ReadFile(filepath.Join(downstreamDir, "seed-dest.md"))
	require.NoError(t, err)
	assert.Equal(t, "downstream custom\n", string(got),
		"rename-form downstream-owned entries must not overwrite the resolved destination if it already exists")
}

func TestIntegratorDownstreamOwned_nestedDestinationDirsCreated(t *testing.T) {
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"deep/nested/path/seed.md": "nested content\n",
	})
	entries := []config.OwnedEntry{{Pattern: "deep/nested/path/seed.md"}}

	// Downstream has no deep/ directory yet — syncFile must create the tree.
	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	got, err := os.ReadFile(filepath.Join(downstreamDir, "deep/nested/path/seed.md"))
	require.NoError(t, err)
	assert.Equal(t, "nested content\n", string(got))
}

func TestIntegratorDownstreamOwned_renameToNestedDestinationCreatesDirs(t *testing.T) {
	// Rename lands the file under a nested path the downstream doesn't have yet.
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"flat.md": "content\n",
	})
	entries := []config.OwnedEntry{{From: "flat.md", To: "nested/deep/flat.md"}}

	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	got, err := os.ReadFile(filepath.Join(downstreamDir, "nested/deep/flat.md"))
	require.NoError(t, err)
	assert.Equal(t, "content\n", string(got))
}

func TestIntegratorDownstreamOwned_multipleEntriesProcessedInOrder(t *testing.T) {
	// Two entries share a config: verify each entry's SourcePattern is
	// resolved and copied independently, and unrelated entries don't
	// interfere with each other.
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"a.md": "content-a\n",
		"b.md": "content-b\n",
	})
	entries := []config.OwnedEntry{
		{Pattern: "a.md"},
		{Pattern: "b.md"},
	}

	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	for _, e := range []struct{ name, want string }{
		{"a.md", "content-a\n"},
		{"b.md", "content-b\n"},
	} {
		got, err := os.ReadFile(filepath.Join(downstreamDir, e.name))
		require.NoError(t, err, "expected %s to be seeded", e.name)
		assert.Equal(t, e.want, string(got))
	}
}

func TestIntegratorDownstreamOwned_emptyEntriesIsNoOp(t *testing.T) {
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"seed.md": "content\n",
	})
	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(nil, upstreamDir, downstreamDir, sdktypes.NoopLogger()))

	// Nothing should have been copied — no entries means no downstream-owned bootstrapping.
	entries, err := os.ReadDir(downstreamDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no entries means no files seeded into downstream")
}

// TestIntegratorDownstreamOwned_reintegrateIsIdempotent asserts running the
// integrator twice with the same config produces the same result on run 2
// as run 1 (given no upstream OR downstream changes in between). This is a
// belt-and-suspenders test — the "does not overwrite" invariant already
// guarantees this — but a common regression class would be an integrator
// that copies unconditionally, so the assertion is worth pinning.
func TestIntegratorDownstreamOwned_reintegrateIsIdempotent(t *testing.T) {
	upstreamDir, downstreamDir := setupDownstreamOwnedFixture(t, map[string]string{
		"downstream-owned.md": "seed content\n",
	})
	entries := []config.OwnedEntry{{Pattern: "downstream-owned.md"}}

	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))
	firstBytes, err := os.ReadFile(filepath.Join(downstreamDir, "downstream-owned.md"))
	require.NoError(t, err)
	firstStat, err := os.Stat(filepath.Join(downstreamDir, "downstream-owned.md"))
	require.NoError(t, err)

	require.NoError(t, (&IntegratorDownstreamOwned{}).Integrate(entries, upstreamDir, downstreamDir, sdktypes.NoopLogger()))
	secondBytes, err := os.ReadFile(filepath.Join(downstreamDir, "downstream-owned.md"))
	require.NoError(t, err)
	secondStat, err := os.Stat(filepath.Join(downstreamDir, "downstream-owned.md"))
	require.NoError(t, err)

	assert.Equal(t, firstBytes, secondBytes, "content must be byte-identical across re-integrates")
	// File mtime shouldn't change on the second run — proves we didn't re-open/rewrite it.
	assert.Equal(t, firstStat.ModTime(), secondStat.ModTime(),
		"file mtime must not advance on the second integrate — the file was not re-written")
}
