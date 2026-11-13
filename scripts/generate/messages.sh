#!/bin/bash
set -e
trap 'echo "Error on line $LINENO: $BASH_COMMAND"; exit 1' ERR

script_dir="$(dirname "$0")"
source "${script_dir}/parse-args.sh"
parse_args "$@"

l1_tx_hash="${REMAINING_ARGS[0]}"
rpc_url="$RPC_URL"

if [ -z "$l1_tx_hash" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] <l1_transaction_hash>" >&2
    echo "" >&2
    echo "Generates a starknet_getMessagesStatus test for the L1 (Ethereum) transaction that sent" >&2
    echo "one or more L1->L2 messages, then checks every returned L2 handler against starknet_getTransactionStatus." >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 0xaa92c70b30832505b01ef4a72f410f56618e70d95528834036eb317fcc81a740" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0 0xaa92c70b30832505b01ef4a72f410f56618e70d95528834036eb317fcc81a740" >&2
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

method="starknet_getMessagesStatus"
test_name="$l1_tx_hash"
input_file="tests/${network}/v${spec_version}/${method}/${test_name}.input.json"
mkdir -p "$(dirname "$input_file")"

jq -nc \
    --arg method "$method" \
    --arg tx "$l1_tx_hash" \
    '{id: 1, jsonrpc: "2.0", method: $method, params: {transaction_hash: $tx}}' \
    >"$input_file"

echo "Processing $method..."
STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$spec_version" "$method" "$test_name"

output_file="${input_file%.input.json}.output.json"
message_count=$(jq -r '.result | if type == "array" then length else -1 end' "$output_file")

if [ "$message_count" -lt 0 ]; then
    echo "Error: $method did not return a message list" >&2
    exit 1
fi
if [ "$message_count" -eq 0 ]; then
    echo "⚠️  $method returned no messages for $l1_tx_hash (cancelled message, or the node has not indexed the L1 transaction)" >&2
fi
echo "Found $message_count L1->L2 message(s)"

# Cross-check every reported L2 handler transaction against starknet_getTransactionStatus
while IFS=$'\t' read -r l2_tx finality execution; do
    status_response=$(jq -nc --arg tx "$l2_tx" \
        '{id: 1, jsonrpc: "2.0", method: "starknet_getTransactionStatus", params: {transaction_hash: $tx}}' \
        | STARKNET_RPC="$rpc_url" "${script_dir}/../run/query-rpc.sh")
    expected=$(echo "$status_response" | jq -r '"\(.result.finality_status)\t\(.result.execution_status // "null")"')
    if [ "$expected" != "$(printf '%s\t%s' "$finality" "$execution")" ]; then
        echo "  ❌ $l2_tx: getMessagesStatus says ${finality}/${execution}, getTransactionStatus says ${expected//$'\t'//}" >&2
        exit 1
    fi
    echo "  ✅ $l2_tx matches starknet_getTransactionStatus (${finality}/${execution})"
done < <(jq -r '.result[] | "\(.transaction_hash)\t\(.finality_status)\t\(.execution_status // "null")"' "$output_file")

echo "Done processing $method for L1 transaction $l1_tx_hash"
