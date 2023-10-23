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
     node generate-validator.js
     INIT_HOLDER_ADDRESSES=$(ls ${workspace}/init-holders | tr '\n' ',')
     INIT_HOLDER_ADDRESSES=${INIT_HOLDER_ADDRESSES/%,/}
     node generate-initHolders.js --initHolders ${INIT_HOLDER_ADDRESSES}
     node generate-genesis.js --chainid ${BSC_CHAIN_ID}
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

function prepareBLSWallet(){
     node_id=$1
     echo "123456" > ${workspace}/storage/${node_id}/blspassword.txt
     expect ${workspace}/scripts/create_bls_key.sh ${workspace}/storage/${node_id}

     sed -i -e 's/DataDir/BLSPasswordFile = \"{{BLSPasswordFile}}\"\nBLSWalletDir = \"{{BLSWalletDir}}\"\nDataDir/g' ${workspace}/storage/${node_id}/config.toml
     PassWordPath="/root/.ethereum/blspassword.txt"
     sed -i -e "s:{{BLSPasswordFile}}:${PassWordPath}:g" ${workspace}/storage/${node_id}/config.toml
     WalletPath="/root/.ethereum/bls/wallet"
     sed -i -e "s:{{BLSWalletDir}}:${WalletPath}:g" ${workspace}/storage/${node_id}/config.toml
}

prepare

# Step 1, generate config for each validator
for((i=1;i<=${NUMS_OF_VALIDATOR};i++)); do
     init_validator "bsc-validator${i}"
done

# Step 2, use validator configs to generate genesis file
generate_genesis

# Step 3, use genesis file to init cluster data
init_genesis_data bsc-rpc bsc-rpc

for((i=1;i<=${NUMS_OF_VALIDATOR};i++)); do
     init_genesis_data validator "bsc-validator${i}"
done

#Step 4, prepare bls wallet, used by fast finality vote
prepareBLSWallet bsc-rpc

for((i=1;i<=${NUMS_OF_VALIDATOR};i++)); do
     prepareBLSWallet "bsc-validator${i}"
done