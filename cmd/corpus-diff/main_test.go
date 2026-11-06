package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
	"github.com/NethermindEth/starknet-golden-tests/internal/store"
)

const fixtureDir = "../../tests/sepolia/v0.10"

// fixture is one golden test case from the tests/ folder.
type fixture struct {
	label  string
	method string
	params json.RawMessage
	output json.RawMessage
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	return loadFixturesFrom(t, fixtureDir)
}

func loadFixturesFrom(t *testing.T, dir string) []fixture {
	t.Helper()
	var out []fixture
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !strings.HasSuffix(path, ".input.json") {
			return err
		}
		input, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		output, err := os.ReadFile(strings.TrimSuffix(path, ".input.json") + ".output.json")
		if err != nil {
			return err
		}
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return err
		}
		label, _ := filepath.Rel(dir, strings.TrimSuffix(path, ".input.json"))
		out = append(out, fixture{label: label, method: req.Method, params: req.Params, output: output})
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	return out
}

// fakeNode serves the fixtures as a JSON-RPC node. mutate, if set, changes each
// result before it is sent.
type fakeNode struct {
	t       *testing.T
	chainID string
	byKey   map[string]json.RawMessage
	// mu serializes mutate, which the handler calls from many goroutines.
	mu     sync.Mutex
	mutate func(method string, result any) any
}

func newFakeNode(t *testing.T, fixtures []fixture) *fakeNode {
	n := &fakeNode{t: t, chainID: "0x534e5f5345504f4c4941", byKey: make(map[string]json.RawMessage)}
	for _, f := range fixtures {
		key, err := corpus.Key(f.method, f.params)
		require.NoError(t, err)
		n.byKey[key] = f.output
	}
	return n
}

func (n *fakeNode) start() string {
	srv := httptest.NewServer(http.HandlerFunc(n.serve))
	n.t.Cleanup(srv.Close)
	return srv.URL
}

type fakeRequest struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (n *fakeNode) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	assert.NoError(n.t, err)
	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		var reqs []fakeRequest
		assert.NoError(n.t, json.Unmarshal(body, &reqs))
		resps := make([]json.RawMessage, len(reqs))
		for i, req := range reqs {
			resps[i] = n.answer(req)
		}
		// A batch response can come back in any order.
		slices.Reverse(resps)
		assert.NoError(n.t, json.NewEncoder(w).Encode(resps))
		return
	}
	var req fakeRequest
	assert.NoError(n.t, json.Unmarshal(body, &req))
	_, err = w.Write(n.answer(req))
	assert.NoError(n.t, err)
}

func (n *fakeNode) answer(req fakeRequest) json.RawMessage {
	reply := func(result any) json.RawMessage {
		data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		assert.NoError(n.t, err)
		return data
	}
	switch req.Method {
	case "starknet_chainId":
		return reply(n.chainID)
	case "starknet_blockHashAndNumber":
		return reply(map[string]any{"block_number": 9000000, "block_hash": "0x123"})
	case "juno_version":
		return reply("v0.0.0-test")
	}
	key, err := corpus.Key(req.Method, req.Params)
	assert.NoError(n.t, err)
	output, ok := n.byKey[key]
	if !ok {
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID,
			"error": map[string]any{"code": -32601, "message": "Method not found"}})
		return data
	}
	var resp map[string]any
	dec := json.NewDecoder(bytes.NewReader(output))
	dec.UseNumber()
	assert.NoError(n.t, dec.Decode(&resp))
	resp["id"] = req.ID
	if n.mutate != nil && resp["result"] != nil {
		n.mu.Lock()
		resp["result"] = n.mutate(req.Method, resp["result"])
		n.mu.Unlock()
	}
	data, err := json.Marshal(resp)
	assert.NoError(n.t, err)
	return data
}

