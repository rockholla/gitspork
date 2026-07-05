package integrate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseJSON_preservesTopLevelKeyOrder(t *testing.T) {
	in := []byte(`{"zebra":1,"apple":2,"mango":3}`)
	n, err := parseJSON(in)
	require.NoError(t, err)
	require.Equal(t, nodeMapping, n.kind)
	assert.Equal(t, []string{"zebra", "apple", "mango"}, n.mapping.Keys())
}

func TestParseJSON_preservesNestedKeyOrder(t *testing.T) {
	in := []byte(`{"outer":{"b":"bee","a":"apple"},"other":"value"}`)
	n, err := parseJSON(in)
	require.NoError(t, err)
	assert.Equal(t, []string{"outer", "other"}, n.mapping.Keys())
	outer, ok := n.mapping.Get("outer")
	require.True(t, ok)
	assert.Equal(t, []string{"b", "a"}, outer.mapping.Keys())
}

func TestParseJSON_parsesArrays(t *testing.T) {
	in := []byte(`{"langs":["go","node","python"]}`)
	n, err := parseJSON(in)
	require.NoError(t, err)
	langs, ok := n.mapping.Get("langs")
	require.True(t, ok)
	require.Equal(t, nodeSequence, langs.kind)
	require.Len(t, langs.seq, 3)
	assert.Equal(t, "go", langs.seq[0].scalar)
}

func TestParseJSON_parsesScalarTypes(t *testing.T) {
	in := []byte(`{"s":"hello","i":42,"f":3.14,"b":true,"n":null}`)
	n, err := parseJSON(in)
	require.NoError(t, err)

	s, _ := n.mapping.Get("s")
	i, _ := n.mapping.Get("i")
	f, _ := n.mapping.Get("f")
	b, _ := n.mapping.Get("b")
	nul, _ := n.mapping.Get("n")

	assert.Equal(t, "hello", s.scalar)
	assert.Equal(t, json.Number("42"), i.scalar)
	assert.Equal(t, json.Number("3.14"), f.scalar)
	assert.Equal(t, true, b.scalar)
	assert.Nil(t, nul.scalar)
}

func TestWriteJSON_roundTripPreservesKeyOrder(t *testing.T) {
	in := []byte(`{"zebra":1,"apple":2,"mango":3}`)
	n, err := parseJSON(in)
	require.NoError(t, err)
	out, err := writeJSON(n)
	require.NoError(t, err)

	roundTripped, err := parseJSON(out)
	require.NoError(t, err)
	assert.Equal(t, []string{"zebra", "apple", "mango"}, roundTripped.mapping.Keys())
}

func TestWriteJSON_roundTripPreservesNestedKeyOrder(t *testing.T) {
	in := []byte(`{"outer":{"b":"bee","a":"apple"},"other":"value"}`)
	n, err := parseJSON(in)
	require.NoError(t, err)
	out, err := writeJSON(n)
	require.NoError(t, err)

	roundTripped, err := parseJSON(out)
	require.NoError(t, err)
	assert.Equal(t, []string{"outer", "other"}, roundTripped.mapping.Keys())
	outer, ok := roundTripped.mapping.Get("outer")
	require.True(t, ok)
	assert.Equal(t, []string{"b", "a"}, outer.mapping.Keys())
}

func TestWriteJSON_usesTwoSpaceIndent(t *testing.T) {
	n := newMappingNode()
	n.mapping.Set("a", newScalarNode("one"))
	n.mapping.Set("b", newScalarNode("two"))
	out, err := writeJSON(n)
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"a\": \"one\",\n  \"b\": \"two\"\n}", string(out))
}

func TestWriteJSON_emptyStructures(t *testing.T) {
	t.Run("empty mapping", func(t *testing.T) {
		out, err := writeJSON(newMappingNode())
		require.NoError(t, err)
		assert.Equal(t, "{}", string(out))
	})
	t.Run("empty sequence", func(t *testing.T) {
		out, err := writeJSON(newSequenceNode())
		require.NoError(t, err)
		assert.Equal(t, "[]", string(out))
	})
}
