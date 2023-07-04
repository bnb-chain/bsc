FROM golang:1.19-alpine as bsc

RUN apk add --no-cache make cmake gcc musl-dev linux-headers git bash build-base libc-dev

ADD . /bsc
WORKDIR /bsc
ENV CGO_CFLAGS="-O -D__BLST_PORTABLE__" 
ENV CGO_CFLAGS_ALLOW="-O -D__BLST_PORTABLE__"
RUN make geth
RUN mv /bsc/build/bin/geth /usr/local/bin/geth

EXPOSE 8545 8547 30303 30303/udp
ENTRYPOINT [ "/usr/local/bin/geth" ]

FROM ethereum/solc:0.6.4-alpine as bsc-genesis

RUN apk add --no-cache ca-certificates npm nodejs bash alpine-sdk expect

RUN git clone https://github.com/bnb-chain/bsc-genesis-contract.git /root/genesis \
    && cd /root/genesis && npm install

COPY docker/init_holders.template /root/genesis/init_holders.template

COPY --from=bsc /usr/local/bin/geth /usr/local/bin/geth

ENTRYPOINT [ "/bin/bash" ]
