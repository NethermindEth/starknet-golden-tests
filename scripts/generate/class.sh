#!/bin/bash

rpc_url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    rpc_url="$2"
    shift 2
fi
class_hash="$1"
block_id_arg="$2"

if [ -z "$class_hash" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] <class_hash> [block_id]" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "block_id is optional: if starts with 0x, uses block_hash; if numeric, uses block_number; defaults to 'latest'." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 0x1efa8f84fd4dff9e2902ec88717cf0dafc8c188f80c3450615944a469428f7f" >&2
    echo "  $0 --rpc-url http://localhost:6060 0x1efa8f84fd4dff9e2902ec88717cf0dafc8c188f80c3450615944a469428f7f 100" >&2
    echo "  $0 --rpc-url http://localhost:6060 0x1efa8f84fd4dff9e2902ec88717cf0dafc8c188f80c3450615944a469428f7f 0x4223f3e4f2d1e6c9753b04974acdf045e602ccfe784ea6d3722697bda0fc4d2" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0 0x1efa8f84fd4dff9e2902ec88717cf0dafc8c188f80c3450615944a469428f7f" >&2
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

# Determine block_id format
if [ -z "$block_id_arg" ] || [ "$block_id_arg" = "latest" ]; then
    block_id_json='"latest"'
    test_suffix=""
elif [[ "$block_id_arg" == 0x* ]]; then
    block_id_json=$(jq -nc --arg hash "$block_id_arg" '{block_hash: $hash}')
    test_suffix="-${block_id_arg}"
else
    block_id_json=$(jq -nc --argjson num "$block_id_arg" '{block_number: $num}')
    test_suffix="-${block_id_arg}"
fi

test_name="${class_hash}${test_suffix}"

# starknet_getClass requires block_id
method="starknet_getClass"
input_file="tests/${network}/${method}/${test_name}.input.json"
input_dir="$(dirname "$input_file")"

mkdir -p "$input_dir"

jq -nc \
    --arg method "$method" \
    --arg class_hash "$class_hash" \
    --argjson block_id "$block_id_json" \
    '{id: 1, jsonrpc: "2.0", method: $method, params: {block_id: $block_id, class_hash: $class_hash}}' \
    >"$input_file"

echo "Processing $method..."
STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$test_name"

# starknet_getCompiledCasm only requires class_hash (no block_id)
method="starknet_getCompiledCasm"
input_file="tests/${network}/${method}/${class_hash}.input.json"
input_dir="$(dirname "$input_file")"

mkdir -p "$input_dir"

jq -nc \
    --arg method "$method" \
    --arg class_hash "$class_hash" \
    '{id: 1, jsonrpc: "2.0", method: $method, params: {class_hash: $class_hash}}' \
    >"$input_file"

echo "Processing $method..."
STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$class_hash"

echo "Done processing all methods for class $class_hash"
