package integrate

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func parseJSON(data []byte) (*node, error) {
	if len(data) == 0 {
		return newMappingNode(), nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return parseJSONFromToken(dec, tok)
}

func parseJSONFromToken(dec *json.Decoder, tok json.Token) (*node, error) {
	if delim, ok := tok.(json.Delim); ok {
		switch delim {
		case '{':
			return parseJSONObject(dec)
		case '[':
			return parseJSONArray(dec)
		default:
			return nil, fmt.Errorf("unexpected json delim %q", delim)
		}
	}
	return newScalarNode(tok), nil
}

func parseJSONObject(dec *json.Decoder) (*node, error) {
	m := newMappingNode()
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected json object key string, got %T", keyTok)
		}
		valTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		val, err := parseJSONFromToken(dec, valTok)
		if err != nil {
			return nil, err
		}
		m.mapping.Set(key, val)
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return nil, err
	}
	return m, nil
}

func parseJSONArray(dec *json.Decoder) (*node, error) {
	seq := newSequenceNode()
	for dec.More() {
		valTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		val, err := parseJSONFromToken(dec, valTok)
		if err != nil {
			return nil, err
		}
		seq.seq = append(seq.seq, val)
	}
	if _, err := dec.Token(); err != nil { // consume ']'
		return nil, err
	}
	return seq, nil
}

func writeJSON(n *node) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSONNode(&buf, n, "", "  "); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeJSONNode(buf *bytes.Buffer, n *node, currentIndent, indentUnit string) error {
	if n == nil {
		buf.WriteString("null")
		return nil
	}
	switch n.kind {
	case nodeScalar:
		b, err := json.Marshal(n.scalar)
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil
	case nodeMapping:
		if len(n.mapping.keys) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteString("{\n")
		inner := currentIndent + indentUnit
		for i, k := range n.mapping.keys {
			buf.WriteString(inner)
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(keyBytes)
			buf.WriteString(": ")
			if err := writeJSONNode(buf, n.mapping.values[k], inner, indentUnit); err != nil {
				return err
			}
			if i < len(n.mapping.keys)-1 {
				buf.WriteString(",")
			}
			buf.WriteString("\n")
		}
		buf.WriteString(currentIndent)
		buf.WriteString("}")
		return nil
	case nodeSequence:
		if len(n.seq) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteString("[\n")
		inner := currentIndent + indentUnit
		for i, item := range n.seq {
			buf.WriteString(inner)
			if err := writeJSONNode(buf, item, inner, indentUnit); err != nil {
				return err
			}
			if i < len(n.seq)-1 {
				buf.WriteString(",")
			}
			buf.WriteString("\n")
		}
		buf.WriteString(currentIndent)
		buf.WriteString("]")
		return nil
	}
	return nil
}
