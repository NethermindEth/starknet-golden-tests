#!/bin/bash
set -e
trap 'echo "Error on line $LINENO: $BASH_COMMAND"; exit 1' ERR

rpc_url="$STARKNET_RPC"
variants=""
while [[ "${1:-}" == --* ]]; do
    case "$1" in
        --rpc-url) rpc_url="$2"; shift 2 ;;
        --variants) variants="$2"; shift 2 ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done
contract_address="$1"
block_number="$2"

missing=""
if [ -z "$contract_address" ]; then
    missing="contract_address"
fi
if [ -z "$block_number" ]; then
    if [ -n "$missing" ]; then
        missing="$missing and block_number"
    else
        missing="block_number"
    fi
fi
if [ -z "$rpc_url" ]; then
    if [ -n "$missing" ]; then
        missing="$missing and rpc_url"
    else
        missing="rpc_url"
    fi
fi

if [ -n "$missing" ]; then
    echo "Error: Missing $missing argument(s)." >&2
    echo "" >&2
    echo "Usage: $0 [--rpc-url <url>] [--variants <variant,...>] <contract_address> <block_number>" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "Available variants: include-last-update-block" >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 0x049d36570d4e46f48e99674bd3fcc84644ddd6b96f7c741b1562b82f9e004dc7 100" >&2
    echo "  $0 --variants include-last-update-block 0x049d36570d4e46f48e99674bd3fcc84644ddd6b96f7c741b1562b82f9e004dc7 100" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0 0x049d36570d4e46f48e99674bd3fcc84644ddd6b96f7c741b1562b82f9e004dc7 100" >&2
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

# Query starknet_getStateUpdate to find storage diffs for the contract
echo "🔍 Querying starknet_getStateUpdate for block $block_number..."
state_update_request=$(jq -nc \
    --argjson block_number "$block_number" \
    '{id: 1, jsonrpc: "2.0", method: "starknet_getStateUpdate", params: {block_id: {block_number: $block_number}}}')

state_update_response=$(echo "$state_update_request" | STARKNET_RPC="$rpc_url" "${script_dir}/../run/query-rpc.sh" 2>/dev/null)

# Extract the first storage key for the given contract
storage_key=$(echo "$state_update_response" | jq -r \
    --arg addr "$contract_address" \
    '.result.state_diff.storage_diffs[] | select(.address == $addr) | .storage_entries[0].key // empty')

if [ -z "$storage_key" ]; then
    echo "Error: No storage diffs found for contract $contract_address at block $block_number" >&2
    exit 1
fi

echo "✅ Found storage key: $storage_key"

method="starknet_getStorageAt"
method_dir="tests/${network}/${method}"
mkdir -p "$method_dir"

# Test 1: Without response flags
test_name="${contract_address}-${storage_key}-${block_number}"
input_file="${method_dir}/${test_name}.input.json"

jq -nc \
    --arg method "$method" \
    --arg contract_address "$contract_address" \
    --arg key "$storage_key" \
    --argjson block_number "$block_number" \
    '{id: 1, jsonrpc: "2.0", method: $method, params: {contract_address: $contract_address, key: $key, block_id: {block_number: $block_number}}}' \
    >"$input_file"

echo "Processing $method without flags..."
STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$test_name"

# Test 2: With INCLUDE_LAST_UPDATE_BLOCK flag (if requested)
if [[ ",$variants," == *",include-last-update-block,"* ]]; then
    test_name_flagged="${contract_address}-${storage_key}-${block_number}-include-last-update-block"
    input_file_flagged="${method_dir}/${test_name_flagged}.input.json"

    jq -nc \
        --arg method "$method" \
        --arg contract_address "$contract_address" \
        --arg key "$storage_key" \
        --argjson block_number "$block_number" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {contract_address: $contract_address, key: $key, block_id: {block_number: $block_number}, response_flags: ["INCLUDE_LAST_UPDATE_BLOCK"]}}' \
        >"$input_file_flagged"

    echo "Processing $method with INCLUDE_LAST_UPDATE_BLOCK flag..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$test_name_flagged"
fi

echo "Done processing storage tests for contract $contract_address at block $block_number"
