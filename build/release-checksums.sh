#!/usr/bin/env bash
set -euo pipefail

for artifact in linux/geth_linux macos/geth_macos windows/geth_windows.exe \
    arm64/geth-linux-arm64 mainnet.zip testnet.zip; do
    (
        cd "$(dirname "$artifact")"
        sha256sum "$(basename "$artifact")"
    )
done
