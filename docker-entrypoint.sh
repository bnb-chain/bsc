#!/usr/bin/env sh
set -e


NETWORK_VAR="${NETWORK:="mainnet"}"
echo "NETWORK:" "${NETWORK_VAR}"

BSC_CONFIG_VAR="https://github.com/binance-chain/bsc/releases/latest/download/${NETWORK_VAR}.zip"

curl -fLJo /tmp/testnet.zip ${BSC_CONFIG_VAR}
unzip -o /tmp/testnet.zip -d ${BSC_HOME}/config/
rm -rf /tmp/testnet.zip

BSC_CONFIG=${BSC_HOME}/config/config.toml
BSC_GENESIS=${BSC_HOME}/config/genesis.json

# Init genesis state if geth not exist
CFG_DATA_DIR=$(cat ${BSC_CONFIG} | grep -A1 '\[Node\]' | grep -oP '\"\K.*?(?=\")')

GETH_DIR="${DATA_DIR}/geth/${CFG_DATA_DIR}"
if [ ! -d ${GETH_DIR} ]; then
  geth --datadir ${DATA_DIR} init ${BSC_GENESIS}
  mkdir -p ${GETH_DIR}
fi

exec "geth" "--config" ${BSC_CONFIG} "--datadir" ${DATA_DIR} "$@"
