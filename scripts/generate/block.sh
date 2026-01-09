#!/bin/bash

network="$1"
block_number="$2"
rpc_url="$3"

if [ -z "$network" ] || [ -z "$block_number" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 <network> <block_number> <rpc_url>" >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 mainnet 100 http://localhost:6060" >&2
    exit 1
fi

script_dir="$(dirname "$0")"
methods=(
    "starknet_getBlockTransactionCount"
    "starknet_getBlockWithReceipts"
    "starknet_getBlockWithTxHashes"
    "starknet_getBlockWithTxs"
    "starknet_getStateUpdate"
    "starknet_traceBlockTransactions"
)

for method in "${methods[@]}"; do
    input_file="tests/${network}/${method}/${block_number}.input.json"
    input_dir="$(dirname "$input_file")"

    # Create input directory if it doesn't exist
    mkdir -p "$input_dir"

    # Generate input JSON for this method
    jq -nc \
        --arg method "$method" \
        --argjson block_number "$block_number" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {block_id: {block_number: $block_number}}}' \
        >"$input_file"

    # Run write-output.sh for this method
    echo "Processing $method with block number..."
    "${script_dir}/../run/write-output.sh" "$network" "$method" "$block_number" "$rpc_url"
done

# Extract block hash from starknet_getBlockWithTxHashes output
block_hash=$(jq -r '.result.block_hash' "tests/${network}/starknet_getBlockWithTxHashes/${block_number}.output.json")

if [ -z "$block_hash" ] || [ "$block_hash" = "null" ]; then
    echo "Error: Could not extract block_hash from starknet_getBlockWithTxHashes output" >&2
    exit 1
fi

echo "Extracted block hash: $block_hash"

# Generate tests with block hash input
for method in "${methods[@]}"; do
    test_name="${block_number}-${block_hash}"
    input_file="tests/${network}/${method}/${test_name}.input.json"

    # Generate input JSON with block_hash
    jq -nc \
        --arg method "$method" \
        --arg block_hash "$block_hash" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {block_id: {block_hash: $block_hash}}}' \
        >"$input_file"

    echo "Processing $method with block hash..."
    "${script_dir}/../run/write-output.sh" "$network" "$method" "$test_name" "$rpc_url"
done

# Diff outputs from block number vs block hash queries
echo "Comparing block number vs block hash outputs..."
for method in "${methods[@]}"; do
    block_number_output="tests/${network}/${method}/${block_number}.output.json"
    block_hash_output="tests/${network}/${method}/${block_number}-${block_hash}.output.json"

    if ! diff --color=auto -u \
        <(jq '.' "$block_number_output") \
        <(jq '.' "$block_hash_output"); then
        echo "  ❌ $method outputs differ" >&2
        exit 1
    fi
    echo "  ✅ $method outputs match"
done

echo "Done processing all methods for block $block_number"
