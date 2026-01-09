#!/bin/bash

# Usage: diff.sh [--rpc-url <url>] <input_file> [output_file]
#   --rpc-url: RPC URL (default: $STARKNET_RPC env var)
#   If output_file is not provided, it's derived from input_file by replacing .input.json with .output.json

script_dir="$(dirname "$0")"

rpc_url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    rpc_url="$2"
    shift 2
fi
input_file="$1"
output_file="${2:-${input_file%.input.json}.output.json}"

if [ -z "$rpc_url" ] || [ -z "$input_file" ]; then
    echo "Usage: $0 [--rpc-url <url>] <input_file> [output_file]" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    exit 1
fi

if [ ! -f "$input_file" ]; then
    echo "Error: Input file '$input_file' does not exist" >&2
    exit 1
fi

if [ ! -f "$output_file" ]; then
    echo "Error: Output file '$output_file' does not exist" >&2
    exit 1
fi

# Compare live response (already normalized by query-rpc.sh) with expected output (already normalized)
# Pretty-print both sides for better diff readability
diff --color=always -u \
    <(STARKNET_RPC="$rpc_url" "${script_dir}/query-rpc.sh" <"$input_file" | jq '.') \
    <(jq '.' "$output_file")
