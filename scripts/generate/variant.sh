#!/bin/bash

# Usage: variant.sh [--rpc-url <url>] [tests_folder]
#   --rpc-url: RPC URL (default: $STARKNET_RPC env var)
#   tests_folder: Optional folder path to search for tests (default: auto-detect by chain ID)
#
# Creates version-specific variant output files (e.g., 100.output.0.8.0.json)
# only when the output differs from the resolved output for that version.
# Intended for older node versions that produce different outputs.

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
    echo "Creates version-specific variant files only when output differs." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://old-node:6060" >&2
    echo "  $0 --rpc-url http://old-node:6060 tests/mainnet" >&2
    echo "  STARKNET_RPC=http://old-node:6060 $0" >&2
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

# Detect spec version
echo "🔍 Detecting spec version..."
if ! spec_version=$(STARKNET_RPC="$rpc_url" "${script_dir}/../run/detect-version.sh") || [ -z "$spec_version" ]; then
    echo "Error: Could not detect spec version" >&2
    exit 1
fi
echo "✅ Spec version: $spec_version"

total=0
variants_created=0
variants_skipped=0
failed=0

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo ""
echo "🔄 Creating variants (v$spec_version) for tests in $tests_folder..."
echo ""

# Find all .input.json files recursively in the specified folder
while IFS= read -r -d '' input_file; do
    ((total++))

    # Derive variant output path
    variant_output="${input_file%.input.json}.output.${spec_version}.json"

    printf "🔄 %-160s" "$input_file"

    # Remove existing variant (so resolve-output.sh finds the next best match)
    rm -f "$variant_output"

    # Resolve what output file would be used for this version
    resolved_output=$("${script_dir}/../run/resolve-output.sh" "$input_file" "$spec_version" 2>/dev/null)
    if [ -z "$resolved_output" ] || [ ! -f "$resolved_output" ]; then
        echo -e "${RED}❌ (no resolved output)${NC}"
        ((failed++))
        continue
    fi

    # Query RPC and write variant
    if ! STARKNET_RPC="$rpc_url" "${script_dir}/../run/query-rpc.sh" <"$input_file" >"$variant_output" 2>/dev/null; then
        echo -e "${RED}❌${NC}"
        rm -f "$variant_output"
        ((failed++))
        continue
    fi

    # Delete variant if same as resolved output
    if diff -q "$variant_output" "$resolved_output" >/dev/null 2>&1; then
        rm -f "$variant_output"
        echo -e "${YELLOW}⏭️  (same as $(basename "$resolved_output"))${NC}"
        ((variants_skipped++))
    else
        echo -e "${GREEN}✅ (variant created)${NC}"
        ((variants_created++))
    fi

done < <(find "$tests_folder" -type f -name "*.input.json" -print0 2>/dev/null | sort -z)

# Summary
echo ""
echo "========================================="
echo "📊 Summary:"
echo "  📈 Total tests: $total"
echo "  ✅ Variants created: $variants_created"
echo "  ⏭️  Skipped (same as resolved): $variants_skipped"
if [ "$failed" -gt 0 ]; then
    echo "  ❌ Failed: $failed"
fi
echo "========================================="

# Exit with non-zero if there were failures
if [ "$failed" -gt 0 ]; then
    exit 1
fi

exit 0
