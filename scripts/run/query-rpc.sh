#!/bin/bash

url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    url="$2"
    shift 2
elif [[ -n "$1" ]]; then
    url="$1"
fi

if [ -z "$url" ]; then
    echo "Usage: $0 [--rpc-url <url>] or $0 <url>" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag, positional arg, or STARKNET_RPC env var." >&2
    exit 1
fi

# Read JSON from stdin and store it (we need to use it twice)
json_input=$(cat)

# Extract method from JSON
method=$(echo "$json_input" | jq -r '.method // empty')

if [ -z "$method" ]; then
    echo "Error: JSON input must contain a 'method' field" >&2
    exit 1
fi

script_dir="$(dirname "$0")"

# Determine which normalization script to use: method-specific if exists, otherwise default
if [ -f "${script_dir}/../normalize/${method}.sh" ]; then
    normalize_script="${script_dir}/../normalize/${method}.sh"
else
    normalize_script="${script_dir}/../normalize/default.sh"
fi

# Make curl request and pipe through normalization script
echo "$json_input" | curl --silent --show-error --location --fail-with-body --compressed \
    --retry 5 --retry-delay 2 \
    --header 'Content-Type: application/json' \
    --header 'Accept: application/json' \
    --header 'Accept-Encoding: gzip' \
    --data @- \
    "$url" | jq -s '.[-1]' | "$normalize_script"
