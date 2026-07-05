package integrate

import (
	"fmt"

	"github.com/goccy/go-yaml"
)

func parseYAML(data []byte) (*node, error) {
	if len(data) == 0 {
		return newMappingNode(), nil
	}
	var raw any
	if err := yaml.UnmarshalWithOptions(data, &raw, yaml.UseOrderedMap()); err != nil {
		return nil, err
	}
	return yamlValueToNode(raw), nil
}

func writeYAML(n *node) ([]byte, error) {
	return yaml.Marshal(nodeToYAMLValue(n))
}

func yamlValueToNode(v any) *node {
	switch val := v.(type) {
	case yaml.MapSlice:
		m := newMappingNode()
		for _, item := range val {
			key, ok := item.Key.(string)
			if !ok {
				key = fmt.Sprint(item.Key)
			}
			m.mapping.Set(key, yamlValueToNode(item.Value))
		}
		return m
	case []any:
		items := make([]*node, len(val))
		for i, item := range val {
			items[i] = yamlValueToNode(item)
		}
		return newSequenceNode(items...)
	default:
		return newScalarNode(v)
	}
}

func nodeToYAMLValue(n *node) any {
	if n == nil {
		return nil
	}
	switch n.kind {
	case nodeScalar:
		return n.scalar
	case nodeMapping:
		items := make(yaml.MapSlice, 0, len(n.mapping.keys))
		for _, k := range n.mapping.keys {
			items = append(items, yaml.MapItem{Key: k, Value: nodeToYAMLValue(n.mapping.values[k])})
		}
		return items
	case nodeSequence:
		items := make([]any, len(n.seq))
		for i, item := range n.seq {
			items[i] = nodeToYAMLValue(item)
		}
		return items
	}
	return nil
}
