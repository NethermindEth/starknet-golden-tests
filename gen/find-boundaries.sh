#!/usr/bin/env bash
# Print the first block of each Starknet protocol version, from block 0 to
# <last block>, as "<block> <version>" lines. Block versions only increase, so
# a binary search finds each change.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <node url> <last block>" >&2
  exit 1
fi
url=$1
last=$2

version() {
  curl -sS --fail --retry 3 -X POST -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"starknet_getBlockWithTxHashes\",\"params\":{\"block_id\":{\"block_number\":$1}}}" \
    "$url" | jq -er '.result.starknet_version // "none"'
}

# split <lo> <hi> <version at lo> <version at hi> prints each change in (lo, hi].
split() {
  local lo=$1 hi=$2 vlo=$3 vhi=$4
  if [[ $vlo == "$vhi" ]]; then
    return
  fi
  if ((hi - lo == 1)); then
    echo "$hi $vhi"
    return
  fi
  local mid=$(((lo + hi) / 2)) vmid
  vmid=$(version "$mid")
  split "$lo" "$mid" "$vlo" "$vmid"
  split "$mid" "$hi" "$vmid" "$vhi"
}

v0=$(version 0)
echo "0 $v0"
split 0 "$last" "$v0" "$(version "$last")"
