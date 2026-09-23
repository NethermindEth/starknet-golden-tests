#!/bin/bash
# Shared library for CLI arg parsing and RPC method param handling.
# Source this file, then use parse_args and add_method_params.

# Parse all CLI args. Sets RPC_URL, flag variables, and REMAINING_ARGS.
parse_args() {
  RPC_URL="${STARKNET_RPC:-}"
  RESPONSE_FLAGS=""
  TRACE_FLAGS=""
  SIMULATION_FLAGS=""
  REMAINING_ARGS=()

  while [[ $# -gt 0 ]]; do
    case "$1" in
    --rpc-url | --response-flags | --trace-flags | --simulation-flags)
      if [[ $# -lt 2 ]]; then
        echo "Error: $1 requires a value" >&2
        exit 1
      fi
      case "$1" in
      --rpc-url) RPC_URL="$2" ;;
      --response-flags) RESPONSE_FLAGS="$2" ;;
      --trace-flags) TRACE_FLAGS="$2" ;;
      --simulation-flags) SIMULATION_FLAGS="$2" ;;
      esac
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
  starknet_getBlockWithReceipts | starknet_getBlockWithTxs | \
    starknet_getTransactionByHash | starknet_getTransactionByBlockIdAndIndex | \
    starknet_getStorageAt)
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

# Minimum spec minor version (major is always 0) in which a flag param exists.
# response_flags and trace_flags were introduced in spec v0.10; simulation_flags predates v0.9.
flag_min_minor() {
  case "$1" in
  response_flags | trace_flags) echo 10 ;;
  simulation_flags) echo 0 ;;
  esac
}

# Allowed values for a flag key, given the method and the detected spec version (major.minor).
flag_allowed_values() {
  local flag_key="$1" method="$2" minor="${3#*.}"
  case "$flag_key" in
  response_flags)
    if [[ "$method" == "starknet_getStorageAt" ]]; then echo "INCLUDE_LAST_UPDATE_BLOCK"; else echo "INCLUDE_PROOF_FACTS"; fi
    ;;
  trace_flags) echo "RETURN_INITIAL_READS" ;;
  simulation_flags)
    if ((minor >= 10)); then echo "SKIP_VALIDATE SKIP_FEE_CHARGE RETURN_INITIAL_READS"; else echo "SKIP_VALIDATE SKIP_FEE_CHARGE"; fi
    ;;
  esac
}

# Returns the flag value for a given flag key, honouring the detected spec version.
# $1: flag key; $2: spec version (major.minor, e.g. "0.10"; defaults to $spec_version)
# trace_flags and simulation_flags default to [] when not explicitly set, but only
# in spec versions that know the param. response_flags is only added when explicitly set.
get_flag_value() {
  local flag_key="$1" version="${2:-${spec_version:-}}" minor
  minor="${version#*.}"
  if [[ -n "$version" ]] && ((minor < $(flag_min_minor "$flag_key"))); then
    echo ""
    return
  fi
  case "$flag_key" in
  response_flags) echo "$RESPONSE_FLAGS" ;;
  trace_flags) echo "${TRACE_FLAGS:-[]}" ;;
  simulation_flags) echo "${SIMULATION_FLAGS:-[]}" ;;
  esac
}

# Fails when explicitly requested flags are unknown to the spec version or to the method.
# $1: method; $2: spec version (major.minor)
validate_flags() {
  local method="$1" version="$2" flag_key explicit minor allowed v
  flag_key=$(get_flag_key "$method")
  [ -z "$flag_key" ] && return
  case "$flag_key" in
  response_flags) explicit="$RESPONSE_FLAGS" ;;
  trace_flags) explicit="$TRACE_FLAGS" ;;
  simulation_flags) explicit="$SIMULATION_FLAGS" ;;
  esac
  [ -z "$explicit" ] && return
  minor="${version#*.}"
  if ((minor < $(flag_min_minor "$flag_key"))); then
    echo "Error: $flag_key is not part of spec v${version} (introduced in v0.$(flag_min_minor "$flag_key"))" >&2
    exit 1
  fi
  allowed=$(flag_allowed_values "$flag_key" "$method" "$version")
  for v in $(echo "$explicit" | jq -r '.[]'); do
    if [[ " $allowed " != *" $v "* ]]; then
      echo "Error: unknown $flag_key value '$v' for $method in spec v${version} (allowed: ${allowed// /, })" >&2
      exit 1
    fi
  done
}

# Returns "with_<flag_key>/<sorted+joined values>" or "" if key or flags are empty/absent.
# $1: flag key (e.g. "trace_flags", "simulation_flags", "response_flags")
# $2: flag values JSON array (e.g. '["RETURN_INITIAL_READS"]')
flags_to_subdir() {
  local flag_key="$1"
  local flags="$2"
  if [[ -z "$flag_key" || -z "$flags" || "$flags" == "[]" ]]; then
    echo ""
  else
    local values
    values=$(echo "$flags" | jq -r 'sort | join("+")')
    echo "with_${flag_key}/${values}"
  fi
}

# Reads full RPC JSON from stdin, merges appropriate flags into .params, writes to stdout.
# Reads the caller's $spec_version (major.minor, set by detect-version.sh) to skip flags the
# spec version does not know and to reject invalid flag values.
# Usage: echo '{"id":1,...}' | add_method_params "starknet_getBlockWithTxs"
add_method_params() {
  local method="$1"
  local flag_key flag_value
  flag_key=$(get_flag_key "$method")
  [ -z "$flag_key" ] && {
    cat
    return
  }
  validate_flags "$method" "${spec_version:-}"
  flag_value=$(get_flag_value "$flag_key" "${spec_version:-}")
  [ -z "$flag_value" ] && {
    cat
    return
  }
  jq --arg key "$flag_key" --argjson val "$flag_value" '.params += {($key): $val}'
}
