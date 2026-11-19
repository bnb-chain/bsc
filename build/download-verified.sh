#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "Usage: $0 <https-url> <destination>" >&2
    exit 1
fi

url=$1
destination=$2
checksums="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/checksums.txt"
filename=${url##*/}

if [[ $url != https://* ]]; then
    echo "Download URL must use HTTPS" >&2
    exit 1
fi
if [[ -z $destination || -d $destination ]]; then
    echo "Destination must be a file path" >&2
    exit 1
fi

checksum=$(awk -v filename="$filename" '$2 == filename { print $1 }' "$checksums")
if [[ ! $checksum =~ ^[a-f0-9]{64}$ ]]; then
    echo "Missing or ambiguous SHA-256 for $filename" >&2
    exit 1
fi

temporary=$(mktemp "${destination}.XXXXXX")
trap 'rm -f "$temporary"' EXIT
curl --fail --show-error --silent --location --retry 3 \
    --connect-timeout 30 --max-time 600 \
    --proto '=https' --proto-redir '=https' \
    "$url" -o "$temporary"
printf '%s  %s\n' "$checksum" "$temporary" | sha256sum --check --strict -
mv "$temporary" "$destination"
