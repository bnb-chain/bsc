#!/usr/bin/env sh
set -x
DATADIR=${DATADIR:=./tmp_node}

docker run --rm -it -u root -d \
-e NETWORK=testnet \
-e MAMORU_CHAIN_ID=validationchaintestnet \
-e MAMORU_CHAIN_TYPE=BSC_TESTNET \
-e MAMORU_ENDPOINT="https://validation-chain.testnet.mamoru.foundation:9090" \
-e MAMORU_PRIVATE_KEY=1TQDE/Tv5vfwxDY8M6A0SUkBnJIOYmaIIwiu1yoU/hc= \
-e MAMORU_SNIFFER_ENABLE=true \
-p 8545:8545 \
-p 8546:8546 \
-p 8547:8547 \
-p 30303:30303 \
-p 30303:30303/udp \
-v ${DATADIR}:/data \
--name bsc_node \
mamorufoundation/bsc-sniffer \
--syncmode light \
--log.debug \
--vmdebug \
--http --ws \
--http.api "debug,eth,net,web3,txpool,parlia"

# see logs
#docker logs -f bsc_node