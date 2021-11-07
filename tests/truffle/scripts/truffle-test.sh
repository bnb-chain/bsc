#!/usr/bin/env bash

sed -i -e "s/localhost:8545/${RPC_HOST}:${RPC_PORT}/g" test/TestProxyBEP20.js

npm run truffle:test
