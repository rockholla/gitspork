package integrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nodeToPlain flattens a *node into ordinary Go values so table-driven tests can compare
// expected output against actual using assert.Equal. Mapping order is preserved via a
// slice of alternating key/value entries (`[]any{k1, v1, k2, v2, ...}`) so we can check
// both the values AND the key order in a single assertion.
func nodeToPlain(n *node) any {
	if n == nil {
		return nil
	}
	switch n.kind {
	case nodeScalar:
		return n.scalar
	case nodeMapping:
		flat := make([]any, 0, len(n.mapping.keys)*2)
		for _, k := range n.mapping.keys {
			flat = append(flat, k, nodeToPlain(n.mapping.values[k]))
		}
		return flat
	case nodeSequence:
		out := make([]any, len(n.seq))
		for i, item := range n.seq {
			out[i] = nodeToPlain(item)
		}
		return out
	}
	return nil
}

// mapping builds a mapping node from an alternating (key, value) sequence for terse
// test fixtures. Values may be *node instances or plain primitives (auto-wrapped as
// scalars). Panics if given an odd number of args or a non-string key.
func mapping(kv ...any) *node {
	if len(kv)%2 != 0 {
		panic("mapping requires an even number of args (k1, v1, k2, v2, ...)")
	}
	n := newMappingNode()
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			panic("mapping keys must be strings")
		}
		var val *node
		switch v := kv[i+1].(type) {
		case *node:
			val = v
		default:
			val = newScalarNode(v)
		}
		n.mapping.Set(key, val)
	}
	return n
}

// seq builds a sequence node. Items may be *node instances or plain primitives.
func seq(items ...any) *node {
	n := newSequenceNode()
	for _, item := range items {
		switch v := item.(type) {
		case *node:
			n.seq = append(n.seq, v)
		default:
			n.seq = append(n.seq, newScalarNode(v))
		}
	}
	return n
}

func TestMergeNodes_scalarPreferSrc(t *testing.T) {
	dst := newScalarNode("dst")
	src := newScalarNode("src")
	result := mergeNodes(dst, src, true)
	assert.Equal(t, "src", result.scalar)
}

func TestMergeNodes_scalarPreferDst(t *testing.T) {
	dst := newScalarNode("dst")
	src := newScalarNode("src")
	result := mergeNodes(dst, src, false)
	assert.Equal(t, "dst", result.scalar)
}

func TestMergeNodes_kindMismatch_preferredWins(t *testing.T) {
	t.Run("scalar vs mapping, prefer src (mapping)", func(t *testing.T) {
		dst := newScalarNode("dst")
		src := mapping("k", "v")
		result := mergeNodes(dst, src, true)
		assert.Equal(t, nodeMapping, result.kind)
	})
	t.Run("scalar vs mapping, prefer dst (scalar)", func(t *testing.T) {
		dst := newScalarNode("dst")
		src := mapping("k", "v")
		result := mergeNodes(dst, src, false)
		assert.Equal(t, nodeScalar, result.kind)
		assert.Equal(t, "dst", result.scalar)
	})
}

func TestMergeNodes_mapping_deepMergeNoCollisions(t *testing.T) {
	dst := mapping("a", "dst-a", "b", "dst-b")
	src := mapping("c", "src-c", "d", "src-d")
	result := mergeNodes(dst, src, true)

	// preferSrc=true → src keys first, dst-only appended
	assert.Equal(t, []any{"c", "src-c", "d", "src-d", "a", "dst-a", "b", "dst-b"}, nodeToPlain(result))
}

func TestMergeNodes_mapping_preferSrcCollisionSrcWins(t *testing.T) {
	dst := mapping("shared", "dst-value", "dst-only", "d")
	src := mapping("shared", "src-value", "src-only", "s")
	result := mergeNodes(dst, src, true)

	// preferSrc=true → src key order first, dst-only appended after
	assert.Equal(t, []any{"shared", "src-value", "src-only", "s", "dst-only", "d"}, nodeToPlain(result))
}

