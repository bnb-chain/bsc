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

# Init genesis state:
geth --datadir ${DATA_DIR} init genesis.json

exec "/usr/local/bin/geth" "--config" ${BSC_CONFIG} "--datadir" ${DATA_DIR} "$@" 