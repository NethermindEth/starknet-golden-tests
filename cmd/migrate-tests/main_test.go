package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
	"github.com/NethermindEth/starknet-golden-tests/internal/store"
)

func writeCase(t *testing.T, dir, name, input, output string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".input.json"), []byte(input), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".output.json"), []byte(output), 0o644))
}

// testTree writes a small tests/ folder for network "testnet".
func testTree(t *testing.T, latest bool) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tests")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "testnet"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "testnet", "metadata.json"), []byte(`{"chain_id":"0x5445"}`), 0o644))
	v := filepath.Join(root, "testnet", "v0.10")
	writeCase(t, v, "starknet_specVersion/default",
		`{"id":1,"jsonrpc":"2.0","method":"starknet_specVersion","params":[]}`,
		`{"id":1,"jsonrpc":"2.0","result":"0.10.3"}`)
	writeCase(t, v, "starknet_getStateUpdate/7",
		`{"id":1,"jsonrpc":"2.0","method":"starknet_getStateUpdate","params":{"block_id":{"block_number":7}}}`,
		`{"id":1,"jsonrpc":"2.0","result":{"state_diff":{"nonces":[{"contract_address":"0x2"},{"contract_address":"0x1"}]}}}`)
	writeCase(t, v, "starknet_getStateUpdate/7-0xab",
		`{"id":1,"jsonrpc":"2.0","method":"starknet_getStateUpdate","params":{"block_id":{"block_hash":"0xab"}}}`,
		`{"id":1,"jsonrpc":"2.0","result":{"state_diff":{"nonces":[{"contract_address":"0x1"},{"contract_address":"0x2"}]}}}`)
	writeCase(t, v, "starknet_traceBlockTransactions/with_trace_flags/RETURN_INITIAL_READS/7",
		`{"id":1,"jsonrpc":"2.0","method":"starknet_traceBlockTransactions","params":{"block_id":{"block_number":7},"trace_flags":["RETURN_INITIAL_READS"]}}`,
		`{"id":1,"jsonrpc":"2.0","error":{"code":24,"message":"Block not found"}}`)
	if latest {
		writeCase(t, v, "starknet_getClassHashAt/0x1",
			`{"id":1,"jsonrpc":"2.0","method":"starknet_getClassHashAt","params":{"block_id":"latest","contract_address":"0x1"}}`,
			`{"id":1,"jsonrpc":"2.0","result":"0xc1"}`)
	}
	return root
}

func TestMigrate(t *testing.T) {
	tests := testTree(t, true)
	out := t.TempDir()
	require.NoError(t, run([]string{"--tests", tests, "--out", out, "--pin", "testnet=5"}))

	c, err := corpus.Load(filepath.Join(out, "corpora", "testnet", "v0.10", "handpicked.json"))
	require.NoError(t, err)
	require.NoError(t, c.Validate())
	assert.Equal(t, "mixed", c.Meta.Method)
	assert.Equal(t, "0x5445", c.Meta.ChainID)
	assert.Equal(t, "0.10.3", c.Meta.RPCVersion)

	byLabel := make(map[string]corpus.Request)
	for _, r := range c.Requests() {
		byLabel[c.Label(r.ID)] = r
	}
	require.Len(t, byLabel, 5)
	assert.JSONEq(t, `{"block_id":{"block_number":5},"contract_address":"0x1"}`,
		string(byLabel["starknet_getClassHashAt/0x1"].Params), "latest is pinned")
	assert.Contains(t, byLabel, "starknet_traceBlockTransactions/with_trace_flags/RETURN_INITIAL_READS/7")
	assert.Equal(t, [][]int{{
		byLabel["starknet_getStateUpdate/7"].ID, byLabel["starknet_getStateUpdate/7-0xab"].ID,
	}}, c.Meta.Equivalent)

	recs, err := store.ReadFile(filepath.Join(out, "recorded", "testnet", "v0.10", "handpicked.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, c.Digest(), recs.Header.CorpusDigest)
	assert.Equal(t, "0.10.3", recs.Header.SpecVersion)
	require.Len(t, recs.ByKey, 5)
	for label, r := range byLabel {
		rec, ok := recs.ByKey[r.Key]
		require.True(t, ok, label)
		assert.Equal(t, label, rec.Label)
	}
	assert.JSONEq(t, `{"state_diff":{"nonces":[{"contract_address":"0x1"},{"contract_address":"0x2"}]}}`,
		string(recs.ByKey[byLabel["starknet_getStateUpdate/7"].Key].Response.Result), "sets are sorted")
	assert.JSONEq(t, `{"code":24,"message":"Block not found"}`,
		string(recs.ByKey[byLabel["starknet_traceBlockTransactions/with_trace_flags/RETURN_INITIAL_READS/7"].Key].Response.Error))
}

func TestMigrateIsStable(t *testing.T) {
	tests := testTree(t, false)
	read := func(out string) map[string]string {
		files := make(map[string]string)
		require.NoError(t, filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(out, path)
			files[rel] = string(data)
			return err
		}))
		return files
	}
	a, b := t.TempDir(), t.TempDir()
	require.NoError(t, run([]string{"--tests", tests, "--out", a}))
	require.NoError(t, run([]string{"--tests", tests, "--out", b}))
	assert.Equal(t, read(a), read(b))
	for name, data := range read(a) {
		if strings.HasSuffix(name, ".json") {
			assert.Contains(t, data, "\n    {\"id\":1,", "one request per line")
		}
	}
}

