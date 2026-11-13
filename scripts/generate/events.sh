#!/bin/bash
set -e
trap 'echo "Error on line $LINENO: $BASH_COMMAND"; exit 1' ERR

script_dir="$(dirname "$0")"
source "${script_dir}/parse-args.sh"
parse_args "$@"

block_number="${REMAINING_ARGS[0]}"
rpc_url="$RPC_URL"

if [ -z "$block_number" ] || [ -z "$rpc_url" ]; then
    echo "Usage: $0 [--rpc-url <url>] <block_number>" >&2
    echo "" >&2
    echo "Generates starknet_getEvents tests derived from the events emitted in the given block:" >&2
    echo "  whole block (by number and by hash), address filter, key filter, address+key filter," >&2
    echo "  two-position key filter, a range over the two preceding blocks, an empty result," >&2
    echo "  and a two-page pagination walk with chunk_size 3." >&2
    echo "" >&2
    echo "RPC URL can be provided via --rpc-url flag or STARKNET_RPC env var." >&2
    echo "" >&2
    echo "Examples:" >&2
    echo "  $0 --rpc-url http://localhost:6060 465000" >&2
    echo "  STARKNET_RPC=http://localhost:6060 $0 465000" >&2
    exit 1
fi

# Auto-detect network
echo "🔍 Auto-detecting network by querying starknet_chainId..."
if ! tests_folder=$(STARKNET_RPC="$rpc_url" "${script_dir}/../run/detect-network.sh") || [ -z "$tests_folder" ]; then
    exit 1
fi
network=$(basename "$tests_folder")
echo "✅ Using network: $network"

# Detect spec version
echo "🔍 Detecting spec version..."
if ! spec_version=$(STARKNET_RPC="$rpc_url" "${script_dir}/../run/detect-version.sh") || [ -z "$spec_version" ]; then
    echo "Error: Could not detect spec version" >&2
    exit 1
fi
echo "✅ Spec version: $spec_version"

method="starknet_getEvents"
chunk_size=1000 # Pathfinder rejects anything above 1024
test_dir="tests/${network}/v${spec_version}/${method}"
mkdir -p "$test_dir"

# Source of truth for the block's events: the receipts
echo "🔍 Querying starknet_getBlockWithReceipts for block $block_number..."
receipts=$(jq -nc --argjson n "$block_number" \
    '{id: 1, jsonrpc: "2.0", method: "starknet_getBlockWithReceipts", params: {block_id: {block_number: $n}}}' \
    | STARKNET_RPC="$rpc_url" "${script_dir}/../run/query-rpc.sh")

block_hash=$(echo "$receipts" | jq -r '.result.block_hash // empty')
if [ -z "$block_hash" ]; then
    echo "Error: Could not fetch block $block_number" >&2
    exit 1
fi