// writeCorpus writes a mixed corpus of the fixtures, batch entries of size
// batch (0 for plain requests).
func writeCorpus(t *testing.T, dir string, fixtures []fixture, batch int, edit func(meta map[string]any)) string {
	t.Helper()
	labels := make(map[string]string)
	var entries []any
	var current []any
	for i, f := range fixtures {
		id := i + 1
		labels[fmt.Sprint(id)] = f.label
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": f.method}
		if len(f.params) > 0 {
			req["params"] = f.params
		}
		if batch == 0 {
			entries = append(entries, req)
			continue
		}
		current = append(current, req)
		if len(current) == batch {
			entries = append(entries, current)
			current = nil
		}
	}
	require.Empty(t, current, "the fixture count must be a multiple of the batch size")
	meta := map[string]any{
		"method": "mixed", "rpcVersion": "0.10.3", "chainId": "0x534e5f5345504f4c4941",
		"count": len(entries), "batch": batch, "labels": labels,
	}
	if edit != nil {
		edit(meta)
	}
	data, err := json.Marshal(map[string]any{"meta": meta, "requests": entries})
	require.NoError(t, err)
	path := filepath.Join(dir, "handpicked.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := run(context.Background(), args, &out, &out)
	return code, out.String()
}

// reverseArrays reverses every array (of two or more items) that is the value
// of one of the named members, and counts the changes for each name.
func reverseArrays(v any, names map[string]bool, changed map[string]int) any {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			child = reverseArrays(child, names, changed)
			if arr, ok := child.([]any); ok && names[k] && len(arr) > 1 {
				slices.Reverse(arr)
				changed[k]++
			}
			v[k] = child
		}
	case []any:
		for i, child := range v {
			v[i] = reverseArrays(child, names, changed)
		}
	}
	return v
}

