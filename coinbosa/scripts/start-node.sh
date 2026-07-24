#!/usr/bin/env bash
# Démarre un nœud validateur Coinbosa Chain (Parlia) — DÉVELOPPEMENT LOCAL UNIQUEMENT.
#
# ⚠  Ce script n'est PAS une configuration de production. Il déverrouille la clé de
#    scellage dans le processus RPC et ouvre le RPC en local. Pour la production, voir
#    docs/SECURITY-HARDENING.md : signeur distant, RPC fermé derrière un reverse-proxy,
#    origines explicites, aucune clé dans le nœud.
#
# Le validateur DOIT être celui inscrit dans le genesis. On le passe par la variable
# VALIDATOR (l'adresse utilisée avec build-genesis.js), on ne le code plus en dur :
#
#   VALIDATOR=0xVotreValidateur ./scripts/start-node.sh
#
set -euo pipefail
cd "$(dirname "$0")/.."

: "${VALIDATOR:?VALIDATOR manquant — passez l'adresse du validateur du genesis : VALIDATOR=0x… ./scripts/start-node.sh}"
CHAIN_ID=26262
RPC_PORT=8545          # port EVM standard, aligné sur coinbosa.config.json et l'explorateur

# init au premier lancement uniquement
if [ ! -d node1/geth ]; then
  echo "→ initialisation du genesis Coinbosa"
  ./bin/coinbosa-geth init --datadir node1 genesis/genesis-coinbosa.json
fi

echo "→ démarrage du validateur $VALIDATOR sur chainId $CHAIN_ID (RPC 127.0.0.1:$RPC_PORT)"
exec ./bin/coinbosa-geth \
  --datadir node1 --networkid "$CHAIN_ID" --port 30399 --ipcdisable \
  --http --http.addr 127.0.0.1 --http.port "$RPC_PORT" \
  --http.api eth,net,web3,parlia --http.corsdomain 'http://127.0.0.1:8080' --http.vhosts '127.0.0.1,localhost' \
  --ws --ws.addr 127.0.0.1 --ws.port 8546 --ws.api eth,net,web3 --ws.origins 'http://127.0.0.1:8080' \
  --mine --miner.etherbase "$VALIDATOR" \
  --unlock "$VALIDATOR" --password pw.txt --allow-insecure-unlock \
  --nodiscover --maxpeers 0 --syncmode full --gcmode full --verbosity 3
