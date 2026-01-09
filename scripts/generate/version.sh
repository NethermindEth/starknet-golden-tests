#!/bin/bash

network="$1"
rpc_url="$2"

if [ -z "$network" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 <network> <rpc_url>" >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 mainnet http://localhost:6060" >&2
    exit 1
fi

script_dir="$(dirname "$0")"
methods=(
    "starknet_specVersion"
    "starknet_chainId"
)

for method in "${methods[@]}"; do
    test_name="default"
    input_file="tests/${network}/${method}/${test_name}.input.json"
    input_dir="$(dirname "$input_file")"

    # Create input directory if it doesn't exist
    mkdir -p "$input_dir"

    # Generate input JSON for this method (no params)
    jq -nc \
        --arg method "$method" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: []}' \
        >"$input_file"

    # Run write-output.sh for this method
    echo "Processing $method..."
    "${script_dir}/../run/write-output.sh" "$network" "$method" "$test_name" "$rpc_url"
done

echo "Done processing version methods"
