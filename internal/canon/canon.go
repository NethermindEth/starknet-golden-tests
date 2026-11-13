// Package canon decodes JSON without loss and writes it in one canonical form.
package canon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Decode parses data into maps, slices, strings, bools, nil and json.Number,
// so that numbers keep their exact text.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return v, nil
}

// Encode writes v compactly, with object keys sorted and no HTML escapes.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Canonical returns the canonical form of data. Empty data stays empty.
func Canonical(data []byte) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	v, err := Decode(data)
	if err != nil {
		return nil, err
	}
	return Encode(v)
}
