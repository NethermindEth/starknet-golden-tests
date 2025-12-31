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
output_file="tests/${network}/${method}/${test_name}.output.json"

# Create output directory if it doesn't exist
output_dir="$(dirname "$output_file")"
mkdir -p "$output_dir"

# Run the test (output is already normalized by run.sh) and write to file
"${script_dir}/run.sh" "$network" "$method" "$test_name" "$rpc_url" >"$output_file"
