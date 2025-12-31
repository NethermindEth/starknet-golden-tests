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

if [ ! -f "$input_file" ]; then
    echo "Error: Input file '$input_file' does not exist" >&2
    exit 1
fi

# Determine which normalization script to use: method-specific if exists, otherwise default
if [ -f "${script_dir}/../normalize/${method}.sh" ]; then
    normalize_script="${script_dir}/../normalize/${method}.sh"
else
    normalize_script="${script_dir}/../normalize/default.sh"
fi

"${script_dir}/query-rpc.sh" "$rpc_url" <"$input_file" |
    "$normalize_script"
