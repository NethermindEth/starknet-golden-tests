package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/NethermindEth/starknet-golden-tests/internal/canon"
	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
	"github.com/NethermindEth/starknet-golden-tests/internal/diff"
	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
	"github.com/NethermindEth/starknet-golden-tests/internal/store"
)

// volatileStatuses are block and transaction statuses that can still change.
var volatileStatuses = map[string]bool{
	"RECEIVED":       true,
	"CANDIDATE":      true,
	"PRE_CONFIRMED":  true,
	"ACCEPTED_ON_L2": true,
}

type recordConfig struct {
	corpus        string
	node          string
	out           string
	allowVolatile bool
	concurrency   int
}

func runRecord(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var cfg recordConfig
	fs := newFlagSet("record", stderr)
	fs.StringVar(&cfg.corpus, "corpus", "", "corpus file, or a directory of corpora (required)")
	fs.StringVar(&cfg.node, "node", "", "JSON-RPC URL of the node to record (required)")
	fs.StringVar(&cfg.out, "out", "", "records file, or a directory if --corpus is one (required)")
	fs.BoolVar(&cfg.allowVolatile, "allow-volatile", false,
		"record responses with a status that can still change (not ACCEPTED_ON_L1)")
	fs.IntVar(&cfg.concurrency, "concurrency", defaultConcurrency(), "concurrent requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.corpus == "" || cfg.node == "" || cfg.out == "" {
		return fmt.Errorf("record needs --corpus, --node and --out")
	}
	files, err := corpusFiles(cfg.corpus)
	if err != nil {
		return err
	}
	node := newNodeSource("node", cfg.node, cfg.concurrency)
	for _, f := range files {
		if err := recordCorpus(ctx, cfg, node, f, stdout, stderr); err != nil {
			return fmt.Errorf("%s: %w", f.path, err)
		}
	}
	return nil
}

func recordCorpus(
	ctx context.Context,
	cfg recordConfig,
	node *nodeSource,
	f corpusFile,
	stdout, stderr io.Writer,
) error {
	c, err := corpus.Load(f.path)
	if err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if err := checkSetup(ctx, c, []source{node}); err != nil {
		return err
	}
	header, err := recordHeader(ctx, c, node)
	if err != nil {
		return err
	}

	var (
		mu       sync.Mutex
		records  []store.Record
		skipped  int
		problems []string
	)
	forEach(ctx, len(c.Entries), cfg.concurrency, func(i int) {
		e := c.Entries[i]
		responses, err := node.fetch(ctx, e)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			problems = append(problems, fmt.Sprintf("entry %d: %v", i, err))
			return
		}
		for _, r := range e.Requests {
			if r.HeadDependent() {
				skipped++
				continue
			}
			resp, ok := responses[r.ID]
			if !ok {
				problems = append(problems, fmt.Sprintf("id %d: no response", r.ID))
				continue
			}
			rec, problem := newRecord(c, r, resp, cfg.allowVolatile)
			if problem != "" {
				problems = append(problems, problem)
				continue
			}
			if resp.Error != nil && !c.Meta.ExpectErrors {
				fmt.Fprintf(stderr, "warning: %s: id %d (%s) returned an error: %s\n",
					f.name, r.ID, r.Method, resp.Error)
			}
			records = append(records, rec)
		}
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d requests not recorded:\n  %s", len(problems), indent(strings.Join(problems, "\n")))
	}
	records = uniqueRecords(records)
	errorResp := 0
	for _, r := range records {
		if r.Response.Error != nil {
			errorResp++
		}
	}
	if err := store.WriteFile(f.recordsPath(cfg.out), header, records); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "recorded %s: %d responses (%d errors), %d head-dependent skipped -> %s\n",
		f.name, len(records), errorResp, skipped, f.recordsPath(cfg.out))
	return nil
}

func recordHeader(ctx context.Context, c *corpus.Corpus, node *nodeSource) (store.Header, error) {
	chainID, specVersion, err := node.info(ctx)
	if err != nil {
		return store.Header{}, err
	}
	var head store.Head
	if err := node.client.Call(ctx, "starknet_blockHashAndNumber", nil, &head); err != nil {
		return store.Header{}, err
	}
	var version string
	// Only Juno serves juno_version; the header field is informative.
	_ = node.client.Call(ctx, "juno_version", nil, &version)
	return store.Header{
		ChainID:      chainID,
		SpecVersion:  specVersion,
		CorpusDigest: c.Digest(),
		Node:         version,
		Head:         &head,
		RecordedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// newRecord returns the record of a response, or a problem that stops the
// recording.
func newRecord(c *corpus.Corpus, r corpus.Request, resp rpc.Response, allowVolatile bool) (store.Record, string) {
	result, err := diff.Normalize(resp.Result)
	if err != nil {
		return store.Record{}, fmt.Sprintf("id %d: result: %v", r.ID, err)
	}
	if status := volatileStatus(result); status != "" && !allowVolatile {
		return store.Record{}, fmt.Sprintf(
			"id %d (%s): status %s can still change; record older blocks or use --allow-volatile",
			r.ID, r.Method, status)
	}
	return store.Record{
		Key:      r.Key,
		Method:   r.Method,
		Label:    c.Label(r.ID),
		Response: rpc.Response{Result: result, Error: resp.Error},
	}, ""
}

// volatileStatus returns the first status or finality_status in the result
// that can still change, or "".
func volatileStatus(result []byte) string {
	if len(result) == 0 {
		return ""
	}
	v, err := canon.Decode(result)
	if err != nil {
		return ""
	}
	return findVolatile(v)
}

func findVolatile(v any) string {
	switch v := v.(type) {
	case map[string]any:
		for _, k := range []string{"status", "finality_status"} {
			if s, ok := v[k].(string); ok && volatileStatuses[s] {
				return s
			}
		}
		for _, child := range v {
			if s := findVolatile(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range v {
			if s := findVolatile(child); s != "" {
				return s
			}
		}
	}
	return ""
}

// uniqueRecords keeps one record for each key. A corpus can hold the same
// request twice (random sampling can draw it again); the record with the
// smallest label is kept, so the output does not depend on fetch order.
func uniqueRecords(records []store.Record) []store.Record {
	slices.SortFunc(records, func(a, b store.Record) int {
		return cmp.Or(strings.Compare(a.Key, b.Key), strings.Compare(a.Label, b.Label))
	})
	return slices.CompactFunc(records, func(a, b store.Record) bool { return a.Key == b.Key })
}
