#!/bin/bash

rpc_url="$STARKNET_RPC"
with_initial_reads=false

while [[ $# -gt 1 ]]; do
    case "$1" in
        --rpc-url)
            rpc_url="$2"
            shift 2
            ;;
        --with-initial-reads)
            with_initial_reads=true
            shift
            ;;
        *)
            break
            ;;
    esac
done
block_number="$1"

if [ -z "$block_number" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] [--with-initial-reads] <block_number>" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 100" >&2
    echo "  $0 --rpc-url http://localhost:6060 --with-initial-reads 100" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0 100" >&2
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
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$block_number"
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
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$test_name"
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

if [[ "$with_initial_reads" == "true" ]]; then
    method="starknet_traceBlockTransactions"

    input_file="tests/${network}/${method}/${block_number}-initial-reads.input.json"
    mkdir -p "$(dirname "$input_file")"

    jq -nc --argjson block_number "$block_number" \
        '{id: 1, jsonrpc: "2.0", method: "starknet_traceBlockTransactions", params: {block_id: {block_number: $block_number}, trace_flags: ["RETURN_INITIAL_READS"]}}' \
        >"$input_file"

    echo "Processing $method with block number (initial reads)..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "${block_number}-initial-reads"

    test_name="${block_number}-${block_hash}-initial-reads"
    input_file="tests/${network}/${method}/${test_name}.input.json"

    jq -nc --arg block_hash "$block_hash" \
        '{id: 1, jsonrpc: "2.0", method: "starknet_traceBlockTransactions", params: {block_id: {block_hash: $block_hash}, trace_flags: ["RETURN_INITIAL_READS"]}}' \
        >"$input_file"

    echo "Processing $method with block hash (initial reads)..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$test_name"

    echo "Comparing initial reads variants (block number vs block hash outputs)..."
    block_number_output="tests/${network}/${method}/${block_number}-initial-reads.output.json"
    block_hash_output="tests/${network}/${method}/${test_name}.output.json"

    if ! diff --color=auto -u \
        <(jq '.' "$block_number_output") \
        <(jq '.' "$block_hash_output"); then
        echo "  ❌ $method initial reads outputs differ" >&2
        exit 1
    fi
    echo "  ✅ $method initial reads outputs match"
fi

echo "Done processing all methods for block $block_number"
