#!/bin/bash
set -e
trap 'echo "Error on line $LINENO: $BASH_COMMAND"; exit 1' ERR

script_dir="$(dirname "$0")"
source "${script_dir}/parse-args.sh"
parse_args "$@"

block_number="${REMAINING_ARGS[0]}"
rpc_url="$RPC_URL"

if [ -z "$block_number" ]; then
    missing="block_number"
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
    echo "Usage: $0 [--rpc-url <url>] [--response-flags <json>] [--trace-flags <json>] <block_number>" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 100" >&2
    echo "  $0 --rpc-url http://localhost:6060 --response-flags '[\"INCLUDE_PROOF_FACTS\"]' 100" >&2
    echo "  $0 --rpc-url http://localhost:6060 --trace-flags '[\"RETURN_INITIAL_READS\"]' 100" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0 100" >&2
    exit 1
fi

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

if [[ "$block_number" == "latest" ]]; then
    echo "Getting latest block number"
    latest_block_request='{"id":1,"jsonrpc":"2.0","method":"starknet_blockNumber","params":[]}'
    block_number=$(echo "$latest_block_request" | STARKNET_RPC="$rpc_url" "${script_dir}/../run/query-rpc.sh" 2>/dev/null | jq -r '.result // empty')
    echo "Latest block is: $block_number"
fi

methods=(
    "starknet_getBlockTransactionCount"
    "starknet_getBlockWithReceipts"
    "starknet_getBlockWithTxHashes"
    "starknet_getBlockWithTxs"
    "starknet_getStateUpdate"
    "starknet_traceBlockTransactions"
)

for method in "${methods[@]}"; do
    flag_key=$(get_flag_key "$method")
    flag_subdir=$(flags_to_subdir "$flag_key" "$(get_flag_value "$flag_key")")
    test_name="${flag_subdir:+${flag_subdir}/}${block_number}"
    input_file="tests/${network}/v${spec_version}/${method}/${test_name}.input.json"
    input_dir="$(dirname "$input_file")"

    # Create input directory if it doesn't exist
    mkdir -p "$input_dir"

    # Generate input JSON for this method
    jq -nc \
        --arg method "$method" \
        --argjson block_number "$block_number" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {block_id: {block_number: $block_number}}}' \
        | add_method_params "$method" \
        >"$input_file"

    # Run write-output.sh for this method
    echo "Processing $method with block number..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$spec_version" "$method" "$test_id"
done

# Extract block hash from starknet_getBlockWithTxHashes output
block_hash=$(jq -r '.result.block_hash' "tests/${network}/v${spec_version}/starknet_getBlockWithTxHashes/${block_number}.output.json")

if [ -z "$block_hash" ] || [ "$block_hash" = "null" ]; then
    echo "Error: Could not extract block_hash from starknet_getBlockWithTxHashes output" >&2
    exit 1
fi

echo "Extracted block hash: $block_hash"

# Generate tests with block hash input
for method in "${methods[@]}"; do
    flag_key=$(get_flag_key "$method")
    flag_subdir=$(flags_to_subdir "$flag_key" "$(get_flag_value "$flag_key")")
    test_name="${flag_subdir:+${flag_subdir}/}${block_number}-${block_hash}"
    input_file="tests/${network}/v${spec_version}/${method}/${test_name}.input.json"

    # Generate input JSON with block_hash
    jq -nc \
        --arg method "$method" \
        --arg block_hash "$block_hash" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {block_id: {block_hash: $block_hash}}}' \
        | add_method_params "$method" \
        >"$input_file"

    echo "Processing $method with block hash..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$spec_version" "$method" "$test_name"
done

# Diff outputs from block number vs block hash queries
echo "Comparing block number vs block hash outputs..."
for method in "${methods[@]}"; do
    flag_key=$(get_flag_key "$method")
    flag_subdir=$(flags_to_subdir "$flag_key" "$(get_flag_value "$flag_key")")
    block_number_output="tests/${network}/v${spec_version}/${method}/${flag_subdir:+${flag_subdir}/}${block_number}.output.json"
    block_hash_output="tests/${network}/v${spec_version}/${method}/${flag_subdir:+${flag_subdir}/}${block_number}-${block_hash}.output.json"

    if ! diff --color=auto -u \
        <(jq '.' "$block_number_output") \
        <(jq '.' "$block_hash_output"); then
        echo "  ❌ $method outputs differ" >&2
        exit 1
    fi
    echo "  ✅ $method outputs match"
done

echo "Done processing all methods for block $block_number"
