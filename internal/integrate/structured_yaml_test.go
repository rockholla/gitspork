package integrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseYAML_preservesMappingKeyOrder(t *testing.T) {
	yamlBytes := []byte("zebra: 1\napple: 2\nmango: 3\n")
	n, err := parseYAML(yamlBytes)
	require.NoError(t, err)
	require.Equal(t, nodeMapping, n.kind)
	assert.Equal(t, []string{"zebra", "apple", "mango"}, n.mapping.Keys())
}

func TestParseYAML_recursesIntoNestedMaps(t *testing.T) {
	yamlBytes := []byte("outer:\n  b: bee\n  a: apple\n")
	n, err := parseYAML(yamlBytes)
	require.NoError(t, err)
	outer, ok := n.mapping.Get("outer")
	require.True(t, ok)
	require.Equal(t, nodeMapping, outer.kind)
	assert.Equal(t, []string{"b", "a"}, outer.mapping.Keys())
}

func TestParseYAML_parsesSequences(t *testing.T) {
	yamlBytes := []byte("langs:\n  - go\n  - node\n  - python\n")
	n, err := parseYAML(yamlBytes)
	require.NoError(t, err)
	langs, ok := n.mapping.Get("langs")
	require.True(t, ok)
	require.Equal(t, nodeSequence, langs.kind)
	require.Len(t, langs.seq, 3)
	assert.Equal(t, "go", langs.seq[0].scalar)
	assert.Equal(t, "python", langs.seq[2].scalar)
}

func TestWriteYAML_roundTripPreservesKeyOrder(t *testing.T) {
	in := []byte("zebra: 1\napple: 2\nmango: 3\n")
	n, err := parseYAML(in)
	require.NoError(t, err)
	out, err := writeYAML(n)
	require.NoError(t, err)

	// re-parse the output and confirm key order survived the round trip
	roundTripped, err := parseYAML(out)
	require.NoError(t, err)
	assert.Equal(t, []string{"zebra", "apple", "mango"}, roundTripped.mapping.Keys())
}

func TestWriteYAML_roundTripPreservesNestedKeyOrder(t *testing.T) {
	in := []byte("outer:\n  b: bee\n  a: apple\nother: value\n")
	n, err := parseYAML(in)
	require.NoError(t, err)
	out, err := writeYAML(n)
	require.NoError(t, err)

	roundTripped, err := parseYAML(out)
	require.NoError(t, err)
	assert.Equal(t, []string{"outer", "other"}, roundTripped.mapping.Keys())
	outer, ok := roundTripped.mapping.Get("outer")
	require.True(t, ok)
	assert.Equal(t, []string{"b", "a"}, outer.mapping.Keys())
}

// TestParseYAML_preservesFourLevelNestingOrder locks the mapping-order
// invariant at three-plus levels of nesting. Existing tests only go two
// levels deep; a regression that collapsed key order at deeper levels
// (e.g., a map value stored as unordered map[string]any at layer 3+)
// would slip past the shallow tests.
func TestParseYAML_preservesFourLevelNestingOrder(t *testing.T) {
	in := []byte(`root:
  layer_z:
    layer_y:
      layer_x:
        zebra: 1
        apple: 2
        mango: 3
      layer_before_x: before
    layer_before_y: before
  layer_after_z: after
`)
	n, err := parseYAML(in)
	require.NoError(t, err)

	// Walk the hierarchy and assert order at every level.
	root, ok := n.mapping.Get("root")
	require.True(t, ok)
	assert.Equal(t, []string{"layer_z", "layer_after_z"}, root.mapping.Keys(), "level 2 key order")

	layerZ, ok := root.mapping.Get("layer_z")
	require.True(t, ok)
	assert.Equal(t, []string{"layer_y", "layer_before_y"}, layerZ.mapping.Keys(), "level 3 key order")

	layerY, ok := layerZ.mapping.Get("layer_y")
	require.True(t, ok)
	assert.Equal(t, []string{"layer_x", "layer_before_x"}, layerY.mapping.Keys(), "level 4 key order")

	layerX, ok := layerY.mapping.Get("layer_x")
	require.True(t, ok)
	assert.Equal(t, []string{"zebra", "apple", "mango"}, layerX.mapping.Keys(), "level 5 key order")
}

// TestParseYAML_nonStringMappingKey_fallbackToFmtSprint exercises the
// fmt.Sprint(item.Key) fallback in yamlValueToNode when a mapping key
// isn't already a string (e.g., integer keys, which are legal in YAML
// even though gitspork's schema doesn't use them).
//
// Documents the current behaviour rather than a specific bug: integer
// keys survive as their string representation. Users authoring configs
// don't need to worry about this — the schema is string-keyed — but a
// user's downstream file might carry non-string keys, and gitspork's
// merge must handle it without panicking.
func TestParseYAML_nonStringMappingKey_fallbackToFmtSprint(t *testing.T) {
	in := []byte("1: first\n2: second\ntrue: yes-value\n")
	n, err := parseYAML(in)
	require.NoError(t, err)
	require.Equal(t, nodeMapping, n.kind)

	// Order preserved even for non-string keys — fmt.Sprint yields
	// deterministic string forms.
	assert.Equal(t, []string{"1", "2", "true"}, n.mapping.Keys(),
		"non-string keys must fmt.Sprint to their canonical form and preserve order")

	first, ok := n.mapping.Get("1")
	require.True(t, ok)
	assert.Equal(t, "first", first.scalar)
}

// TestYAML_commentsAreDroppedOnRoundTrip documents the current behaviour:
// parseYAML → writeYAML strips YAML comments. shared_ownership.structured
// merges of a downstream YAML file that carried comments will lose them.
// This is a KNOWN limitation worth pinning explicitly — a future
// enhancement to preserve comments would flip this test.
func TestYAML_commentsAreDroppedOnRoundTrip(t *testing.T) {
	in := []byte(`# top-level comment
key1: value1
# inline comment
key2: value2  # trailing comment
`)
	n, err := parseYAML(in)
	require.NoError(t, err)

	out, err := writeYAML(n)
	require.NoError(t, err)

	// Values survive but comments do not — assert both halves so a future
	// change that preserves comments flips this test loudly.
	assert.NotContains(t, string(out), "top-level comment")
	assert.NotContains(t, string(out), "inline comment")
	assert.NotContains(t, string(out), "trailing comment")
	assert.Contains(t, string(out), "key1: value1")
	assert.Contains(t, string(out), "key2: value2")
}

func TestWriteYAML_preservesScalarTypes(t *testing.T) {
	in := []byte("s: hello\ni: 42\nb: true\nn:\n")
	n, err := parseYAML(in)
	require.NoError(t, err)
	out, err := writeYAML(n)
	require.NoError(t, err)

	roundTripped, err := parseYAML(out)
	require.NoError(t, err)

	sVal, _ := roundTripped.mapping.Get("s")
	iVal, _ := roundTripped.mapping.Get("i")
	bVal, _ := roundTripped.mapping.Get("b")
	nVal, _ := roundTripped.mapping.Get("n")

	assert.Equal(t, "hello", sVal.scalar)
	assert.Equal(t, uint64(42), iVal.scalar) // goccy uses uint64 for positive integers
	assert.Equal(t, true, bVal.scalar)
	assert.Nil(t, nVal.scalar)
}
