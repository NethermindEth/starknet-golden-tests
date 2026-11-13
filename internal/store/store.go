// Package store reads and writes recorded responses as JSON lines: a header
// line, then one line for each request, sorted by request key.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
)

// Header describes how and where the responses were recorded.
type Header struct {
	ChainID      string `json:"chainId"`
	SpecVersion  string `json:"specVersion"`
	CorpusDigest string `json:"corpusDigest"`
	Node         string `json:"node,omitempty"`
	Head         *Head  `json:"head,omitempty"`
	RecordedAt   string `json:"recordedAt,omitempty"`
}

// Head is the chain head of the node when the recording started.
type Head struct {
	BlockNumber uint64 `json:"block_number"`
	BlockHash   string `json:"block_hash"`
}

// Record is the recorded response to one request.
type Record struct {
	Key      string       `json:"key"`
	Method   string       `json:"method"`
	Label    string       `json:"label,omitempty"`
	Response rpc.Response `json:"response"`
}

// Records is a header and its records by key.
type Records struct {
	Header Header
	ByKey  map[string]Record
}

// Write writes the header and the records, sorted by key.
func Write(w io.Writer, header Header, records []Record) error {
	records = slices.Clone(records)
	slices.SortFunc(records, func(a, b Record) int { return strings.Compare(a.Key, b.Key) })
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(struct {
		Header Header `json:"header"`
	}{header}); err != nil {
		return err
	}
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// WriteFile writes the records to path, and makes its directory.
func WriteFile(path string, header Header, records []Record) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := Write(f, header, records); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

// Read reads records written by Write.
func Read(r io.Reader) (*Records, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var first struct {
		Header *Header `json:"header"`
	}
	if err := json.Unmarshal(line, &first); err != nil || first.Header == nil {
		return nil, fmt.Errorf("line 1 is not a header")
	}
	recs := &Records{Header: *first.Header, ByKey: make(map[string]Record)}
	for n := 2; ; n++ {
		line, err := br.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var rec Record
			if jsonErr := json.Unmarshal(line, &rec); jsonErr != nil {
				return nil, fmt.Errorf("line %d: %w", n, jsonErr)
			}
			recs.ByKey[rec.Key] = rec
		}
		if errors.Is(err, io.EOF) {
			return recs, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// ReadFile reads records from path.
func ReadFile(path string) (*Records, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	recs, err := Read(f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return recs, nil
}
