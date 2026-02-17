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

# Query RPC and store in temp files to properly capture errors
temp_actual=$(mktemp)
temp_expected=$(mktemp)
trap 'rm -f "$temp_actual" "$temp_expected"' EXIT

# Run query and capture exit code - if this fails, the test should fail
if ! STARKNET_RPC="$rpc_url" "${script_dir}/query-rpc.sh" <"$input_file" >"$temp_actual"; then
    exit 1
fi

# Normalize and pretty-print actual response
if ! jq -S -f "${script_dir}/normalize-json.jq" "$temp_actual" >"${temp_actual}.pretty"; then
    echo "Error: Failed to parse RPC response as JSON" >&2
    exit 1
fi

# Normalize and pretty-print expected output
if ! jq -S -f "${script_dir}/normalize-json.jq" "$output_file" >"$temp_expected"; then
    echo "Error: Failed to parse expected output as JSON" >&2
    exit 1
fi

# Compare the results
diff --color=always -u "${temp_actual}.pretty" "$temp_expected"
