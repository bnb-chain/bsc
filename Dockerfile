# Support setting various labels on the final image
ARG COMMIT=""
ARG VERSION=""
ARG BUILDNUM=""

# Build Geth in a stock Go builder container
FROM golang:1.19 as builder

RUN apt install make gcc git bash

# Get dependencies - will also be cached if we won't change go.mod/go.sum
COPY go.mod /go-ethereum/
COPY go.sum /go-ethereum/
RUN cd /go-ethereum && go mod download

ADD . /go-ethereum

# For blst
ENV CGO_CFLAGS="-O -D__BLST_PORTABLE__"
ENV CGO_CFLAGS_ALLOW="-O -D__BLST_PORTABLE__"
RUN cd /go-ethereum && go run build/ci.go install ./cmd/geth

# Pull Geth into a second stage deploy alpine container
FROM debian:bullseye-slim

ARG BSC_USER=bsc
ARG BSC_USER_UID=1000
ARG BSC_USER_GID=1000

ENV BSC_HOME=/bsc
ENV HOME=${BSC_HOME}
ENV DATA_DIR=/data

ENV PACKAGES ca-certificates jq unzip\
  bash tini \
  grep curl sed gcc

RUN apt-get update && apt-get install -y $PACKAGES \
  && addgroup --gid ${BSC_USER_GID} ${BSC_USER} \
  && adduser -u ${BSC_USER_UID} --gid ${BSC_USER_GID} ${BSC_USER} --shell /sbin/nologin --no-create-home \
  && addgroup ${BSC_USER} tty \
  && sed -i -e "s/bin\/sh/bin\/bash/" /etc/passwd \
  && apt-get clean

RUN echo "[ ! -z \"\$TERM\" -a -r /etc/motd ] && cat /etc/motd" >> /etc/bash/bashrc

WORKDIR ${BSC_HOME}

COPY --from=builder /go-ethereum/build/bin/geth /usr/local/bin/

COPY docker-entrypoint.sh ./

RUN chmod +x docker-entrypoint.sh \
    && mkdir -p ${DATA_DIR} \
    && chown -R ${BSC_USER_UID}:${BSC_USER_GID} ${BSC_HOME} ${DATA_DIR}

VOLUME ${DATA_DIR}

USER ${BSC_USER_UID}:${BSC_USER_GID}

# rpc ws graphql
EXPOSE 8545 8546 8547 30303 30303/udp

ENTRYPOINT ["/usr/bin/tini", "--", "./docker-entrypoint.sh"]
