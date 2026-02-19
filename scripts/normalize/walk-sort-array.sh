#!/bin/bash

# Walk a JSON tree and sort an array at arbitrary depth.
# Usage: walk-sort-array.sh <parent_key> <array_path> <sort_expression> < input.json
#
# Recursively walks the JSON. When it encounters an object with a key matching
# <parent_key>, it applies: .<parent_key><array_path> |= sort_by(<sort_expression>)
#
# Examples:
#   walk-sort-array.sh 'state_diff' '.storage_diffs' '.address'
#   walk-sort-array.sh 'initial_reads' '.storage' '[.contract_address, .storage_key, .value]'

if [ $# -ne 3 ]; then
    echo "Error: Three arguments required: parent_key, array_path, sort_expression" >&2
    echo "Usage: $0 <parent_key> <array_path> <sort_expression>" >&2
    exit 1
fi

PARENT_KEY="$1"
ARRAY_PATH="$2"
SORT_EXPR="$3"

jq -Sc "
def walk(f):
  . as \$in
  | if type == \"object\" then
      reduce keys[] as \$key ({}; . + { (\$key): (\$in[\$key] | walk(f)) }) | f
    elif type == \"array\" then
      map(walk(f)) | f
    else
      f
    end;

walk(
  if type == \"object\" and (.${PARENT_KEY}? | type == \"object\" or type == \"array\") then
    .${PARENT_KEY}${ARRAY_PATH} |= sort_by(${SORT_EXPR})
  else
    .
  end
)
"