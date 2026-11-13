package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sync"

	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
	"github.com/NethermindEth/starknet-golden-tests/internal/diff"
	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
)

type sourceSpec struct {
	kind  string // "node" or "records"
	value string
}

// sourceFlag adds a source of one kind each time the flag is given. Both
// kinds share one list, so the order of the flags gives the source order.
type sourceFlag struct {
	kind  string
	specs *[]sourceSpec
}

func (f sourceFlag) String() string { return "" }

func (f sourceFlag) Set(v string) error {
	*f.specs = append(*f.specs, sourceSpec{kind: f.kind, value: v})
	return nil
}

type compareConfig struct {
	corpus      string
	sources     []sourceSpec
	report      string
	filter      string
	concurrency int
	opts        diff.Options
}

func runCompare(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var cfg compareConfig
	fs := newFlagSet("compare", stderr)
	fs.StringVar(&cfg.corpus, "corpus", "", "corpus file, or a directory of corpora (required)")
	fs.Var(sourceFlag{kind: "records", specs: &cfg.sources}, "records",
		"records file, or a directory if --corpus is one (a source; repeatable)")
	fs.Var(sourceFlag{kind: "node", specs: &cfg.sources}, "node", "JSON-RPC URL of a node (a source; repeatable)")
	fs.StringVar(&cfg.report, "report", "", "directory for summary.md and failures.jsonl")
	fs.StringVar(&cfg.filter, "filter", "", "compare only methods that match this glob, such as 'starknet_getBlock*'")
	fs.IntVar(&cfg.concurrency, "concurrency", defaultConcurrency(), "concurrent entries")
	fs.BoolVar(&cfg.opts.StrictErrorData, "strict-error-data", false, "also compare the data member of errors")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: corpus-diff compare --corpus <file|dir> (--records <file|dir> | --node <url>)... [flags]")
		fmt.Fprintln(stderr, "The first source is the reference. Each other source must give the same responses.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.corpus == "" || len(cfg.sources) < 2 {
		fs.Usage()
		return fmt.Errorf("compare needs --corpus and two or more sources")
	}
	if cfg.filter != "" {
		if _, err := path.Match(cfg.filter, ""); err != nil {
			return fmt.Errorf("--filter: %w", err)
		}
	}

	var recordsRoots []string
	for _, s := range cfg.sources {
		if s.kind == "records" {
			recordsRoots = append(recordsRoots, s.value)
		}
	}
	// A records root must exist, so a typo is a setup error. In directory mode,
	// a corpus without its file under the root fails as not recorded.
	corpusIsDir := isDir(cfg.corpus)
	for _, r := range recordsRoots {
		info, err := os.Stat(r)
		switch {
		case err != nil:
			return fmt.Errorf("--records: %w", err)
		case corpusIsDir && !info.IsDir():
			return fmt.Errorf("--corpus is a directory, so --records %s must be one too", r)
		case !corpusIsDir && info.IsDir():
			return fmt.Errorf("--corpus is a file, so --records %s must be a file too", r)
		}
	}
	files, err := corpusFiles(cfg.corpus)
	if err != nil {
		return err
	}

	nodes := make(map[string]*nodeSource)
	var all []result
	// A setup error stops the run, but the results so far are still reported.
	var setupErr error
	for _, f := range files {
		sources, err := openSources(cfg, f, nodes)
		if err == nil {
			var results []result
			results, err = compareCorpus(ctx, cfg, f, sources, stderr)
			all = append(all, results...)
		}
		if err != nil {
			setupErr = fmt.Errorf("%s: %w", f.path, err)
			break
		}
	}

	summary := summarize(all)
	printResults(stdout, all, summary)
	if cfg.report != "" {
		if err := writeReport(cfg.report, all, summary); err != nil {
			return errors.Join(setupErr, err)
		}
	}
	switch {
	case setupErr != nil:
		return setupErr
	case cfg.filter != "" && len(all) == 0:
		return fmt.Errorf("--filter %q matches no request", cfg.filter)
	case summary.total.fail > 0 || summary.total.notRecorded > 0:
		return errFailed
	}
	return nil
}

func openSources(cfg compareConfig, f corpusFile, nodes map[string]*nodeSource) ([]source, error) {
	sources := make([]source, len(cfg.sources))
	for i, s := range cfg.sources {
		switch s.kind {
		case "node":
			if nodes[s.value] == nil {
				nodes[s.value] = newNodeSource(fmt.Sprintf("node %d", i+1), s.value, cfg.concurrency)
			}
			sources[i] = nodes[s.value]
		case "records":
			rs, err := newRecordsSource(f.recordsPath(s.value))
			if err != nil {
				return nil, err
			}
			sources[i] = rs
		}
	}
	return sources, nil
}

type status int

const (
	statusPass status = iota
	statusFail
	statusSkip
	statusNotRecorded
)

// result is the outcome of one request, or of one equivalence group.
type result struct {
	corpus  string
	request corpus.Request
	label   string
	status  status
	// group is set for an equivalence group failure: the ids of the group.
	group []int
	// reference and other name the two sources of a failure.
	reference, other string
	diffs            []diff.Difference
	err              string
	want, got        rpc.Response
}

