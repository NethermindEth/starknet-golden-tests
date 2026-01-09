#!/bin/bash

# Usage: detect-network.sh [--rpc-url <url>]
# Detects the network by querying starknet_chainId and matching against test folders.
# Outputs the matching tests folder path (e.g., "tests/mainnet") on success.
# RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var.

rpc_url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    rpc_url="$2"
    shift 2
elif [[ -n "$1" ]]; then
    rpc_url="$1"
fi

if [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>]" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    exit 1
fi

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

# Query starknet_chainId from the RPC
chain_id_request='{"id":1,"jsonrpc":"2.0","method":"starknet_chainId","params":[]}'
rpc_chain_id=$(echo "$chain_id_request" | STARKNET_RPC="$rpc_url" "${script_dir}/query-rpc.sh" 2>/dev/null | jq -r '.result // empty')

if [ -z "$rpc_chain_id" ]; then
    echo "Error: Failed to query starknet_chainId from $rpc_url" >&2
    exit 1
fi

echo "Chain ID from RPC: $rpc_chain_id" >&2

# Find matching network folder
for network_dir in "$repo_root/tests"/*/; do
    [ -d "$network_dir" ] || continue
    network_name=$(basename "$network_dir")
    chain_id_file="$network_dir/starknet_chainId/default.output.json"

    if [ -f "$chain_id_file" ]; then
        expected_chain_id=$(jq -r '.result // empty' "$chain_id_file")
        if [ "$rpc_chain_id" = "$expected_chain_id" ]; then
            echo "Matched network: $network_name" >&2
            echo "tests/$network_name"
            exit 0
        fi
    fi
done

echo "Error: No matching network found for chain ID: $rpc_chain_id" >&2
echo "" >&2
echo "Available networks:" >&2
for network_dir in "$repo_root/tests"/*/; do
    [ -d "$network_dir" ] || continue
    network_name=$(basename "$network_dir")
    chain_id_file="$network_dir/starknet_chainId/default.output.json"
    if [ -f "$chain_id_file" ]; then
        expected_chain_id=$(jq -r '.result // empty' "$chain_id_file")
        echo "  - $network_name (chain ID: $expected_chain_id)" >&2
    else
        echo "  - $network_name (no chain ID configured)" >&2
    fi
done
exit 1
