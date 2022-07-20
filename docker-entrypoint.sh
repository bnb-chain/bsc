#!/bin/bash
set -e

BSC_CONFIG=${BSC_HOME}/config/config.toml
BSC_GENESIS=${BSC_HOME}/config/genesis.json

# Init genesis state if geth not exist
DATA_DIR=$(cat ${BSC_CONFIG} | grep -A1 '\[Node\]' | grep -oP '\"\K.*?(?=\")')

GETH_DIR=${DATA_DIR}/geth
if [ ! -d "$GETH_DIR" ]; then
  geth --datadir ${DATA_DIR} init ${BSC_GENESIS}
fi

exec "geth" "--config" ${BSC_CONFIG} "$@"
