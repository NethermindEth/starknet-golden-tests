#!/bin/bash

# Usage: detect-version.sh [--rpc-url <url>]
# Detects the spec version by querying starknet_specVersion.
# Outputs the version string (e.g., "0.8.0") on success.
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

# Query starknet_specVersion from the RPC
spec_version_request='{"id":1,"jsonrpc":"2.0","method":"starknet_specVersion","params":[]}'
spec_version=$(echo "$spec_version_request" | STARKNET_RPC="$rpc_url" "${script_dir}/query-rpc.sh" 2>/dev/null | jq -r '.result // empty')

if [ -z "$spec_version" ]; then
    echo "Error: Failed to query starknet_specVersion from $rpc_url" >&2
    exit 1
fi

echo "$spec_version"
