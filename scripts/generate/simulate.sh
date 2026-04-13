#!/bin/bash
set -e
trap 'echo "Error on line $LINENO: $BASH_COMMAND"; exit 1' ERR

script_dir="$(dirname "$0")"
source "${script_dir}/parse-args.sh"
parse_args "$@"

block_number="${REMAINING_ARGS[0]}"
rpc_url="$RPC_URL"

if [ -z "$block_number" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] [--simulation-flags <json>] <block_number>" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "Requires 'generate block' to have been run first for this block number." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 100" >&2
    echo "  $0 --rpc-url http://localhost:6060 --simulation-flags '[\"RETURN_INITIAL_READS\"]' 100" >&2
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

# Check that block test outputs exist (requires 'generate block' to have been run first)
block_with_txs_output="tests/${network}/v${spec_version}/starknet_getBlockWithTxs/${block_number}.output.json"
block_with_tx_hashes_output="tests/${network}/v${spec_version}/starknet_getBlockWithTxHashes/${block_number}.output.json"

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

# Extract block hash and parent hash from block outputs.
# We simulate at the previous block (block_number - 1) because the transaction
# was already executed in block_number, so the account nonce has advanced.
block_hash=$(jq -r '.result.block_hash' "$block_with_tx_hashes_output")
parent_hash=$(jq -r '.result.parent_hash' "$block_with_tx_hashes_output")
sim_block_number=$((block_number - 1))

if [ -z "$block_hash" ] || [ "$block_hash" = "null" ]; then
    echo "Error: Could not extract block_hash from starknet_getBlockWithTxHashes output" >&2
    exit 1
fi

if [ -z "$parent_hash" ] || [ "$parent_hash" = "null" ]; then
    echo "Error: Could not extract parent_hash from starknet_getBlockWithTxHashes output" >&2
    exit 1
fi

echo "Extracted block hash: $block_hash"
echo "Simulating at previous block: $sim_block_number (parent hash: $parent_hash)"

# Extract all v3 transactions from starknet_getBlockWithTxs output.
# Since spec v0.8.0, BROADCASTED_TXN only accepts v3 variants
# (INVOKE_TXN_V3, DECLARE_TXN_V3, DEPLOY_ACCOUNT_TXN_V3).
# Prefer INVOKE v3, then fall back to any v3.
v3_txs_json=$(jq -c '
    .result.transactions
    | (map(select(.type == "INVOKE" and .version == "0x3"))
       | if length > 0 then . else null end)
    // map(select(.version == "0x3"))
    | map(del(.transaction_hash))
' "$block_with_txs_output")

v3_tx_count=$(echo "$v3_txs_json" | jq 'length')

if [ -z "$v3_txs_json" ] || [ "$v3_txs_json" = "null" ] || [ "$v3_tx_count" -eq 0 ]; then
    echo "Error: No v3 transactions found in block $block_number." >&2
    echo "starknet_simulateTransactions requires v3 transaction format (spec >= v0.8.0)." >&2
    echo "Please use a block that contains v3 transactions." >&2
    exit 1
fi

echo "Found $v3_tx_count v3 candidate transaction(s) in block $block_number"

# Try simulating a transaction and return 0 if successful, 1 if error.
try_simulate_tx() {
    local tx="$1"
    local trial_input
    trial_input=$(jq -nc \
        --argjson sim_block_number "$sim_block_number" \
        --argjson tx "$tx" \
        '{id: 1, jsonrpc: "2.0", method: "starknet_simulateTransactions", params: {block_id: {block_number: $sim_block_number}, transactions: [$tx]}}' \
        | add_method_params "starknet_simulateTransactions")

    local trial_response
    trial_response=$(echo "$trial_input" | STARKNET_RPC="$rpc_url" "${script_dir}/../run/query-rpc.sh" 2>/dev/null)

    local has_result
    has_result=$(echo "$trial_response" | jq 'has("result")')
    [ "$has_result" = "true" ]
}

# Try each v3 transaction candidate until one simulates successfully
tx_json=""
for i in $(seq 0 $((v3_tx_count - 1))); do
    candidate=$(echo "$v3_txs_json" | jq -c ".[$i]")
    echo "Trying v3 transaction candidate $((i + 1))/$v3_tx_count..."
    if try_simulate_tx "$candidate"; then
        tx_json="$candidate"
        echo "  ✅ Transaction candidate $((i + 1)) simulates successfully"
        break
    else
        echo "  ⚠️  Transaction candidate $((i + 1)) failed simulation, trying next..."
    fi
done

if [ -z "$tx_json" ]; then
    echo "Error: None of the $v3_tx_count v3 transactions in block $block_number simulate successfully at block $sim_block_number." >&2
    echo "This typically means all accounts had insufficient balance at the previous block." >&2
    echo "Please try a different block number." >&2
    exit 1
fi

methods=(
    "starknet_simulateTransactions"
)

# Generate tests with block number (simulate at previous block)
for method in "${methods[@]}"; do
    flag_key=$(get_flag_key "$method")
    flag_subdir=$(flags_to_subdir "$flag_key" "$(get_flag_value "$flag_key")")
    test_name="${flag_subdir:+${flag_subdir}/}${block_number}"
    input_file="tests/${network}/v${spec_version}/${method}/${test_name}.input.json"
    mkdir -p "$(dirname "$input_file")"

    jq -nc \
        --arg method "$method" \
        --argjson sim_block_number "$sim_block_number" \
        --argjson tx "$tx_json" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {block_id: {block_number: $sim_block_number}, transactions: [$tx]}}' \
        | add_method_params "$method" \
        >"$input_file"

    echo "Processing $method with block number..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$spec_version" "$method" "$test_name"

    output_file="tests/${network}/v${spec_version}/${method}/${test_name}.output.json"
    if jq -e '.error' "$output_file" >/dev/null 2>&1; then
        echo "Error: $method output contains an error response:" >&2
        jq '.error' "$output_file" >&2
        exit 1
    fi
