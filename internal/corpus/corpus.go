// Package corpus reads corpora: JSON-RPC requests with a meta header, as
// written by Juno's corpus-gen or by hand.
package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/NethermindEth/starknet-golden-tests/internal/canon"
)

// MixedMethod is the meta method of a corpus that has more than one method.
const MixedMethod = "mixed"

// Meta is the corpus header. Fields that corpus-gen writes and this tool does
// not use (seed, sampling, generatedAt) are ignored.
type Meta struct {
	Method     string `json:"method"`
	RPCVersion string `json:"rpcVersion"`
	ChainID    string `json:"chainId"`
	Count      int    `json:"count"`
	Batch      int    `json:"batch"`
	// ExpectErrors marks a corpus whose requests return errors on purpose.
	// Benchmarks skip it.
	ExpectErrors bool `json:"expectErrors,omitempty"`
	// Labels maps a request id to a readable name.
	Labels map[string]string `json:"labels,omitempty"`
	// Equivalent lists groups of request ids that must give the same response.
	Equivalent [][]int `json:"equivalent,omitempty"`
}

// Corpus is a parsed corpus file.
type Corpus struct {
	Meta    Meta
	Entries []Entry
}

// Entry is one unit of work: a single request, or a batch that is sent as one
// JSON-RPC array.
type Entry struct {
	Raw      json.RawMessage
	Batch    bool
	Requests []Request
}

// Request is one JSON-RPC request of an entry.
type Request struct {
	ID     int
	Method string
	Params json.RawMessage
	// Key identifies the request by method and canonical params, not by id.
	Key string
}

type wireRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Load reads and parses a corpus file.
func Load(path string) (*Corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse parses a corpus.
func Parse(data []byte) (*Corpus, error) {
	var wire struct {
		Meta     *Meta             `json:"meta"`
		Requests []json.RawMessage `json:"requests"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	if wire.Meta == nil {
		return nil, fmt.Errorf("no meta")
	}
	c := &Corpus{Meta: *wire.Meta, Entries: make([]Entry, len(wire.Requests))}
	for i, raw := range wire.Requests {
		entry, err := parseEntry(raw)
		if err != nil {
			return nil, fmt.Errorf("request entry %d: %w", i, err)
		}
		c.Entries[i] = entry
	}
	return c, nil
}

func parseEntry(raw json.RawMessage) (Entry, error) {
	entry := Entry{Raw: raw, Batch: bytes.HasPrefix(bytes.TrimSpace(raw), []byte("["))}
	var wire []wireRequest
	if entry.Batch {
		if err := json.Unmarshal(raw, &wire); err != nil {
			return Entry{}, err
		}
	} else {
		var single wireRequest
		if err := json.Unmarshal(raw, &single); err != nil {
			return Entry{}, err
		}
		wire = []wireRequest{single}
	}
	entry.Requests = make([]Request, len(wire))
	for i, w := range wire {
		key, err := Key(w.Method, w.Params)
		if err != nil {
			return Entry{}, fmt.Errorf("id %d: params: %w", w.ID, err)
		}
		if w.JSONRPC != "2.0" {
			return Entry{}, fmt.Errorf("id %d: jsonrpc must be \"2.0\" (got %q)", w.ID, w.JSONRPC)
		}
		entry.Requests[i] = Request{ID: w.ID, Method: w.Method, Params: w.Params, Key: key}
	}
	return entry, nil
}

// Key returns the sha256 of the method and the canonical params, in hex.
// Absent, null, [] and {} params are the same: no params.
func Key(method string, params json.RawMessage) (string, error) {
	canonical, err := canon.Canonical(params)
	if err != nil {
		return "", err
	}
	switch string(canonical) {
	case "null", "[]", "{}":
		canonical = nil
	}
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{'\n'})
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Requests returns all requests in corpus order.
func (c *Corpus) Requests() []Request {
	var all []Request
	for _, e := range c.Entries {
		all = append(all, e.Requests...)
	}
	return all
}

// Digest is the sha256 of the request keys in corpus order. Ids, labels and
// meta do not change it.
func (c *Corpus) Digest() string {
	h := sha256.New()
	for _, r := range c.Requests() {
		h.Write([]byte(r.Key))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Label returns the label of a request id, or "".
func (c *Corpus) Label(id int) string {
	return c.Meta.Labels[strconv.Itoa(id)]
}
