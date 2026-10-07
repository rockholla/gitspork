package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// resolveStructuredPath reads filePath (JSON or YAML, detected by extension),
// navigates to the dot-delimited dotPath within it, and returns the value as
// a string. Scalars are returned as-is; sequences are serialized as a JSON
// array string (e.g. ["a","b"]). Mapping nodes at the terminal path always
// return not-found.
//
// Returns ("", false, nil) when the file is absent or the path is not found.
// Returns ("", false, err) when the file exists but cannot be parsed, or when
// a path segment is malformed.
//
// Path segments may include bracket index notation to index into a sequence,
// e.g. "items[0].name" navigates to the element at index 0 of "items" and
// then reads "name" from that mapping. A bracket may instead hold a key=value
// selector: "items[name=web].port" takes the first mapping element of "items"
// whose "name" is the scalar "web". Selector values cannot contain '.' or ']'.
func resolveStructuredPath(filePath, dotPath string) (string, bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	var parseFn func([]byte) (*node, error)
	switch ext {
	case ".json":
		parseFn = parseJSON
	case ".yml", ".yaml":
		parseFn = parseYAML
	default:
		return "", false, fmt.Errorf("unsupported file extension %q for from_destination_structured (must be .json, .yml, or .yaml)", ext)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("error reading destination file %s: %v", filePath, err)
	}

	root, err := parseFn(data)
	if err != nil {
		return "", false, fmt.Errorf("error parsing destination file %s: %v", filePath, err)
	}

	current := root
	for _, seg := range strings.Split(dotPath, ".") {
		name, sel, hasSel, segErr := parsePathSegment(seg)
		if segErr != nil {
			return "", false, segErr
		}

		if current.kind != nodeMapping {
			return "", false, nil
		}
		child, ok := current.mapping.Get(name)
		if !ok {
			return "", false, nil
		}
		current = child

		if hasSel {
			if current.kind != nodeSequence {
				return "", false, nil
			}
			item, ok := sel.pick(current.seq)
			if !ok {
				return "", false, nil
			}
			current = item
		}
	}

	switch current.kind {
	case nodeScalar:
		if current.scalar == nil {
			return "", false, nil
		}
		return fmt.Sprint(current.scalar), true, nil
	case nodeSequence:
		v, err := sequenceToJSONString(current)
		if err != nil {
			return "", false, fmt.Errorf("serializing sequence at %q in %s: %w", dotPath, filePath, err)
		}
		return v, true, nil
	default: // nodeMapping
		return "", false, nil
	}
}

// pathSelector picks one element of a sequence: by index, or by the first
// mapping element whose key holds the scalar value.
type pathSelector struct {
	index      int
	key, value string
}

func (p pathSelector) pick(seq []*node) (*node, bool) {
	if p.key == "" {
		if p.index >= len(seq) {
			return nil, false
		}
		return seq[p.index], true
	}
	for _, item := range seq {
		if item.kind != nodeMapping {
			continue
		}
		if v, ok := item.mapping.Get(p.key); ok && v.kind == nodeScalar && v.scalar != nil && fmt.Sprint(v.scalar) == p.value {
			return item, true
		}
	}
	return nil, false
}

// parsePathSegment splits a dot-path segment into its field name and an
// optional bracket selector. "items[2]" → ("items", index 2, true, nil);
// "items[name=web]" → ("items", key "name" value "web", true, nil);
// "name" → ("name", _, false, nil).
func parsePathSegment(seg string) (name string, sel pathSelector, hasSel bool, err error) {
	open := strings.LastIndex(seg, "[")
	if open == -1 {
		return seg, sel, false, nil
	}
	if !strings.HasSuffix(seg, "]") {
		return "", sel, false, fmt.Errorf("invalid path segment %q: '[' without matching ']'", seg)
	}
	inner := seg[open+1 : len(seg)-1]
	if key, value, ok := strings.Cut(inner, "="); ok {
		if key == "" || value == "" {
			return "", sel, false, fmt.Errorf("invalid path segment %q: selector must be key=value", seg)
		}
		return seg[:open], pathSelector{key: key, value: value}, true, nil
	}
	i, convErr := strconv.Atoi(inner)
	if convErr != nil || i < 0 {
		return "", sel, false, fmt.Errorf("invalid path segment %q: index must be a non-negative integer or a key=value selector", seg)
	}
	return seg[:open], pathSelector{index: i}, true, nil
}

// sequenceToJSONString serializes a sequence node whose elements are all
// scalars into a JSON array string, e.g. ["foo","bar"]. Returns an error if
// any element is a non-scalar node (mapping or nested sequence).
func sequenceToJSONString(n *node) (string, error) {
	elems := make([]any, 0, len(n.seq))
	for _, item := range n.seq {
		if item.kind != nodeScalar {
			return "", fmt.Errorf("sequence element is not a scalar (kind=%d); only flat string/number arrays are supported", item.kind)
		}
		elems = append(elems, item.scalar)
	}
	b, err := json.Marshal(elems)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
