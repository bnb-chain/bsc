<<<<<<< HEAD
FROM golang:1.15-alpine as builder
=======
# Build Geth in a stock Go builder container
FROM golang:1.16-alpine as builder
>>>>>>> origin/master

RUN apk add --no-cache make gcc musl-dev linux-headers git bash

# Temporarily pull a custom Go bundle
ADD https://golang.org/dl/go1.15.5.src.tar.gz /tmp/go.tar.gz
RUN (cd /tmp && tar -xf go.tar.gz)
RUN (cd /tmp/go/src && ./make.bash)
ENV PATH="/tmp/go/bin:${PATH}"

ADD . /go-ethereum
RUN cd /go-ethereum && make geth

# Pull Geth into a second stage deploy alpine container
FROM alpine:latest

RUN apk add --no-cache ca-certificates curl jq tini
COPY --from=builder /go-ethereum/build/bin/geth /usr/local/bin/

<<<<<<< HEAD
EXPOSE 8545 8546 8547 30303 30303/udp
ENTRYPOINT ["geth"]
=======
EXPOSE 8545 8546 30303 30303/udp
ENTRYPOINT ["geth"]
>>>>>>> origin/master
