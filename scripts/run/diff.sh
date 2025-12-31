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

if [ ! -f "$output_file" ]; then
    echo "Error: Output file '$output_file' does not exist" >&2
    exit 1
fi

# Compare live response (already normalized by run.sh) with expected output (already normalized)
diff -u \
    <("${script_dir}/run.sh" "$network" "$method" "$test_name" "$rpc_url") \
    "$output_file"