func compareCorpus(
	ctx context.Context,
	cfg compareConfig,
	f corpusFile,
	sources []source,
	stderr io.Writer,
) ([]result, error) {
	c, err := corpus.Load(f.path)
	if err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := checkSetup(ctx, c, sources); err != nil {
		return nil, err
	}
	digest := c.Digest()
	for _, s := range sources {
		rs, ok := s.(*recordsSource)
		if !ok {
			continue
		}
		if rs.missing() {
			fmt.Fprintf(stderr, "note: %s does not exist; every request of %s fails as not recorded\n",
				rs.path, f.name)
			continue
		}
		if rs.records.Header.CorpusDigest != digest {
			fmt.Fprintf(stderr, "note: %s was recorded from another version of %s; "+
				"requests without a record fail as not recorded\n", rs.path, f.name)
		}
	}

	grouped := make(map[int]bool)
	for _, g := range c.Meta.Equivalent {
		for _, id := range g {
			grouped[id] = true
		}
	}

	var (
		mu       sync.Mutex
		results  = make([][]result, len(c.Entries))
		groupRes = make([]map[int]rpc.Response, len(sources))
	)
	for i := range groupRes {
		groupRes[i] = make(map[int]rpc.Response)
	}
	forEach(ctx, len(c.Entries), cfg.concurrency, func(i int) {
		e := c.Entries[i]
		responses := make([]map[int]rpc.Response, len(sources))
		errs := make([]error, len(sources))
		for s, src := range sources {
			responses[s], errs[s] = src.fetch(ctx, e)
		}
		var out []result
		for _, r := range e.Requests {
			if cfg.filter != "" {
				if ok, _ := path.Match(cfg.filter, r.Method); !ok {
					continue
				}
			}
			out = append(out, compareRequest(c, f.name, r, sources, responses, errs, cfg.opts))
		}
		results[i] = out
		mu.Lock()
		defer mu.Unlock()
		for s := range sources {
			for _, r := range e.Requests {
				if resp, ok := responses[s][r.ID]; ok && grouped[r.ID] {
					groupRes[s][r.ID] = resp
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var all []result
	for _, r := range results {
		all = append(all, r...)
	}
	all = append(all, compareGroups(c, f.name, sources, groupRes, cfg)...)
	return all, nil
}

func compareRequest(
	c *corpus.Corpus,
	name string,
	r corpus.Request,
	sources []source,
	responses []map[int]rpc.Response,
	errs []error,
	opts diff.Options,
) result {
	res := result{corpus: name, request: r, label: c.Label(r.ID)}
	if r.HeadDependent() {
		res.status = statusSkip
		return res
	}
	for s := range sources {
		if errs[s] != nil {
			res.status, res.other, res.err = statusFail, sources[s].name(), errs[s].Error()
			return res
		}
		if _, ok := responses[s][r.ID]; !ok {
			res.status, res.other = statusNotRecorded, sources[s].name()
			if _, isNode := sources[s].(*nodeSource); isNode {
				res.status, res.err = statusFail, "no response with this id"
			}
			return res
		}
	}
	want := responses[0][r.ID]
	for s := 1; s < len(sources); s++ {
		got := responses[s][r.ID]
		diffs, err := diff.Responses(r.Method, want, got, opts)
		if err != nil || len(diffs) > 0 {
			res.status = statusFail
			res.reference, res.other = sources[0].name(), sources[s].name()
			res.diffs, res.want, res.got = diffs, want, got
			if err != nil {
				res.err = err.Error()
			}
			return res
		}
	}
	return res
}

// compareGroups checks in each source that all requests of an equivalence
// group give the same response.
func compareGroups(
	c *corpus.Corpus,
	name string,
	sources []source,
	responses []map[int]rpc.Response,
	cfg compareConfig,
) []result {
	byID := make(map[int]corpus.Request)
	for _, r := range c.Requests() {
		byID[r.ID] = r
	}
	var out []result
	for _, group := range c.Meta.Equivalent {
		first := byID[group[0]]
		if first.HeadDependent() {
			continue
		}
		if cfg.filter != "" {
			if ok, _ := path.Match(cfg.filter, first.Method); !ok {
				continue
			}
		}
		res := result{corpus: name, request: first, label: c.Label(first.ID), group: group}
		compared := false
		for s, src := range sources {
			// Members without a response are reported on the request itself;
			// the first member with one is the reference.
			var present []int
			for _, id := range group {
				if _, ok := responses[s][id]; ok {
					present = append(present, id)
				}
			}
			if len(present) < 2 {
				continue
			}
			compared = true
			want := responses[s][present[0]]
			for _, id := range present[1:] {
				got := responses[s][id]
				diffs, err := diff.Responses(first.Method, want, got, cfg.opts)
				if err != nil || len(diffs) > 0 {
					res.status = statusFail
					res.reference = fmt.Sprintf("%s id %d", src.name(), present[0])
					res.other = fmt.Sprintf("%s id %d", src.name(), id)
					res.diffs, res.want, res.got = diffs, want, got
					if err != nil {
						res.err = err.Error()
					}
					break
				}
			}
			if res.status == statusFail {
				break
			}
		}
		if !compared {
			continue
		}
		out = append(out, res)
	}
	return out
}
