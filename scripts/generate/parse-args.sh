#!/bin/bash
# Shared library for CLI arg parsing and RPC method param handling.
# Source this file, then use parse_args and add_method_params.

RESPONSE_FLAGS=""
TRACE_FLAGS=""
SIMULATION_FLAGS=""
RPC_URL="${STARKNET_RPC:-}"
REMAINING_ARGS=()

# Parse all CLI args. Sets RPC_URL, flag variables, and REMAINING_ARGS.
parse_args() {
    RPC_URL="${STARKNET_RPC:-}"
    RESPONSE_FLAGS=""
    TRACE_FLAGS=""
    SIMULATION_FLAGS=""
    REMAINING_ARGS=()

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --rpc-url)
                RPC_URL="$2"
                shift 2
                ;;
            --response-flags)
                RESPONSE_FLAGS="$2"
                shift 2
                ;;
            --trace-flags)
                TRACE_FLAGS="$2"
                shift 2
                ;;
            --simulation-flags)
                SIMULATION_FLAGS="$2"
                shift 2
                ;;
            *)
                REMAINING_ARGS+=("$1")
                shift
                ;;
        esac
    done
}

# Returns the flag key name for a given RPC method, or empty if none.
get_flag_key() {
    case "$1" in
        starknet_getBlockWithReceipts|starknet_getBlockWithTxs|\
        starknet_getTransactionByHash|starknet_getTransactionByBlockIdAndIndex)
            echo "response_flags"
            ;;
        starknet_traceBlockTransactions)
            echo "trace_flags"
            ;;
        starknet_simulateTransactions)
            echo "simulation_flags"
            ;;
    esac
}

# Returns the flag value for a given flag key.
# trace_flags and simulation_flags default to [] when not explicitly set.
# response_flags returns empty when not set (meaning: don't add to params).
get_flag_value() {
    case "$1" in
        response_flags)   echo "$RESPONSE_FLAGS" ;;
        trace_flags)      echo "${TRACE_FLAGS:-[]}" ;;
        simulation_flags) echo "${SIMULATION_FLAGS:-[]}" ;;
    esac
}

# Reads full RPC JSON from stdin, merges appropriate flags into .params, writes to stdout.
# Usage: echo '{"id":1,...}' | add_method_params "starknet_getBlockWithTxs"
add_method_params() {
    local method="$1"
    local flag_key flag_value
    flag_key=$(get_flag_key "$method")
    [ -z "$flag_key" ] && { cat; return; }
    flag_value=$(get_flag_value "$flag_key")
    [ -z "$flag_value" ] && { cat; return; }
    jq --arg key "$flag_key" --argjson val "$flag_value" '.params += {($key): $val}'
}
