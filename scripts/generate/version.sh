#!/bin/bash

rpc_url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    rpc_url="$2"
    shift 2
fi
network="$1"

if [ -z "$network" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] <network>" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 mainnet" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0 mainnet" >&2
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
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$test_name"
done

echo "Done processing version methods"