done

# Generate tests with block hash (simulate at parent block)
for method in "${methods[@]}"; do
    flag_key=$(get_flag_key "$method")
    flag_subdir=$(flags_to_subdir "$flag_key" "$(get_flag_value "$flag_key")")
    test_name="${flag_subdir:+${flag_subdir}/}${block_number}-${parent_hash}"
    input_file="tests/${network}/v${spec_version}/${method}/${test_name}.input.json"

    jq -nc \
        --arg method "$method" \
        --arg parent_hash "$parent_hash" \
        --argjson tx "$tx_json" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {block_id: {block_hash: $parent_hash}, transactions: [$tx]}}' \
        | add_method_params "$method" \
        >"$input_file"

    echo "Processing $method with block hash..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$spec_version" "$method" "$test_name"

    output_file="tests/${network}/v${spec_version}/${method}/${test_name}.output.json"
    if jq -e '.error' "$output_file" >/dev/null 2>&1; then
        echo "Error: $method output contains an error response:" >&2
        jq '.error' "$output_file" >&2
        exit 1
    fi
done

# Diff outputs from block number vs block hash queries
echo "Comparing block number vs block hash outputs..."
for method in "${methods[@]}"; do
    flag_key=$(get_flag_key "$method")
    flag_subdir=$(flags_to_subdir "$flag_key" "$(get_flag_value "$flag_key")")
    block_number_output="tests/${network}/v${spec_version}/${method}/${flag_subdir:+${flag_subdir}/}${block_number}.output.json"
    block_hash_output="tests/${network}/v${spec_version}/${method}/${flag_subdir:+${flag_subdir}/}${block_number}-${parent_hash}.output.json"

    if ! diff --color=auto -u \
        <(jq '.' "$block_number_output") \
        <(jq '.' "$block_hash_output"); then
        echo "  ❌ $method outputs differ" >&2
        exit 1
    fi
    echo "  ✅ $method outputs match"
done

echo "Done processing all simulate methods for block $block_number"
