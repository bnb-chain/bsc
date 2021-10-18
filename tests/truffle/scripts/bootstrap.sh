#!/usr/bin/env bash

workspace=$(cd `dirname $0`; pwd)/..

function prepare() {
   if ! [[ -f /usr/local/bin/geth ]];then
        echo "geth do not exist!"
        exit 1
   fi
   rm -rf ${workspace}/storage/*
   cd ${workspace}/genesis
   rm -rf validators.conf
}

function init_validator() {
     node_id=$1
     mkdir -p ${workspace}/storage/${node_id}
     geth --datadir ${workspace}/storage/${node_id} account new   --password /dev/null > ${workspace}/storage/${node_id}Info
     validatorAddr=`cat ${workspace}/storage/${node_id}Info|grep 'Public address of the key'|awk '{print $6}'`
     echo "${validatorAddr},${validatorAddr},${validatorAddr},0x0000000010000000" >> ${workspace}/genesis/validators.conf
     echo ${validatorAddr} > ${workspace}/storage/${node_id}/address
}

function generate_genesis() {
     INIT_HOLDER_ADDRESSES=$(ls ${workspace}/init-holders | tr '\n' ',')
     INIT_HOLDER_ADDRESSES=${INIT_HOLDER_ADDRESSES/%,/}
     sed  "s/{{INIT_HOLDER_ADDRESSES}}/${INIT_HOLDER_ADDRESSES}/g" ${workspace}/genesis/init_holders.template | sed  "s/{{INIT_HOLDER_BALANCE}}/${INIT_HOLDER_BALANCE}/g" > ${workspace}/genesis/init_holders.js
     node generate-validator.js
     chainIDHex=$(printf '%04x\n' ${BSC_CHAIN_ID})
     node generate-genesis.js --chainid ${BSC_CHAIN_ID} --bscChainId ${chainIDHex}
}

function init_genesis_data() {
     node_type=$1
     node_id=$2
     geth --datadir ${workspace}/storage/${node_id} init ${workspace}/genesis/genesis.json
     cp ${workspace}/config/config-${node_type}.toml  ${workspace}/storage/${node_id}/config.toml
     sed -i -e "s/{{NetworkId}}/${BSC_CHAIN_ID}/g" ${workspace}/storage/${node_id}/config.toml
     if [ "${node_id}" == "bsc-rpc" ]; then
          cp ${workspace}/init-holders/* ${workspace}/storage/${node_id}/keystore
          cp ${workspace}/genesis/genesis.json ${workspace}/storage/${node_id}
     fi
}

prepare

# First, generate config for each validator
for((i=1;i<=${NUMS_OF_VALIDATOR};i++)); do
     init_validator "bsc-validator${i}"
done

# Then, use validator configs to generate genesis file
generate_genesis

# Finally, use genesis file to init cluster data
init_genesis_data bsc-rpc bsc-rpc

for((i=1;i<=${NUMS_OF_VALIDATOR};i++)); do
     init_genesis_data validator "bsc-validator${i}"
done
