// Package jsonx reads and writes devcontainer.json documents.
//
// devcontainer.json is JSONC: the reference CLI parses it with a tolerant
// parser that accepts // comments and trailing commas, so we must too.
// Documents are represented as map[string]any with json.Number for numbers,
// which keeps integers such as forwardPorts from being reformatted as floats.
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tailscale/hujson"
)

// Parse decodes a JSONC document into a generic map.
func Parse(data []byte) (map[string]any, error) {
	std, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("parse JSONC: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(std))
	dec.UseNumber()

	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}

	obj, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("document must be a JSON object literal, got %T", doc)
	}
	return obj, nil
}

// ReadFile parses the JSONC document at path.
func ReadFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

// Marshal renders a document as indented JSON. Keys are sorted by
// encoding/json, which makes the output stable across runs.
func Marshal(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "\t")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Clone returns a deep copy of v so that merging never aliases a layer's data.
func Clone(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, sub := range t {
			out[k] = Clone(sub)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, sub := range t {
			out[i] = Clone(sub)
		}
		return out
	default:
		return v
	}
}

// Equal reports whether two decoded JSON values are structurally equal.
func Equal(a, b any) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}