func TestMigrateNeedsAPinForLatest(t *testing.T) {
	err := run([]string{"--tests", testTree(t, true), "--out", t.TempDir()})
	require.ErrorContains(t, err, `block_id "latest" needs --pin testnet=<block number>`)
}

func TestMigrateRejects(t *testing.T) {
	tests := map[string]struct {
		name, input, output string
		want                string
	}{
		"a head tag that is not pinned": {
			name:   "starknet_getEvents/1-latest",
			input:  `{"id":1,"jsonrpc":"2.0","method":"starknet_getEvents","params":{"filter":{"from_block":{"block_number":1},"to_block":"latest","chunk_size":10}}}`,
			output: `{"id":1,"jsonrpc":"2.0","result":{"events":[]}}`,
			want:   "starknet_getEvents/1-latest: the request depends on the chain head",
		},
		"a pre_confirmed block": {
			name:   "starknet_getStateUpdate/pre_confirmed",
			input:  `{"id":1,"jsonrpc":"2.0","method":"starknet_getStateUpdate","params":{"block_id":"pre_confirmed"}}`,
			output: `{"id":1,"jsonrpc":"2.0","result":{}}`,
			want:   "the request depends on the chain head",
		},
		"a number and hash pair with different outputs": {
			name:   "starknet_getStateUpdate/7-0xcd",
			input:  `{"id":1,"jsonrpc":"2.0","method":"starknet_getStateUpdate","params":{"block_id":{"block_hash":"0xcd"}}}`,
			output: `{"id":1,"jsonrpc":"2.0","result":{"state_diff":{"nonces":[]}}}`,
			want:   "starknet_getStateUpdate/7 and starknet_getStateUpdate/7-0xcd look like the same block",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := testTree(t, false)
			writeCase(t, filepath.Join(root, "testnet", "v0.10"), tt.name, tt.input, tt.output)
			err := run([]string{"--tests", root, "--out", t.TempDir()})
			require.ErrorContains(t, err, tt.want)
		})
	}

	t.Run("an output without input", func(t *testing.T) {
		root := testTree(t, false)
		orphan := filepath.Join(root, "testnet", "v0.10", "starknet_getStateUpdate", "9.output.json")
		require.NoError(t, os.WriteFile(orphan, []byte(`{"result":1}`), 0o644))
		require.ErrorContains(t, run([]string{"--tests", root, "--out", t.TempDir()}), "9.output.json has no input file")
	})

	t.Run("a folder that does not match the spec version", func(t *testing.T) {
		root := testTree(t, false)
		require.NoError(t, os.Rename(filepath.Join(root, "testnet", "v0.10"), filepath.Join(root, "testnet", "v0.9")))
		require.ErrorContains(t, run([]string{"--tests", root, "--out", t.TempDir()}),
			"folder v0.9 does not match spec version 0.10.3")
	})
}

func TestMigrateLabelsThatAreNotPairs(t *testing.T) {
	root := testTree(t, false)
	v := filepath.Join(root, "testnet", "v0.10")
	for _, name := range []string{"starknet_getEvents/5-7", "starknet_getEvents/7-address-0xab", "starknet_getNonce/0xab-7"} {
		writeCase(t, v, name,
			`{"id":1,"jsonrpc":"2.0","method":"starknet_getNonce","params":{"block_id":{"block_number":7},"contract_address":"`+name+`"}}`,
			`{"id":1,"jsonrpc":"2.0","result":"0x1"}`)
	}
	out := t.TempDir()
	require.NoError(t, run([]string{"--tests", root, "--out", out}))
	c, err := corpus.Load(filepath.Join(out, "corpora", "testnet", "v0.10", "handpicked.json"))
	require.NoError(t, err)
	assert.Len(t, c.Meta.Equivalent, 1, "only getStateUpdate/7 and 7-0xab pair up")
}

func TestMigrateRemovesAGoneFolder(t *testing.T) {
	root := testTree(t, false)
	out := t.TempDir()
	stale := []string{
		filepath.Join(out, "corpora", "gone", "v0.10", "handpicked.json"),
		filepath.Join(out, "recorded", "gone", "v0.10", "handpicked.jsonl"),
	}
	for _, f := range stale {
		require.NoError(t, os.MkdirAll(filepath.Dir(f), 0o755))
		require.NoError(t, os.WriteFile(f, []byte("{}"), 0o644))
	}
	require.NoError(t, run([]string{"--tests", root, "--out", out}))
	for _, f := range stale {
		assert.NoFileExists(t, f)
	}
	assert.FileExists(t, filepath.Join(out, "corpora", "testnet", "v0.10", "handpicked.json"))
}
