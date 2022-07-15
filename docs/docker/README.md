## Docker Image

Included in this repo is a Dockerfile that you can launch BSC node for trying it out. Docker images are available on `ghcr.io/bnb-chain/bsc`.

You can build the docker image with the following commands:
```bash
make docker
```

If your build machine has an ARM-based chip, like Apple silicon (M1), the image is built for `linux/arm64` by default. To build for `x86_64`, apply the --platform arg:
```bash
docker build --platform linux/amd64 -t bnb-chain/bsc -f Dockerfile .
```

You can start your docker container with the following command:
```bash
docker run --rm --name bsc -it bnb-chain/bsc
```
This will start a full node in `testnet` by default.

To start a full node in `mainnet`, you can pass in env variable `BSC_NETWORK`:
```bash
docker run --rm --name bsc -it -e BSC_NETWORK=mainnet bnb-chain/bsc
```

Another env variable `BSC_DATA_DIR` is also avaiable to overwrite the default DataDir. You can also use `--datadir` to overwrite DataDir too.
To overwrite default `DataDir` with env `BSC_DATA_DIR`:
```bash
docker run --rm --name bsc -it -e BSC_DATA_DIR=/data2 bnb-chain/bsc
```

To overwrite default `DataDir` with `ETHEREUM OPTIONS` `--datadir`:
```bash
docker run --rm --name bsc -it bnb-chain/bsc --datadir /data2
```
`--datadir` will overrides `BSC_DATA_DIR`


To start a node with geth command with `ETHEREUM OPTIONS`:
```bash
docker run --rm --name bsc -it -e BSC_NETWORK=mainnet bnb-chain/bsc --http.addr 0.0.0.0 --http.port 8545 --http.vhosts '*' --verbosity 3
```

If you need to open another shell, just do:
```bash
docker exec -it bsc /bin/bash
```
