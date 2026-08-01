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

# GARDE DURE : ce script est DÉVELOPPEMENT LOCAL UNIQUEMENT (il déverrouille la clé de
# scellage dans le processus RPC). Il refuse de démarrer sans COINBOSA_DEV=1, pour empêcher
# tout usage en production par erreur. Production => docs/SECURITY-HARDENING.md (signeur
# distant/HSM, RPC fermé, aucune clé dans le nœud).
if [ "${COINBOSA_DEV:-}" != "1" ]; then
  echo "refus : start-node.sh est DEV uniquement (deverrouille la cle de scellage dans le process)." >&2
  echo "  usage dev volontaire : COINBOSA_DEV=1 ./scripts/start-node.sh" >&2
  echo "  production : voir docs/SECURITY-HARDENING.md (signeur distant/HSM, RPC ferme, aucune cle dans le noeud)." >&2
  exit 1
fi

umask 077                              # nouveaux secrets non lisibles par groupe/autres
[ -f pw.txt ] && chmod 600 pw.txt      # mot de passe de la clé (F26)

if [ -z "${VALIDATOR:-}" ]; then
  echo "VALIDATOR manquant — passez l'adresse du validateur du genesis :" >&2
  echo "  VALIDATOR=0x... ./scripts/start-node.sh" >&2
  exit 1
fi
CHAIN_ID=26262
RPC_PORT=8545          # port EVM standard, aligné sur coinbosa.config.json et l'explorateur

# Le binaire est produit par `make geth` à la racine du dépôt : build/bin/geth
# (soit ../build/bin/geth depuis ce dossier coinbosa/). Il n'existe pas de ./bin/.
GETH=../build/bin/geth
if [ ! -x "$GETH" ]; then
  echo "erreur : $GETH introuvable — compilez d'abord le client : (à la racine) make geth" >&2
  exit 1
fi

# init au premier lancement uniquement — genesis de DÉV (ce script est DEV-only).
if [ ! -d node1/geth ]; then
  echo "→ initialisation du genesis Coinbosa (développement)"
  "$GETH" init --datadir node1 genesis/genesis-coinbosa-dev.json
fi

echo "→ démarrage du validateur $VALIDATOR sur chainId $CHAIN_ID (RPC 127.0.0.1:$RPC_PORT)"
exec "$GETH" \
  --datadir node1 --networkid "$CHAIN_ID" --port 30399 --ipcdisable \
  --http --http.addr 127.0.0.1 --http.port "$RPC_PORT" \
  --http.api eth,net,web3,parlia --http.corsdomain 'http://127.0.0.1:8080' --http.vhosts '127.0.0.1,localhost' \
  --ws --ws.addr 127.0.0.1 --ws.port 8546 --ws.api eth,net,web3 --ws.origins 'http://127.0.0.1:8080' \
  --mine --miner.etherbase "$VALIDATOR" \
  --unlock "$VALIDATOR" --password pw.txt --allow-insecure-unlock \
  --nodiscover --maxpeers 0 --syncmode full --gcmode full --verbosity 3
