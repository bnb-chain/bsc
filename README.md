# BSC Client

## Background

The Greenfield Community has introduced the Block Archiver(https://github.com/bnb-chain/greenfield-bsc-archiver),
BSC historical block data is now accessible on Greenfield. To fullfill the need of BSC node operators requiring full sync from the genesis block,
and provide a more efficient synchronization, the Greenfield Peer is introduced.

## How Greenfield Peer Works

The diagram below illustrates the functionality of the Greenfield Peer. While the Greenfield peer does not participate in
other operations within the BSC network, it solely provides block data to BSC nodes. It does not persist any data on its own;
instead, when it receives requests (GetBodies and GetHeaders) from other BSC nodes, it fetches a bundle of blocks (# of blocks determined
by the Block Archiver Service) from Greenfield and caches them in memory. This ensures the Greenfield peer delivers block data
to BSC nodes efficiently.

![gnfd peer](/resource/greenfield-peer.png)

## How to Run Greenfield Peer

### Build

```shell
make geth
```

### Config the connection to Block archiver

The Greenfield Peer will integrate with Block Archiver as backend, so need to config the Block Archiver service in the config file.
take the following config for testnet Block-Archiver as an example:

```toml
[Eth.BlockArchiverConfig]
RPCAddress = "https://gnfd-bsc-archiver-testnet.bnbchain.org"
SPAddress = "https://gnfd-testnet-sp2.bnbchain.org"
BucketName = "testnet-bsc-block"
BlockCacheSize = 1000000
```

- RPCAddress: the RPC address of the Block Archiver service
- SPAddress: the SP address of the bucket on Greenfield which serves the block data
- BucketName: the bucket name on Greenfield which serves the block data
- BlockCacheSize: the cache size of the block data, note that Greenfield Peer will cache the block data in memory

### Run

```shell
./geth --config ./config.toml --datadir ./node
```

## How to interact with Greenfield Peer as a BSC node

Configure your BSC node to connect to the Greenfield Peer by adjusting the settings in your configuration file.

Navigate to the P2P section of your BSC node configuration file and specify the enode info of the Greenfield Peer.

```toml
# other configurations are omitted
...
[Node.P2P]
MaxPeers = 1
NoDiscovery = true
TrustedNodes = []
StaticNodes = ["${enode_info}"]
...
```

the `enode_info` for BSC Testnet and Mainnet can be found in the [network-info](https://docs.bnbchain.org/bnb-greenfield/for-developers/data-archive/greenfield-peer) page.


