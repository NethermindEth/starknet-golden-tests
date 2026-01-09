#!/bin/bash

rpc_url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    rpc_url="$2"
    shift 2
fi
contract_address="$1"
block_id_arg="$2"

if [ -z "$contract_address" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] <contract_address> [block_id]" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "block_id is optional: if starts with 0x, uses block_hash; if numeric, uses block_number; defaults to 'latest'." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 0x4f1cb86e5067045d8264cc542da0517b9afcc4219575a184a76265423e8a213" >&2
    echo "  $0 --rpc-url http://localhost:6060 0x4f1cb86e5067045d8264cc542da0517b9afcc4219575a184a76265423e8a213 100" >&2
    echo "  $0 --rpc-url http://localhost:6060 0x4f1cb86e5067045d8264cc542da0517b9afcc4219575a184a76265423e8a213 0x4223f3e4f2d1e6c9753b04974acdf045e602ccfe784ea6d3722697bda0fc4d2" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0 0x4f1cb86e5067045d8264cc542da0517b9afcc4219575a184a76265423e8a213" >&2
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
    block_id_arg_for_class=""
elif [[ "$block_id_arg" == 0x* ]]; then
    block_id_json=$(jq -nc --arg hash "$block_id_arg" '{block_hash: $hash}')
    test_suffix="-${block_id_arg}"
    block_id_arg_for_class="$block_id_arg"
else
    block_id_json=$(jq -nc --argjson num "$block_id_arg" '{block_number: $num}')
    test_suffix="-${block_id_arg}"
    block_id_arg_for_class="$block_id_arg"
fi

test_name="${contract_address}${test_suffix}"

methods=(
    "starknet_getClassAt"
    "starknet_getClassHashAt"
)

# starknet_getNonce - only if block_id is specified (not empty/latest)
if [ -n "$block_id_arg_for_class" ]; then
    methods+=("starknet_getNonce")
fi

for method in "${methods[@]}"; do
    input_file="tests/${network}/${method}/${test_name}.input.json"
    input_dir="$(dirname "$input_file")"

    mkdir -p "$input_dir"

    jq -nc \
        --arg method "$method" \
        --arg contract_address "$contract_address" \
        --argjson block_id "$block_id_json" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {block_id: $block_id, contract_address: $contract_address}}' \
        >"$input_file"

    echo "Processing $method..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$test_name"
done

# Extract class_hash from starknet_getClassHashAt output
class_hash_output="tests/${network}/starknet_getClassHashAt/${test_name}.output.json"
class_hash=$(jq -r '.result' "$class_hash_output")

if [ -z "$class_hash" ] || [ "$class_hash" = "null" ]; then
    echo "Error: Could not extract class_hash from starknet_getClassHashAt output" >&2
    exit 1
fi

echo "Extracted class hash: $class_hash"

# Call class.sh to generate class tests
echo "Generating class tests for $class_hash..."
STARKNET_RPC="$rpc_url" "${script_dir}/class.sh" "$class_hash" "$block_id_arg_for_class"

# Compare starknet_getClassAt output with starknet_getClass output
echo "Comparing starknet_getClassAt and starknet_getClass outputs..."
class_at_output="tests/${network}/starknet_getClassAt/${test_name}.output.json"
class_output="tests/${network}/starknet_getClass/${class_hash}${test_suffix}.output.json"

if ! diff --color=auto -u \
    <(jq '.' "$class_at_output") \
    <(jq '.' "$class_output"); then
    echo "  ❌ starknet_getClassAt and starknet_getClass outputs differ" >&2
    exit 1
fi
echo "  ✅ starknet_getClassAt and starknet_getClass outputs match"

echo "Done processing all methods for contract $contract_address"
