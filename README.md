## BNB Smart Chain

The goal of BNB Smart Chain is to bring programmability and interoperability to BNB Beacon Chain. To embrace the existing community and advanced technology, it provides significant benefits by staying compatible with existing Ethereum smart contracts and tooling. To achieve that, the straightforward solution is to build on a go-ethereum fork, as we greatly respect the work of Ethereum.

BNB Smart Chain starts its development based on a go-ethereum fork. So you may see many tooling, binaries, and docs aligned with Ethereum, such as the name "geth".

[![API Reference](
https://pkg.go.dev/badge/github.com/ethereum/go-ethereum
)](https://pkg.go.dev/github.com/ethereum/go-ethereum?tab=doc)
[![Build Test](https://github.com/bnb-chain/bsc/actions/workflows/build-test.yml/badge.svg)](https://github.com/bnb-chain/bsc/actions)
[![Discord](https://img.shields.io/badge/discord-join%20chat-blue.svg)](https://discord.gg/z2VpC455eU)

From that EVM-compatible baseline, BNB Smart Chain introduces a system of 21 validators with Proof of Staked Authority (PoSA) consensus that supports short block times and lower fees. Validator candidates with the highest stake become validators and produce blocks. Double-sign detection and other slashing logic help guarantee security, stability, and chain finality.

**The BNB Smart Chain** will be:

- **A self-sovereign blockchain**: Provides security and safety with elected validators.
- **EVM-compatible**: Supports existing Ethereum tooling along with faster finality and cheaper transaction fees.
- **Distributed with on-chain governance**: Proof of Staked Authority brings decentralization and community participation. As the native token, BNB serves as both gas for smart contract execution and the token for staking.

More details are in the [White Paper](https://github.com/bnb-chain/whitepaper/blob/master/WHITEPAPER.md).

## Release Types
There are three release types, each with a clear purpose and version scheme:

- **1. Stable Release**: production-ready builds for the vast majority of users. Format: `v<Major>.<Minor>.<Patch>`, example: [v1.5.19](https://github.com/bnb-chain/bsc/releases/tag/v1.5.19).
- **2. Feature Release**: early access to a single feature without affecting the core product. Format: `v<Major>.<Minor>.<Patch>-feature-<FeatureName>`, example: [v1.5.19-feature-SI](https://github.com/bnb-chain/bsc/releases/tag/v1.5.19-feature-SI).
- **3. Preview Release**: bleeding-edge builds for users who want the latest code. Format: `v<Major>.<Minor>.<Patch>-<Meta>` where Meta values indicate maturity: alpha (experimental), beta (largely complete), rc (release candidate). Example: [v1.5.0-alpha](https://github.com/bnb-chain/bsc/releases/tag/v1.5.0-alpha).

## Key features

### Proof of Staked Authority
Although Proof-of-Work (PoW) has been proven as a practical mechanism to implement a decentralized network, it is not environmentally friendly and requires a large number of participants to maintain security.

Proof-of-Authority (PoA) provides some defense against 51% attacks, with improved efficiency and tolerance to certain levels of Byzantine actors (malicious or hacked). Meanwhile, PoA is often criticized for not being as decentralized as PoW, as validators (nodes that take turns producing blocks) hold concentrated authority and can be more prone to corruption and security attacks.

Other blockchains, such as EOS and Cosmos, introduce Delegated Proof of Stake (DPoS) to allow token holders to vote and elect the validator set. It increases decentralization and favors community governance.

To combine DPoS and PoA for consensus, BNB Smart Chain implements a novel consensus engine called Parlia that:

1. Blocks are produced by a limited set of validators.
2. Validators take turns to produce blocks in a PoA manner, similar to Ethereum's Clique consensus engine.
3. The validator set is elected in and out based on staking-based governance on BNB Smart Chain.
4. The Parlia consensus engine interacts with a set of [system contracts](https://docs.bnbchain.org/bnb-smart-chain/staking/overview/#system-contracts) to perform liveness slashing, revenue distribution, and validator set renewal functions.

## Native Token

BNB runs on BNB Smart Chain in the same way as ETH runs on Ethereum, so it remains the `native token` for BSC. This means BNB will be used to:

1. Pay `gas` to deploy or invoke smart contracts on BSC.

## Building the source

Many of the steps below are the same as or similar to go-ethereum.

For prerequisites and detailed build instructions, please read the [Installation Instructions](https://geth.ethereum.org/docs/getting-started/installing-geth).

Building `geth` requires both a Go (version 1.24 or later) and a C compiler (GCC 5 or higher). You can install
them using your favourite package manager. Once the dependencies are installed, run

```shell
make geth
```

or, to build the full suite of utilities:

```shell
make all
```

If you get this error when running the node with a self-built binary:
```shell
Caught SIGILL in blst_cgo_init, consult <blst>/bindings/go/README.md.
```
please try to add the following environment variables and build again:
```shell
export CGO_CFLAGS="-O -D__BLST_PORTABLE__"
export CGO_CFLAGS_ALLOW="-O -D__BLST_PORTABLE__"
```

## Executables

The bsc project comes with several wrappers/executables found in the `cmd`
directory.

|  Command   | Description |
| :--------: | ----------- |
| **`geth`** | Main BNB Smart Chain client binary. It is the entry point into the BSC network (main-, test- or private net), capable of running as a full node (default), archive node (retaining all historical state) or a light node (retrieving data live). It has the same RPC interfaces (and more) as go-ethereum and can be used by other processes as a gateway into the BSC network via JSON RPC endpoints exposed on top of HTTP, WebSocket and/or IPC transports. See `geth --help` and the [CLI page](https://geth.ethereum.org/docs/interface/command-line-options) for command line options. |
|   `clef`   | Stand-alone signing tool, which can be used as a backend signer for `geth`. |
|  `devp2p`  | Utilities to interact with nodes on the networking layer, without running a full blockchain. |
|  `abigen`  | Source code generator to convert Ethereum contract definitions into easy-to-use, compile-time type-safe Go packages. It operates on plain [Ethereum contract ABIs](https://docs.soliditylang.org/en/develop/abi-spec.html) with expanded functionality if the contract bytecode is also available. However, it also accepts Solidity source files, making development much more streamlined. Please see our [Native DApps](https://geth.ethereum.org/docs/dapp/native-bindings) page for details. |
| `bootnode` | Stripped-down version of our Ethereum client implementation that only takes part in the network node discovery protocol, but does not run any of the higher-level application protocols. It can be used as a lightweight bootstrap node to aid in finding peers in private networks. |
|   `evm`    | Developer utility version of the EVM (Ethereum Virtual Machine) that is capable of running bytecode snippets within a configurable environment and execution mode. Its purpose is to allow isolated, fine-grained debugging of EVM opcodes (e.g. `evm --code 60ff60ff --debug run`). |
| `rlpdump`  | Developer utility tool to convert binary RLP ([Recursive Length Prefix](https://ethereum.org/en/developers/docs/data-structures-and-encoding/rlp)) dumps (data encoding used by the Ethereum protocol both network as well as consensus-wise) to a more user-friendly hierarchical representation (e.g. `rlpdump --hex CE0183FFFFFFC4C304050583616263`). |

## Running `geth`

Going through all the possible command line flags is out of scope here (please consult our
[CLI Wiki page](https://geth.ethereum.org/docs/fundamentals/command-line-options)),
but we've enumerated a few common parameter combinations to get you up to speed quickly
on how you can run your own `geth` instance.

### Hardware Requirements

The hardware must meet certain requirements to run a full node on mainnet:
- VPS running recent versions of macOS, Linux, or Windows.
- **IMPORTANT:** 3 TB (Dec 2023) of free disk space; solid-state drive (SSD), gp3, 8k IOPS, 500 MB/s throughput, read latency < 1 ms. (If the node is started with snap sync, it will need an NVMe SSD.)
- 16 cores of CPU and 64 GB of memory (RAM).
- Suggested: `m5zn.6xlarge` or `r7iz.4xlarge` instance types on AWS; `c2-standard-16` on Google Cloud.
- A broadband Internet connection with upload/download speeds of 5 MB/s.

Requirements for testnet:
- VPS running recent versions of macOS, Linux, or Windows.
- 500 GB of storage for testnet.
- 4 cores of CPU and 16 GB of memory (RAM).

### Steps to Run a Fullnode

#### 1. Download the pre-built binaries
```shell
# Linux
wget $(curl -s https://api.github.com/repos/bnb-chain/bsc/releases/latest | grep browser_ | grep geth_linux | cut -d" -f4)
mv geth_linux geth
chmod -v u+x geth

# macOS
wget $(curl -s https://api.github.com/repos/bnb-chain/bsc/releases/latest | grep browser_ | grep geth_mac | cut -d" -f4)
mv geth_macos geth
chmod -v u+x geth
```

#### 2. Download the config files
```shell
# == mainnet
wget $(curl -s https://api.github.com/repos/bnb-chain/bsc/releases/latest | grep browser_ | grep mainnet | cut -d" -f4)
unzip mainnet.zip

# == testnet
wget $(curl -s https://api.github.com/repos/bnb-chain/bsc/releases/latest | grep browser_ | grep testnet | cut -d" -f4)
unzip testnet.zip
```

#### 3. Download snapshot
Download the latest chaindata snapshot from [here](https://github.com/bnb-chain/bsc-snapshots). Follow the guide to structure your files.

#### 4. Start a full node
```shell
# Runs with Path-Based Storage Scheme by default and enables inline state prune, keeping the latest 90,000 blocks' historical state.
./geth --config ./config.toml --datadir ./node --cache 8000 --rpc.allow-unprotected-txs --history.transactions 0

# Recommended for high performance if you care little about state consistency.
./geth --config ./config.toml --datadir ./node --cache 8000 --rpc.allow-unprotected-txs --history.transactions 0 --tries-verify-mode none
```

#### 5. Monitor node status

Monitor the log from **./node/bsc.log** by default. When the node has started syncing, you should see output like:
```shell
t=2022-09-08T13:00:27+0000 lvl=info msg="Imported new chain segment"             blocks=1    txs=177   mgas=17.317   elapsed=31.131ms    mgasps=556.259  number=21,153,429 hash=0x42e6b54ba7106387f0650defc62c9ace3160b427702dab7bd1c5abb83a32d8db dirty="0.00 B"
t=2022-09-08T13:00:29+0000 lvl=info msg="Imported new chain segment"             blocks=1    txs=251   mgas=39.638   elapsed=68.827ms    mgasps=575.900  number=21,153,430 hash=0xa3397b273b31b013e43487689782f20c03f47525b4cd4107c1715af45a88796e dirty="0.00 B"
t=2022-09-08T13:00:33+0000 lvl=info msg="Imported new chain segment"             blocks=1    txs=197   mgas=19.364   elapsed=34.663ms    mgasps=558.632  number=21,153,431 hash=0x0c7872b698f28cb5c36a8a3e1e315b1d31bda6109b15467a9735a12380e2ad14 dirty="0.00 B"
```

#### 6. Interact with fullnode
Start up `geth`'s built-in interactive [JavaScript console](https://geth.ethereum.org/docs/interface/javascript-console)
(via the trailing `console` subcommand), through which you can interact using [`web3` methods](https://web3js.readthedocs.io/en/)
(note: the `web3` version bundled within `geth` is very old, and not up to date with official docs),
as well as `geth`'s own [management APIs](https://geth.ethereum.org/docs/rpc/server).
This tool is optional, and if you leave it out you can always attach to an already running
`geth` instance with `geth attach`.

#### 7. More

More details about [running a node](https://docs.bnbchain.org/bnb-smart-chain/developers/node_operators/full_node/) and [becoming a validator](https://docs.bnbchain.org/bnb-smart-chain/validator/create-val/)

*Note: Although some internal protective measures prevent transactions from
crossing over between the main network and test network, you should always
use separate accounts for play and real money. Unless you manually move
accounts, `geth` will by default correctly separate the two networks and will not make any
accounts available between them.*

### Configuration

As an alternative to passing numerous flags to the `geth` binary, you can also pass a
configuration file via:

```shell
geth --config /path/to/your_config.toml
```

To get an idea of how the file should look, you can use the `dumpconfig` subcommand to
export your existing configuration:

```shell
geth --your-favourite-flags dumpconfig
```

### Programmatically interfacing `geth` nodes

As a developer, sooner rather than later you'll want to start interacting with `geth` and the
BSC network via your own programs and not manually through the console. To aid
this, `geth` has built-in support for JSON-RPC based APIs ([standard APIs](https://ethereum.org/en/developers/docs/apis/json-rpc/),
[`geth` specific APIs](https://geth.ethereum.org/docs/interacting-with-geth/rpc), and [BSC's JSON-RPC API Reference](rpc/json-rpc-api.md)).
These can be exposed via HTTP, WebSockets, and IPC (UNIX sockets on UNIX-based
platforms, and named pipes on Windows).

The IPC interface is enabled by default and exposes all the APIs supported by `geth`,
whereas the HTTP and WS interfaces need to be manually enabled and only expose a
subset of APIs due to security reasons. These can be turned on/off and configured as
you'd expect.

HTTP-based JSON-RPC API options:

  * `--http` Enable the HTTP-RPC server
  * `--http.addr` HTTP-RPC server listening interface (default: `localhost`)
  * `--http.port` HTTP-RPC server listening port (default: `8545`)
  * `--http.api` APIs offered over the HTTP-RPC interface (default: `eth,net,web3`)
  * `--http.corsdomain` Comma-separated list of domains from which to accept cross-origin requests (browser enforced)
  * `--ws` Enable the WS-RPC server
  * `--ws.addr` WS-RPC server listening interface (default: `localhost`)
  * `--ws.port` WS-RPC server listening port (default: `8546`)
  * `--ws.api` APIs offered over the WS-RPC interface (default: `eth,net,web3`)
  * `--ws.origins` Origins from which to accept WebSocket requests
  * `--ipcdisable` Disable the IPC-RPC server
  * `--ipcpath` Filename for IPC socket/pipe within the datadir (explicit paths escape it)

You'll need to use your own programming environment’s capabilities (libraries, tools, etc.) to
connect via HTTP, WS, or IPC to a `geth` node configured with the above flags, and you'll
need to speak [JSON-RPC](https://www.jsonrpc.org/specification) on all transports. You
can reuse the same connection for multiple requests!

**Note: Please understand the security implications of opening up an HTTP/WS-based
transport before doing so! Hackers on the internet are actively trying to subvert
BSC nodes with exposed APIs! Further, all browser tabs can access locally
running web servers, so malicious web pages could try to subvert locally available
APIs!**

### Operating a private network
- [BSC-Deploy](https://github.com/bnb-chain/node-deploy/): deployment tool for setting up BNB Smart Chain.

## Running a bootnode

Bootnodes are super-lightweight nodes that are not behind a NAT and are running just the discovery protocol. When you start up a node, it should log your enode, which is a public identifier that others can use to connect to your node.

First the bootnode requires a key, which can be created with the following command, which will save a key to boot.key:

```
bootnode -genkey boot.key
```

This key can then be used to generate a bootnode as follows:

```
bootnode -nodekey boot.key -addr :30311 -network bsc
```

The choice of port passed to `-addr` is arbitrary. 
The bootnode command returns the following logs to the terminal, confirming that it is running:

```
enode://3063d1c9e1b824cfbb7c7b6abafa34faec6bb4e7e06941d218d760acdd7963b274278c5c3e63914bd6d1b58504c59ec5522c56f883baceb8538674b92da48a96@127.0.0.1:0?discport=30311
Note: you're using cmd/bootnode, a developer tool.
We recommend using a regular node as bootstrap node for production deployments.
INFO [08-21|11:11:30.687] New local node record                    seq=1,692,616,290,684 id=2c9af1742f8f85ce ip=<nil> udp=0 tcp=0
INFO [08-21|12:11:30.753] New local node record                    seq=1,692,616,290,685 id=2c9af1742f8f85ce ip=54.217.128.118 udp=30311 tcp=0
INFO [09-01|02:46:26.234] New local node record                    seq=1,692,616,290,686 id=2c9af1742f8f85ce ip=34.250.32.100  udp=30311 tcp=0
```

## Contribution

Thank you for considering helping out with the source code! We welcome contributions
from anyone on the internet and are grateful for even the smallest fixes!

If you'd like to contribute to bsc, please fork, fix, commit, and send a pull request
for the maintainers to review and merge into the main code base. If you wish to submit
more complex changes, please check with the core devs first on [our Discord channel](https://discord.gg/bnbchain)
to ensure those changes are in line with the general philosophy of the project and/or get
some early feedback which can make both your efforts much lighter as well as our review
and merge procedures quick and simple.

Please make sure your contributions adhere to our coding guidelines:

 * Code must adhere to the official Go [formatting](https://golang.org/doc/effective_go.html#formatting)
   guidelines (i.e. uses [gofmt](https://golang.org/cmd/gofmt/)).
 * Code must be documented adhering to the official Go [commentary](https://golang.org/doc/effective_go.html#commentary)
   guidelines.
 * Pull requests need to be based on and opened against the `master` branch.
 * Commit messages should be prefixed with the package(s) they modify.
   * E.g. "eth, rpc: make trace configs optional"

Please see the [Developers' Guide](https://geth.ethereum.org/docs/developers/geth-developer/dev-guide)
for more details on configuring your environment, managing project dependencies, and
testing procedures.

## License

The bsc library (i.e. all code outside of the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0/en.html),
also included in our repository in the `COPYING.LESSER` file.

The bsc binaries (i.e. all code inside of the `cmd` directory) are licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html), also
included in our repository in the `COPYING` file.
