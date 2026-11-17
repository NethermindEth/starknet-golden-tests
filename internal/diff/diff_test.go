package diff_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NethermindEth/starknet-golden-tests/internal/diff"
	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
)

func result(s string) rpc.Response { return rpc.Response{Result: json.RawMessage(s)} }

func rpcError(s string) rpc.Response { return rpc.Response{Error: json.RawMessage(s)} }

func TestResponses(t *testing.T) {
	tests := map[string]struct {
		method    string
		want, got rpc.Response
		opts      diff.Options
		// paths is nil when the responses must be equal.
		paths []string
	}{
		"equal": {
			want: result(`{"a":[1,2],"b":"0x1"}`),
			got:  result(`{"b":"0x1","a":[1,2]}`),
		},
		"storage diffs in another order": {
			want: result(`{"state_diff":{"storage_diffs":[
				{"address":"0x1","storage_entries":[{"key":"0x1","value":"0x5"},{"key":"0x2","value":"0x6"}]},
				{"address":"0x2","storage_entries":[]}]}}`),
			got: result(`{"state_diff":{"storage_diffs":[
				{"address":"0x2","storage_entries":[]},
				{"address":"0x1","storage_entries":[{"key":"0x2","value":"0x6"},{"key":"0x1","value":"0x5"}]}]}}`),
		},
		"other state diff sets in another order": {
			want: result(`{"state_diff":{
				"nonces":[{"contract_address":"0x1","nonce":"0x1"},{"contract_address":"0x2","nonce":"0x1"}],
				"deployed_contracts":[{"address":"0x1","class_hash":"0xa"},{"address":"0x2","class_hash":"0xa"}],
				"declared_classes":[{"class_hash":"0x1","compiled_class_hash":"0xa"},{"class_hash":"0x2","compiled_class_hash":"0xb"}],
				"deprecated_declared_classes":["0x1","0x2"],
				"replaced_classes":[{"contract_address":"0x1","class_hash":"0xa"},{"contract_address":"0x2","class_hash":"0xa"}]}}`),
			got: result(`{"state_diff":{
				"nonces":[{"contract_address":"0x2","nonce":"0x1"},{"contract_address":"0x1","nonce":"0x1"}],
				"deployed_contracts":[{"address":"0x2","class_hash":"0xa"},{"address":"0x1","class_hash":"0xa"}],
				"declared_classes":[{"class_hash":"0x2","compiled_class_hash":"0xb"},{"class_hash":"0x1","compiled_class_hash":"0xa"}],
				"deprecated_declared_classes":["0x2","0x1"],
				"replaced_classes":[{"contract_address":"0x2","class_hash":"0xa"},{"contract_address":"0x1","class_hash":"0xa"}]}}`),
		},
		"state diff inside a trace": {
			method: "starknet_traceBlockTransactions",
			want:   result(`{"traces":[{"trace_root":{"state_diff":{"nonces":[{"contract_address":"0x1"},{"contract_address":"0x2"}]}}}]}`),
			got:    result(`{"traces":[{"trace_root":{"state_diff":{"nonces":[{"contract_address":"0x2"},{"contract_address":"0x1"}]}}}]}`),
		},
		"initial reads in another order": {
			want: result(`{"initial_reads":{"storage":[
				{"contract_address":"0x1","key":"0x2","value":"0x0"},{"contract_address":"0x1","key":"0x1","value":"0x0"}]}}`),
			got: result(`{"initial_reads":{"storage":[
				{"contract_address":"0x1","key":"0x1","value":"0x0"},{"contract_address":"0x1","key":"0x2","value":"0x0"}]}}`),
		},
		"proof facts keep their order": {
			// The transaction hash covers proof_facts in order.
			want:  result(`{"transactions":[{"proof_facts":["0x1","0x2"]}]}`),
			got:   result(`{"transactions":[{"proof_facts":["0x2","0x1"]}]}`),
			paths: []string{"/result/transactions/0/proof_facts/0", "/result/transactions/0/proof_facts/1"},
		},
		"storage proof nodes in another order": {
			method: "starknet_getStorageProof",
			want: result(`{"classes_proof":[{"node_hash":"0x1"},{"node_hash":"0x2"}],
				"contracts_storage_proofs":[[{"node_hash":"0x3"},{"node_hash":"0x4"}]]}`),
			got: result(`{"classes_proof":[{"node_hash":"0x2"},{"node_hash":"0x1"}],
				"contracts_storage_proofs":[[{"node_hash":"0x4"},{"node_hash":"0x3"}]]}`),
		},
		"contract storage proofs keep the request order": {
			method: "starknet_getStorageProof",
			want:   result(`{"contracts_storage_proofs":[[{"node_hash":"0x3"}],[{"node_hash":"0x4"}]]}`),
			got:    result(`{"contracts_storage_proofs":[[{"node_hash":"0x4"}],[{"node_hash":"0x3"}]]}`),
			paths:  []string{"/result/contracts_storage_proofs/0/0/node_hash", "/result/contracts_storage_proofs/1/0/node_hash"},
		},
		"transactions keep their order": {
			want:  result(`{"transactions":[{"transaction_hash":"0x1"},{"transaction_hash":"0x2"}]}`),
			got:   result(`{"transactions":[{"transaction_hash":"0x2"},{"transaction_hash":"0x1"}]}`),
			paths: []string{"/result/transactions/0/transaction_hash", "/result/transactions/1/transaction_hash"},
		},
		"calldata keeps its order": {
			want:  result(`{"calldata":["0x1","0x2"]}`),
			got:   result(`{"calldata":["0x2","0x1"]}`),
			paths: []string{"/result/calldata/0", "/result/calldata/1"},
		},
		"felt text is exact": {
			want:  result(`"0x1"`),
			got:   result(`"0x01"`),
			paths: []string{"/result"},
		},
		"number text is exact": {
			want:  result(`{"n":1}`),
			got:   result(`{"n":1.0}`),
			paths: []string{"/result/n"},
		},
		"missing member": {
			want:  result(`{"a":1,"b":2}`),
			got:   result(`{"a":1}`),
			paths: []string{"/result/b"},
		},
		"array length": {
			want:  result(`[1,2]`),
			got:   result(`[1]`),
			paths: []string{"/result"},
		},
		"result against error": {
			want:  result(`{"a":1}`),
			got:   rpcError(`{"code":24,"message":"Block not found"}`),
			paths: []string{"/error", "/result"},
		},
		"error data is ignored": {
			want: rpcError(`{"code":-32602,"message":"Invalid params","data":"x"}`),
			got:  rpcError(`{"code":-32602,"message":"Invalid params","data":"y"}`),
		},
		"error data with strict error data": {
			want:  rpcError(`{"code":-32602,"message":"Invalid params","data":"x"}`),
			got:   rpcError(`{"code":-32602,"message":"Invalid params","data":"y"}`),
			opts:  diff.Options{StrictErrorData: true},
			paths: []string{"/error/data"},
		},
		"error message": {
			want:  rpcError(`{"code":24,"message":"Block not found"}`),
			got:   rpcError(`{"code":24,"message":"Block missing"}`),
			paths: []string{"/error/message"},
		},
		"spec version patch": {
			method: "starknet_specVersion",
			want:   result(`"0.10.3"`),
			got:    result(`"0.10.4"`),
		},
		"spec version minor": {
			method: "starknet_specVersion",
			want:   result(`"0.10.3"`),
			got:    result(`"0.9.0"`),
			paths:  []string{"/result"},
		},
		"continuation token value": {
			method: "starknet_getEvents",
			want:   result(`{"events":[],"continuation_token":"12-0"}`),
			got:    result(`{"events":[],"continuation_token":"abc"}`),
		},
		"continuation token presence": {
			method: "starknet_getEvents",
			want:   result(`{"events":[],"continuation_token":"12-0"}`),
			got:    result(`{"events":[]}`),
			paths:  []string{"/result/continuation_token"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			diffs, err := diff.Responses(tt.method, tt.want, tt.got, tt.opts)
			require.NoError(t, err)
			var paths []string
			for _, d := range diffs {
				paths = append(paths, d.Path)
			}
			assert.Equal(t, tt.paths, paths)
		})
	}
}

func TestNormalize(t *testing.T) {
	a, err := diff.Normalize(json.RawMessage(`{"state_diff":{"nonces":[{"contract_address":"0x2"},{"contract_address":"0x1"}]},"b":1.50}`))
	require.NoError(t, err)
	b, err := diff.Normalize(json.RawMessage(`{"b":1.50,"state_diff":{"nonces":[{"contract_address":"0x1"},{"contract_address":"0x2"}]}}`))
	require.NoError(t, err)
	assert.Equal(t, `{"b":1.50,"state_diff":{"nonces":[{"contract_address":"0x1"},{"contract_address":"0x2"}]}}`, string(a))
	assert.Equal(t, a, b)
}
