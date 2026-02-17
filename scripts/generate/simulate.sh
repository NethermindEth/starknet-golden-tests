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
    echo "Requires 'generate block' to have been run first for this block number." >&2
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

# Check that block test outputs exist (requires 'generate block' to have been run first)
block_with_txs_output="tests/${network}/starknet_getBlockWithTxs/${block_number}.output.json"
block_with_tx_hashes_output="tests/${network}/starknet_getBlockWithTxHashes/${block_number}.output.json"

if [ ! -f "$block_with_txs_output" ]; then
    echo "Error: $block_with_txs_output not found." >&2
    echo "Please run 'generate block' first for block $block_number." >&2
    exit 1
fi

if [ ! -f "$block_with_tx_hashes_output" ]; then
    echo "Error: $block_with_tx_hashes_output not found." >&2
    echo "Please run 'generate block' first for block $block_number." >&2
    exit 1
fi

# Extract block hash from starknet_getBlockWithTxHashes output
block_hash=$(jq -r '.result.block_hash' "$block_with_tx_hashes_output")

if [ -z "$block_hash" ] || [ "$block_hash" = "null" ]; then
    echo "Error: Could not extract block_hash from starknet_getBlockWithTxHashes output" >&2
    exit 1
fi

echo "Extracted block hash: $block_hash"

# Extract a transaction from starknet_getBlockWithTxs output
tx_json=$(jq -c '
    .result.transactions
    | (map(select(.type == "INVOKE"))[0] // .[0])
    | del(.transaction_hash)
' "$block_with_txs_output")

if [ -z "$tx_json" ] || [ "$tx_json" = "null" ]; then
    echo "Error: Could not extract a transaction from starknet_getBlockWithTxs output" >&2
    exit 1
fi

methods=(
    "starknet_simulateTransactions"
)

# Generate tests with block number
for method in "${methods[@]}"; do
    input_file="tests/${network}/${method}/${block_number}.input.json"
    input_dir="$(dirname "$input_file")"
    mkdir -p "$input_dir"

    jq -nc \
        --argjson block_number "$block_number" \
        --argjson tx "$tx_json" \
        '{id: 1, jsonrpc: "2.0", method: "starknet_simulateTransactions", params: {block_id: {block_number: $block_number}, transactions: [$tx], simulation_flags: []}}' \
        >"$input_file"

    echo "Processing $method with block number..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$block_number"
done

# Generate tests with block hash
for method in "${methods[@]}"; do
    test_name="${block_number}-${block_hash}"
    input_file="tests/${network}/${method}/${test_name}.input.json"

    jq -nc \
        --arg block_hash "$block_hash" \
        --argjson tx "$tx_json" \
        '{id: 1, jsonrpc: "2.0", method: "starknet_simulateTransactions", params: {block_id: {block_hash: $block_hash}, transactions: [$tx], simulation_flags: []}}' \
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
    # Generate initial reads variants with block number
    for method in "${methods[@]}"; do
        input_file="tests/${network}/${method}/${block_number}-initial-reads.input.json"

        jq -nc \
            --argjson block_number "$block_number" \
            --argjson tx "$tx_json" \
            '{id: 1, jsonrpc: "2.0", method: "starknet_simulateTransactions", params: {block_id: {block_number: $block_number}, transactions: [$tx], simulation_flags: ["RETURN_INITIAL_READS"]}}' \
            >"$input_file"

        echo "Processing $method with block number (initial reads)..."
        STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "${block_number}-initial-reads"
    done

    # Generate initial reads variants with block hash
    for method in "${methods[@]}"; do
        test_name="${block_number}-${block_hash}-initial-reads"
        input_file="tests/${network}/${method}/${test_name}.input.json"

        jq -nc \
            --arg block_hash "$block_hash" \
            --argjson tx "$tx_json" \
            '{id: 1, jsonrpc: "2.0", method: "starknet_simulateTransactions", params: {block_id: {block_hash: $block_hash}, transactions: [$tx], simulation_flags: ["RETURN_INITIAL_READS"]}}' \
            >"$input_file"

        echo "Processing $method with block hash (initial reads)..."
        STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$method" "$test_name"
    done

    # Diff initial reads outputs
    echo "Comparing initial reads variants (block number vs block hash outputs)..."
    for method in "${methods[@]}"; do
        block_number_output="tests/${network}/${method}/${block_number}-initial-reads.output.json"
        block_hash_output="tests/${network}/${method}/${block_number}-${block_hash}-initial-reads.output.json"

        if ! diff --color=auto -u \
            <(jq '.' "$block_number_output") \
            <(jq '.' "$block_hash_output"); then
            echo "  ❌ $method initial reads outputs differ" >&2
            exit 1
        fi
        echo "  ✅ $method initial reads outputs match"
    done
fi

echo "Done processing all simulate methods for block $block_number"
