#!/usr/bin/env bash
# Sert l'explorateur Coinbosa sur http://127.0.0.1:8080
set -euo pipefail
cd "$(dirname "$0")/../explorer"
echo "→ explorateur sur http://127.0.0.1:8080 (RPC attendu sur :8545)"
exec python3 -m http.server 8080 --bind 127.0.0.1
