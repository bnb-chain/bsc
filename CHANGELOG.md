# Changelog

## v1.1.1
IMPROVEMENT
* [\#355](https://github.com/binance-chain/bsc/pull/355) miner should propose block on a proper fork

BUGFIX
* [\#350](https://github.com/binance-chain/bsc/pull/350) flag: fix TriesInmemory specified but not work
* [\#358](https://github.com/binance-chain/bsc/pull/358) miner: fix null pending block
* [\#360](https://github.com/binance-chain/bsc/pull/360) pruner: fix state bloom sync permission in Windows 
* [\#366](https://github.com/binance-chain/bsc/pull/366) fix double close channel of subfetcher


## v1.1.1-beta
* [\#333](https://github.com/binance-chain/bsc/pull/333) improve block fetcher efficiency
* [\#326](https://github.com/binance-chain/bsc/pull/326) eth/tracers: improve tracing performance
* [\#257](https://github.com/binance-chain/bsc/pull/257) performance improvement in many aspects


## v1.1.0
* [\#282](https://github.com/binance-chain/bsc/pull/282) update discord link
* [\#280](https://github.com/binance-chain/bsc/pull/280) update discord link
* [\#227](https://github.com/binance-chain/bsc/pull/227) use more aggressive write cache policy

## v1.1.0-beta
* [\#152](https://github.com/binance-chain/bsc/pull/152) upgrade to go-ethereum 1.10.3

## v1.0.7-hf.2
BUGFIX
* [\#194](https://github.com/binance-chain/bsc/pull/194) bump btcd to v0.20.1-beta

## v1.0.7-hf.1
BUGFIX
* [\#190](https://github.com/binance-chain/bsc/pull/190) fix disk increase dramaticly
* [\#191](https://github.com/binance-chain/bsc/pull/191) fix the reorg routine of tx pool stuck issue

## v1.0.7
* [\#120](https://github.com/binance-chain/bsc/pull/120) add health check endpoint
* [\#116](https://github.com/binance-chain/bsc/pull/116) validator only write database state when enough distance 
* [\#115](https://github.com/binance-chain/bsc/pull/115) add batch query methods
* [\#112](https://github.com/binance-chain/bsc/pull/112) apply max commit tx time for miner worker to avoid empty block
* [\#101](https://github.com/binance-chain/bsc/pull/101) apply block number limit for the `eth_getLogs` api
* [\#99](https://github.com/binance-chain/bsc/pull/99) enable directbroadcast flag to decrease the block propagation time
* [\#90](https://github.com/binance-chain/bsc/pull/90) add tini in docker image 
* [\#84](https://github.com/binance-chain/bsc/pull/84) add jq in docker image


## v1.0.6
* [\#68](https://github.com/binance-chain/bsc/pull/68) apply mirror sync upgrade on mainnet

## v1.0.5

SECURITY
* [\#63](https://github.com/binance-chain/bsc/pull/63) security patches from go-ethereum 
* [\#54](https://github.com/binance-chain/bsc/pull/54) les: fix GetProofsV2 that could potentially cause a panic.

FEATURES
* [\#56](https://github.com/binance-chain/bsc/pull/56) apply mirror sync upgrade 
* [\#53](https://github.com/binance-chain/bsc/pull/53) support fork id in header; elegant upgrade

IMPROVEMENT
* [\#61](https://github.com/binance-chain/bsc/pull/61)Add `x-forward-for` log message when handle message failed
* [\#60](https://github.com/binance-chain/bsc/pull/61) add rpc method request gauge

BUGFIX
* [\#59](https://github.com/binance-chain/bsc/pull/59) fix potential deadlock of pub/sub module 



## v1.0.4

IMPROVEMENT
* [\#35](https://github.com/binance-chain/bsc/pull/35) use fixed gas price when network is idle 
* [\#38](https://github.com/binance-chain/bsc/pull/38) disable noisy log from consensus engine 
* [\#47](https://github.com/binance-chain/bsc/pull/47) upgrade to golang1.15.5
* [\#49](https://github.com/binance-chain/bsc/pull/49) Create pull request template for all developer to follow 


## v1.0.3

IMPROVEMENT
* [\#36](https://github.com/binance-chain/bsc/pull/36) add max gas allwance calculation

## v1.0.2

IMPROVEMENT
* [\#29](https://github.com/binance-chain/bsc/pull/29) eth/tracers: revert reason in call_tracer + error for failed internal…

## v1.0.1-beta

IMPROVEMENT
* [\#22](https://github.com/binance-chain/bsc/pull/22) resolve best practice advice 

FEATURES
* [\#23](https://github.com/binance-chain/bsc/pull/23) enforce backoff time for out-turn validator

BUGFIX
* [\#25](https://github.com/binance-chain/bsc/pull/25) minor fix for ramanujan upgrade

UPGRADE
* [\#26](https://github.com/binance-chain/bsc/pull/26) update chapel network config for ramanujan fork

## v1.0.0-beta.0

FEATURES
* [\#5](https://github.com/binance-chain/bsc/pull/5) enable bep2e tokens for faucet
* [\#14](https://github.com/binance-chain/bsc/pull/14) add cross chain contract to system contract
* [\#15](https://github.com/binance-chain/bsc/pull/15) Allow liveness slash fail

IMPROVEMENT
* [\#11](https://github.com/binance-chain/bsc/pull/11) remove redundant gaslimit check 

BUGFIX
* [\#4](https://github.com/binance-chain/bsc/pull/4) fix validator failed to sync a block produced by itself
* [\#6](https://github.com/binance-chain/bsc/pull/6) modify params for Parlia consensus with 21 validators 
* [\#10](https://github.com/binance-chain/bsc/pull/10) add gas limit check in parlia implement
* [\#13](https://github.com/binance-chain/bsc/pull/13) fix debug_traceTransaction crashed issue
