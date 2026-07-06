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

// TestParseJSON_preservesLargeIntegerPrecision is a regression guard against
// dropping `dec.UseNumber()` from parseJSON. Without UseNumber, integer tokens
// are decoded into float64 — which loses precision above 2^53 = 9007199254740992.
// A regression would silently corrupt IDs, epoch timestamps in nanoseconds,
// and other JSON integers users legitimately write. The test asserts the
// literal survives a full parse+write round-trip byte-for-byte, which is only
// possible if the scalar was captured as json.Number and not float64.
func TestParseJSON_preservesLargeIntegerPrecision(t *testing.T) {
	// 2^53 + 1 — the smallest positive integer that float64 cannot represent
	// exactly. If UseNumber() is removed, this parses as float64 9007199254740992
	// (rounded down) and the round-trip output will contain the WRONG value.
	const largeInt = "9007199254740993"
	in := []byte(`{"id":` + largeInt + `}`)

	n, err := parseJSON(in)
	require.NoError(t, err)

	idNode, ok := n.mapping.Get("id")
	require.True(t, ok)
	// The scalar type is what enforces the invariant — json.Number is a string
	// alias that carries the literal characters verbatim. Any other type
	// (float64 in particular) indicates UseNumber() is missing.
	num, isNumber := idNode.scalar.(json.Number)
	require.True(t, isNumber, "large integer scalar must be json.Number (was %T) — indicates dec.UseNumber() removed from parseJSON", idNode.scalar)
	assert.Equal(t, largeInt, string(num))

	out, err := writeJSON(n)
	require.NoError(t, err)
	assert.Contains(t, string(out), largeInt,
		"round-tripped output must contain the exact %s literal — loss of precision here means dec.UseNumber() was removed from parseJSON", largeInt)
	assert.NotContains(t, string(out), "9007199254740992",
		"a value of 9007199254740992 in the output would indicate float64 rounding — the exact regression this test guards against")
}

func TestParseJSON_preservesLargeNegativeIntegerPrecision(t *testing.T) {
	// Same invariant for the negative direction — a value beyond -2^53.
	const largeNegInt = "-9007199254740993"
	in := []byte(`{"id":` + largeNegInt + `}`)

	n, err := parseJSON(in)
	require.NoError(t, err)
	out, err := writeJSON(n)
	require.NoError(t, err)
	assert.Contains(t, string(out), largeNegInt)
}

// TestParseJSON_integerScalarsRoundTripAsIntegers guards against a subtler
// regression: even for small integers where float64 has enough precision,
// dropping UseNumber() would decode `100` as `float64(100)`, and
// `json.Marshal(100.0)` emits `100` — that's fine, BUT for smaller values
// like `1e-6` or numbers that look integer-ish in JSON but round through
// float, we can see scientific-notation output. The safer assertion is that
// the exact input literal is preserved on round-trip.
func TestParseJSON_integerScalarsRoundTripAsIntegers(t *testing.T) {
	// A moderate integer that both branches handle equivalently, plus a
	// float that must not be re-emitted as scientific notation.
	in := []byte(`{"count":100,"ratio":0.5}`)

	n, err := parseJSON(in)
	require.NoError(t, err)
	out, err := writeJSON(n)
	require.NoError(t, err)

	assert.Contains(t, string(out), `"count": 100`, "integer scalar must not be re-emitted as 100.0 or 1e+02")
	assert.Contains(t, string(out), `"ratio": 0.5`, "float scalar must round-trip as decimal, not scientific notation")
}

// TestParseJSON_preservesFourLevelNestingOrder pins mapping-order at 3+
// levels of nesting on the JSON side. Existing tests only go two deep.
func TestParseJSON_preservesFourLevelNestingOrder(t *testing.T) {
	in := []byte(`{"root":{"layer_z":{"layer_y":{"layer_x":{"zebra":1,"apple":2,"mango":3},"layer_before_x":"before"},"layer_before_y":"before"},"layer_after_z":"after"}}`)
	n, err := parseJSON(in)
	require.NoError(t, err)

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

// TestParseJSON_topLevelArray: existing tests only exercise arrays nested
// inside objects. parseJSONFromToken's '[' branch is reachable at the top
// level too, and this test locks that branch.
func TestParseJSON_topLevelArray(t *testing.T) {
	in := []byte(`["alpha","beta","gamma"]`)
	n, err := parseJSON(in)
	require.NoError(t, err)
	require.Equal(t, nodeSequence, n.kind)
	require.Len(t, n.seq, 3)
	assert.Equal(t, "alpha", n.seq[0].scalar)
	assert.Equal(t, "gamma", n.seq[2].scalar)

	// Round-trip must also work at the top level.
	out, err := writeJSON(n)
	require.NoError(t, err)
	roundTripped, err := parseJSON(out)
	require.NoError(t, err)
	require.Equal(t, nodeSequence, roundTripped.kind)
	require.Len(t, roundTripped.seq, 3)
	assert.Equal(t, "beta", roundTripped.seq[1].scalar)
}

// TestParseJSON_topLevelScalar: the parseJSONFromToken branch that returns
// newScalarNode for a non-delimiter token IS reachable at the top level
// (e.g., a JSON file containing just `"hello"` or `42`). Locks the branch
// so a regression restricting parseJSON to object-only inputs would fail
// here.
func TestParseJSON_topLevelScalar(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{"top-level string", []byte(`"hello"`)},
		{"top-level bool", []byte(`true`)},
		{"top-level null", []byte(`null`)},
		{"top-level integer", []byte(`42`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parseJSON(tc.in)
			require.NoError(t, err)
			require.Equal(t, nodeScalar, n.kind,
				"top-level JSON scalar must parse as nodeScalar, not fall through to a mapping or error")
		})
	}
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
