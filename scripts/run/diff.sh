#!/bin/bash

# Usage: diff.sh <rpc_url> <input_file> [output_file]
#   If output_file is not provided, it's derived from input_file by replacing .input.json with .output.json

script_dir="$(dirname "$0")"

rpc_url="$1"
input_file="$2"
output_file="${3:-${input_file%.input.json}.output.json}"

if [ -z "$rpc_url" ] || [ -z "$input_file" ]; then
    echo "Usage: $0 <rpc_url> <input_file> [output_file]" >&2
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
    <("${script_dir}/query-rpc.sh" "$rpc_url" <"$input_file" | jq '.') \
    <(jq '.' "$output_file")