# Flat list of the block's events in emission order, shaped like EMITTED_EVENT
expected_events=$(echo "$receipts" | jq -c '.result as $b
    | [$b.transactions | to_entries[] | .key as $ti | .value.receipt.transaction_hash as $tx
       | .value.receipt.events | to_entries[]
       | {from_address: .value.from_address, keys: .value.keys, data: .value.data,
          transaction_hash: $tx, transaction_index: $ti, event_index: .key,
          block_number: $b.block_number, block_hash: $b.block_hash}]')
event_count=$(echo "$expected_events" | jq 'length')
if [ "$event_count" -eq 0 ]; then
    echo "Error: Block $block_number emits no events; pick another block" >&2
    exit 1
fi

top_address=$(echo "$expected_events" | jq -r 'group_by(.from_address) | max_by(length) | .[0].from_address')
top_selector=$(echo "$expected_events" | jq -r 'group_by(.keys[0]) | max_by(length) | .[0].keys[0]')
top_address_selector=$(echo "$expected_events" | jq -r --arg a "$top_address" 'map(select(.from_address == $a)) | group_by(.keys[0]) | max_by(length) | .[0].keys[0]')
two_keys=$(echo "$expected_events" | jq -c 'map(select(.keys | length > 1)) | .[0].keys[0:2] // empty')
echo "✅ Block $block_number: $event_count events; busiest emitter $top_address; commonest selector $top_selector"

# write_test <test_name> <filter_json>
write_test() {
    local test_name="$1" filter="$2"
    jq -nc --arg method "$method" --argjson filter "$filter" \
        '{id: 1, jsonrpc: "2.0", method: $method, params: {filter: $filter}}' \
        >"${test_dir}/${test_name}.input.json"
    echo "Processing $method ($test_name)..."
    STARKNET_RPC="$rpc_url" "${script_dir}/write-output.sh" "$network" "$spec_version" "$method" "$test_name"
}

# check_events <test_name> <expected_events_json> : output events must equal expected exactly
check_events() {
    local output="${test_dir}/$1.output.json"
    if ! diff -u <(echo "$2" | jq -S .) <(jq -S '.result.events' "$output"); then
        echo "  ❌ $1: events differ from the block receipts" >&2
        exit 1
    fi
    echo "  ✅ $1 matches the receipts ($(echo "$2" | jq 'length') events)"
}

by_number=$(jq -nc --argjson n "$block_number" '{block_number: $n}')
by_hash=$(jq -nc --arg h "$block_hash" '{block_hash: $h}')

# 1. Whole block, by number and by hash
write_test "${block_number}" "$(jq -nc --argjson b "$by_number" --argjson c $chunk_size '{from_block: $b, to_block: $b, chunk_size: $c}')"
check_events "${block_number}" "$expected_events"
write_test "${block_number}-${block_hash}" "$(jq -nc --argjson b "$by_hash" --argjson c $chunk_size '{from_block: $b, to_block: $b, chunk_size: $c}')"
check_events "${block_number}-${block_hash}" "$expected_events"

# 2. Address filter
write_test "${block_number}-address-${top_address}" "$(jq -nc --argjson b "$by_number" --argjson c $chunk_size --arg a "$top_address" '{from_block: $b, to_block: $b, address: $a, chunk_size: $c}')"
check_events "${block_number}-address-${top_address}" "$(echo "$expected_events" | jq -c --arg a "$top_address" 'map(select(.from_address == $a))')"

# 3. Key filter (first key position)
write_test "${block_number}-keys-${top_selector}" "$(jq -nc --argjson b "$by_number" --argjson c $chunk_size --arg k "$top_selector" '{from_block: $b, to_block: $b, keys: [[$k]], chunk_size: $c}')"
check_events "${block_number}-keys-${top_selector}" "$(echo "$expected_events" | jq -c --arg k "$top_selector" 'map(select(.keys[0] == $k))')"

# 4. Address + key filter
write_test "${block_number}-address-${top_address}-keys-${top_address_selector}" "$(jq -nc --argjson b "$by_number" --argjson c $chunk_size --arg a "$top_address" --arg k "$top_address_selector" '{from_block: $b, to_block: $b, address: $a, keys: [[$k]], chunk_size: $c}')"
check_events "${block_number}-address-${top_address}-keys-${top_address_selector}" "$(echo "$expected_events" | jq -c --arg a "$top_address" --arg k "$top_address_selector" 'map(select(.from_address == $a and .keys[0] == $k))')"

# 5. Two-position key filter (exact match on keys[0] and keys[1])
if [ -n "$two_keys" ]; then
    k0=$(echo "$two_keys" | jq -r '.[0]'); k1=$(echo "$two_keys" | jq -r '.[1]')
    write_test "${block_number}-keys-${k0}-${k1}" "$(jq -nc --argjson b "$by_number" --argjson c $chunk_size --arg k0 "$k0" --arg k1 "$k1" '{from_block: $b, to_block: $b, keys: [[$k0], [$k1]], chunk_size: $c}')"
    check_events "${block_number}-keys-${k0}-${k1}" "$(echo "$expected_events" | jq -c --arg k0 "$k0" --arg k1 "$k1" 'map(select(.keys[0] == $k0 and .keys[1] == $k1))')"
else
    echo "⚠️  no event with two or more keys in block $block_number; skipping the two-position key test" >&2
fi

# 6. Range over the two preceding blocks and this one (ordering across blocks; not checked against receipts)
range_from=$((block_number - 2))
if [ "$range_from" -ge 0 ]; then
    write_test "${range_from}-${block_number}" "$(jq -nc --argjson f "$range_from" --argjson t "$block_number" --argjson c $chunk_size '{from_block: {block_number: $f}, to_block: {block_number: $t}, chunk_size: $c}')"
    tail_events=$(jq -c --argjson n "$block_number" '[.result.events[] | select(.block_number == $n)]' "${test_dir}/${range_from}-${block_number}.output.json")
    if [ "$(echo "$tail_events" | jq -S .)" != "$(echo "$expected_events" | jq -S .)" ]; then
        echo "  ❌ ${range_from}-${block_number}: events of block $block_number inside the range differ from the receipts" >&2
        exit 1
    fi
    echo "  ✅ ${range_from}-${block_number}: block $block_number's events inside the range match the receipts"
fi

# 7. Empty result: an address that emits nothing
write_test "${block_number}-address-none" "$(jq -nc --argjson b "$by_number" --argjson c $chunk_size '{from_block: $b, to_block: $b, address: "0xdead", chunk_size: $c}')"
check_events "${block_number}-address-none" "[]"

# 8. Pagination: two pages of three events. The continuation token is node-defined, so page 2 embeds
#    whatever token the reference node returned.
write_test "${block_number}-chunk3-page1" "$(jq -nc --argjson b "$by_number" '{from_block: $b, to_block: $b, chunk_size: 3}')"
check_events "${block_number}-chunk3-page1" "$(echo "$expected_events" | jq -c '.[0:3]')"
token=$(jq -r '.result.continuation_token // empty' "${test_dir}/${block_number}-chunk3-page1.output.json")
if [ -n "$token" ]; then
    write_test "${block_number}-chunk3-page2" "$(jq -nc --argjson b "$by_number" --arg t "$token" '{from_block: $b, to_block: $b, chunk_size: 3, continuation_token: $t}')"
    check_events "${block_number}-chunk3-page2" "$(echo "$expected_events" | jq -c '.[3:6]')"
else
    echo "⚠️  page 1 returned no continuation_token (block has $event_count events); skipping page 2" >&2
fi

echo "Done processing $method for block $block_number"
