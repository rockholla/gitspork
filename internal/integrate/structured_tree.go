package integrate

type nodeKind int

const (
	nodeScalar nodeKind = iota
	nodeMapping
	nodeSequence
)

type node struct {
	kind    nodeKind
	scalar  any
	mapping *orderedMap
	seq     []*node
}

type orderedMap struct {
	keys   []string
	values map[string]*node
}

func newScalarNode(v any) *node {
	return &node{kind: nodeScalar, scalar: v}
}

func newMappingNode() *node {
	return &node{kind: nodeMapping, mapping: newOrderedMap()}
}

func newSequenceNode(items ...*node) *node {
	return &node{kind: nodeSequence, seq: items}
}

func newOrderedMap() *orderedMap {
	return &orderedMap{values: map[string]*node{}}
}

func (m *orderedMap) Set(k string, v *node) {
	if _, ok := m.values[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.values[k] = v
}

func (m *orderedMap) Get(k string) (*node, bool) {
	v, ok := m.values[k]
	return v, ok
}

func (m *orderedMap) Keys() []string {
	return m.keys
}