func TestRecordThenCompare(t *testing.T) {
	fixtures := loadFixtures(t)
	dir := t.TempDir()
	corpusPath := writeCorpus(t, dir, fixtures, 0, nil)
	recordsPath := filepath.Join(dir, "handpicked.jsonl")

	code, out := runCLI(t, "validate", corpusPath)
	require.Equal(t, exitOK, code, out)

	node := newFakeNode(t, fixtures)
	url := node.start()
	code, out = runCLI(t, "record", "--corpus", corpusPath, "--node", url, "--out", recordsPath)
	require.Equal(t, exitOK, code, out)
	// The 3 "latest" cases in tests/sepolia/v0.10.
	require.Contains(t, out, "3 head-dependent skipped")

	t.Run("same node passes", func(t *testing.T) {
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--node", url)
		require.Equal(t, exitOK, code, out)
		assert.NotContains(t, out, "FAIL")
	})

	t.Run("a record is stable when sets come back in another order", func(t *testing.T) {
		before, err := os.ReadFile(recordsPath)
		require.NoError(t, err)
		shuffled := newFakeNode(t, fixtures)
		changed := map[string]int{}
		shuffled.mutate = func(_ string, result any) any {
			return reverseArrays(result, map[string]bool{
				"storage_diffs": true, "storage_entries": true, "nonces": true,
				"deployed_contracts": true, "declared_classes": true,
				"deprecated_declared_classes": true, "replaced_classes": true,
			}, changed)
		}
		again := filepath.Join(t.TempDir(), "again.jsonl")
		code, out := runCLI(t, "record", "--corpus", corpusPath, "--node", shuffled.start(), "--out", again)
		require.Equal(t, exitOK, code, out)
		require.Positive(t, changed["storage_diffs"], "the fixtures must have sets to reorder")
		after, err := os.ReadFile(again)
		require.NoError(t, err)
		assert.Equal(t, recordLines(t, before), recordLines(t, after))
	})

	t.Run("sets in another order pass", func(t *testing.T) {
		shuffled := newFakeNode(t, fixtures)
		changed := map[string]int{}
		shuffled.mutate = func(_ string, result any) any {
			return reverseArrays(result, map[string]bool{
				"storage_diffs": true, "storage_entries": true, "nonces": true,
				"deployed_contracts": true, "declared_classes": true, "replaced_classes": true,
				"deprecated_declared_classes": true, "storage": true, "class_hashes": true,
				"declared_contracts": true,
			}, changed)
		}
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--node", shuffled.start())
		for _, name := range []string{
			"storage_diffs", "storage_entries", "nonces", "storage", "class_hashes", "declared_contracts",
		} {
			require.Positive(t, changed[name], "the fixtures must reorder %s", name)
		}
		require.Equal(t, exitOK, code, out)
	})

	t.Run("transactions in another order fail", func(t *testing.T) {
		shuffled := newFakeNode(t, fixtures)
		changed := map[string]int{}
		shuffled.mutate = func(_ string, result any) any {
			return reverseArrays(result, map[string]bool{"transactions": true}, changed)
		}
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--node", shuffled.start())
		require.Positive(t, changed["transactions"])
		require.Equal(t, exitFail, code, out)
		assert.Contains(t, out, "FAIL handpicked.json starknet_getBlockWithTxs/")
		assert.Contains(t, out, "/result/transactions/0/")
	})

	t.Run("events in another order fail", func(t *testing.T) {
		shuffled := newFakeNode(t, fixtures)
		changed := map[string]int{}
		shuffled.mutate = func(_ string, result any) any {
			return reverseArrays(result, map[string]bool{"events": true}, changed)
		}
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--node", shuffled.start(),
			"--filter", "starknet_getBlockWithReceipts")
		require.Positive(t, changed["events"])
		require.Equal(t, exitFail, code, out)
		assert.Contains(t, out, "/receipt/events/0/")
	})

	t.Run("report has the failures", func(t *testing.T) {
		shuffled := newFakeNode(t, fixtures)
		changed := map[string]int{}
		shuffled.mutate = func(_ string, result any) any {
			return reverseArrays(result, map[string]bool{"transactions": true}, changed)
		}
		report := filepath.Join(t.TempDir(), "report")
		code, _ := runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--node", shuffled.start(),
			"--report", report)
		require.Equal(t, exitFail, code)
		summary, err := os.ReadFile(filepath.Join(report, "summary.md"))
		require.NoError(t, err)
		assert.Contains(t, string(summary), "| starknet_getBlockWithTxs |")
		failures, err := os.ReadFile(filepath.Join(report, "failures.jsonl"))
		require.NoError(t, err)
		var first failureLine
		require.NoError(t, json.Unmarshal(bytes.SplitN(failures, []byte("\n"), 2)[0], &first))
		assert.Equal(t, "fail", first.Status)
		assert.NotEmpty(t, first.Diffs)
		assert.NotEmpty(t, first.Want)
	})

	t.Run("a request without a record fails", func(t *testing.T) {
		extra := append(slices.Clone(fixtures), fixture{
			label: "extra", method: "starknet_getBlockWithTxHashes",
			params: json.RawMessage(`{"block_id":{"block_number":1}}`),
		})
		corpusDir := t.TempDir()
		extraCorpus := writeCorpus(t, corpusDir, extra, 0, nil)
		code, out := runCLI(t, "compare", "--corpus", extraCorpus, "--records", recordsPath, "--node", url)
		require.Equal(t, exitFail, code, out)
		assert.Contains(t, out, "NOT RECORDED handpicked.json extra")
	})

	t.Run("records against records pass", func(t *testing.T) {
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--records", recordsPath)
		require.Equal(t, exitOK, code, out)
	})
}

func recordLines(t *testing.T, data []byte) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Greater(t, len(lines), 1)
	return lines[1:] // the header has the time
}

