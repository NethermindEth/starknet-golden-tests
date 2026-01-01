#!/bin/bash

network="$1"
method="$2"
test_name="$3"
rpc_url="$4"

if [ -z "$network" ] || [ -z "$method" ] || [ -z "$test_name" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 <network> <method> <test_name> <rpc_url>" >&2
    exit 1
fi

script_dir="$(dirname "$0")"
input_file="tests/${network}/${method}/${test_name}.input.json"
output_file="tests/${network}/${method}/${test_name}.output.json"

if [ ! -f "$input_file" ]; then
    echo "Error: Input file '$input_file' does not exist" >&2
    exit 1
fi

# Create output directory if it doesn't exist
output_dir="$(dirname "$output_file")"
mkdir -p "$output_dir"

# Run the test (output is already normalized by query-rpc.sh) and write to file
"${script_dir}/query-rpc.sh" "$rpc_url" <"$input_file" >"$output_file"
