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

# Create timestamped results folder
timestamp=$(date +"%Y%m%d-%H%M%S")
results_dir="$repo_root/results/${timestamp}"
mkdir -p "$results_dir"

total=0
passed=0
failed_tests=()
failed_diffs=()

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
    printf "🧪 %-160s" "$rel_input_file"

    # Flatten path: tests/mainnet/method/100.input.json -> mainnet.method.100.diff
    rel_path="${rel_input_file#tests/}"
    rel_path="${rel_path%.input.json}"
    flat_name="${rel_path//\//.}"
    diff_file="$results_dir/${flat_name}.diff"

    # Run diff, tee to file and stderr
    "${script_dir}/diff.sh" "$rpc_url" "$abs_input_file" 2>&1 | tee "$diff_file" >&2
    if [ "${PIPESTATUS[0]}" -eq 0 ]; then
        echo -e "${GREEN}✅ PASSED${NC}"
        ((passed++))
        rm -f "$diff_file" # Remove empty/passed diff file
    else
        echo -e "${RED}❌ FAILED${NC}"
        failed_tests+=("$rel_input_file")
        failed_diffs+=("$diff_file")
    fi

done < <(find "$tests_folder" -type f -name "*.input.json" -print0 2>/dev/null | sort -z)

# Summary
echo "========================================="
echo "📊 Summary:"
echo "  📈 Total tests: $total"
echo "  ✅ Passed: $passed"
echo "  ❌ Failed: ${#failed_tests[@]}"
if [ ${#failed_tests[@]} -gt 0 ]; then
    echo ""
    echo "📁 Results saved to: $results_dir"
    echo ""
    echo "📋 Failed tests:"
    for i in "${!failed_tests[@]}"; do
        echo "  ❌ ${failed_tests[$i]}"
        echo "    📄 ${failed_diffs[$i]}"
    done
fi
echo "========================================="

# Exit with non-zero if there were failures
if [ ${#failed_tests[@]} -gt 0 ]; then
    exit 1
fi

exit 0
