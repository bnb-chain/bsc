#!/usr/bin/env bash
# Démarre le validateur Coinbosa Chain (Parlia PoSA).
set -euo pipefail
cd "$(dirname "$0")/.."

VALIDATOR=0x3B78F3D76c6739c34872A34F9090cCb7607DD334
CHAIN_ID=26262

# init au premier lancement uniquement
if [ ! -d node1/geth ]; then
  echo "→ initialisation du genesis Coinbosa"
  ./bin/coinbosa-geth init --datadir node1 genesis/genesis-coinbosa-parlia.json
fi

echo "→ démarrage du validateur $VALIDATOR sur chainId $CHAIN_ID"
exec ./bin/coinbosa-geth \
  --datadir node1 --networkid "$CHAIN_ID" --port 30399 --ipcdisable \
  --http --http.addr 127.0.0.1 --http.port 8595 \
  --http.api eth,net,web3,txpool,debug,parlia --http.corsdomain '*' --http.vhosts '*' \
  --ws --ws.addr 127.0.0.1 --ws.port 8596 --ws.api eth,net,web3 --ws.origins '*' \
  --mine --miner.etherbase "$VALIDATOR" \
  --unlock "$VALIDATOR" --password pw.txt --allow-insecure-unlock \
  --nodiscover --maxpeers 0 --syncmode full --gcmode archive --verbosity 3
