#!/bin/bash

url="$1"

if [ -z "$url" ]; then
    echo "Usage: $0 <url>" >&2
    exit 1
fi

curl --silent --show-error --location --fail-with-body --compressed \
    --header 'Content-Type: application/json' \
    --header 'Accept: application/json' \
    --header 'Accept-Encoding: gzip' \
    --data @- \
    "$url"
