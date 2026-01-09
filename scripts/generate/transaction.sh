#!/bin/bash

network="$1"
transaction_hash="$2"
rpc_url="$3"

if [ -z "$network" ] || [ -z "$transaction_hash" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 <network> <transaction_hash> <rpc_url>" >&2
    exit 1
fi

script_dir="$(dirname "$0")"
methods=(
    "starknet_getTransactionByHash"
    "starknet_getTransactionReceipt"
    "starknet_traceTransaction"
)

# Generate tests for basic transaction methods
for method in "${methods[@]}"; do
    input_file="tests/${network}/${method}/${transaction_hash}.input.json"
    input_dir="$(dirname "$input_file")"

    # Create input directory if it doesn't exist
    mkdir -p "$input_dir"

    # Generate input JSON for this method
    jq -nc \
        --arg method "$method" \
        --arg transaction_hash "$transaction_hash" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {transaction_hash: $transaction_hash}}' \
        >"$input_file"

    # Run write-output.sh for this method
    echo "Processing $method..."
    "${script_dir}/../run/write-output.sh" "$network" "$method" "$transaction_hash" "$rpc_url"
done

# Extract block info from starknet_getTransactionReceipt output
receipt_output="tests/${network}/starknet_getTransactionReceipt/${transaction_hash}.output.json"
block_number=$(jq -r '.result.block_number' "$receipt_output")
block_hash=$(jq -r '.result.block_hash' "$receipt_output")

if [ -z "$block_number" ] || [ "$block_number" = "null" ]; then
    echo "Error: Could not extract block_number from starknet_getTransactionReceipt output" >&2
    exit 1
fi

if [ -z "$block_hash" ] || [ "$block_hash" = "null" ]; then
    echo "Error: Could not extract block_hash from starknet_getTransactionReceipt output" >&2
    exit 1
fi

echo "Extracted block number: $block_number"
echo "Extracted block hash: $block_hash"

# Query starknet_getBlockWithTxHashes to find the transaction index
block_txs_response=$(jq -nc \
    --argjson block_number "$block_number" \
    '{id: 1, jsonrpc: "2.0", method: "starknet_getBlockWithTxHashes", params: {block_id: {block_number: $block_number}}}' \
    | "${script_dir}/../run/query-rpc.sh" "$rpc_url")

tx_index=$(echo "$block_txs_response" | jq -r --arg tx_hash "$transaction_hash" \
    '.result.transactions | to_entries | map(select(.value == $tx_hash)) | .[0].key')

if [ -z "$tx_index" ] || [ "$tx_index" = "null" ]; then
    echo "Error: Could not find transaction index in block" >&2
    exit 1
fi

echo "Found transaction at index: $tx_index"

# Generate starknet_getTransactionByBlockIdAndIndex tests
index_method="starknet_getTransactionByBlockIdAndIndex"
index_method_dir="tests/${network}/${index_method}"
mkdir -p "$index_method_dir"

# Test 1: by block_number
test_name_block_number="${transaction_hash}-block-number"
input_file="${index_method_dir}/${test_name_block_number}.input.json"

jq -nc \
    --argjson block_number "$block_number" \
    --argjson index "$tx_index" \
    '{id: 1, jsonrpc: "2.0", method: "starknet_getTransactionByBlockIdAndIndex", params: {block_id: {block_number: $block_number}, index: $index}}' \
    >"$input_file"

echo "Processing $index_method with block number..."
"${script_dir}/../run/write-output.sh" "$network" "$index_method" "$test_name_block_number" "$rpc_url"

# Test 2: by block_hash
test_name_block_hash="${transaction_hash}-block-hash"
input_file="${index_method_dir}/${test_name_block_hash}.input.json"

jq -nc \
    --arg block_hash "$block_hash" \
    --argjson index "$tx_index" \
    '{id: 1, jsonrpc: "2.0", method: "starknet_getTransactionByBlockIdAndIndex", params: {block_id: {block_hash: $block_hash}, index: $index}}' \
    >"$input_file"

echo "Processing $index_method with block hash..."
"${script_dir}/../run/write-output.sh" "$network" "$index_method" "$test_name_block_hash" "$rpc_url"

# Verify outputs match starknet_getTransactionByHash
echo "Comparing starknet_getTransactionByBlockIdAndIndex outputs with starknet_getTransactionByHash..."
tx_by_hash_output="tests/${network}/starknet_getTransactionByHash/${transaction_hash}.output.json"

# Compare block number variant
block_number_output="${index_method_dir}/${test_name_block_number}.output.json"
if ! diff --color=auto -u \
    <(jq '.' "$tx_by_hash_output") \
    <(jq '.' "$block_number_output"); then
    echo "  ❌ starknet_getTransactionByBlockIdAndIndex (block-number) output differs from starknet_getTransactionByHash" >&2
    exit 1
fi
echo "  ✅ starknet_getTransactionByBlockIdAndIndex (block-number) matches starknet_getTransactionByHash"

# Compare block hash variant
block_hash_output="${index_method_dir}/${test_name_block_hash}.output.json"
if ! diff --color=auto -u \
    <(jq '.' "$tx_by_hash_output") \
    <(jq '.' "$block_hash_output"); then
    echo "  ❌ starknet_getTransactionByBlockIdAndIndex (block-hash) output differs from starknet_getTransactionByHash" >&2
    exit 1
fi
echo "  ✅ starknet_getTransactionByBlockIdAndIndex (block-hash) matches starknet_getTransactionByHash"

echo "Done processing all methods for transaction $transaction_hash"
