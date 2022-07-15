#!/bin/bash
set -e

NETWORK=${BSC_NETWORK:-testnet}
DATA_DIR=${BSC_DATA_DIR:-/data}
BSC_CONFIG=${BSC_HOME}/config.toml

# Create symlink to network config.toml & genesis.json
ln -sf ./${NETWORK}/config.toml config.toml
ln -sf ./${NETWORK}/genesis.json genesis.json

# Default log to console
sed -i '/Node\.LogConfig/,/^$/d' ./${NETWORK}/config.toml

# TO-DO: remove after the default value in config.toml updated to empty
# Issue: https://github.com/bnb-chain/bsc/issues/994
sed -i 's/^HTTPHost.*/HTTPHost = "0.0.0.0"/'  ./${NETWORK}/config.toml

# Init genesis state if geth not exist
GETH_DIR=${DATA_DIR}/geth
if [ ! -d "$GETH_DIR" ]; then
  geth --datadir ${DATA_DIR} init genesis.json
fi

exec "geth" "--config" ${BSC_CONFIG} "--datadir" ${DATA_DIR} "$@"