#!/bin/bash

# Normalize .result.state_diff.deployed_contracts and .result.state_diff.storage_diffs by sorting by .address
script_dir="$(dirname "$0")"
"$script_dir/sort-array.sh" '.result.state_diff.deployed_contracts' '.address' \
    | "$script_dir/sort-array.sh" '.result.state_diff.storage_diffs' '.address' \
    | "$script_dir/sort-array.sh" '.result.state_diff.storage_diffs[].storage_entries' '.key'
