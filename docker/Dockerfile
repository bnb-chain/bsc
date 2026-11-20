FROM golang:1.26-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab as bsc

RUN apk add --no-cache make cmake gcc musl-dev linux-headers git bash build-base libc-dev

ADD . /bsc
WORKDIR /bsc
ENV CGO_CFLAGS="-O -D__BLST_PORTABLE__" 
ENV CGO_CFLAGS_ALLOW="-O -D__BLST_PORTABLE__"
RUN make geth
RUN mv /bsc/build/bin/geth /usr/local/bin/geth

ARG BSC_USER_UID=1000
ARG BSC_USER_GID=1000
ENV HOME=/home/bsc
RUN addgroup -g ${BSC_USER_GID} bsc \
    && adduser -u ${BSC_USER_UID} -G bsc -h ${HOME} -D bsc

USER ${BSC_USER_UID}:${BSC_USER_GID}
RUN mkdir -p -m 0700 "${HOME}/.ethereum"

EXPOSE 8545 8547 30303 30303/udp
ENTRYPOINT [ "/usr/local/bin/geth" ]

FROM ethereum/solc:0.6.4-alpine@sha256:39f727ab074268979a342c16843ffcadb0862206be78c0b8413d7c19a13ff3f8 as solc

FROM node:22-alpine3.21@sha256:af8023ec879993821f6d5b21382ed915622a1b0f1cc03dbeb6804afaf01f8885 as bsc-genesis

RUN if [ "$(node -p 'process.arch')" != "x64" ]; then echo "bsc-genesis requires linux/amd64 for solc 0.6.4" >&2; exit 1; fi \
    && apk add --no-cache ca-certificates bash alpine-sdk expect

ARG BSC_USER_UID=1000
ARG BSC_USER_GID=1000
ENV HOME=/home/bsc
RUN deluser node \
    && addgroup -g ${BSC_USER_GID} bsc \
    && adduser -u ${BSC_USER_UID} -G bsc -h ${HOME} -D bsc

COPY --from=solc /usr/local/bin/solc /usr/local/bin/solc

USER ${BSC_USER_UID}:${BSC_USER_GID}
WORKDIR ${HOME}

RUN git clone https://github.com/bnb-chain/bsc-genesis-contract.git ${HOME}/genesis \
    && cd ${HOME}/genesis \
    && git checkout --detach 84641812a013eda1593e058aeed4dc116c7746eb \
    && npm ci --ignore-scripts --no-audit
#RUN curl -L https://foundry.paradigm.xyz | bash

COPY --from=bsc /usr/local/bin/geth /usr/local/bin/geth

ENTRYPOINT [ "/bin/bash" ]
