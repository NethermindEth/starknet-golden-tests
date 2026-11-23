#!/usr/bin/env bash
# Generate the sampled and edge corpora of one network and spec version with
# Juno's corpus-gen (build it with `make corpus-gen` in the Juno repo).
#
#   sampled/<name>.json  SAMPLED_COUNT requests for each entry of sampled.json,
#                        from blocks 0..<block end>
#   edge/<version>.json  each entry of edge.json for blocks N-1, N, N+1 of each
#                        version change in <boundaries file> (blocks 0, 1 for
#                        the first version)
set -euo pipefail

if [[ $# -ne 5 ]]; then
  echo "usage: $0 <corpus-gen> <node url> <out dir> <block end> <boundaries file>" >&2
  exit 1
fi
cg=$1 url=$2 out=$3 end=$4 boundaries=$5
dir=$(cd "$(dirname "$0")" && pwd)
jobs_max=${JOBS:-4}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$out/sampled"
jq -r 'to_entries[] | "\(.key)\t\(.value)"' "$dir/sampled.json" | while IFS=$'\t' read -r name args; do
  echo "sampled $name" >&2
  # shellcheck disable=SC2086 # args holds the subcommand and its flags
  "$cg" $args --count "${SAMPLED_COUNT:-20}" --seed 1 --block-end "$end" --source-url "$url" \
    >"$tmp/sampled.json" 2>/dev/null
  "$dir/format-corpus.sh" "$tmp/sampled.json" >"$out/sampled/$name.json"
done

blocks=$(awk '!/^#/ && NF { if ($1 > 0) print $1 - 1, $1, $1 + 1 } END { print 0, 1 }' "$boundaries" |
  tr ' ' '\n' | sort -nu)

# edge_run <name> <args> <block> writes one corpus-gen run to $tmp/edge/.
edge_run() {
  local name=$1 sub=${2%% *} rest=${2#* } block=$3
  [[ $rest == "$2" ]] && rest=""
  # shellcheck disable=SC2086
  if ! "$cg" "$sub" --count 1 $rest --seed 1 --block-start "$block" --block-end "$block" \
    --source-url "$url" >"$tmp/edge/$block-$name.json" 2>"$tmp/edge/$block-$name.err"; then
    echo "skip $name at block $block: $(tail -1 "$tmp/edge/$block-$name.err")" >&2
    rm -f "$tmp/edge/$block-$name.json"
  fi
}

mkdir -p "$tmp/edge"
for block in $blocks; do
  txs=$(curl -sS --fail --retry 3 -X POST -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"starknet_getBlockTransactionCount\",\"params\":{\"block_id\":{\"block_number\":$block}}}" \
    "$url" | jq -er '.result')
  echo "edge block $block ($txs txs)" >&2
  while IFS=$'\t' read -r name args; do
    # Entries marked tx: sample transactions or state, which an empty block lacks.
    [[ $name == tx:* && $txs == 0 ]] && continue
    while (($(jobs -rp | wc -l) >= jobs_max)); do wait -n; done
    edge_run "${name#tx:}" "$args" "$block" &
  done < <(jq -r 'to_entries[] | "\(.key)\t\(.value)"' "$dir/edge.json")
done
wait

mapfile -t runs < <(printf '%s\n' "$tmp/edge"/*.json | sort -V)
for f in "${runs[@]}"; do
  label=$(basename "$f" .json)
  jq -c --arg label "${label#*-}/${label%%-*}" '.requests[] | {label: $label, req: .}' "$f"
done >"$tmp/edge.jsonl"
"$dir/merge-edge.sh" "$tmp/edge.jsonl" "$(jq -r .meta.chainId "${runs[0]}")" \
  "$(jq -r .meta.rpcVersion "${runs[0]}")" "$boundaries" "$out/edge"
