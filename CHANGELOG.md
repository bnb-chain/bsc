# Changelog

## v1.1.9

IMPROVEMENT
* [\#792](https://github.com/binance-chain/bsc/pull/792) add shared storage for prefetching state data
* [\#795](https://github.com/binance-chain/bsc/pull/795) implement state verification pipeline in pipecommit
* [\#803](https://github.com/binance-chain/bsc/pull/803) prefetch state data during the mining process
* [\#812](https://github.com/bnb-chain/bsc/pull/812) skip verification on account storage root to tolerate with fastnode when doing diffsync
* [\#818](https://github.com/bnb-chain/bsc/pull/818) add shared storage to the prefetcher of miner
* [\#820](https://github.com/bnb-chain/bsc/pull/820) disable diffsync when pipecommit is enabled
* [\#830](https://github.com/bnb-chain/bsc/pull/830) change the number of prefetch threads

BUGFIX
* [\#797](https://github.com/bnb-chain/bsc/pull/797) fix race condition on preimage in pipecommit
* [\#808](https://github.com/bnb-chain/bsc/pull/808) fix code of difflayer not assign when new smart contract created
* [\#817](https://github.com/bnb-chain/bsc/pull/817) fix bugs of prune block tool
* [\#834](https://github.com/bnb-chain/bsc/pull/834) fix deadlock when failed to verify state root in pipecommit
* [\#835](https://github.com/bnb-chain/bsc/pull/835) fix deadlock on miner module when failed to commit trie
* [\#842](https://github.com/bnb-chain/bsc/pull/842) fix invalid nil check of statedb in diffsync

## v1.1.8
FEATURES
* [\#668](https://github.com/binance-chain/bsc/pull/668) implement State Verification && Snapshot Commit pipeline
* [\#581](https://github.com/binance-chain/bsc/pull/581) implement geth native trace 
* [\#543](https://github.com/binance-chain/bsc/pull/543) implement offline block prune tools

IMPROVEMENT
* [\#704](https://github.com/binance-chain/bsc/pull/704) prefetch state by applying the transactions within one block 
* [\#713](https://github.com/binance-chain/bsc/pull/713) add ARM binaries for release pipeline

BUGFIX
* [\#667](https://github.com/binance-chain/bsc/pull/667) trie: reject deletions when verifying range proofs #667
* [\#643](https://github.com/binance-chain/bsc/pull/643) add timeout for stopping p2p server to fix can not gracefully shutdown issue
* [\#740](https://github.com/binance-chain/bsc/pull/740) update discord link which won't expire 

## v1.1.7

BUGFIX
* [\#628](https://github.com/binance-chain/bsc/pull/628) fix state inconsistent when doing diffsync

## v1.1.6
BUGFIX
* [\#582](https://github.com/binance-chain/bsc/pull/582) the DoS vulnerabilities fixed in go-ethereum v1.10.9

IMPROVEMENT
* [\#578](https://github.com/binance-chain/bsc/pull/578) reduce memory allocation and upgrade snappy version

FEATURES
* [\#570](https://github.com/binance-chain/bsc/pull/570) reannounce local pending transactions

## v1.1.5
BUGFIX
* [\#509](https://github.com/binance-chain/bsc/pull/509) fix graceful shutdown bug

IMPROVEMENT
* [\#536](https://github.com/binance-chain/bsc/pull/536) get diff accounts by replaying block when diff layer not found
* [\#527](https://github.com/binance-chain/bsc/pull/527) improve diffsync protocol in many aspects
* [\#493](https://github.com/binance-chain/bsc/pull/493) implement fast getDiffAccountsWithScope API

## v1.1.4
Improvement
* [\#472](https://github.com/binance-chain/bsc/pull/472) add metrics for contract code bitmap cache
* [\#473](https://github.com/binance-chain/bsc/pull/473) fix ci test flow

BUGFIX
* [\#491](https://github.com/binance-chain/bsc/pull/491) fix prefetcher related bugs

FEATURES
* [\#480](https://github.com/binance-chain/bsc/pull/480) implement bep 95


## v1.1.3
Improvement
* [\#456](https://github.com/binance-chain/bsc/pull/456) git-flow support lint, unit test, and integration test
* [\#449](https://github.com/binance-chain/bsc/pull/449) cache bitmap and change the cache type of GetCode
* [\#454](https://github.com/binance-chain/bsc/pull/454) fix cache key do not have hash func
* [\#446](https://github.com/binance-chain/bsc/pull/446) parallel bloom calculation
* [\#442](https://github.com/binance-chain/bsc/pull/442) ignore empty tx in GetDiffAccountsWithScope 
* [\#426](https://github.com/binance-chain/bsc/pull/426) add block proccess backoff time when validator is not in turn and received in turn block
* [\#398](https://github.com/binance-chain/bsc/pull/398) ci pipeline for release page


BUGFIX
* [\#446](https://github.com/binance-chain/bsc/pull/446) fix concurrent write of subfetcher
* [\#444](https://github.com/binance-chain/bsc/pull/444) fix blockhash not correct for the logs of system tx receipt
* [\#409](https://github.com/binance-chain/bsc/pull/409) fix nil point in downloader
* [\#408](https://github.com/binance-chain/bsc/pull/408) core/state/snapshot: fix typo


FEATURES
* [\#431](https://github.com/binance-chain/bsc/pull/431) Export get diff accounts in block api 
* [\#412](https://github.com/binance-chain/bsc/pull/412) add extension in eth protocol handshake to disable tx broadcast
* [\#376](https://github.com/binance-chain/bsc/pull/376) implement diff sync

## v1.1.2
Security
* [\#379](https://github.com/binance-chain/bsc/pull/379) A pre-announced hotfix release to patch a vulnerability in the EVM (CVE-2021-39137).


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
