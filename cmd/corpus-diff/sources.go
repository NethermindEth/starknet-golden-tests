package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
	"github.com/NethermindEth/starknet-golden-tests/internal/store"
)

// source gives the responses to the requests of a corpus.
type source interface {
	name() string
	// info returns the chain id and the spec version of the source.
	info(ctx context.Context) (chainID, specVersion string, err error)
	// fetch returns the responses to an entry by request id. A request with
	// no response is missing from the map.
	fetch(ctx context.Context, e corpus.Entry) (map[int]rpc.Response, error)
}

type nodeSource struct {
	label  string
	client *rpc.Client

	once        sync.Once
	chainID     string
	specVersion string
	infoErr     error
}

// newNodeSource returns a node source named label plus the URL's scheme and
// host. The rest of the URL can hold an API key, so it is never shown.
func newNodeSource(label, url string, concurrency int) *nodeSource {
	return &nodeSource{label: label + " (" + rpc.Redact(url) + ")", client: rpc.New(url, concurrency)}
}

func (s *nodeSource) name() string { return s.label }

func (s *nodeSource) info(ctx context.Context) (string, string, error) {
	s.once.Do(func() {
		if err := s.client.Call(ctx, "starknet_chainId", nil, &s.chainID); err != nil {
			s.infoErr = err
			return
		}
		s.infoErr = s.client.Call(ctx, "starknet_specVersion", nil, &s.specVersion)
	})
	return s.chainID, s.specVersion, s.infoErr
}

func (s *nodeSource) fetch(ctx context.Context, e corpus.Entry) (map[int]rpc.Response, error) {
	data, err := s.client.Post(ctx, e.Raw)
	if err != nil {
		return nil, err
	}
	return rpc.Split(data, e.Batch, e.Requests[0].ID)
}

// recordsSource serves recorded responses. A missing records file is a source
// with no records, so each request reports as not recorded.
type recordsSource struct {
	path    string
	records *store.Records // nil if the file does not exist
}

func newRecordsSource(path string) (*recordsSource, error) {
	recs, err := store.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &recordsSource{path: path}, nil
	}
	if err != nil {
		return nil, err
	}
	return &recordsSource{path: path, records: recs}, nil
}

func (s *recordsSource) name() string { return "records " + s.path }

func (s *recordsSource) info(context.Context) (string, string, error) {
	return s.records.Header.ChainID, s.records.Header.SpecVersion, nil
}

func (s *recordsSource) missing() bool { return s.records == nil }

func (s *recordsSource) fetch(_ context.Context, e corpus.Entry) (map[int]rpc.Response, error) {
	out := make(map[int]rpc.Response, len(e.Requests))
	if s.missing() {
		return out, nil
	}
	for _, r := range e.Requests {
		if rec, ok := s.records.ByKey[r.Key]; ok {
			out[r.ID] = rec.Response
		}
	}
	return out, nil
}

// checkSetup checks that every source serves the corpus chain and that all
// sources and the corpus have the same spec major.minor.
func checkSetup(ctx context.Context, c *corpus.Corpus, sources []source) error {
	want, err := corpus.MajorMinor(c.Meta.RPCVersion)
	if err != nil {
		return err
	}
	for _, s := range sources {
		if rs, ok := s.(*recordsSource); ok && rs.missing() {
			continue
		}
		chainID, version, err := s.info(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name(), err)
		}
		if !strings.EqualFold(chainID, c.Meta.ChainID) {
			return fmt.Errorf("%s: chain id %s, but the corpus is for %s", s.name(), chainID, c.Meta.ChainID)
		}
		got, err := corpus.MajorMinor(version)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name(), err)
		}
		if got != want {
			return fmt.Errorf("%s: spec version %s, but the corpus is for %s", s.name(), version, c.Meta.RPCVersion)
		}
	}
	return nil
}