func TestCompareNodes(t *testing.T) {
	fixtures := loadFixtures(t)
	dir := t.TempDir()
	node := newFakeNode(t, fixtures)
	url := node.start()

	t.Run("batches split by id", func(t *testing.T) {
		batch := fixtures[:len(fixtures)/2*2]
		corpusPath := writeCorpus(t, dir, batch, 2, nil)
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--node", url, "--node", url)
		require.Equal(t, exitOK, code, out)
	})

	t.Run("equivalence groups", func(t *testing.T) {
		byLabel := make(map[string]int)
		for i, f := range fixtures {
			byLabel[f.label] = i + 1
		}
		number := byLabel["starknet_getBlockWithTxs/100"]
		hash := byLabel["starknet_getBlockWithTxs/100-0x1b2aa5b3d3549f4d20e5da0c4e883569062bba77b83deaf05023ff25ed263ab"]
		other := byLabel["starknet_getBlockWithTxs/2695000"]
		require.NotZero(t, number)
		require.NotZero(t, hash)
		require.NotZero(t, other)

		corpusPath := writeCorpus(t, dir, fixtures, 0, func(meta map[string]any) {
			meta["equivalent"] = [][]int{{number, hash}}
		})
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--node", url, "--node", url)
		require.Equal(t, exitOK, code, out)

		corpusPath = writeCorpus(t, dir, fixtures, 0, func(meta map[string]any) {
			meta["equivalent"] = [][]int{{number, other}}
		})
		code, out = runCLI(t, "compare", "--corpus", corpusPath, "--node", url, "--node", url)
		require.Equal(t, exitFail, code, out)
		assert.Contains(t, out, fmt.Sprintf("group [%d %d]", number, other))
	})

	t.Run("another chain is a setup error", func(t *testing.T) {
		corpusPath := writeCorpus(t, dir, fixtures, 0, func(meta map[string]any) {
			meta["chainId"] = "0x534e5f4d41494e"
		})
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--node", url, "--node", url)
		require.Equal(t, exitSetup, code, out)
		assert.Contains(t, out, "but the corpus is for 0x534e5f4d41494e")
	})

	t.Run("another spec version is a setup error", func(t *testing.T) {
		corpusPath := writeCorpus(t, dir, fixtures, 0, func(meta map[string]any) {
			meta["rpcVersion"] = "0.9.0"
		})
		code, out := runCLI(t, "compare", "--corpus", corpusPath, "--node", url, "--node", url)
		require.Equal(t, exitSetup, code, out)
	})
}

func TestValidateReportsProblems(t *testing.T) {
	fixtures := loadFixtures(t)[:3]
	corpusPath := writeCorpus(t, t.TempDir(), fixtures, 0, func(meta map[string]any) {
		meta["count"] = 7
		meta["labels"] = map[string]string{"99": "missing"}
	})
	code, out := runCLI(t, "validate", corpusPath)
	require.Equal(t, exitFail, code, out)
	assert.Contains(t, out, "meta.count is 7, but the corpus has 3 entries")
	assert.Contains(t, out, `meta.labels: no request has id "99"`)
}

// TestRoundTripAllFixtures records and compares every fixture folder
// (tests/<network>/v<version>), so the rules see each version's shapes.
func TestRoundTripAllFixtures(t *testing.T) {
	dirs, err := filepath.Glob("../../tests/*/v*")
	require.NoError(t, err)
	require.NotEmpty(t, dirs)
	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			var metadata struct {
				ChainID string `json:"chain_id"`
			}
			data, err := os.ReadFile(filepath.Join(dir, "..", "metadata.json"))
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(data, &metadata))
			specFiles, err := filepath.Glob(filepath.Join(dir, "starknet_specVersion", "*.output.json"))
			require.NoError(t, err)
			require.Len(t, specFiles, 1)
			data, err = os.ReadFile(specFiles[0])
			require.NoError(t, err)
			var spec struct {
				Result string `json:"result"`
			}
			require.NoError(t, json.Unmarshal(data, &spec))

			fixtures := loadFixturesFrom(t, dir)
			tmp := t.TempDir()
			corpusPath := writeCorpus(t, tmp, fixtures, 0, func(meta map[string]any) {
				meta["chainId"] = metadata.ChainID
				meta["rpcVersion"] = spec.Result
			})
			recordsPath := filepath.Join(tmp, "records.jsonl")
			node := newFakeNode(t, fixtures)
			node.chainID = metadata.ChainID
			code, out := runCLI(t, "record", "--corpus", corpusPath, "--node", node.start(), "--out", recordsPath)
			require.Equal(t, exitOK, code, out)

			shuffled := newFakeNode(t, fixtures)
			shuffled.chainID = metadata.ChainID
			changed := map[string]int{}
			shuffled.mutate = func(_ string, result any) any {
				return reverseArrays(result, map[string]bool{
					"storage_diffs": true, "storage_entries": true, "nonces": true,
					"deployed_contracts": true, "declared_classes": true, "replaced_classes": true,
					"deprecated_declared_classes": true, "storage": true, "class_hashes": true,
					"declared_contracts": true,
				}, changed)
			}
			code, out = runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--node", shuffled.start())
			require.Equal(t, exitOK, code, out)
			assert.NotContains(t, out, "FAIL")
		})
	}
}

