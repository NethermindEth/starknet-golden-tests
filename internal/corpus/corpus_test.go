package corpus_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NethermindEth/starknet-golden-tests/internal/corpus"
)

func TestKey(t *testing.T) {
	a, err := corpus.Key("m", json.RawMessage(`{"a":1,"b":{"c":"0x1"}}`))
	require.NoError(t, err)
	b, err := corpus.Key("m", json.RawMessage(` { "b" : {"c":"0x1"}, "a" : 1 } `))
	require.NoError(t, err)
	assert.Equal(t, a, b, "member order and spaces do not change the key")

	c, err := corpus.Key("m", json.RawMessage(`{"a":1,"b":{"c":"0x01"}}`))
	require.NoError(t, err)
	assert.NotEqual(t, a, c, "felt text changes the key")

	d, err := corpus.Key("other", json.RawMessage(`{"a":1,"b":{"c":"0x1"}}`))
	require.NoError(t, err)
	assert.NotEqual(t, a, d, "the method changes the key")

	none, err := corpus.Key("m", nil)
	require.NoError(t, err)
	for _, empty := range []string{`null`, `[]`, `{}`} {
		k, err := corpus.Key("m", json.RawMessage(empty))
		require.NoError(t, err)
		assert.Equal(t, none, k, empty)
	}
}

func parse(t *testing.T, s string) *corpus.Corpus {
	t.Helper()
	c, err := corpus.Parse([]byte(s))
	require.NoError(t, err)
	return c
}

func TestValidate(t *testing.T) {
	valid := parse(t, `{"meta":{"method":"mixed","rpcVersion":"0.10.3","chainId":"0x1","count":2,"batch":0,
		"labels":{"1":"a"},"equivalent":[[1,2]]},
		"requests":[
			{"jsonrpc":"2.0","id":1,"method":"starknet_getBlockWithTxs","params":{"block_id":{"block_number":1}}},
			{"jsonrpc":"2.0","id":2,"method":"starknet_getBlockWithTxs","params":{"block_id":{"block_hash":"0x5"}}}]}`)
	require.NoError(t, valid.Validate())
	assert.Equal(t, "a", valid.Label(1))

	invalid := parse(t, `{"meta":{"method":"starknet_getBlockWithTxs","rpcVersion":"x","chainId":"","count":1,"batch":2,
		"labels":{"9":"a"},"equivalent":[[1]]},
		"requests":[
			{"jsonrpc":"2.0","id":1,"method":"starknet_getBlockWithTxs"},
			[{"jsonrpc":"2.0","id":1,"method":"starknet_getStateUpdate"}]]}`)
	err := invalid.Validate()
	require.Error(t, err)
	for _, want := range []string{
		"meta.chainId is empty",
		"meta.rpcVersion",
		"meta.count is 1, but the corpus has 2 entries",
		"entry 0 is not a batch, but meta.batch is 2",
		"entry 1 has 1 requests, but meta.batch is 2",
		"entry 1: id 1 is used more than once",
		"id 1: method starknet_getStateUpdate is not meta.method starknet_getBlockWithTxs",
		`meta.labels: no request has id "9"`,
		"meta.equivalent[0]: a group needs two or more ids",
	} {
		assert.ErrorContains(t, err, want)
	}
}

func TestDigestIgnoresIDs(t *testing.T) {
	a := parse(t, `{"meta":{},"requests":[{"jsonrpc":"2.0","id":1,"method":"m","params":[1]}]}`)
	b := parse(t, `{"meta":{"labels":{"7":"x"}},"requests":[{"jsonrpc":"2.0","id":7,"method":"m","params":[1]}]}`)
	c := parse(t, `{"meta":{},"requests":[{"jsonrpc":"2.0","id":1,"method":"m","params":[2]}]}`)
	assert.Equal(t, a.Digest(), b.Digest())
	assert.NotEqual(t, a.Digest(), c.Digest())
}

func TestHeadDependent(t *testing.T) {
	tests := map[string]struct {
		request string
		want    bool
	}{
		"block number":       {`{"method":"starknet_getBlockWithTxs","params":{"block_id":{"block_number":1}}}`, false},
		"block hash":         {`{"method":"starknet_getBlockWithTxs","params":{"block_id":{"block_hash":"0x1"}}}`, false},
		"latest":             {`{"method":"starknet_getBlockWithTxs","params":{"block_id":"latest"}}`, true},
		"pre_confirmed":      {`{"method":"starknet_getStateUpdate","params":["pre_confirmed"]}`, true},
		"latest in class":    {`{"method":"starknet_getClassAt","params":{"block_id":"latest","contract_address":"0x1"}}`, true},
		"head method":        {`{"method":"starknet_blockNumber"}`, true},
		"storage proof":      {`{"method":"starknet_getStorageProof","params":{"block_id":{"block_number":1}}}`, true},
		"events to_block":    {`{"method":"starknet_getEvents","params":{"filter":{"from_block":{"block_number":1},"to_block":{"block_number":2},"chunk_size":10}}}`, false},
		"events no to_block": {`{"method":"starknet_getEvents","params":{"filter":{"from_block":{"block_number":1},"chunk_size":10}}}`, true},
		"tx hash":            {`{"method":"starknet_getTransactionByHash","params":{"transaction_hash":"0x1"}}`, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var wire map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(tt.request), &wire))
			var corpusJSON = `{"meta":{},"requests":[{"jsonrpc":"2.0","id":1,"method":` + string(wire["method"])
			if p, ok := wire["params"]; ok {
				corpusJSON += `,"params":` + string(p)
			}
			corpusJSON += `}]}`
			c := parse(t, corpusJSON)
			assert.Equal(t, tt.want, c.Requests()[0].HeadDependent())
		})
	}
}
