#!/bin/bash

# Usage: resolve-output.sh <input_file> <version>
# Resolves the best matching output file for a given input file and version.
#
# Resolution order:
#   1. Exact version match (e.g., 100.output.0.8.0.json for version 0.8.0)
#   2. Closest newer version variant
#   3. Default output file (e.g., 100.output.json)
#
# Outputs the resolved output file path.

input_file="$1"
version="$2"

if [ -z "$input_file" ] || [ -z "$version" ]; then
    echo "Usage: $0 <input_file> <version>" >&2
    exit 1
fi

# Derive base output path (without .json extension)
base_output="${input_file%.input.json}.output"
default_output="${base_output}.json"

# Check for exact version match first
exact_match="${base_output}.${version}.json"
if [ -f "$exact_match" ]; then
    echo "$exact_match"
    exit 0
fi

# Find all version variant files
dir=$(dirname "$input_file")
base_name=$(basename "${base_output}")

# Collect all variant versions
variant_versions=()
while IFS= read -r -d '' variant_file; do
    filename=$(basename "$variant_file")
    # Remove base prefix and .json suffix to get version
    variant_version="${filename#"${base_name}".}"
    variant_version="${variant_version%.json}"

    # Skip if this looks like the default (no version)
    if [ -n "$variant_version" ] && [ "$variant_version" != "json" ]; then
        variant_versions+=("$variant_version")
    fi
done < <(find "$dir" -maxdepth 1 -name "${base_name}.*.json" -type f -print0 2>/dev/null)

# Find closest newer version using sort -V
# Add our version to the list, sort, then find the next version after ours
if [ ${#variant_versions[@]} -gt 0 ]; then
    # Sort versions with our target version included, find the one right after it
    closest_newer=$(printf '%s\n' "${variant_versions[@]}" "$version" | sort -V | grep -A1 "^${version}$" | tail -1)

    # If closest_newer is different from our version, we found a newer variant
    if [ -n "$closest_newer" ] && [ "$closest_newer" != "$version" ]; then
        echo "${base_output}.${closest_newer}.json"
        exit 0
    fi
fi

# Fall back to default output file
if [ -f "$default_output" ]; then
    echo "$default_output"
    exit 0
fi

echo "Error: No output file found for $input_file" >&2
exit 1
