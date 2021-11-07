FROM golang:1.16-alpine as bsc

RUN apk add --no-cache make gcc musl-dev linux-headers git bash

ADD . /bsc
WORKDIR /bsc
RUN make geth
RUN mv /bsc/build/bin/geth /usr/local/bin/geth

EXPOSE 8545 8547 30303 30303/udp
ENTRYPOINT [ "/usr/local/bin/geth" ]

FROM ethereum/solc:0.6.4-alpine as bsc-genesis

RUN apk add --d --no-cache ca-certificates npm nodejs bash alpine-sdk

RUN git clone https://github.com/binance-chain/bsc-genesis-contract.git /root/genesis \
    && rm /root/genesis/package-lock.json && cd /root/genesis && npm install

COPY docker/init_holders.template /root/genesis/init_holders.template

COPY --from=bsc /usr/local/bin/geth /usr/local/bin/geth

ENTRYPOINT [ "/bin/bash" ]