func TestReportHidesTheAPIKey(t *testing.T) {
	all := loadFixtures(t)
	var fixtures []fixture
	for _, f := range all {
		if f.method == "starknet_getBlockWithTxs" {
			fixtures = append(fixtures, f)
			break
		}
	}
	dir := t.TempDir()
	corpusPath := writeCorpus(t, dir, fixtures, 0, nil)
	node := newFakeNode(t, all)
	live := node.start()

	// A node that passes the setup check, then drops each corpus request.
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req fakeRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
			return
		}
		if req.Method == "starknet_chainId" || req.Method == "starknet_specVersion" {
			_, err := w.Write(node.answer(req))
			assert.NoError(t, err)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if assert.NoError(t, err) {
			assert.NoError(t, conn.Close())
		}
	}))
	t.Cleanup(broken.Close)
	secretURL := strings.Replace(broken.URL, "http://", "http://user:secret@", 1) + "/rpc/secret?apikey=secret"

	report := filepath.Join(dir, "report")
	code, out := runCLI(t, "compare", "--corpus", corpusPath, "--node", live, "--node", secretURL, "--report", report)
	require.Equal(t, exitFail, code, out)
	assert.Contains(t, out, "node 2 (http://127.0.0.1:")
	assert.NotContains(t, out, "secret")
	failures, err := os.ReadFile(filepath.Join(report, "failures.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, failures)
	assert.NotContains(t, string(failures), "secret")
}

func TestDirectoryMode(t *testing.T) {
	fixtures := loadFixtures(t)
	node := newFakeNode(t, fixtures)
	url := node.start()
	root := t.TempDir()
	corpora := filepath.Join(root, "corpora")
	records := filepath.Join(root, "recorded")
	require.NoError(t, os.MkdirAll(filepath.Join(corpora, "sampled"), 0o755))
	require.NoError(t, os.Rename(writeCorpus(t, root, fixtures[:5], 0, nil), filepath.Join(corpora, "a.json")))
	require.NoError(t, os.Rename(writeCorpus(t, root, fixtures[5:9], 0, nil), filepath.Join(corpora, "sampled", "b.json")))

	code, out := runCLI(t, "record", "--corpus", corpora, "--node", url, "--out", records)
	require.Equal(t, exitOK, code, out)
	assert.FileExists(t, filepath.Join(records, "a.jsonl"))
	assert.FileExists(t, filepath.Join(records, "sampled", "b.jsonl"))

	code, out = runCLI(t, "compare", "--corpus", corpora, "--records", records, "--node", url)
	require.Equal(t, exitOK, code, out)
	assert.Contains(t, out, "total")

	t.Run("a corpus without records fails as not recorded", func(t *testing.T) {
		require.NoError(t, os.Rename(writeCorpus(t, root, fixtures[9:11], 0, nil), filepath.Join(corpora, "c.json")))
		t.Cleanup(func() { assert.NoError(t, os.Remove(filepath.Join(corpora, "c.json"))) })
		code, out := runCLI(t, "compare", "--corpus", corpora, "--records", records, "--node", url)
		require.Equal(t, exitFail, code, out)
		assert.Contains(t, out, "NOT RECORDED c.json "+fixtures[9].label)
		assert.Regexp(t, `total\s+9\s+0\s+0\s+2`, out, "the other corpora are still compared")
	})

	t.Run("a filter that matches nothing is a setup error", func(t *testing.T) {
		code, out := runCLI(t, "compare", "--corpus", corpora, "--records", records, "--node", url,
			"--filter", "starknet_nope*")
		require.Equal(t, exitSetup, code, out)
		assert.Contains(t, out, `--filter "starknet_nope*" matches no request`)
	})
}

func TestGroupWithoutFirstRecord(t *testing.T) {
	fixtures := loadFixtures(t)
	var blocks []fixture // three different blocks
	for _, f := range fixtures {
		if f.method == "starknet_getBlockWithTxs" && !strings.Contains(f.label, "-") && len(blocks) < 3 {
			blocks = append(blocks, f)
		}
	}
	require.Len(t, blocks, 3)
	recorded := blocks[1:]
	node := newFakeNode(t, fixtures)
	dir := t.TempDir()
	recordsPath := filepath.Join(dir, "records.jsonl")
	code, out := runCLI(t, "record", "--corpus", writeCorpus(t, dir, recorded, 0, nil),
		"--node", node.start(), "--out", recordsPath)
	require.Equal(t, exitOK, code, out)

	// Id 1 has no record, so ids 2 and 3 are compared with each other.
	withExtra := blocks
	corpusPath := writeCorpus(t, dir, withExtra, 0, func(meta map[string]any) {
		meta["equivalent"] = [][]int{{1, 2, 3}}
	})
	code, out = runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--records", recordsPath)
	require.Equal(t, exitFail, code, out)
	assert.Contains(t, out, "group [1 2 3]")
	assert.Contains(t, out, "id 3 differs from records "+recordsPath+" id 2")

	// With one recorded member, there is nothing to compare in the group.
	corpusPath = writeCorpus(t, dir, blocks[:2], 0, func(meta map[string]any) {
		meta["equivalent"] = [][]int{{1, 2}}
	})
	code, out = runCLI(t, "compare", "--corpus", corpusPath, "--records", recordsPath, "--records", recordsPath)
	require.Equal(t, exitFail, code, out, "id 1 is not recorded")
	assert.NotContains(t, out, "(equivalent)")
}

func TestRecordKeepsOneLinePerRequest(t *testing.T) {
	fixtures := loadFixtures(t)
	twice := []fixture{fixtures[0], fixtures[1], fixtures[0]}
	twice[2].label = "again"
	dir := t.TempDir()
	recordsPath := filepath.Join(dir, "records.jsonl")
	code, out := runCLI(t, "record", "--corpus", writeCorpus(t, dir, twice, 0, nil),
		"--node", newFakeNode(t, fixtures).start(), "--out", recordsPath)
	require.Equal(t, exitOK, code, out)
	recs, err := store.ReadFile(recordsPath)
	require.NoError(t, err)
	require.Len(t, recs.ByKey, 2)
	key, err := corpus.Key(fixtures[0].method, fixtures[0].params)
	require.NoError(t, err)
	assert.Equal(t, min(fixtures[0].label, "again"), recs.ByKey[key].Label, "the smallest label is kept")
}

func TestSetupErrorKeepsEarlierResults(t *testing.T) {
	fixtures := loadFixtures(t)
	node := newFakeNode(t, fixtures)
	url := node.start()
	root := t.TempDir()
	corpora := filepath.Join(root, "corpora")
	require.NoError(t, os.MkdirAll(corpora, 0o755))
	require.NoError(t, os.Rename(writeCorpus(t, root, fixtures[:3], 0, nil), filepath.Join(corpora, "a.json")))
	require.NoError(t, os.Rename(writeCorpus(t, root, fixtures[3:5], 0, func(meta map[string]any) {
		meta["chainId"] = "0x534e5f4d41494e"
	}), filepath.Join(corpora, "b.json")))

	report := filepath.Join(root, "report")
	code, out := runCLI(t, "compare", "--corpus", corpora, "--node", url, "--node", url, "--report", report)
	require.Equal(t, exitSetup, code, out)
	assert.Contains(t, out, "b.json: node 1")
	assert.Regexp(t, `total\s+3\s+0`, out, "a.json results are still printed")
	summary, err := os.ReadFile(filepath.Join(report, "summary.md"))
	require.NoError(t, err)
	assert.Contains(t, string(summary), "| total | 3 |")
}

func TestRecordsPathErrors(t *testing.T) {
	fixtures := loadFixtures(t)
	url := newFakeNode(t, fixtures).start()
	dir := t.TempDir()
	corpusPath := writeCorpus(t, dir, fixtures[:2], 0, nil)

	code, out := runCLI(t, "compare", "--corpus", corpusPath, "--records", filepath.Join(dir, "typo.jsonl"), "--node", url)
	require.Equal(t, exitSetup, code, out)
	assert.Contains(t, out, "--records:")

	code, out = runCLI(t, "compare", "--corpus", corpusPath, "--records", dir, "--node", url)
	require.Equal(t, exitSetup, code, out)
	assert.Contains(t, out, "--corpus is a file, so --records "+dir+" must be a file too")
}
