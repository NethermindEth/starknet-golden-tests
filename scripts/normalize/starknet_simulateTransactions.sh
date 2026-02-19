#!/bin/bash

# Normalize .result[].transaction_trace.state_diff.storage_diffs by sorting by .address
script_dir="$(dirname "$0")"
"$script_dir/walk-sort-array.sh" 'state_diff' '.storage_diffs' '.address' \
    | "$script_dir/walk-sort-array.sh" 'state_diff' '.storage_diffs[].storage_entries' '.key' \
    | "$script_dir/walk-sort-array.sh" 'initial_reads' '.class_hashes' '[.contract_address, .class_hash]' \
    | "$script_dir/walk-sort-array.sh" 'initial_reads' '.declared_contracts' '.class_hash' \
    | "$script_dir/walk-sort-array.sh" 'initial_reads' '.nonces' '.contract_address' \
    | "$script_dir/walk-sort-array.sh" 'initial_reads' '.storage' '[.contract_address, .storage_key, .value]'