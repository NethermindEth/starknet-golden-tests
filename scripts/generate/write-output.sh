#!/bin/bash

rpc_url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    rpc_url="$2"
    shift 2
fi
network="$1"
method="$2"
test_name="$3"

if [ -z "$network" ] || [ -z "$method" ] || [ -z "$test_name" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] <network> <method> <test_name>" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
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
STARKNET_RPC="$rpc_url" "${script_dir}/../run/query-rpc.sh" <"$input_file" >"$output_file"