func TestMergeNodes_mapping_preferDstCollisionDstWins(t *testing.T) {
	dst := mapping("shared", "dst-value", "dst-only", "d")
	src := mapping("shared", "src-value", "src-only", "s")
	result := mergeNodes(dst, src, false)

	// preferSrc=false → dst key order first, src-only appended after
	assert.Equal(t, []any{"shared", "dst-value", "dst-only", "d", "src-only", "s"}, nodeToPlain(result))
}

func TestMergeNodes_mapping_recursesIntoNestedMaps(t *testing.T) {
	dst := mapping("outer", mapping("inner1", "dst-1", "shared", "dst-shared"))
	src := mapping("outer", mapping("inner2", "src-2", "shared", "src-shared"))
	result := mergeNodes(dst, src, true)

	// src.outer key order is [inner2, shared] → preferred-order iteration produces
	// [inner2, shared] first, then dst-only keys [inner1] appended.
	assert.Equal(t,
		[]any{"outer", []any{"inner2", "src-2", "shared", "src-shared", "inner1", "dst-1"}},
		nodeToPlain(result),
	)
}

func TestMergeNodes_mapping_downstreamOnlyKeysSurvive(t *testing.T) {
	// this is the semantic that the prefer-upstream write-target bug used to violate:
	// even with preferSrc=true, non-conflicting dst keys must land in the output
	dst := mapping("dst-only", "d-value")
	src := mapping("src-only", "s-value")
	result := mergeNodes(dst, src, true)

	found, ok := result.mapping.Get("dst-only")
	require.True(t, ok, "dst-only key must survive merge under preferSrc=true")
	assert.Equal(t, "d-value", found.scalar)
}

func TestMergeNodes_primitiveSequence_concatAndDedupe(t *testing.T) {
	t.Run("preferSrc: src items first then dst-only items", func(t *testing.T) {
		dst := seq("node", "python")
		src := seq("go", "node")
		result := mergeNodes(dst, src, true)
		assert.Equal(t, []any{"go", "node", "python"}, nodeToPlain(result))
	})
	t.Run("preferDst: dst items first then src-only items", func(t *testing.T) {
		dst := seq("node", "python")
		src := seq("go", "node")
		result := mergeNodes(dst, src, false)
		assert.Equal(t, []any{"node", "python", "go"}, nodeToPlain(result))
	})
	t.Run("mixed primitive types dedupe by value", func(t *testing.T) {
		dst := seq(1, 2, "same")
		src := seq(2, 3, "same")
		result := mergeNodes(dst, src, true)
		assert.Equal(t, []any{2, 3, "same", 1}, nodeToPlain(result))
	})
}

func TestMergeNodes_objectSequence_wholesaleReplaceByPreferred(t *testing.T) {
	dst := seq(mapping("name", "d1"), mapping("name", "d2"))
	src := seq(mapping("name", "s1"))
	t.Run("preferSrc: result is src slice unchanged", func(t *testing.T) {
		result := mergeNodes(dst, src, true)
		assert.Equal(t, []any{[]any{"name", "s1"}}, nodeToPlain(result))
	})
	t.Run("preferDst: result is dst slice unchanged", func(t *testing.T) {
		result := mergeNodes(dst, src, false)
		assert.Equal(t, []any{[]any{"name", "d1"}, []any{"name", "d2"}}, nodeToPlain(result))
	})
}

func TestMergeNodes_mixedSequence_wholesaleReplaceByPreferred(t *testing.T) {
	// if either side has any non-scalar element, safe default is wholesale replace
	dst := seq("a", mapping("k", "v"))
	src := seq("b", "c")
	result := mergeNodes(dst, src, true)
	assert.Equal(t, []any{"b", "c"}, nodeToPlain(result))
}

func TestMergeNodes_nilHandling(t *testing.T) {
	t.Run("nil dst returns src", func(t *testing.T) {
		src := mapping("k", "v")
		result := mergeNodes(nil, src, true)
		assert.Same(t, src, result)
	})
	t.Run("nil src returns dst", func(t *testing.T) {
		dst := mapping("k", "v")
		result := mergeNodes(dst, nil, true)
		assert.Same(t, dst, result)
	})
}
