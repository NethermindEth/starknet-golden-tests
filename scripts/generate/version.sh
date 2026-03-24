#!/bin/bash

rpc_url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    rpc_url="$2"
    shift 2
fi

if [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>]" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0" >&2
    exit 1
fi

script_dir="$(dirname "$0")"

# Auto-detect network
echo "🔍 Auto-detecting network by querying starknet_chainId..."
if ! tests_folder=$(STARKNET_RPC="$rpc_url" "${script_dir}/../run/detect-network.sh") || [ -z "$tests_folder" ]; then
    exit 1
fi
network=$(basename "$tests_folder")
echo "✅ Using network: $network"

# Detect spec version
echo "🔍 Detecting spec version..."
if ! spec_version=$(STARKNET_RPC="$rpc_url" "${script_dir}/../run/detect-version.sh") || [ -z "$spec_version" ]; then
    echo "Error: Could not detect spec version" >&2
    exit 1
fi
echo "✅ Spec version: $spec_version"

methods=(
    "starknet_specVersion"
    "starknet_chainId"
)

for method in "${methods[@]}"; do
    test_name="default"
    input_file="tests/${network}/v${spec_version}/${method}/${test_name}.input.json"
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
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$spec_version" "$method" "$test_name"
done

echo "Done processing version methods"
