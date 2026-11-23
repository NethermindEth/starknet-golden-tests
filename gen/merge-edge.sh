#!/usr/bin/env bash
# Merge edge runs into one mixed corpus for each version change: <out dir>/<version>.json
# holds blocks N-1, N and N+1 of the change at block N (blocks 0 and 1 for the first
# version). Input: JSON lines {"label": "<entry>/<block>", "req": <request>}.
# Each corpus has ids 1..N, no duplicate requests, and a group for each block
# method by number and by hash.
set -euo pipefail

if [[ $# -ne 5 ]]; then
  echo "usage: $0 <runs.jsonl> <chain id> <rpc version> <boundaries file> <out dir>" >&2
  exit 1
fi
runs=$1 chain=$2 rpc=$3 boundaries=$4 out=$5
dir=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$out"

while read -r block version; do
  [[ -z $block || $block == \#* ]] && continue
  if ((block == 0)); then from=0 to=1; else from=$((block - 1)) to=$((block + 1)); fi
  jq -s --arg chain "$chain" --arg rpc "$rpc" --argjson from "$from" --argjson to "$to" '
    map(select((.label | split("/")[-1] | tonumber) as $b | $b >= $from and $b <= $to))
    | reduce .[] as $x ({seen: {}, out: []};
        ($x.req | [.method, .params] | tojson) as $k
        | if .seen[$k] then . else .seen[$k] = true | .out += [$x] end)
    | .out
    | (to_entries | map({key: .value.label, value: (.key + 1)}) | from_entries) as $ids
    | {meta: {
        method: "mixed",
        rpcVersion: $rpc,
        chainId: $chain,
        count: length,
        batch: 0,
        labels: (to_entries | map({key: (.key + 1 | tostring), value: .value.label}) | from_entries),
        equivalent: [$ids | to_entries[]
          | select(.key | test("-blockHash/"))
          | [$ids[.key | sub("-blockHash/"; "-blockNumber/")], .value]
          | select(.[0] != null)]
      },
      requests: (to_entries | map(.key as $i | .value.req | .id = $i + 1))}
  ' "$runs" >"$out/$version.tmp"
  "$dir/format-corpus.sh" "$out/$version.tmp" >"$out/$version.json"
  rm "$out/$version.tmp"
done <"$boundaries"
