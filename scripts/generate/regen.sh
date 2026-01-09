#!/bin/bash

# Usage: regen.sh [--rpc-url <url>] [tests_folder]
#   --rpc-url: RPC URL (default: $STARKNET_RPC env var)
#   tests_folder: Optional folder path to search for tests (default: auto-detect by chain ID)
#
# Regenerates default .output.json files from the latest spec version.
# For older versions, use variant.sh to create version-specific outputs.

rpc_url="$STARKNET_RPC"
if [[ "$1" == "--rpc-url" ]]; then
    rpc_url="$2"
    shift 2
fi
tests_folder="$1"

if [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] [tests_folder]" >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060" >&2
    echo "  $0 --rpc-url http://localhost:6060 tests/mainnet" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0" >&2
    exit 1
fi

script_dir="$(cd "$(dirname "$0")" && pwd)"
# Get repo root (parent of scripts/)
repo_root="$(cd "$script_dir/../.." && pwd)"
# Change to repo root to ensure paths are consistent
cd "$repo_root" || exit 1

# Auto-detect test folder by chain ID if not specified
if [ -z "$tests_folder" ]; then
    echo "🔍 Auto-detecting network by querying starknet_chainId..."
    if ! tests_folder=$(STARKNET_RPC="$rpc_url" "${script_dir}/../run/detect-network.sh") || [ -z "$tests_folder" ]; then
        exit 1
    fi
    echo "✅ Using: $tests_folder"
fi

total=0
regenerated=0
failed=0

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo ""
echo "🔄 Regenerating outputs for all tests in $tests_folder..."
echo ""

# Find all .input.json files recursively in the specified folder
while IFS= read -r -d '' input_file; do
    ((total++))

    # Derive output file path from input file path
    output_file="${input_file%.input.json}.output.json"

    printf "🔄 %-160s" "$input_file"

    # Query RPC and write to output file
    if STARKNET_RPC="$rpc_url" "${script_dir}/../run/query-rpc.sh" <"$input_file" >"$output_file" 2>/dev/null; then
        echo -e "${GREEN}✅${NC}"
        ((regenerated++))
    else
        echo -e "${RED}❌${NC}"
        ((failed++))
    fi

done < <(find "$tests_folder" -type f -name "*.input.json" -print0 2>/dev/null | sort -z)

# Summary
echo ""
echo "========================================="
echo "📊 Summary:"
echo "  📈 Total tests: $total"
echo "  ✅ Regenerated: $regenerated"
if [ "$failed" -gt 0 ]; then
    echo "  ❌ Failed: $failed"
fi
echo "========================================="

# Exit with non-zero if there were failures
if [ "$failed" -gt 0 ]; then
    exit 1
fi

exit 0
