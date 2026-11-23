#!/usr/bin/env bash
# Print a corpus with the meta indented and one request per line. It drops
# generatedAt, so the same corpus gives the same bytes.
set -euo pipefail
printf '{\n  "meta": '
jq '.meta | del(.generatedAt)' "$1" | sed '2,$s/^/  /'
printf ',\n  "requests": [\n'
jq -c '.requests[]' "$1" | sed '$!s/$/,/; s/^/    /'
printf '  ]\n}\n'
