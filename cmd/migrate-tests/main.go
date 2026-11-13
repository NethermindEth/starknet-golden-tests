// Command migrate-tests converts the golden test cases in tests/ to corpora and
// records for corpus-diff:
//
//	tests/<network>/v<version>/<method>/[with_<flag>/<VALUE>/]<name>.input.json
//	  -> corpora/<network>/v<version>/handpicked.json
//	tests/.../<name>.output.json
//	  -> recorded/<network>/v<version>/handpicked.jsonl
//
// The output depends only on tests/ and the pins, so CI can check that it is up
// to date. Requests with block_id "latest" change with the chain head, so they
// are pinned to a fixed block of their network (defaultPins, or --pin).
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/NethermindEth/starknet-golden-tests/internal/canon"
	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
	"github.com/NethermindEth/starknet-golden-tests/internal/diff"
	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
	"github.com/NethermindEth/starknet-golden-tests/internal/store"
)

const corpusName = "handpicked"

// defaultPins are the blocks that replace block_id "latest". Each is at or
// below the L1-accepted head of its network when it was chosen.
var defaultPins = map[string]uint64{"sepolia": 18065000}

// pins maps a network to the block number that replaces block_id "latest".
type pins map[string]uint64

func (p pins) String() string { return fmt.Sprint(map[string]uint64(p)) }

func (p pins) Set(v string) error {
	network, block, ok := strings.Cut(v, "=")
	n, err := strconv.ParseUint(block, 10, 64)
	if !ok || network == "" || err != nil {
		return fmt.Errorf("want <network>=<block number>, got %q", v)
	}
	p[network] = n
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("migrate-tests", flag.ContinueOnError)
	testsDir := flags.String("tests", "tests", "folder with <network>/v<version>/ test cases")
	outDir := flags.String("out", ".", "folder for corpora/ and recorded/")
	pinned := pins{}
	for network, block := range defaultPins {
		pinned[network] = block
	}
	flags.Var(pinned, "pin", "<network>=<block number> to use for block_id \"latest\" (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	versions, err := filepath.Glob(filepath.Join(*testsDir, "*", "v*"))
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return fmt.Errorf("%s has no <network>/v<version> folder", *testsDir)
	}
	if err := prune(*outDir, versions); err != nil {
		return err
	}
	for _, dir := range versions {
		if err := migrate(dir, *outDir, pinned); err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
	}
	return nil
}

// prune removes the generated files of a <network>/v<version> that is no
// longer in tests/.
func prune(outDir string, versions []string) error {
	keep := make(map[string]bool, len(versions))
	for _, dir := range versions {
		keep[filepath.Join(filepath.Base(filepath.Dir(dir)), filepath.Base(dir))] = true
	}
	for _, pattern := range []string{
		filepath.Join(outDir, "corpora", "*", "*", corpusName+".json"),
		filepath.Join(outDir, "recorded", "*", "*", corpusName+".jsonl"),
	} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}
		for _, f := range files {
			version := filepath.Dir(f)
			if keep[filepath.Join(filepath.Base(filepath.Dir(version)), filepath.Base(version))] {
				continue
			}
			if err := os.Remove(f); err != nil {
				return err
			}
			fmt.Printf("removed %s: its tests/ folder is gone\n", f)
		}
	}
	return nil
}

// testCase is one input and output pair.
type testCase struct {
	label  string
	method string
	params json.RawMessage
	output rpc.Response
}

