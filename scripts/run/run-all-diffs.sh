#!/bin/bash

# Usage: run-all-diffs.sh <rpc_url> [tests_folder]
#   tests_folder: Optional folder path to search for tests (default: "tests")

rpc_url="$1"
tests_folder="${2:-tests}"

if [ -z "$rpc_url" ]; then
    echo "Usage: $0 <rpc_url> [tests_folder]" >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 http://localhost:6060" >&2
    echo "  $0 http://localhost:6060 tests/mainnet" >&2
    exit 1
fi

script_dir="$(cd "$(dirname "$0")" && pwd)"
# Get repo root (parent of scripts/)
repo_root="$(cd "$script_dir/../.." && pwd)"
# Change to repo root to ensure paths are consistent
cd "$repo_root" || exit 1

total=0
passed=0
failed=0

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Find all .input.json files recursively in the specified folder
while IFS= read -r -d '' input_file; do
    # input_file is relative to current directory (repo root)
    rel_input_file="$input_file"

    ((total++))

    # Run diff (use absolute path to be safe)
    abs_input_file="$repo_root/$input_file"
    printf "🧪 %-80s" "$rel_input_file"
    if "${script_dir}/diff.sh" "$rpc_url" "$abs_input_file" >/dev/null 2>&1; then
        echo -e "${GREEN}✅ PASSED${NC}"
        ((passed++))
    else
        echo -e "${RED}❌ FAILED${NC}"
        ((failed++))
        # Show the actual diff
        "${script_dir}/diff.sh" "$rpc_url" "$abs_input_file"
    fi

done < <(find "$tests_folder" -type f -name "*.input.json" -print0 2>/dev/null)

# Summary
echo "========================================="
echo "📊 Summary:"
echo "  📈 Total tests: $total"
echo "  ✅ Passed: $passed"
echo "  ❌ Failed: $failed"
echo "========================================="

# Exit with non-zero if there were failures
if [ $failed -gt 0 ]; then
    exit 1
fi

exit 0
