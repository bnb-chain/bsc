#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/release.env"

if [[ -z "$CONFIG_RELEASE" ]]; then
    if ! CONFIG_RELEASE=$(gh api --paginate --slurp 'repos/bnb-chain/bsc/releases?per_page=100' |
        jq -er --arg version "$RELEASE_VERSION" '
            def version_number:
                capture("^(?:pre-)?v(?<major>[0-9]+)\\.(?<minor>[0-9]+)\\.(?<patch>[0-9]+)(?:-[0-9A-Za-z.-]+)?$")
                | [.major, .minor, .patch] | map(tonumber);
            ($version | version_number) as $target
            | [ .[][]
                | select(.draft == false and .prerelease == false)
                | select(.tag_name | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
                | . + {version: (.tag_name | version_number)}
                | select(.version < $target)
              ]
            | sort_by(.version) | last | .tag_name // empty
        '); then
        echo "Cannot find the previous stable release for $RELEASE_VERSION; set CONFIG_RELEASE explicitly" >&2
        exit 1
    fi
fi

if [[ -z "$CONFIG_RELEASE" || "$CONFIG_RELEASE" == "$RELEASE_VERSION" ]]; then
    echo "Cannot use '$CONFIG_RELEASE' as the config source for $RELEASE_VERSION" >&2
    exit 1
fi

echo "Config source: $CONFIG_RELEASE"
for network in mainnet testnet; do
    curl --fail --location --retry 3 \
        "https://github.com/bnb-chain/bsc/releases/download/$CONFIG_RELEASE/$network.zip" \
        -o "$network.zip"
    unzip -t "$network.zip"
    unzip -p "$network.zip" "$network/genesis.json" | jq -e 'type == "object"' > /dev/null
    unzip -p "$network.zip" "$network/config.toml" | grep . > /dev/null
done