func migrate(dir, outDir string, pinned pins) error {
	version := filepath.Base(dir)
	network := filepath.Base(filepath.Dir(dir))
	chainID, err := readChainID(filepath.Join(filepath.Dir(dir), "metadata.json"))
	if err != nil {
		return err
	}
	cases, err := readCases(dir)
	if err != nil {
		return err
	}
	specVersion, err := findSpecVersion(cases)
	if err != nil {
		return err
	}
	if mm, err := corpus.MajorMinor(specVersion); err != nil || "v"+mm != version {
		return fmt.Errorf("folder %s does not match spec version %s", version, specVersion)
	}

	labels := make(map[string]string, len(cases))
	requests := make([]json.RawMessage, len(cases))
	records := make([]store.Record, len(cases))
	ids := make(map[string]int, len(cases))
	for i, tc := range cases {
		id := i + 1
		params, err := pinLatest(tc.params, network, pinned)
		if err != nil {
			return fmt.Errorf("%s: %w", tc.label, err)
		}
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": tc.method}
		if len(params) > 0 {
			req["params"] = params
		}
		if requests[i], err = canon.Encode(req); err != nil {
			return err
		}
		key, err := corpus.Key(tc.method, params)
		if err != nil {
			return err
		}
		if (corpus.Request{Method: tc.method, Params: params}).HeadDependent() {
			return fmt.Errorf("%s: the request depends on the chain head; pin it to a block", tc.label)
		}
		result, err := diff.Normalize(tc.output.Result)
		if err != nil {
			return fmt.Errorf("%s: %w", tc.label, err)
		}
		labels[strconv.Itoa(id)] = tc.label
		ids[tc.label] = id
		records[i] = store.Record{
			Key:      key,
			Method:   tc.method,
			Label:    tc.label,
			Response: rpc.Response{Result: result, Error: tc.output.Error},
		}
	}

	meta := corpus.Meta{
		Method:     corpus.MixedMethod,
		RPCVersion: specVersion,
		ChainID:    chainID,
		Count:      len(requests),
		Labels:     labels,
	}
	if meta.Equivalent, err = byNumberAndHash(cases, ids); err != nil {
		return err
	}
	data, err := encodeCorpus(meta, requests)
	if err != nil {
		return err
	}
	c, err := corpus.Parse(data)
	if err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}

	corpusPath := filepath.Join(outDir, "corpora", network, version, corpusName+".json")
	if err := os.MkdirAll(filepath.Dir(corpusPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(corpusPath, data, 0o644); err != nil {
		return err
	}
	header := store.Header{
		ChainID:      chainID,
		SpecVersion:  specVersion,
		CorpusDigest: c.Digest(),
		Node:         "migrated from tests/" + network + "/" + version,
	}
	recordsPath := filepath.Join(outDir, "recorded", network, version, corpusName+".jsonl")
	if err := store.WriteFile(recordsPath, header, records); err != nil {
		return err
	}
	fmt.Printf("%s: %d requests -> %s, %s\n", dir, len(cases), corpusPath, recordsPath)
	return nil
}

func readChainID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var metadata struct {
		ChainID string `json:"chain_id"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if metadata.ChainID == "" {
		return "", fmt.Errorf("%s: no chain_id", path)
	}
	return metadata.ChainID, nil
}

// readCases reads the cases under dir in path order, so ids are stable.
func readCases(dir string) ([]testCase, error) {
	var cases []testCase
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if base, ok := strings.CutSuffix(path, ".output.json"); ok {
			if _, err := os.Stat(base + ".input.json"); err != nil {
				return fmt.Errorf("%s has no input file", path)
			}
			return nil
		}
		if !strings.HasSuffix(path, ".input.json") {
			return nil
		}
		base := strings.TrimSuffix(path, ".input.json")
		label, err := filepath.Rel(dir, base)
		if err != nil {
			return err
		}
		tc, err := readCase(base, filepath.ToSlash(label))
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		cases = append(cases, tc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, errors.New("no *.input.json")
	}
	return cases, nil
}

func readCase(base, label string) (testCase, error) {
	input, err := os.ReadFile(base + ".input.json")
	if err != nil {
		return testCase{}, err
	}
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return testCase{}, fmt.Errorf("input: %w", err)
	}
	if req.Method == "" {
		return testCase{}, errors.New("input has no method")
	}
	output, err := os.ReadFile(base + ".output.json")
	if err != nil {
		return testCase{}, err
	}
	var resp rpc.Response
	if err := json.Unmarshal(output, &resp); err != nil {
		return testCase{}, fmt.Errorf("output: %w", err)
	}
	if resp.Result == nil && resp.Error == nil {
		return testCase{}, errors.New("output has no result or error")
	}
	params, err := canon.Canonical(req.Params)
	if err != nil {
		return testCase{}, fmt.Errorf("params: %w", err)
	}
	return testCase{label: label, method: req.Method, params: params, output: resp}, nil
}

func findSpecVersion(cases []testCase) (string, error) {
	for _, tc := range cases {
		if tc.method == "starknet_specVersion" {
			var version string
			if err := json.Unmarshal(tc.output.Result, &version); err != nil {
				return "", fmt.Errorf("starknet_specVersion output: %w", err)
			}
			return version, nil
		}
	}
	return "", errors.New("no starknet_specVersion case to give the spec version")
}

// pinLatest replaces block_id "latest" in the params with the pinned block
// number of the network.
func pinLatest(params json.RawMessage, network string, pinned pins) (json.RawMessage, error) {
	if !bytes.Contains(params, []byte(`"latest"`)) {
		return params, nil
	}
	v, err := canon.Decode(params)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(map[string]any)
	if !ok || obj["block_id"] != "latest" {
		return params, nil
	}
	block, ok := pinned[network]
	if !ok {
		return nil, fmt.Errorf(`block_id "latest" needs --pin %s=<block number>`, network)
	}
	obj["block_id"] = map[string]any{"block_number": block}
	return canon.Encode(obj)
}

// hashLabel matches a case for a block by hash: <number>-0x<hash>.
var hashLabel = regexp.MustCompile(`^(.*/)?(\d+)-0x[0-9a-fA-F]+$`)

// byNumberAndHash groups each by-hash case with the by-number case of the same
// block and method. The label shape alone could pair unrelated cases (such as
// <block>-<storage key>), so the two recorded outputs must also be equal.
func byNumberAndHash(cases []testCase, ids map[string]int) ([][]int, error) {
	byLabel := make(map[string]testCase, len(cases))
	for _, tc := range cases {
		byLabel[tc.label] = tc
	}
	var groups [][]int
	for _, tc := range cases {
		m := hashLabel.FindStringSubmatch(tc.label)
		if m == nil {
			continue
		}
		numberCase, ok := byLabel[m[1]+m[2]]
		if !ok {
			continue
		}
		diffs, err := diff.Responses(tc.method, numberCase.output, tc.output, diff.Options{StrictErrorData: true})
		if err != nil {
			return nil, err
		}
		if len(diffs) > 0 {
			return nil, fmt.Errorf("%s and %s look like the same block by number and by hash, "+
				"but their outputs differ: %s", numberCase.label, tc.label, diffs[0])
		}
		groups = append(groups, []int{ids[numberCase.label], ids[tc.label]})
	}
	slices.SortFunc(groups, func(a, b []int) int { return cmp.Compare(a[0], b[0]) })
	return groups, nil
}

// encodeCorpus writes the meta indented and one request per line, so that a
// change to one case is a small diff.
func encodeCorpus(meta corpus.Meta, requests []json.RawMessage) ([]byte, error) {
	metaJSON, err := json.MarshalIndent(meta, "  ", "  ")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("{\n  \"meta\": ")
	buf.Write(metaJSON)
	buf.WriteString(",\n  \"requests\": [\n")
	for i, r := range requests {
		buf.WriteString("    ")
		buf.Write(r)
		if i < len(requests)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("  ]\n}\n")
	return buf.Bytes(), nil
}
