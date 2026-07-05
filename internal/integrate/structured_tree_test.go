package integrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderedMap_setNewKeyAppends(t *testing.T) {
	m := newOrderedMap()
	m.Set("first", newScalarNode("a"))
	m.Set("second", newScalarNode("b"))
	m.Set("third", newScalarNode("c"))
	assert.Equal(t, []string{"first", "second", "third"}, m.Keys())
}

func TestOrderedMap_setExistingKeyReplacesValueButKeepsPosition(t *testing.T) {
	m := newOrderedMap()
	m.Set("first", newScalarNode("a"))
	m.Set("second", newScalarNode("b"))
	m.Set("third", newScalarNode("c"))
	m.Set("second", newScalarNode("replaced"))

	assert.Equal(t, []string{"first", "second", "third"}, m.Keys())
	v, ok := m.Get("second")
	require.True(t, ok)
	require.Equal(t, nodeScalar, v.kind)
	assert.Equal(t, "replaced", v.scalar)
}

func TestOrderedMap_getMissingReturnsFalse(t *testing.T) {
	m := newOrderedMap()
	_, ok := m.Get("nope")
	assert.False(t, ok)
}

func TestNewScalarNode_carriesValueAndKind(t *testing.T) {
	n := newScalarNode(42)
	assert.Equal(t, nodeScalar, n.kind)
	assert.Equal(t, 42, n.scalar)
}

func TestNewMappingNode_initializesEmptyOrderedMap(t *testing.T) {
	n := newMappingNode()
	require.Equal(t, nodeMapping, n.kind)
	require.NotNil(t, n.mapping)
	assert.Empty(t, n.mapping.Keys())
}

func TestNewSequenceNode_carriesItemsAndKind(t *testing.T) {
	a := newScalarNode("a")
	b := newScalarNode("b")
	n := newSequenceNode(a, b)
	assert.Equal(t, nodeSequence, n.kind)
	require.Len(t, n.seq, 2)
	assert.Same(t, a, n.seq[0])
	assert.Same(t, b, n.seq[1])
}
