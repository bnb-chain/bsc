# Changelog

## v1.1.17
IMPROVEMENT

* [\#1114](https://github.com/bnb-chain/bsc/pull/1114) typo: .github fix job name
* [\#1126](https://github.com/bnb-chain/bsc/pull/1126) ci: specify bind-tools version
* [\#1140](https://github.com/bnb-chain/bsc/pull/1140) p2p: upstream go-ethereum: use errors.Is for error comparison
* [\#1141](https://github.com/bnb-chain/bsc/pull/1141) all: prefer new(big.Int) over big.NewInt(0)
* [\#1159](https://github.com/bnb-chain/bsc/pull/1159) core: remove redundant func

BUGFIX

* [\#1138](https://github.com/bnb-chain/bsc/pull/1138) fix: upstream patches from go-ethereum 1.10.19
* [\#1139](https://github.com/bnb-chain/bsc/pull/1139) fix: upstream go-ethereum: fix duplicate fields names in the generted go struct
* [\#1145](https://github.com/bnb-chain/bsc/pull/1145) consensus: the newChainHead mights not be imported to Parlia.Snapshot
* [\#1146](https://github.com/bnb-chain/bsc/pull/1146) fix: upstream patches from go-ethereum 1.10.20

## v1.1.16

* [\#1121](https://github.com/bnb-chain/bsc/pull/1121) vm: add two proof verifier to fix the vulnerability in range proof

## v1.1.15
* [\#1109](https://github.com/bnb-chain/bsc/pull/1109) nanofork: block exploitation accounts and suspend cross chain bridge related precompile contracts

## v1.1.14
IMPROVEMENT
* [\#1057](https://github.com/bnb-chain/bsc/pull/1057) ci: allow merge pull request
* [\#1063](https://github.com/bnb-chain/bsc/pull/1063) ci: fix the pattern of commit lint

BUGFIX
* [\#1062](https://github.com/bnb-chain/bsc/pull/1062) test: fix TestOfflineBlockPrune failed randomly
* [\#1076](https://github.com/bnb-chain/bsc/pull/1076) bug: pick some patches from go-ethereum on v1.10.18
* [\#1079](https://github.com/bnb-chain/bsc/pull/1079) core: fix potential goroutine leak

## v1.1.13

FEATURE
* [\#1051](https://github.com/bnb-chain/bsc/pull/1051) Implement BEP153: Native Staking
* [\#1066](https://github.com/bnb-chain/bsc/pull/1066) Upgrade cross chain logic of native staking

IMPROVEMENT
* [\#952](https://github.com/bnb-chain/bsc/pull/952) Improve trie prefetch
* [\#975](https://github.com/bnb-chain/bsc/pull/975) broadcast block before commit block and add metrics
* [\#992](https://github.com/bnb-chain/bsc/pull/992) Pipecommit enable trie prefetcher
* [\#996](https://github.com/bnb-chain/bsc/pull/996) Trie prefetch on state pretch

BUGFIX
* [\#1053](https://github.com/bnb-chain/bsc/pull/1053) state: fix offline tool start failed when start with pruneancient
* [\#1060](https://github.com/bnb-chain/bsc/pull/1060) consensus: fix the GasLimitBoundDivisor
* [\#1061](https://github.com/bnb-chain/bsc/pull/1061) fix: upstream patches from go-ethereum
* [\#1067](https://github.com/bnb-chain/bsc/pull/1067) fix:fix potential goroutine leak
* [\#1068](https://github.com/bnb-chain/bsc/pull/1068) core trie rlp: patches from go-ethereum
* [\#1070](https://github.com/bnb-chain/bsc/pull/1070) txpool: reheap the priced list if london fork not enabled

## v1.1.12

FEATURE
* [\#862](https://github.com/bnb-chain/bsc/pull/862) Pruning AncientDB inline at runtime
* [\#926](https://github.com/bnb-chain/bsc/pull/926) Separate Processing and State Verification on BSC

IMPROVEMENT
* [\#816](https://github.com/bnb-chain/bsc/pull/816) merge go-ethereum v1.10.15
* [\#950](https://github.com/bnb-chain/bsc/pull/950) code optimizations for state prefetcher
* [\#972](https://github.com/bnb-chain/bsc/pull/972) redesign triePrefetcher to make it thread safe
* [\#998](https://github.com/bnb-chain/bsc/pull/998) update dockerfile with a few enhancement
* [\#1015](https://github.com/bnb-chain/bsc/pull/1015) disable noisy logs since system transaction will cause gas capping

BUGFIX
* [\#932](https://github.com/bnb-chain/bsc/pull/932) fix account root was not set correctly when committing mpt during pipeline commit
* [\#953](https://github.com/bnb-chain/bsc/pull/953) correct logic for eip check of NewEVMInterpreter
* [\#958](https://github.com/bnb-chain/bsc/pull/958) define DiscReason as uint8
* [\#959](https://github.com/bnb-chain/bsc/pull/959) update some packages' version
* [\#983](https://github.com/bnb-chain/bsc/pull/983) fix nil pointer issue when stopping mining new block
* [\#1002](https://github.com/bnb-chain/bsc/pull/1002) Fix pipecommit active statedb
* [\#1005](https://github.com/bnb-chain/bsc/pull/1005) freezer batch compatible offline prunblock command
* [\#1007](https://github.com/bnb-chain/bsc/pull/1007) missing contract upgrades and incorrect behavior when miners enable pipecommit
* [\#1009](https://github.com/bnb-chain/bsc/pull/1009) resolve the concurrent cache read and write issue for fast node
* [\#1011](https://github.com/bnb-chain/bsc/pull/1011) Incorrect merkle root issue when enabling pipecommit with miner
* [\#1013](https://github.com/bnb-chain/bsc/pull/1013) tools broken because of writting metadata when open a readyonly db
* [\#1014](https://github.com/bnb-chain/bsc/pull/1014) fast node can not recover from force kill or panic
* [\#1019](https://github.com/bnb-chain/bsc/pull/1019) memory leak issue with diff protocol
* [\#1020](https://github.com/bnb-chain/bsc/pull/1020) remove diffhash patch introduced from separate node
* [\#1024](https://github.com/bnb-chain/bsc/pull/1024) verify node is not treated as verify node

## v1.1.11

UPGRADE
* [\#927](https://github.com/bnb-chain/bsc/pull/927) add readme for validators about how to enter/exit maintenance
* [\#942](https://github.com/bnb-chain/bsc/pull/942) update the blockNumber of Euler Fork upgrade on BSC Mainnet


## v1.1.10

FEATURE
* [\#885](https://github.com/bnb-chain/bsc/pull/885) add Euler Hardfork, including BEP-127 and BEP-131


BUGFIX
* [\#856](https://github.com/bnb-chain/bsc/pull/856) fix logic issue: handlers.removePeer() is called twice
* [\#860](https://github.com/bnb-chain/bsc/pull/860) fix:defer bloomprocessor close
* [\#877](https://github.com/bnb-chain/bsc/pull/877) fix validator pipecommit issue
* [\#870](https://github.com/bnb-chain/bsc/pull/870) fix:Shift panic for zero length of heads

## v1.1.9

IMPROVEMENT
* [\#792](https://github.com/bnb-chain/bsc/pull/792) add shared storage for prefetching state data
* [\#795](https://github.com/bnb-chain/bsc/pull/795) implement state verification pipeline in pipecommit
* [\#803](https://github.com/bnb-chain/bsc/pull/803) prefetch state data during the mining process
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
* [\#668](https://github.com/bnb-chain/bsc/pull/668) implement State Verification && Snapshot Commit pipeline
* [\#581](https://github.com/bnb-chain/bsc/pull/581) implement geth native trace 
* [\#543](https://github.com/bnb-chain/bsc/pull/543) implement offline block prune tools

IMPROVEMENT
* [\#704](https://github.com/bnb-chain/bsc/pull/704) prefetch state by applying the transactions within one block 
* [\#713](https://github.com/bnb-chain/bsc/pull/713) add ARM binaries for release pipeline

BUGFIX
* [\#667](https://github.com/bnb-chain/bsc/pull/667) trie: reject deletions when verifying range proofs #667
* [\#643](https://github.com/bnb-chain/bsc/pull/643) add timeout for stopping p2p server to fix can not gracefully shutdown issue
* [\#740](https://github.com/bnb-chain/bsc/pull/740) update discord link which won't expire 

## v1.1.7

BUGFIX
* [\#628](https://github.com/bnb-chain/bsc/pull/628) fix state inconsistent when doing diffsync

## v1.1.6
BUGFIX
* [\#582](https://github.com/bnb-chain/bsc/pull/582) the DoS vulnerabilities fixed in go-ethereum v1.10.9

IMPROVEMENT
* [\#578](https://github.com/bnb-chain/bsc/pull/578) reduce memory allocation and upgrade snappy version

FEATURES
* [\#570](https://github.com/bnb-chain/bsc/pull/570) reannounce local pending transactions

## v1.1.5
BUGFIX
* [\#509](https://github.com/bnb-chain/bsc/pull/509) fix graceful shutdown bug

IMPROVEMENT
* [\#536](https://github.com/bnb-chain/bsc/pull/536) get diff accounts by replaying block when diff layer not found
* [\#527](https://github.com/bnb-chain/bsc/pull/527) improve diffsync protocol in many aspects
* [\#493](https://github.com/bnb-chain/bsc/pull/493) implement fast getDiffAccountsWithScope API

## v1.1.4
Improvement
* [\#472](https://github.com/bnb-chain/bsc/pull/472) add metrics for contract code bitmap cache
* [\#473](https://github.com/bnb-chain/bsc/pull/473) fix ci test flow

BUGFIX
* [\#491](https://github.com/bnb-chain/bsc/pull/491) fix prefetcher related bugs

FEATURES
* [\#480](https://github.com/bnb-chain/bsc/pull/480) implement bep 95


## v1.1.3
Improvement
* [\#456](https://github.com/bnb-chain/bsc/pull/456) git-flow support lint, unit test, and integration test
* [\#449](https://github.com/bnb-chain/bsc/pull/449) cache bitmap and change the cache type of GetCode
* [\#454](https://github.com/bnb-chain/bsc/pull/454) fix cache key do not have hash func
* [\#446](https://github.com/bnb-chain/bsc/pull/446) parallel bloom calculation
* [\#442](https://github.com/bnb-chain/bsc/pull/442) ignore empty tx in GetDiffAccountsWithScope 
* [\#426](https://github.com/bnb-chain/bsc/pull/426) add block proccess backoff time when validator is not in turn and received in turn block
* [\#398](https://github.com/bnb-chain/bsc/pull/398) ci pipeline for release page


BUGFIX
* [\#446](https://github.com/bnb-chain/bsc/pull/446) fix concurrent write of subfetcher
* [\#444](https://github.com/bnb-chain/bsc/pull/444) fix blockhash not correct for the logs of system tx receipt
* [\#409](https://github.com/bnb-chain/bsc/pull/409) fix nil point in downloader
* [\#408](https://github.com/bnb-chain/bsc/pull/408) core/state/snapshot: fix typo


FEATURES
* [\#431](https://github.com/bnb-chain/bsc/pull/431) Export get diff accounts in block api 
* [\#412](https://github.com/bnb-chain/bsc/pull/412) add extension in eth protocol handshake to disable tx broadcast
* [\#376](https://github.com/bnb-chain/bsc/pull/376) implement diff sync

## v1.1.2
Security
* [\#379](https://github.com/bnb-chain/bsc/pull/379) A pre-announced hotfix release to patch a vulnerability in the EVM (CVE-2021-39137).


## v1.1.1
IMPROVEMENT
* [\#355](https://github.com/bnb-chain/bsc/pull/355) miner should propose block on a proper fork

BUGFIX
* [\#350](https://github.com/bnb-chain/bsc/pull/350) flag: fix TriesInmemory specified but not work
* [\#358](https://github.com/bnb-chain/bsc/pull/358) miner: fix null pending block
* [\#360](https://github.com/bnb-chain/bsc/pull/360) pruner: fix state bloom sync permission in Windows 
* [\#366](https://github.com/bnb-chain/bsc/pull/366) fix double close channel of subfetcher


## v1.1.1-beta
* [\#333](https://github.com/bnb-chain/bsc/pull/333) improve block fetcher efficiency
* [\#326](https://github.com/bnb-chain/bsc/pull/326) eth/tracers: improve tracing performance
* [\#257](https://github.com/bnb-chain/bsc/pull/257) performance improvement in many aspects


## v1.1.0
* [\#282](https://github.com/bnb-chain/bsc/pull/282) update discord link
* [\#280](https://github.com/bnb-chain/bsc/pull/280) update discord link
* [\#227](https://github.com/bnb-chain/bsc/pull/227) use more aggressive write cache policy

## v1.1.0-beta
* [\#152](https://github.com/bnb-chain/bsc/pull/152) upgrade to go-ethereum 1.10.3

## v1.0.7-hf.2
BUGFIX
* [\#194](https://github.com/bnb-chain/bsc/pull/194) bump btcd to v0.20.1-beta

## v1.0.7-hf.1
BUGFIX
* [\#190](https://github.com/bnb-chain/bsc/pull/190) fix disk increase dramaticly
* [\#191](https://github.com/bnb-chain/bsc/pull/191) fix the reorg routine of tx pool stuck issue

## v1.0.7
* [\#120](https://github.com/bnb-chain/bsc/pull/120) add health check endpoint
* [\#116](https://github.com/bnb-chain/bsc/pull/116) validator only write database state when enough distance 
* [\#115](https://github.com/bnb-chain/bsc/pull/115) add batch query methods
* [\#112](https://github.com/bnb-chain/bsc/pull/112) apply max commit tx time for miner worker to avoid empty block
* [\#101](https://github.com/bnb-chain/bsc/pull/101) apply block number limit for the `eth_getLogs` api
* [\#99](https://github.com/bnb-chain/bsc/pull/99) enable directbroadcast flag to decrease the block propagation time
* [\#90](https://github.com/bnb-chain/bsc/pull/90) add tini in docker image 
* [\#84](https://github.com/bnb-chain/bsc/pull/84) add jq in docker image


## v1.0.6
* [\#68](https://github.com/bnb-chain/bsc/pull/68) apply mirror sync upgrade on mainnet

## v1.0.5

SECURITY
* [\#63](https://github.com/bnb-chain/bsc/pull/63) security patches from go-ethereum 
* [\#54](https://github.com/bnb-chain/bsc/pull/54) les: fix GetProofsV2 that could potentially cause a panic.

FEATURES
* [\#56](https://github.com/bnb-chain/bsc/pull/56) apply mirror sync upgrade 
* [\#53](https://github.com/bnb-chain/bsc/pull/53) support fork id in header; elegant upgrade

IMPROVEMENT
* [\#61](https://github.com/bnb-chain/bsc/pull/61)Add `x-forward-for` log message when handle message failed
* [\#60](https://github.com/bnb-chain/bsc/pull/61) add rpc method request gauge

BUGFIX
* [\#59](https://github.com/bnb-chain/bsc/pull/59) fix potential deadlock of pub/sub module 



## v1.0.4

IMPROVEMENT
* [\#35](https://github.com/bnb-chain/bsc/pull/35) use fixed gas price when network is idle 
* [\#38](https://github.com/bnb-chain/bsc/pull/38) disable noisy log from consensus engine 
* [\#47](https://github.com/bnb-chain/bsc/pull/47) upgrade to golang1.15.5
* [\#49](https://github.com/bnb-chain/bsc/pull/49) Create pull request template for all developer to follow 


## v1.0.3

IMPROVEMENT
* [\#36](https://github.com/bnb-chain/bsc/pull/36) add max gas allwance calculation

## v1.0.2

IMPROVEMENT
* [\#29](https://github.com/bnb-chain/bsc/pull/29) eth/tracers: revert reason in call_tracer + error for failed internal…

## v1.0.1-beta

IMPROVEMENT
* [\#22](https://github.com/bnb-chain/bsc/pull/22) resolve best practice advice 

FEATURES
* [\#23](https://github.com/bnb-chain/bsc/pull/23) enforce backoff time for out-turn validator

BUGFIX
* [\#25](https://github.com/bnb-chain/bsc/pull/25) minor fix for ramanujan upgrade

UPGRADE
* [\#26](https://github.com/bnb-chain/bsc/pull/26) update chapel network config for ramanujan fork

## v1.0.0-beta.0

FEATURES
* [\#5](https://github.com/bnb-chain/bsc/pull/5) enable bep2e tokens for faucet
* [\#14](https://github.com/bnb-chain/bsc/pull/14) add cross chain contract to system contract
* [\#15](https://github.com/bnb-chain/bsc/pull/15) Allow liveness slash fail

IMPROVEMENT
* [\#11](https://github.com/bnb-chain/bsc/pull/11) remove redundant gaslimit check 

BUGFIX
* [\#4](https://github.com/bnb-chain/bsc/pull/4) fix validator failed to sync a block produced by itself
* [\#6](https://github.com/bnb-chain/bsc/pull/6) modify params for Parlia consensus with 21 validators 
* [\#10](https://github.com/bnb-chain/bsc/pull/10) add gas limit check in parlia implement
* [\#13](https://github.com/bnb-chain/bsc/pull/13) fix debug_traceTransaction crashed issue
