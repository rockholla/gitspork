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
