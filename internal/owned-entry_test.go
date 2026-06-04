package internal

// Verifies the scalar-or-map marshaling of OwnedEntry across goccy (the config
// read/write path) and the schema post-processing that corrects the
// reflection-based marshal.YAMLWithComments output.

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/rockholla/go-lib/marshal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// test-local container so we can exercise list unmarshaling with the real type.
type ownedEntryYAML struct {
	UpstreamOwned   []OwnedEntry `yaml:"upstream_owned" comment:"plain pattern or {from,to} rename"`
	DownstreamOwned []OwnedEntry `yaml:"downstream_owned" comment:"plain pattern or {from,to} rename"`
}

func TestOwnedEntry_UnmarshalUnion(t *testing.T) {
	src := `upstream_owned:
  - src/**
  - from: .markdownlint-downstream.jsonc
    to: .markdownlint.jsonc
  - configs/**
`
	var cfg ownedEntryYAML
	require.NoError(t, yaml.Unmarshal([]byte(src), &cfg))
	require.Len(t, cfg.UpstreamOwned, 3)
	assert.Equal(t, "src/**", cfg.UpstreamOwned[0].Pattern)
	assert.False(t, cfg.UpstreamOwned[0].IsRename())
	assert.Equal(t, ".markdownlint-downstream.jsonc", cfg.UpstreamOwned[1].From)
	assert.Equal(t, ".markdownlint.jsonc", cfg.UpstreamOwned[1].To)
	assert.True(t, cfg.UpstreamOwned[1].IsRename())
	assert.Equal(t, "configs/**", cfg.UpstreamOwned[2].Pattern)
}

func TestOwnedEntry_MarshalRoundTrip(t *testing.T) {
	cfg := ownedEntryYAML{UpstreamOwned: []OwnedEntry{
		{Pattern: "src/**"},
		{From: "a.txt", To: "b.txt"},
	}}
	out, err := yaml.Marshal(&cfg)
	require.NoError(t, err)
	assert.Contains(t, string(out), "- src/**")
	assert.Contains(t, string(out), "from: a.txt")
	assert.Contains(t, string(out), "to: b.txt")

	var back ownedEntryYAML
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, "src/**", back.UpstreamOwned[0].Pattern)
	assert.Equal(t, "a.txt", back.UpstreamOwned[1].From)
	assert.Equal(t, "b.txt", back.UpstreamOwned[1].To)
}

func TestOwnedEntry_SourcePattern(t *testing.T) {
	assert.Equal(t, "src/**", OwnedEntry{Pattern: "src/**"}.SourcePattern())
	assert.Equal(t, "a.txt", OwnedEntry{From: "a.txt", To: "b.txt"}.SourcePattern())
}

func TestOwnedEntry_ResolveDest(t *testing.T) {
	// plain entry: identity
	assert.Equal(t, "x/y.txt", OwnedEntry{Pattern: "x/**"}.ResolveDest("x/y.txt"))
	// exact rename
	assert.Equal(t, "b.txt", OwnedEntry{From: "a.txt", To: "b.txt"}.ResolveDest("a.txt"))
	// glob rename: prefix substitution
	e := OwnedEntry{From: "configs/**", To: ".configs/**"}
	assert.Equal(t, ".configs/app/db.yml", e.ResolveDest("configs/app/db.yml"))
	assert.Equal(t, ".configs/x/y/z.txt", e.ResolveDest("configs/x/y/z.txt"))
}

func TestCollapsePlainOwnedEntries_bothBlocks(t *testing.T) {
	cfg := &ownedEntryYAML{
		UpstreamOwned: []OwnedEntry{
			{Pattern: "upstream-owned.txt"},
			{From: ".markdownlint-downstream.jsonc", To: ".markdownlint.jsonc"},
		},
		DownstreamOwned: []OwnedEntry{
			{Pattern: "downstream-owned.md"},
			{From: "seed-from.md", To: "seed-to.md"},
		},
	}
	raw, err := marshal.YAMLWithComments(cfg, 0)
	require.NoError(t, err)
	out := collapsePlainOwnedEntries(raw)
	// plain entries in both blocks collapse to scalars
	assert.Contains(t, out, `- "upstream-owned.txt"`)
	assert.Contains(t, out, `- "downstream-owned.md"`)
	assert.NotContains(t, out, "- pattern:")
	// rename entries in both blocks are preserved
	assert.Contains(t, out, `from: ".markdownlint-downstream.jsonc"`)
	assert.Contains(t, out, `to: ".markdownlint.jsonc"`)
	assert.Contains(t, out, `from: "seed-from.md"`)
	assert.Contains(t, out, `to: "seed-to.md"`)
}

func TestGitSporkConfig_RenameRoundTripPreservesComments(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.gitspork.yml"
	src := `upstream_owned:
# keep me
- src/**
- from: a.txt
  to: b.txt
`
	require.NoError(t, os.WriteFile(path, []byte(src), 0644))
	cfg, err := ParseGitSporkConfig(path)
	require.NoError(t, err)
	require.Len(t, cfg.UpstreamOwned, 2)
	assert.Equal(t, "src/**", cfg.UpstreamOwned[0].Pattern)
	assert.Equal(t, "a.txt", cfg.UpstreamOwned[1].From)

	require.NoError(t, WriteGitSporkConfig(path, cfg))
	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(out), "keep me")
	assert.Contains(t, string(out), "- src/**")
	assert.Contains(t, string(out), "from: a.txt")
}
