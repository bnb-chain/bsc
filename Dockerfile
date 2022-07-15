# Support setting various labels on the final image
ARG COMMIT=""
ARG VERSION=""
ARG BUILDNUM=""

# Build Geth in a stock Go builder container
FROM golang:1.17-alpine as builder

RUN apk add --no-cache make gcc musl-dev linux-headers git bash

ADD . /go-ethereum
RUN cd /go-ethereum && go run build/ci.go install ./cmd/geth

# Pull Geth into a second stage deploy alpine container
FROM alpine:3.16.0

ARG BSC_VERSION=v1.1.11
ARG BSC_USER=bsc
ARG BSC_USER_UID=1000
ARG BSC_USER_GID=1000

ENV BSC_HOME=/bsc
ENV HOME=${BSC_HOME}
ENV DATA_DIR=/data

RUN apk add --no-cache ca-certificates~=20211220 jq~=1.6 bash~=5.1.16-r2 bind-tools~=9.16.29-r0 tini~=0.19.0 curl==7.83.1-r2 sed~=4.8-r0 \
  && rm -rf /var/cache/apk/* \
  && addgroup -g ${BSC_USER_GID} ${BSC_USER} \
  && adduser -u ${BSC_USER_UID} -G ${BSC_USER} --shell /sbin/nologin --no-create-home -D ${BSC_USER} \
  && addgroup ${BSC_USER} tty \
  && sed -i -e "s/bin\/sh/bin\/bash/" /etc/passwd  

RUN echo "[ ! -z \"\$TERM\" -a -r /etc/motd ] && cat /etc/motd" >> /etc/bash/bashrc \
    && echo "Version: ${BSC_VERSION}" > /etc/motd

WORKDIR ${BSC_HOME}

COPY --from=builder /go-ethereum/build/bin/geth /usr/local/bin/

COPY docker-entrypoint.sh .github/release.env ./

RUN curl -LO $(cat release.env | cut -d'=' -f2 | head -n 1) \
    && unzip mainnet.zip -d mainnet && rm mainnet.zip \
    && curl -LO $(cat release.env | cut -d'=' -f2 | sed -n 2p) \
    && unzip testnet.zip -d testnet && rm testnet.zip

RUN chmod +x docker-entrypoint.sh \
    && mkdir -p ${DATA_DIR} \
    && chown -R ${BSC_USER_UID}:${BSC_USER_GID} ${BSC_HOME} ${DATA_DIR}

VOLUME ${DATA_DIR}

USER ${BSC_USER_UID}:${BSC_USER_GID}

EXPOSE 8545 8546 8547 30303 30303/udp

ENTRYPOINT ["/sbin/tini", "--", "./docker-entrypoint.sh"]
