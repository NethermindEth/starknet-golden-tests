#!/bin/bash

# Normalize state_diff arrays that have no semantic ordering by sorting them on a stable key
script_dir="$(dirname "$0")"
"$script_dir/sort-array.sh" '.result.state_diff.deployed_contracts' '.address' \
    | "$script_dir/sort-array.sh" '.result.state_diff.nonces' '.contract_address' \
    | "$script_dir/sort-array.sh" '.result.state_diff.storage_diffs' '.address' \
    | "$script_dir/sort-array.sh" '.result.state_diff.storage_diffs[].storage_entries' '.key' \
    | "$script_dir/sort-array.sh" '.result.state_diff.declared_classes' '.class_hash' \
    | "$script_dir/sort-array.sh" '.result.state_diff.deprecated_declared_classes' '.' \
    | "$script_dir/sort-array.sh" '.result.state_diff.replaced_classes' '.contract_address'
