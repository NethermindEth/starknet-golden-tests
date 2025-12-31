#!/bin/bash

network="$1"
block_number="$2"
rpc_url="$3"

if [ -z "$network" ] || [ -z "$block_number" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 <network> <block_number> <rpc_url>" >&2
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
    echo "Processing $method..."
    "${script_dir}/../run/write-output.sh" "$network" "$method" "$block_number" "$rpc_url"
done

echo "Done processing all methods for block $block_number"
