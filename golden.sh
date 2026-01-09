#!/bin/bash
# golden - Starknet Golden Tests CLI

set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"

usage() {
    echo "Starknet Golden Tests"
    echo ""
    echo "Commands:"
    echo "  test                  Run all golden test diffs"
    echo "  generate              Generate golden tests"
}

generate_usage() {
    echo "Commands:"
    echo "  block        Generate tests for a block"
    echo "  transaction  Generate tests for a transaction"
}

case "${1:-}" in
    test)
        shift
        "$script_dir/scripts/run/test.sh" "$@"
        ;;
    generate)
        shift
        case "${1:-}" in
            block)
                shift
                "$script_dir/scripts/generate/block.sh" "$@"
                ;;
            transaction)
                shift
                "$script_dir/scripts/generate/transaction.sh" "$@"
                ;;
            "")
                generate_usage
                ;;
            *)
                echo "Unknown generate command: $1" >&2
                generate_usage >&2
                exit 1
                ;;
        esac
        ;;
    "")
        usage
        ;;
    *)
        echo "Unknown command: $1" >&2
        echo "" >&2
        usage >&2
        exit 1
        ;;
esac
