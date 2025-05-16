# Changelog
## v1.5.13
### FEATURE
[\#3019](https://github.com/bnb-chain/bsc/pull/3019) BEP-524: Short Block Interval Phase Two: 0.75 seconds
[\#3044](https://github.com/bnb-chain/bsc/pull/3044) params: double FullImmutabilityThreshold for BEP-520 & BEP-524
[\#3045](https://github.com/bnb-chain/bsc/pull/3045) Feature: StakeHub Contract Interface Implementation
[\#3040](https://github.com/bnb-chain/bsc/pull/3040) bsc: add new block fetching mechanism
[\#3043](https://github.com/bnb-chain/bsc/pull/3043) p2p: support new msg broadcast features
[\#3070](https://github.com/bnb-chain/bsc/pull/3070) chore: renaming for evn and some optmization
[\#3073](https://github.com/bnb-chain/bsc/pull/3073) evn: add support for node id removal
[\#3072](https://github.com/bnb-chain/bsc/pull/3072) config: support more evn configuration in tool
[\#3075](https://github.com/bnb-chain/bsc/pull/3075) config: apply two default miner option
[\#3083](https://github.com/bnb-chain/bsc/pull/3083) evn: improve node ID management with better error handling
[\#3084](https://github.com/bnb-chain/bsc/pull/3084) metrics: add more monitor metrics for EVN
[\#3087](https://github.com/bnb-chain/bsc/pull/3087) bsc2: fix block sidecar fetching issue
[\#3090](https://github.com/bnb-chain/bsc/pull/3090) chore: update maxwell contrats addresses
[\#3091](https://github.com/bnb-chain/bsc/pull/3091) chore: fix several occasional issues for EVN
[\#3049](https://github.com/bnb-chain/bsc/pull/3049) upstream: pick bugfix and feature from latest geth v1.5.9
[\#3096](https://github.com/bnb-chain/bsc/pull/3096) config: update BSC Testnet hardfork time: Maxwell

### BUGFIX
[\#3050](https://github.com/bnb-chain/bsc/pull/3050) miner: fix memory leak caused by no discard env
[\#3061](https://github.com/bnb-chain/bsc/pull/3061) p2p: fix bscExt checking logic

### IMPROVEMENT
[\#3034](https://github.com/bnb-chain/bsc/pull/3034) miner: optimize clear up logic for envs
[\#3039](https://github.com/bnb-chain/bsc/pull/3039) Revert "miner: limit block size to eth protocol msg size (#2696)"
[\#3041](https://github.com/bnb-chain/bsc/pull/3041) feat: add json-rpc-api.md
[\#3057](https://github.com/bnb-chain/bsc/pull/3057) eth/protocols/bsc: adjust vote reception limit
[\#3067](https://github.com/bnb-chain/bsc/pull/3067) ethclient/gethclient: add deduplication and max keys limit to GetProof
[\#3063](https://github.com/bnb-chain/bsc/pull/3063) rpc: add method name length limit and configurable message size limit
[\#3077](https://github.com/bnb-chain/bsc/pull/3077) performance: track large tx execution cost
[\#3074](https://github.com/bnb-chain/bsc/pull/3074) jsutils: add tool GetLargeTxs
[\#3082](https://github.com/bnb-chain/bsc/pull/3082) metrics: optimize mev metrics
[\#3081](https://github.com/bnb-chain/bsc/pull/3081) miner: reset recommit timer on new block
[\#3062](https://github.com/bnb-chain/bsc/pull/3062) refactor: use slices.Contains to simplify code
[\#3088](https://github.com/bnb-chain/bsc/pull/3088) core/vote: change waiting blocks for voting since start mining
[\#3089](https://github.com/bnb-chain/bsc/pull/3089) core/systemcontracts: remove lorentz/rialto
[\#3085](https://github.com/bnb-chain/bsc/pull/0000) miner: fix goroutine leak

## v1.5.12
### BUGFIX
[\#3057](https://github.com/bnb-chain/bsc/pull/3057) eth/protocols/bsc: adjust vote reception limit

## v1.5.11
### FEATURE
[\#3008](https://github.com/bnb-chain/bsc/pull/3008) params: add MaxwellTime
[\#3025](https://github.com/bnb-chain/bsc/pull/3025) config.toml: default value of [Eth.Miner] and [Eth.Miner.Mev]
[\#3026](https://github.com/bnb-chain/bsc/pull/3026) mev: include MaxBidsPerBuilder in MevParams
[\#3027](https://github.com/bnb-chain/bsc/pull/3027) mev: update two default mev paramater for 1.5s block interval

### BUGFIX
[\#3007](https://github.com/bnb-chain/bsc/pull/3007) metrics: fix panic for cocurrently accessing label

### IMPROVEMENT
[\#3006](https://github.com/bnb-chain/bsc/pull/3006) jsutil: update getKeyParameters
[\#3018](https://github.com/bnb-chain/bsc/pull/3018) nancy: update nancy ignore
[\#3021](https://github.com/bnb-chain/bsc/pull/3021) chore: remove duplicate package imports

## v1.5.10
### FEATURE
[\#3015](https://github.com/bnb-chain/bsc/pull/3015) config: update BSC Mainnet hardfork time: Lorentz

### BUGFIX
NA

### IMPROVEMENT
[\#2985](https://github.com/bnb-chain/bsc/pull/2985) core: clearup pascal&prague testflag and rialto code

## v1.5.9
### FEATURE
[\#2932](https://github.com/bnb-chain/bsc/pull/2932) BEP-520: Short Block Interval Phase One: 1.5 seconds
[\#2991](https://github.com/bnb-chain/bsc/pull/2991) config: update BSC Testnet hardfork time: Lorentz

### BUGFIX
[\#2990](https://github.com/bnb-chain/bsc/pull/2990) core/state: fix concurrent map read and write for stateUpdate.accounts

### IMPROVEMENT
[\#2933](https://github.com/bnb-chain/bsc/pull/2933) metrics: add more peer, block/vote metrics
[\#2938](https://github.com/bnb-chain/bsc/pull/2938) cmd/geth: add example for geth bls account generate-proof
[\#2949](https://github.com/bnb-chain/bsc/pull/2949) metrics: add more block/vote stats;
[\#2948](https://github.com/bnb-chain/bsc/pull/2948) go.mod: update crypto to solve CVE-2025-22869
[\#2960](https://github.com/bnb-chain/bsc/pull/2960) pool: debug log instead of warn
[\#2961](https://github.com/bnb-chain/bsc/pull/2961) metric: add more block monitor metrics;
[\#2992](https://github.com/bnb-chain/bsc/pull/2992) core/systemcontracts: update url for lorentz hardfork
[\#2993](https://github.com/bnb-chain/bsc/pull/2993) cmd/jsutils: add tool GetMevStatus

## v1.5.8
### FEATURE
* [\#2955](https://github.com/bnb-chain/bsc/pull/2955) pbs: enable GreedyMergeTx by default

### BUGFIX
* [\#2967](https://github.com/bnb-chain/bsc/pull/2967) fix: gas compare in bid simulator

### IMPROVEMENT
* [\#2951](https://github.com/bnb-chain/bsc/pull/2951) bump golang.org/x/net from 0.34.0 to 0.36.0
* [\#0000](https://github.com/bnb-chain/bsc/pull/0000) golang: upgrade toolchain to v1.23.0 (commit:3be156eec)
* [\#2957](https://github.com/bnb-chain/bsc/pull/2957) miner: stop GreedyMergeTx before worker picking bids
* [\#2959](https://github.com/bnb-chain/bsc/pull/2959) pbs: fix a inaccurate bid result log
* [\#2971](https://github.com/bnb-chain/bsc/pull/2971) mev: no interrupt if it is too later
* [\#2974](https://github.com/bnb-chain/bsc/pull/2974) miner: add metrics for bid simulation

## v1.5.7
v1.5.7 conduct small upstream code merge to follow the latest pectra hard fork and apply some bug fix. There are two PR for the code merge:
* [\#2897](https://github.com/bnb-chain/bsc/pull/2897) upstream: merge tag 'geth-v1.15.1' into bsc-develop
* [\#2926](https://github.com/bnb-chain/bsc/pull/2926) upstream: pick bug fix from latest geth

Besides code merge, there are also several important bugfix/improvements, and setup mainnet Pascal hard fork time:
### FEATURE
* [\#2928](https://github.com/bnb-chain/bsc/pull/2928) config: update BSC Mainnet hardfork date: Pascal & Praque

### BUGFIX
* [\#2907](https://github.com/bnb-chain/bsc/pull/0000) go.mod: downgrade bls-eth-go-binary to make it same as the prysm-v5.0.0

### IMPROVEMENT
* [\#2896](https://github.com/bnb-chain/bsc/pull/2896) consensus/parlia: estimate gas reserved for systemTxs
* [\#2912](https://github.com/bnb-chain/bsc/pull/2912) consensus/parlia: improve performance of func IsSystemTransaction
* [\#2916](https://github.com/bnb-chain/bsc/pull/2916) miner: avoid to collect requests when getting pending blocks
* [\#2913](https://github.com/bnb-chain/bsc/pull/2913) core/vm: add basic test cases for blsSignatureVerify
* [\#2918](https://github.com/bnb-chain/bsc/pull/2918) core/txpool/legacypool/legacypool.go: add gasTip check when reset

## v1.5.6
v1.5.6 performed another small code sync with Geth upstream, mainly sync the 7702 tx type txpool update, refer: https://github.com/bnb-chain/bsc/pull/2888/

And it also setup the Testnet Pascal hard fork date.

## v1.5.5
v1.5.5 mainly did a upstream code sync, it catches up to Geth of date around 5th-Feb-2025 to sync the latest Praque hard fork changes, see: https://github.com/bnb-chain/bsc/pull/2856

Besides the code sync, there are a few improvement PRs of BSC:
### IMPROVEMENT
* [\#2846](https://github.com/bnb-chain/bsc/pull/2846) tracing: pass *tracing.Hooks around instead of vm.Config
* [\#2850](https://github.com/bnb-chain/bsc/pull/2850) metric: revert default expensive metrics in blockchain
* [\#2851](https://github.com/bnb-chain/bsc/pull/2851) eth/tracers: fix call nil OnSystemTxFixIntrinsicGas
* [\#2862](https://github.com/bnb-chain/bsc/pull/2862) cmd/jsutils/getchainstatus.js: add GetEip7623
* [\#2867](https://github.com/bnb-chain/bsc/pull/2867) p2p: lowered log lvl for failed enr request

## v1.5.4
### BUGFIX
* [\#2874](https://github.com/bnb-chain/bsc/pull/2874) crypto: add IsOnCurve check


## v1.5.3
### BUGFIX
* [\#2827](https://github.com/bnb-chain/bsc/pull/2827) triedb/pathdb: fix nil field for stateSet
* [\#2830](https://github.com/bnb-chain/bsc/pull/2830) fastnode: fix some pbss saving&rewind issues
* [\#2835](https://github.com/bnb-chain/bsc/pull/2835) dep: fix nancy issues
* [\#2836](https://github.com/bnb-chain/bsc/pull/2836) Revert "internal/ethapi: remove td field from block (#30386)"

### FEATURE
NA

### IMPROVEMENT
* [\#2834](https://github.com/bnb-chain/bsc/pull/2834) eth: make transaction acceptance depends on syncing status
* [\#2844](https://github.com/bnb-chain/bsc/pull/2844) internal/ethapi: support GetFinalizedBlock by common ratio validators
* [\#2772](https://github.com/bnb-chain/bsc/pull/2772) Push tracing of Parlia system transactions so that live tracers can properly traces those state changes
* [\#2845](https://github.com/bnb-chain/bsc/pull/2845) feat: wait miner finish the later multi-proposals when restarting the node

## v1.5.2
v1.5.2-alpha is another release for upstream code sync, it catches up with [go-ethereum release [v1.14.12]](https://github.com/ethereum/go-ethereum/releases/tag/v1.14.12) and supported 4 BEPs for BSC Pascal hard fork.
- BEP-439: Implement EIP-2537: Precompile for BLS12-381 curve operations
- BEP-440: Implement EIP-2935: Serve historical block hashes from state
- BEP-441: Implement EIP-7702: Set EOA account code
- BEP-466: Make the block format compatible with EIP-7685

#### Code Sync
- [upstream: merge geth v1.14.12](https://github.com/bnb-chain/bsc/pull/2791)
- [upstream: merge geth date 1210](https://github.com/bnb-chain/bsc/pull/2799)
- [upstream: merge geth date 1213](https://github.com/bnb-chain/bsc/pull/2806)

#### Pascal BEPs
- [BEP-441: Implement EIP-7702: Set EOA account code](https://github.com/bnb-chain/bsc/pull/2807)
- [BEP-466: Make the block header format compatible with EIP-7685](https://github.com/bnb-chain/bsc/pull/2777)
- Note: BEP-439 and BEP-440 have already been implemented in previous v1.5.1-alpha release

#### Others
- [eth/fetcher: remove light mode in block fetcher](https://github.com/bnb-chain/bsc/pull/2804)
- [fix: Opt pruneancient issues](https://github.com/bnb-chain/bsc/pull/2800)
- [build(deps): bump github.com/golang-jwt/jwt/v4 from 4.5.0 to 4.5.1](https://github.com/bnb-chain/bsc/pull/2788)
- [build(deps): bump golang.org/x/crypto from 0.22.0 to 0.31.0](https://github.com/bnb-chain/bsc/pull/2797)
- [misc: mini fix and clearup](https://github.com/bnb-chain/bsc/pull/2811)

## v1.5.1
v1.5.1-alpha is for upstream code sync, it catches up with[go-ethereum release [v1.13.15, v1.14.11]](https://github.com/ethereum/go-ethereum/releases)

#### Major Changes
[eth: make transaction propagation paths in the network deterministic (#29034)](https://github.com/ethereum/go-ethereum/pull/29034)
[core/state: parallelise parts of state commit (#29681)](https://github.com/ethereum/go-ethereum/pull/29681)
Load trie nodes concurrently with trie updates, speeding up block import by 5-7% ([#29519](https://github.com/ethereum/go-ethereum/pull/29519), [#29768](https://github.com/ethereum/go-ethereum/pull/29768), [#29919](https://github.com/ethereum/go-ethereum/pull/29919)).
[core/vm: reject contract creation if the storage is non-empty (](https://github.com/ethereum/go-ethereum/commit/c170cc0ab0a1f60adcde80d0af8e3050ee19da93)[#28912](https://github.com/ethereum/go-ethereum/pull/28912)[)](https://github.com/ethereum/go-ethereum/commit/c170cc0ab0a1f60adcde80d0af8e3050ee19da93)
[Add state reader abstraction (#29761)](https://github.com/ethereum/go-ethereum/pull/29761)
[Stateless witness prefetcher changes (#29519)](https://github.com/ethereum/go-ethereum/pull/29519)
_not follow changes with trie_prefetcher, the implemenation in bsc is very different_
[core: use finalized block as the chain freeze indicator (](https://github.com/ethereum/go-ethereum/commit/ca473b81cbe4a96cde4e8424c49b15ab304787bb)[#28683](https://github.com/ethereum/go-ethereum/pull/28683)[)](https://github.com/ethereum/go-ethereum/commit/ca473b81cbe4a96cde4e8424c49b15ab304787bb)
_in bsc, this feature only enabled with multi-database_

#### New EIPs
[core/vm: enable bls-precompiles for Prague (](https://github.com/ethereum/go-ethereum/commit/823719b9e1b72174cd8245ae9e6f6f7d7072a8d6)[#29552](https://github.com/ethereum/go-ethereum/pull/29552)[)](https://github.com/ethereum/go-ethereum/commit/823719b9e1b72174cd8245ae9e6f6f7d7072a8d6)
[EIP-2935: Serve historical block hashes from state](https://eips.ethereum.org/EIPS/eip-2935) ([#29465](https://github.com/ethereum/go-ethereum/pull/29465))

#### Clear Up
[eth, eth/downloader: remove references to LightChain, LightSync (#29711)](https://github.com/ethereum/go-ethereum/pull/29711)
[eth/filters: remove support for pending logs(#29574)](https://github.com/ethereum/go-ethereum/pull/29574)
[Drop large-contract (500MB+) deletion DoS protection from pathdb post Cancun (#28940)](https://github.com/ethereum/go-ethereum/pull/28940).
Remove totalDifficulty field from RPC, in accordance with [spec update](https://github.com/ethereum/execution-apis/pull/570), [#30386](https://github.com/ethereum/go-ethereum/pull/30386)

#### Merged but Reverted
[consensus, cmd, core, eth: remove support for non-merge mode of operation (](https://github.com/ethereum/go-ethereum/commit/f4d53133f6e4b13f0dbcfef3bc45e9650d863b73)[#29169](https://github.com/ethereum/go-ethereum/pull/29169)[)](https://github.com/ethereum/go-ethereum/commit/f4d53133f6e4b13f0dbcfef3bc45e9650d863b73)
[miner: refactor the miner, make the pending block on demand (](https://github.com/ethereum/go-ethereum/commit/d8e0807da22eb922539d15b0d5d01ccdd58b1267)[#28623](https://github.com/ethereum/go-ethereum/pull/28623)[)](https://github.com/ethereum/go-ethereum/commit/d8e0807da22eb922539d15b0d5d01ccdd58b1267)
[miner: lower default min miner tip from 1 gwei to 0.001 gwei(#29895)](https://github.com/ethereum/go-ethereum/pull/29895)
_bsc only has tip, 1 Gwei is the min value now_
[eth/downloader: purge pre-merge sync code (](https://github.com/ethereum/go-ethereum/commit/45baf21111c03d2954c81fdf828e630a8d7b05c1)[#29281](https://github.com/ethereum/go-ethereum/pull/29281)[)](https://github.com/ethereum/go-ethereum/commit/45baf21111c03d2954c81fdf828e630a8d7b05c1)
[all: remove forkchoicer and reorgNeeded (](https://github.com/ethereum/go-ethereum/commit/b0b67be0a2559073c1620555d2b6a73f825f7135)[#29179](https://github.com/ethereum/go-ethereum/pull/29179)[)](https://github.com/ethereum/go-ethereum/commit/b0b67be0a2559073c1620555d2b6a73f825f7135)

#### Others
[all: update to go version 1.23.0 (#30323)](https://github.com/ethereum/go-ethereum/pull/30323)
[Switch to using Go's native log/slog package instead of golang/exp (#29302)](https://github.com/ethereum/go-ethereum/pull/29302).
[Add the geth db inspect-history command to inspect pathdb state history (#29267)](https://github.com/ethereum/go-ethereum/pull/29267).
Improve the discovery protocol's node revalidation ([#29572](https://github.com/ethereum/go-ethereum/pull/29572), [#29864](https://github.com/ethereum/go-ethereum/pull/29864), [#29836](https://github.com/ethereum/go-ethereum/pull/29836)).
[Blobpool related flags in Geth now actually work. (#30203)](https://github.com/ethereum/go-ethereum/pull/30203)
[core/rawdb: implement in-memory freezer (#29135)](https://github.com/ethereum/go-ethereum/commit/f46c878441e2e567e8815f1e252a38ad0ffafbc2)


## v1.5.0
v1.5.0 was skipped as we forgot to pump the version when create the tag, it is replaced by v1.5.1

## v1.4.16
### BUGFIX
* [\#2736](https://github.com/bnb-chain/bsc/pull/2736) ethclient: move TransactionOpts to avoid import internal package;
* [\#2755](https://github.com/bnb-chain/bsc/pull/2755) fix: fix multi-db env
* [\#2759](https://github.com/bnb-chain/bsc/pull/2759) fix: add blobSidecars in db inspect
* [\#2764](https://github.com/bnb-chain/bsc/pull/2764) fix: add blobSidecars in db inspect

### FEATURE
* [\#2692](https://github.com/bnb-chain/bsc/pull/2692) feat: add pascal hardfork
* [\#2718](https://github.com/bnb-chain/bsc/pull/2718) feat: add Prague hardfork
* [\#2734](https://github.com/bnb-chain/bsc/pull/2734) feat: update system contract bytecodes of pascal hardfork
* [\#2737](https://github.com/bnb-chain/bsc/pull/2737) feat: modify LOCK_PERIOD_FOR_TOKEN_RECOVER to 300 seconds on BSC Testnet in pascal hardfork
* [\#2660](https://github.com/bnb-chain/bsc/pull/2660) core/txpool/legacypool: add overflowpool for txs
* [\#2754](https://github.com/bnb-chain/bsc/pull/2754) core/txpool: improve Add() logic, handle edge case

### IMPROVEMENT
* [\#2727](https://github.com/bnb-chain/bsc/pull/2727) core: clearup testflag for Bohr
* [\#2716](https://github.com/bnb-chain/bsc/pull/2716) minor Update group_prover.sage
* [\#2735](https://github.com/bnb-chain/bsc/pull/2735) concensus/parlia.go: make distribute incoming tx more independence
* [\#2742](https://github.com/bnb-chain/bsc/pull/2742) feat: remove pipecommit
* [\#2748](https://github.com/bnb-chain/bsc/pull/2748) jsutil: put all js utils in one file
* [\#2749](https://github.com/bnb-chain/bsc/pull/2749) jsutils: add tool GetKeyParameters
* [\#2756](https://github.com/bnb-chain/bsc/pull/2756) nancy: ignore github.com/golang-jwt/jwt/v4 4.5.0 in .nancy-ignore
* [\#2757](https://github.com/bnb-chain/bsc/pull/2757) util: python script to get stats of reorg
* [\#2758](https://github.com/bnb-chain/bsc/pull/2758) utils: print monikey for reorg script
* [\#2714](https://github.com/bnb-chain/bsc/pull/2714) refactor: Directly swap two variables to optimize code

## v1.4.15
### BUGFIX
* [\#2680](https://github.com/bnb-chain/bsc/pull/2680) txpool: apply miner's gasceil to txpool
* [\#2688](https://github.com/bnb-chain/bsc/pull/2688) txpool: set default GasCeil from 30M to 0
* [\#2696](https://github.com/bnb-chain/bsc/pull/2696) miner: limit block size to eth protocol msg size
* [\#2684](https://github.com/bnb-chain/bsc/pull/2684) eth: Add sidecars when available to broadcasted current block

### FEATURE
* [\#2672](https://github.com/bnb-chain/bsc/pull/2672) faucet: with mainnet balance check, 0.002BNB at least
* [\#2678](https://github.com/bnb-chain/bsc/pull/2678) beaconserver: simulated beacon api server for op-stack
* [\#2687](https://github.com/bnb-chain/bsc/pull/2687) faucet: support customized token
* [\#2698](https://github.com/bnb-chain/bsc/pull/2698) faucet: add example for custimized token
* [\#2706](https://github.com/bnb-chain/bsc/pull/2706) faucet: update DIN token faucet support

### IMPROVEMENT
* [\#2677](https://github.com/bnb-chain/bsc/pull/2677) log: add some p2p log
* [\#2679](https://github.com/bnb-chain/bsc/pull/2679) build(deps): bump actions/download-artifact in /.github/workflows
* [\#2662](https://github.com/bnb-chain/bsc/pull/2662) metrics: add some extra feature flags as node stats
* [\#2675](https://github.com/bnb-chain/bsc/pull/2675) fetcher: Sleep after marking block as done when requeuing
* [\#2695](https://github.com/bnb-chain/bsc/pull/2695) CI: nancy ignore CVE-2024-8421
* [\#2689](https://github.com/bnb-chain/bsc/pull/2689) consensus/parlia: wait more time when processing huge blocks

## v1.4.14

### BUGFIX
* [\#2643](https://github.com/bnb-chain/bsc/pull/2643)core: fix cache for receipts
* [\#2656](https://github.com/bnb-chain/bsc/pull/2656)ethclient: fix BlobSidecars api
* [\#2657](https://github.com/bnb-chain/bsc/pull/2657)fix: update prunefreezer’s offset when pruneancient and the dataset has pruned block

### FEATURE
* [\#2661](https://github.com/bnb-chain/bsc/pull/2661)config: setup Mainnet 2 hardfork date: HaberFix & Bohr

### IMPROVEMENT
* [\#2578](https://github.com/bnb-chain/bsc/pull/2578)core/systemcontracts: use vm.StateDB in UpgradeBuildInSystemContract
* [\#2649](https://github.com/bnb-chain/bsc/pull/2649)internal/debug: remove memsize
* [\#2655](https://github.com/bnb-chain/bsc/pull/2655)internal/ethapi: make GetFinalizedHeader monotonically increasing
* [\#2658](https://github.com/bnb-chain/bsc/pull/2658)core: improve readability of the fork choice logic
* [\#2665](https://github.com/bnb-chain/bsc/pull/2665)faucet: bump and resend faucet transaction if it has been pending for a while

## v1.4.13

### BUGFIX
* [\#2602](https://github.com/bnb-chain/bsc/pull/2602) fix: prune-state when specify --triesInMemory 32
* [\#2579](https://github.com/bnb-chain/bsc/pull/00025790) fix: only take non-mempool tx to calculate bid price

### FEATURE
* [\#2634](https://github.com/bnb-chain/bsc/pull/2634) config: setup Testnet Bohr hardfork date
* [\#2482](https://github.com/bnb-chain/bsc/pull/2482) BEP-341: Validators can produce consecutive blocks
* [\#2502](https://github.com/bnb-chain/bsc/pull/2502) BEP-402: Complete Missing Fields in Block Header to Generate Signature
* [\#2558](https://github.com/bnb-chain/bsc/pull/2558) BEP-404: Clear Miner History when Switching Validators Set
* [\#2605](https://github.com/bnb-chain/bsc/pull/2605) feat: add bohr upgrade contracts bytecode
* [\#2614](https://github.com/bnb-chain/bsc/pull/2614) fix: update stakehub bytecode after zero address agent issue fixed
* [\#2608](https://github.com/bnb-chain/bsc/pull/2608) consensus/parlia: modify mining time for last block in one turn
* [\#2618](https://github.com/bnb-chain/bsc/pull/2618) consensus/parlia: exclude inturn validator when calculate backoffTime
* [\#2621](https://github.com/bnb-chain/bsc/pull/2621) core: not record zero hash beacon block root with Parlia engine

### IMPROVEMENT
* [\#2589](https://github.com/bnb-chain/bsc/pull/2589) core/vote: vote before committing state and writing block
* [\#2596](https://github.com/bnb-chain/bsc/pull/2596) core: improve the network stability when double sign happens
* [\#2600](https://github.com/bnb-chain/bsc/pull/2600) core: cache block after wroten into db
* [\#2629](https://github.com/bnb-chain/bsc/pull/2629) utils: add GetTopAddr to analyse large traffic
* [\#2591](https://github.com/bnb-chain/bsc/pull/2591) consensus/parlia: add GetJustifiedNumber and GetFinalizedNumber
* [\#2611](https://github.com/bnb-chain/bsc/pull/2611) cmd/utils: add new flag OverridePassedForkTime
* [\#2603](https://github.com/bnb-chain/bsc/pull/2603) faucet: rate limit initial implementation
* [\#2622](https://github.com/bnb-chain/bsc/pull/2622) tests: fix evm-test CI
* [\#2628](https://github.com/bnb-chain/bsc/pull/2628) Makefile: use docker compose v2 instead of v1

## v1.4.12

### BUGFIX
* [\#2557](https://github.com/bnb-chain/bsc/pull/2557) fix: fix state inspect error after pruned state
* [\#2562](https://github.com/bnb-chain/bsc/pull/2562) fix: delete unexpected block
* [\#2566](https://github.com/bnb-chain/bsc/pull/2566) core: avoid to cache block before wroten into db
* [\#2567](https://github.com/bnb-chain/bsc/pull/2567) fix: fix statedb copy
* [\#2574](https://github.com/bnb-chain/bsc/pull/2574) core: adapt highestVerifiedHeader to FastFinality
* [\#2542](https://github.com/bnb-chain/bsc/pull/2542) fix: pruneancient freeze from the previous position when the first time
* [\#2564](https://github.com/bnb-chain/bsc/pull/2564) fix: the bug of blobsidecars and downloader with multi-database
* [\#2582](https://github.com/bnb-chain/bsc/pull/2582) fix: remove delete and dangling side chains in prunefreezer

### FEATURE
* [\#2513](https://github.com/bnb-chain/bsc/pull/2513) cmd/jsutils: add a tool to get performance between a range of blocks
* [\#2569](https://github.com/bnb-chain/bsc/pull/2569) cmd/jsutils: add a tool to get slash count
* [\#2583](https://github.com/bnb-chain/bsc/pull/2583) cmd/jsutill: add log about validator name

### IMPROVEMENT
* [\#2546](https://github.com/bnb-chain/bsc/pull/2546) go.mod: update missing dependency
* [\#2559](https://github.com/bnb-chain/bsc/pull/2559) nancy: ignore go-retryablehttp@v0.7.4 in .nancy-ignore
* [\#2556](https://github.com/bnb-chain/bsc/pull/2556) chore: update greenfield cometbft version
* [\#2561](https://github.com/bnb-chain/bsc/pull/2561) tests: fix unstable test
* [\#2572](https://github.com/bnb-chain/bsc/pull/2572) core: clearup testflag for Cancun and Haber
* [\#2573](https://github.com/bnb-chain/bsc/pull/2573) cmd/utils: support use NetworkId to distinguish chapel when do syncing
* [\#2538](https://github.com/bnb-chain/bsc/pull/2538) feat: enhance bid comparison and reply bidding results && detail logs
* [\#2568](https://github.com/bnb-chain/bsc/pull/2568) core/vote: not vote if too late for next in turn validator
* [\#2576](https://github.com/bnb-chain/bsc/pull/2576) miner/worker: broadcast block immediately once sealed
* [\#2580](https://github.com/bnb-chain/bsc/pull/2580) freezer: Opt freezer env checking

## v1.4.11

### BUGFIX
* [\#2534](https://github.com/bnb-chain/bsc/pull/2534) fix: nil pointer when clear simulating bid
* [\#2535](https://github.com/bnb-chain/bsc/pull/2535) upgrade: add HaberFix hardfork

## v1.4.10
### FEATURE
NA

### IMPROVEMENT
* [\#2512](https://github.com/bnb-chain/bsc/pull/2512) feat: add mev helper params and func
* [\#2508](https://github.com/bnb-chain/bsc/pull/2508) perf: speedup pbss trienode read
* [\#2509](https://github.com/bnb-chain/bsc/pull/2509) perf: optimize chain commit performance for multi-database
* [\#2451](https://github.com/bnb-chain/bsc/pull/2451) core/forkchoice: improve stability when inturn block not generate

### BUGFIX
* [\#2518](https://github.com/bnb-chain/bsc/pull/2518) fix: remove zero gasprice check for BSC
* [\#2519](https://github.com/bnb-chain/bsc/pull/2519) UT: random failure of TestSnapSyncWithBlobs
* [\#2515](https://github.com/bnb-chain/bsc/pull/2515) fix getBlobSidecars by ethclient
* [\#2525](https://github.com/bnb-chain/bsc/pull/2525) fix: ensure empty withdrawals after cancun before broadcast

## v1.4.9
### FEATURE
* [\#2463](https://github.com/bnb-chain/bsc/pull/2463)  utils: add check_blobtx.js
* [\#2470](https://github.com/bnb-chain/bsc/pull/2470)  jsutils: faucet successful requests within blocks
* [\#2467](https://github.com/bnb-chain/bsc/pull/2467)  internal/ethapi: add optional parameter for blobSidecars

### IMPROVEMENT
* [\#2462](https://github.com/bnb-chain/bsc/pull/2462)  cmd/utils: add a flag to change breathe block interval for testing
* [\#2497](https://github.com/bnb-chain/bsc/pull/2497)  params/config: add Bohr hardfork
* [\#2479](https://github.com/bnb-chain/bsc/pull/2479)  dev: ensure consistency in BPS bundle result

### BUGFIX
* [\#2461](https://github.com/bnb-chain/bsc/pull/2461)  eth/handler: check lists in body before broadcast blocks
* [\#2455](https://github.com/bnb-chain/bsc/pull/2455)  cmd: fix memory leak when big dataset
* [\#2466](https://github.com/bnb-chain/bsc/pull/2466)  sync: fix some sync issues caused by prune-block.
* [\#2475](https://github.com/bnb-chain/bsc/pull/2475)  fix: move mev op to MinerAPI & add command to console
* [\#2473](https://github.com/bnb-chain/bsc/pull/2473)  fix: limit the gas price of the mev bid
* [\#2484](https://github.com/bnb-chain/bsc/pull/2484)  fix: fix inspect database error
* [\#2481](https://github.com/bnb-chain/bsc/pull/2481)  fix: keep 9W blocks in ancient db when prune block
* [\#2495](https://github.com/bnb-chain/bsc/pull/2495)  fix: add an empty freeze db
* [\#2507](https://github.com/bnb-chain/bsc/pull/2507)  fix: waiting for the last simulation before pick best bid

## v1.4.8
### FEATURE
* [\#2483](https://github.com/bnb-chain/bsc/pull/2483) core/vm: add secp256r1 into PrecompiledContractsHaber
* [\#2400](https://github.com/bnb-chain/bsc/pull/2400) RIP-7212: Precompile for secp256r1 Curve Support

### IMPROVEMENT
NA

### BUGFIX
NA

## v1.4.7
### FEATURE
* [\#2439](https://github.com/bnb-chain/bsc/pull/2439) config: setup Mainnet Tycho(Cancun) hardfork date

### IMPROVEMENT
* [\#2396](https://github.com/bnb-chain/bsc/pull/2396) metrics: add blockInsertMgaspsGauge to trace mgasps
* [\#2411](https://github.com/bnb-chain/bsc/pull/2411) build(deps): bump golang.org/x/net from 0.19.0 to 0.23.0
* [\#2435](https://github.com/bnb-chain/bsc/pull/2435) txpool: limit max gas when mining is enabled
* [\#2438](https://github.com/bnb-chain/bsc/pull/2438) fix: performance issue when load journal
* [\#2440](https://github.com/bnb-chain/bsc/pull/2440) nancy: add files .nancy-ignore

### BUGFIX
NA

## v1.4.6
### FEATURE
* [\#2227](https://github.com/bnb-chain/bsc/pull/2227) core: separated databases for block data
* [\#2404](https://github.com/bnb-chain/bsc/pull/2404) cmd, p2p: filter peers by regex on name

### IMPROVEMENT
* [\#2201](https://github.com/bnb-chain/bsc/pull/2201) chore: render system bytecode by go:embed
* [\#2363](https://github.com/bnb-chain/bsc/pull/2363) feat: greedy merge tx in bid
* [\#2389](https://github.com/bnb-chain/bsc/pull/2389) deps: update prsym to solve warning about quic-go version
* [\#2341](https://github.com/bnb-chain/bsc/pull/2341) core/trie: persist TrieJournal to journal file instead of kv database
* [\#2395](https://github.com/bnb-chain/bsc/pull/2395) fix: trieJournal format compatible old db format
* [\#2406](https://github.com/bnb-chain/bsc/pull/2406) feat: adaptive for loading journal file or journal kv during loadJournal
* [\#2390](https://github.com/bnb-chain/bsc/pull/2390) chore: fix function names in comment
* [\#2399](https://github.com/bnb-chain/bsc/pull/2399) chore: fix some typos in comments
* [\#2408](https://github.com/bnb-chain/bsc/pull/2408) chore: fix some typos in comments
* [\#2416](https://github.com/bnb-chain/bsc/pull/2416) fix: fix function names
* [\#2424](https://github.com/bnb-chain/bsc/pull/2424) feat: recommit bid when newBidCh is empty to maximize mev reward
* [\#2430](https://github.com/bnb-chain/bsc/pull/2430) fix: oom caused by non-discarded mev simulation env
* [\#2428](https://github.com/bnb-chain/bsc/pull/2428) chore: add metric & log for blobTx
* [\#2419](https://github.com/bnb-chain/bsc/pull/2419) metrics: add doublesign counter

### BUGFIX
* [\#2244](https://github.com/bnb-chain/bsc/pull/2244) cmd/geth: fix importBlock
* [\#2391](https://github.com/bnb-chain/bsc/pull/2391) fix: print value instead of pointer in ConfigCompatError
* [\#2398](https://github.com/bnb-chain/bsc/pull/2398) fix: no import blocks before or equal to the finalized height
* [\#2401](https://github.com/bnb-chain/bsc/pull/2401) fix: allow fast node to rewind after abnormal shutdown
* [\#2403](https://github.com/bnb-chain/bsc/pull/2403) fix: NPE
* [\#2423](https://github.com/bnb-chain/bsc/pull/2423) eth/gasprice: add query limit to defend DDOS attack
* [\#2425](https://github.com/bnb-chain/bsc/pull/2425) fix: adapt journal for cmd

## v1.4.5
### FEATURE
* [\#2378](https://github.com/bnb-chain/bsc/pull/2378) config: setup Testnet Tycho(Cancun) hardfork date

### IMPROVEMENT
* [\#2333](https://github.com/bnb-chain/bsc/pull/2333) remove code that will not be executed
* [\#2369](https://github.com/bnb-chain/bsc/pull/2369) core: stateDb has no trie and no snap return err

### BUGFIX
* [\#2359](https://github.com/bnb-chain/bsc/pull/2359) triedb: do not open state freezer under notries

## v1.4.4
### FEATURE
* [\#2279](https://github.com/bnb-chain/bsc/pull/2279) BlobTx: implement EIP-4844 on BSC
* [\#2337](https://github.com/bnb-chain/bsc/pull/2337) 4844: bugfix and improve
* [\#2339](https://github.com/bnb-chain/bsc/pull/2339) fix: missing block asigment WithSidecars
* [\#2350](https://github.com/bnb-chain/bsc/pull/2350) cancun: change empty withdrawHash value of header
* [\#2335](https://github.com/bnb-chain/bsc/pull/2335) upgrade: update system contracts bytes code and hardfork time of Feynman upgrade
* [\#2323](https://github.com/bnb-chain/bsc/pull/2323) feat: export GasCeil in mev_params
* [\#2357](https://github.com/bnb-chain/bsc/pull/2357) feat: add bid fee ceil in mev_params

### IMPROVEMENT
* [\#2321](https://github.com/bnb-chain/bsc/pull/2321) test: use full syncmode to run rpc node
* [\#2338](https://github.com/bnb-chain/bsc/pull/2338) cmd: include more node info in metrics
* [\#2342](https://github.com/bnb-chain/bsc/pull/2342) p2p: add metrics for inbound/outbound peers
* [\#2334](https://github.com/bnb-chain/bsc/pull/2334) core: improve chain rewinding mechanism
* [\#2352](https://github.com/bnb-chain/bsc/pull/2352) core: fix block report when chain is not setHead

### BUGFIX
NA

## v1.4.3
### FEATURE
* [\#2241](https://github.com/bnb-chain/bsc/pull/2241) cmd/utils, core/rawdb, triedb/pathdb: flip hash to path scheme
* [\#2312](https://github.com/bnb-chain/bsc/pull/2312) cmd/utils, node: switch to Pebble as the default db if none exists

### IMPROVEMENT
* [\#2228](https://github.com/bnb-chain/bsc/pull/2228) core: rephrase TriesInMemory log
* [\#2234](https://github.com/bnb-chain/bsc/pull/2234) cmd/utils: disable snap protocol for fast node
* [\#2236](https://github.com/bnb-chain/bsc/pull/2236) build(deps): bump github.com/quic-go/quic-go from 0.39.3 to 0.39.4
* [\#2240](https://github.com/bnb-chain/bsc/pull/2240) core/state: fix taskResult typo

* [\#2280](https://github.com/bnb-chain/bsc/pull/2280) cmd/utils, core: only full sync for fast nodes
* [\#2298](https://github.com/bnb-chain/bsc/pull/2298) cmd, node: initialize ports with --instance
* [\#2302](https://github.com/bnb-chain/bsc/pull/2302) cmd/geth, core/rawdb: add dbDeleteTrieState
* [\#2304](https://github.com/bnb-chain/bsc/pull/2304) eth/ethconfig: remove overridekepler and overrideshanghai
* [\#2307](https://github.com/bnb-chain/bsc/pull/2307) internal/ethapi: add net_nodeInfo
* [\#2311](https://github.com/bnb-chain/bsc/pull/2311) Port cancun related changes from unreleased v1.14.0
* [\#2313](https://github.com/bnb-chain/bsc/pull/2313) tests/truffle: use hbss to run test
* [\#2314](https://github.com/bnb-chain/bsc/pull/2314) cmd/jsutil: dump MinGasPrice for validator
* [\#2317](https://github.com/bnb-chain/bsc/pull/2317) feat: add mev metrics

### BUGFIX
* [\#2272](https://github.com/bnb-chain/bsc/pull/2272) parlia: add state prepare for internal SC transaction
* [\#2277](https://github.com/bnb-chain/bsc/pull/2277) fix: systemTx should be always at the end of block
* [\#2299](https://github.com/bnb-chain/bsc/pull/2299) fix: add FeynmanFix upgrade for a testnet issue
* [\#2310](https://github.com/bnb-chain/bsc/pull/2310) core/vm: fix PrecompiledContractsCancun

## v1.4.2
### FEATURE
* [\#2021](https://github.com/bnb-chain/bsc/pull/2021) feat: support separate trie database
* [\#2224](https://github.com/bnb-chain/bsc/pull/2224) feat: support MEV

### BUGFIX
* [\#2268](https://github.com/bnb-chain/bsc/pull/2268) fix: ensure EIP-4788 not supported with Parlia Engine

### Cancun Code Merge
#### 4844 related
[internal/ethapi: add support for blobs in eth_fillTransaction (#28839)](https://github.com/bnb-chain/bsc/commit/ac5aa672d3b85a1f74667a65a15398f072aa0b2a)
[internal/ethapi: fix defaults for blob fields (#29037)](https://github.com/bnb-chain/bsc/commit/b47cf8fe1de4f97ce38417d8136a58812734a7a9)
[ethereum, ethclient: add blob transaction fields in CallMsg (#28989)](https://github.com/bnb-chain/bsc/commit/9d537f543990d9013d73433dc58fd0e985d9b2b6)
[core/txpool/blobpool: post-crash cleanup and addition/removal metrics(#28914)](https://github.com/bnb-chain/bsc/commit/62affdc9c5ea6f1a73fde42ac5ee5c9795877f88)
[core/txpool/blobpool: update the blob db with corruption handling (#29001)](https://github.com/bnb-chain/bsc/commit/3c30de219f92120248b7b7aeeb2bef82305e9627)
[core/txpool, eth, miner: pre-filter dynamic fees during pending tx retrieval (#29005)](https://github.com/bnb-chain/bsc/commit/593e303485473d9b9194792e4556a451c44dcc6c)
[core/txpool, miner: speed up blob pool pending retrievals (#29008)](https://github.com/bnb-chain/bsc/commit/6fb0d0992bd4eb91faf1e081b3c4aa46adb0ef7d)
[core/txpool, eth, miner: retrieve plain and blob txs separately (#29026)](https://github.com/bnb-chain/bsc/commit/f4852b8ddc8bef962d34210a4f7774b95767e421)
[core/txpool: reject blob txs with blob fee cap below the minimum (#29081)](https://github.com/bnb-chain/bsc/commit/32d4d6e6160432be1cb9780a43253deda7708ced)
[core/txpool/blobpool: reduce default database cap for rollout (#29090)](https://github.com/bnb-chain/bsc/commit/63aaac81007ad46b208570c17cae78b7f60931d4) 
#### Clean Ups
[cmd/devp2p, eth: drop support for eth/67 (#28956)](https://github.com/bnb-chain/bsc/commit/8a76a814a2b9e5b4c1a4c6de44cd702536104507)
[all: remove the dependency from trie to triedb (#28824)](https://github.com/bnb-chain/bsc/commit/fe91d476ba3e29316b6dc99b6efd4a571481d888)
#### Others
[eth, miner: fix enforcing the minimum miner tip (#28933)](https://github.com/bnb-chain/bsc/commit/16ce7bf50fa71c907d1dc6504ed32a9161e71351)
[cmd,internal/era: implement export-history subcommand(#26621)](https://github.com/bnb-chain/bsc/commit/1f50aa76318689c6e74d0c3b4f31421bf7382fc7)
[node, rpc: add configurable HTTP request limit (#28948)](https://github.com/bnb-chain/bsc/commit/69f5d5ba1fe355ff7e3dee5a0c7e662cd82f1071)
[tests: fix goroutine leak related to state snapshot generation (#28974)](https://github.com/bnb-chain/bsc/commit/8321fe2fda0b44d6df3750bcee28b8627525173b)
[internal/ethapi:fix zero rpc gas cap in eth_createAccessList (#28846)](https://github.com/bnb-chain/bsc/commit/b87b9b45331f87fb1da379c5f17a81ebc3738c6e)
[eth/tracers: Fix callTracer logs on onlyTopCall == true (#29068)](https://github.com/bnb-chain/bsc/commit/5a0f468f8cb15b939bd85445d33c614a36942a8e)

## v1.4.1
FEATURE
NA

BUGFIX
* [\#2258](https://github.com/bnb-chain/bsc/pull/2258) core: skip checking state root existence when do snapsync by fast node
* [\#2252](https://github.com/bnb-chain/bsc/pull/2252) fix: add missing args of `bls account generate-proof` cmd (#2252)

IMPROVEMENT
NA

## v1.4.0
#### RPC
[internal/ethapi: implement eth_getBlockReceipts (#27702)](https://github.com/bnb-chain/bsc/commit/f1801a9feda8f81532c92077d2c9a8b785fd699b)
[eth, rpc: add configurable option for wsMessageSizeLimit (#27801)](https://github.com/bnb-chain/bsc/commit/705a51e566bc9215975d08f27d23ddab7baa9dd7)
[api/bind: add CallOpts.BlockHash to allow calling contracts at a specific block hash (#28084)](https://github.com/bnb-chain/bsc/commit/b85c86022e130a76eeb588a5b36e97149b342188)
[internal/ethapi: eth_call block parameter is optional (#28165)](https://github.com/bnb-chain/bsc/commit/adb9b319c9c61f092755000bf0fc4b3349f5cbbc)
[internal/ethapi: ethSendTransaction check baseFee (#27834)](https://github.com/bnb-chain/bsc/commit/54a400ee717caf44603fac390314747c5592ee1b)
#### Command
[cmd/utils: fix a startup issue on deleted chaindata but dangling ancients(#27989)](https://github.com/bnb-chain/bsc/commit/00fead91c4f58bc7f56f81512280d3120860989c)
[cmd/geth, internal/flags, go.mod: colorize cli help, support env vars(#28103)](https://github.com/bnb-chain/bsc/commit/d9fbb71d631d1ad0fb1846042e4c50ab893a6fbf)
[cmd/rlpdump: add -pos flag, displaying byte positions (#28785)](https://github.com/bnb-chain/bsc/commit/1485814f89d8206bb4a1c8e10a4a2893920f683a)
[cmd/geth: make it possible to autopilot removedb (#28725)](https://github.com/bnb-chain/bsc/commit/1010a79c7cbcdb4741e9f30e8cdc19c679ad7377)
#### Flag
[cmd/utils: restore support for txlookuplimit flag (#27917)](https://github.com/bnb-chain/bsc/commit/68855216c903eea9f952a1a7a56e69ea285f284b)
[cmd/utils, eth: disallow invalid snap sync / snapshot flag combos (#28657)](https://github.com/bnb-chain/bsc/commit/d98d70f670297a4bfa86db1a67a9c024f7186f43)
#### Client
[ethclient: use 'input', not 'data' as field for transaction input (#28078)](https://github.com/bnb-chain/bsc/commit/5cf53f51ac556cfff2aee9d405efd336298a3aca)
[ethclient: fix forwarding 1559 gas fields (#28462)](https://github.com/bnb-chain/bsc/commit/e91cdb49beb4b2a3872b5f2548bf2d6559e4f561)
[ethclient/simulated: implement new sim backend (#28202)](https://github.com/bnb-chain/bsc/commit/2d08c9900996b5e798f40a3cc6b47f4e51dc487d)
[ethclient: apply accessList field in toCallArg (#28832)](https://github.com/bnb-chain/bsc/commit/1c488298c807f4daa3cbe260efb88b81902a903d) 
#### Tracer
[eth/tracers: add position field for callTracer logs (#28389)](https://github.com/bnb-chain/bsc/commit/b1cec853bef98acc750298b1c9b8165f2ac6ce5a)
[eth/tracers: tx-level state in debug_traceCall (#28460)](https://github.com/bnb-chain/bsc/commit/5fb8ebc9ecb226b84181420b9871c5f61cf4f77d)
#### Txpool
[eth/fetcher: allow underpriced transactions in after timeout (#28097)](https://github.com/bnb-chain/bsc/commit/2b7bc2c36b0d0efc83e62ba8e13723b943c4fa6e)
#### Sync
[eth/downloader: prevent pivot moves after state commit (#28126)](https://github.com/bnb-chain/bsc/commit/4fa3db49a1e485b8d110c87de6a44f460b45bb9a)
[core, eth/downloader: fix genesis state missing due to state sync (#28124)](https://github.com/bnb-chain/bsc/commit/c53b0fef2ab8e2a00257b898cad5174e6b73f5fc)
[core, accounts, eth, trie: handle genesis state missing (#28171)](https://github.com/bnb-chain/bsc/commit/73f5bcb75b562fcb3c109dd9c51f21956bc1f1f4)
[eth/protocols/snap: fix snap sync failure on empty storage range (#28306)](https://github.com/bnb-chain/bsc/commit/1cb3b6aee4a16ab8e684da63f3cfcd9b961c43af)
[core, eth, trie: filter out boundary nodes and remove dangling nodes in stacktrie (#28327)](https://github.com/bnb-chain/bsc/commit/ab04aeb855605de51dd1e933f45eb84ad877e715)
[core, core/rawdb, eth/sync: no tx indexing during snap sync (#28703)](https://github.com/bnb-chain/bsc/commit/78a3c32ef4deb7755e3367e183639b66242654f7)
#### PBSS Related
[core/rawdb: skip pathdb state inspection in hashdb mode (#28108)](https://github.com/bnb-chain/bsc/commit/8d38b1fe62950e8675795abf63b7c978415ab7ba)
[eth: abort on api operations not available in pbss-mode (#28104)](https://github.com/bnb-chain/bsc/commit/b9b99a12e5159c746ef04b7c8febc4db66817b72)
[trie: remove internal nodes between shortNode and child in path mode (#28163)](https://github.com/bnb-chain/bsc/commit/4773dcbc81aac9d330df29446283361f5a7062c7)
[trie/triedb/pathdb: improve dirty node flushing trigger (#28426)](https://github.com/bnb-chain/bsc/commit/ea2e66a58e48ef63566d5274c4a875e817a1cd39) 
[core/rawdb: fsync the index file after each freezer write (#28483)](https://github.com/bnb-chain/bsc/commit/326fa00759d959061035becc9514660c24897053) 
[trie: remove inconsistent trie nodes during sync in path mode (#28595)](https://github.com/bnb-chain/bsc/commit/e206d3f8975bd98cc86d14055dca40f996bacc60)
[core, cmd, trie: fix the condition of pathdb initialization (#28718)](https://github.com/bnb-chain/bsc/commit/cca94792a4112687ce23e7041b95ccc7f4bf6123) 
#### GraphQL
[graphql: validate block params (#27876](https://github.com/bnb-chain/bsc/commit/5e89ff4d6b0df4bd54d20d90bee5a16abef6b9bc)
[graphql: always set content-type to application/json (#28417)](https://github.com/bnb-chain/bsc/commit/2d7dba024d76603398907a595da98ad4df81b858)
#### Cancun
[core/types: support for optional blob sidecar in BlobTx (#27841)](https://github.com/bnb-chain/bsc/commit/2a6beb6a39d7cb3c5906dd4465d65da6efcc73cd)
[core, params, beacon/engine: implement EIP 4788 BeaconRoot (#27849)](https://github.com/bnb-chain/bsc/commit/b8d38e76ef07c99d338c7bcd485881850382a58f) 
[miner: add to build block with EIP-4844 blobs (#27875)](https://github.com/bnb-chain/bsc/commit/feb8f416acc3f067ecc8cbdabb8e70679547737a)
[core: implement BLOBBASEFEE opcode (0x4a) (#28098)](https://github.com/bnb-chain/bsc/commit/c39cbc1a78aa275523c1b0ff9d21b16ba7bfa486)
[core, eth, miner: start propagating and consuming blob txs (#28243)](https://github.com/bnb-chain/bsc/commit/a8a9c8e4b00c5b9f84242181839234b8e9fd54e3)
[eth/fetcher: throttle tx fetches to 128KB responses (#28304)](https://github.com/bnb-chain/bsc/commit/a6deb2d994e644300bac43455b1c954976e7382e)
[crypto/kzg4844: use the new trusted setup file and format (#28383)](https://github.com/bnb-chain/bsc/commit/a6a0ae45b69a95b38b7cb2d085e7833c88b72164)
[internal/ethapi: handle blobs in API methods (#28786)](https://github.com/bnb-chain/bsc/commit/e5d5e09faae48dac3723634e2b1813e4f2e89535)
#### P2P
[cmd/devp2p, eth: drop eth/66 (#28239)](https://github.com/bnb-chain/bsc/commit/bc6d184872889224480cf9df58b0539b210ffa9e) 
[cmd/devp2p: use bootnodes as crawl input (#28139)](https://github.com/bnb-chain/bsc/commit/41a0ad9f03ae8e8389fbe40131f4e6930b5beac5)
[p2p/discover: add liveness check in collectTableNodes (#28686)](https://github.com/bnb-chain/bsc/commit/5b22a472d6aaaa17daf0543b5914ca1f2f5518a7)
#### Test
[build, tests: add execution-spec-tests (#26985)](https://github.com/bnb-chain/bsc/commit/f174ddba7af5c150ecad37430c167194d66d75f1) 
[tests/fuzzers: update fuzzers to be based on go-native fuzzing (#28352)](https://github.com/bnb-chain/bsc/commit/d10a2f6ab727f79a0acff29c8147d54c5e4689ec) 
[tests/fuzzers: move fuzzers into native packages (#28467)](https://github.com/bnb-chain/bsc/commit/2391fbc676d7464bd42e248155558a2bcd6ecf64)
#### Clear Up
[eth/downloader: remove header rollback mechanism (#28147)](https://github.com/bnb-chain/bsc/commit/b85c183ea7417e973dbbccd27b3fb7d7097b87dd)
#### Others
[core/forkid: correctly compute forkid when timestamp fork is activated in genesis (#27895)](https://github.com/bnb-chain/bsc/commit/32fde3f838d604fdeb7a3ada4f8e02d78301b83d)
[core/vm/runtime: Add Random field to config (#28001)](https://github.com/bnb-chain/bsc/commit/0ba2d3cfa4e4a0a76cd457b8dc0f49bf1a79b723)
[core/rawdb: no need to run truncateFile for readonly mode (#28145)](https://github.com/bnb-chain/bsc/commit/545f4c5547178bc8bde6af08b3ccaf68ca27f2c0)
[core/bloombits: fix deadlock when matcher session hits an error (#28184)](https://github.com/bnb-chain/bsc/commit/c2cfe35f121cb88650b4d90c958bcc4214d0ce7f) 
[core/state, tests: fix memory leak via fastcache (#28387)](https://github.com/bnb-chain/bsc/commit/c1d5a012ea4b824e902db14e47bf147d727c2657)
[internal/ethapi: compact db missing key starts with 0xff (#28207)](https://github.com/bnb-chain/bsc/commit/46c850a9411d7ff15c1a0342fe29f359e6c390ae)
[internal/ethapi: fix codehash lookup in eth_getProof (#28357)](https://github.com/bnb-chain/bsc/commit/8b99ad46027a455971ccf9bd1f425b9c58ec5855) 
[eth: set networkID to chainID by default (#28250)](https://github.com/bnb-chain/bsc/commit/4d9f3cd5d751efccd501b08ab6cf38a83b5e2858)
[eth: fix potential hang in waitSnapExtension (#28744)](https://github.com/bnb-chain/bsc/commit/18e154eaa24d5f7a8b3c48983ad591e6c10963ca)
[metrics, cmd/geth: informational metrics (prometheus, influxdb, opentsb (#24877)](https://github.com/bnb-chain/bsc/commit/53f3c2ae656cd860a700751b6c5f81ca9c66503d)
[ethdb/pebble: don't double-close iterator inside pebbleIterator (#28566)](https://github.com/bnb-chain/bsc/commit/6489a0dd1f98e9ce1c64c2eae93c8a88df7ae674)
[trie/triedb/hashdb: take lock around access to dirties cache (#28542)](https://github.com/bnb-chain/bsc/commit/fa0df76f3cfd186a1f06f2b80aa5dbb89555b009) 
[accounts: properly close managed wallets when closing manager (#28710)](https://github.com/bnb-chain/bsc/commit/d3452a22cc871306c62de52d19295914141863c0)
[event: fix Resubscribe deadlock when unsubscribing after inner sub ends (#28359)](https://github.com/bnb-chain/bsc/commit/ffc6a0f36edda396a8421cf7a3c0feb88be20d0b)
[all: replace log15 with slog (#28187)](https://github.com/bnb-chain/bsc/commit/28e73717016cdc9ebdb5fdb3474cfbd3bd2d2524)

## v1.3.11
BUGFIX
* [\#2288](https://github.com/bnb-chain/bsc/pull/2288) fix: add FeynmanFix upgrade for a testnet issue

## v1.3.10
FEATURE
* [\#2047](https://github.com/bnb-chain/bsc/pull/2047) feat: add new fork block and precompile contract for BEP294 and BEP299

## v1.3.9
FEATURE
* [\#2186](https://github.com/bnb-chain/bsc/pull/2186) log: support maxBackups in config.toml

BUGFIX
* [\#2160](https://github.com/bnb-chain/bsc/pull/2160) cmd: fix dump cli cannot work in path mode
* [\#2183](https://github.com/bnb-chain/bsc/pull/2183) p2p: resolved deadlock on p2p server shutdown

IMPROVEMENT
* [\#2177](https://github.com/bnb-chain/bsc/pull/0000) build(deps): bump github.com/quic-go/quic-go from 0.39.3 to 0.39.4
* [\#2185](https://github.com/bnb-chain/bsc/pull/2185) consensus/parlia: set nonce before evm run
* [\#2190](https://github.com/bnb-chain/bsc/pull/2190) fix(legacypool): deprecate already known error
* [\#2195](https://github.com/bnb-chain/bsc/pull/2195) eth/fetcher: downgrade state tx log

## v1.3.8
FEATURE
* [\#2074](https://github.com/bnb-chain/bsc/pull/2074) faucet: new faucet client
* [\#2082](https://github.com/bnb-chain/bsc/pull/2082) cmd/dbcmd: add inspect trie tool
* [\#2140](https://github.com/bnb-chain/bsc/pull/2140) eth/fetcher: allow underpriced transactions in after timeout
* [\#2115](https://github.com/bnb-chain/bsc/pull/2115) p2p: no peer reconnect if explicitly disconnected
* [\#2128](https://github.com/bnb-chain/bsc/pull/2128) go.mod: upgrade prysm to support built with go@v1.21
* [\#2151](https://github.com/bnb-chain/bsc/pull/2151) feat: enable NoDial should still dial static nodes
* [\#2144](https://github.com/bnb-chain/bsc/pull/2144) p2p: reset disconnect set with magic enode ID

BUGFIX
* [\#2095](https://github.com/bnb-chain/bsc/pull/2095) rpc: fix ns/µs mismatch in metrics
* [\#2083](https://github.com/bnb-chain/bsc/pull/2083) triedb/pathdb: fix async node buffer diskroot mismatches when journaling
* [\#2120](https://github.com/bnb-chain/bsc/pull/2120) ethdb/pebble: cap memory table size as maxMemTableSize-1
* [\#2107](https://github.com/bnb-chain/bsc/pull/2107) cmd/geth: fix parse state scheme
* [\#2121](https://github.com/bnb-chain/bsc/pull/2121) parlia: fix verifyVoteAttestation when verify a batch of headers
* [\#2132](https://github.com/bnb-chain/bsc/pull/2132) core: fix systemcontracts.GenesisHash when run bsc firstly without init
* [\#2155](https://github.com/bnb-chain/bsc/pull/2155) cmd, core: resolve scheme from a read-write database and refactor resolveChainFreezerDir func

IMPROVEMENT
* [\#2099](https://github.com/bnb-chain/bsc/pull/2099) params/config: remove useless toml tag for hardforks
* [\#2100](https://github.com/bnb-chain/bsc/pull/2100) core/genesis: support chapel to run without geth init
* [\#2101](https://github.com/bnb-chain/bsc/pull/2101) core: add metrics for bad block
* [\#2109](https://github.com/bnb-chain/bsc/pull/2109) cmd/geth: tidy flags for geth command
* [\#1953](https://github.com/bnb-chain/bsc/pull/1953) build(deps): bump github.com/docker/docker
* [\#2086](https://github.com/bnb-chain/bsc/pull/2086) build(deps): bump golang.org/x/crypto from 0.12.0 to 0.17.0
* [\#2106](https://github.com/bnb-chain/bsc/pull/2106) params: use rialto to test builtin network logic
* [\#2098](https://github.com/bnb-chain/bsc/pull/2098) cmd, les, tests: remove light client code
* [\#2114](https://github.com/bnb-chain/bsc/pull/2114) p2p: add serve metrics
* [\#2123](https://github.com/bnb-chain/bsc/pull/2123) p2p, eth: improve logs
* [\#2116](https://github.com/bnb-chain/bsc/pull/2116) tests: revive evm test cases
* [\#2161](https://github.com/bnb-chain/bsc/pull/2161) code: remove IsEuler check from worker.go
* [\#2167](https://github.com/bnb-chain/bsc/pull/2167) improve: increase SystemTxsGas from 1,500,000 to 5,000,000
* [\#2172](https://github.com/bnb-chain/bsc/pull/2172) improve: remove sharedpool from miner
* [\#1332](https://github.com/bnb-chain/bsc/pull/1332) core/state: no need to prune block if the same

## v1.3.7
FEATURE
* [\#2067](https://github.com/bnb-chain/bsc/pull/2067) cmd/geth: add check func to validate state scheme
* [\#2068](https://github.com/bnb-chain/bsc/pull/2068) internal/ethapi: implement eth_getBlockReceipts

BUGFIX
* [\#2035](https://github.com/bnb-chain/bsc/pull/2035) all: pull snap sync PRs from upstream v1.13.5
* [\#2072](https://github.com/bnb-chain/bsc/pull/2072) fix: fix the pebble config of level option
* [\#2078](https://github.com/bnb-chain/bsc/pull/2078) core: LoadChainConfig return the predefined config for built-in networks firstly

## v1.3.6
FEATURE
* [\#2012](https://github.com/bnb-chain/bsc/pull/2012) cmd, core, ethdb: enable Pebble on 32 bits and OpenBSD
* [\#2063](https://github.com/bnb-chain/bsc/pull/2063) log: support to disable log rotate by hours
* [\#2064](https://github.com/bnb-chain/bsc/pull/2064) log: limit rotateHours in range [0,23]

BUGFIX
* [\#2058](https://github.com/bnb-chain/bsc/pull/2058) params: set default hardfork times

IMPROVEMENT
* [\#2015](https://github.com/bnb-chain/bsc/pull/2015) cmd, core, eth: change default network from ETH to BSC
* [\#2036](https://github.com/bnb-chain/bsc/pull/2036) cmd/jsutils: add 2 tools get validator version and block txs number
* [\#2037](https://github.com/bnb-chain/bsc/pull/2037) core/txpool/legacypool: respect nolocals-setting
* [\#2042](https://github.com/bnb-chain/bsc/pull/2042) core/systemcontracts: update CommitUrl for keplerUpgrade
* [\#2043](https://github.com/bnb-chain/bsc/pull/2043) tests/truffle: adapt changes in bsc-genesis-contracts
* [\#2051](https://github.com/bnb-chain/bsc/pull/2051) core/vote: wait some blocks before voting since mining begin
* [\#2060](https://github.com/bnb-chain/bsc/pull/2060) cmd/utils: allow HTTPHost and WSHost flags precede

## v1.3.5
FEATURE
* [\#1970](https://github.com/bnb-chain/bsc/pull/1970) core: enable Shanghai EIPs
* [\#1973](https://github.com/bnb-chain/bsc/pull/1973) core/systemcontracts: include BEP-319 on kepler hardfork

BUGFIX
* [\#1964](https://github.com/bnb-chain/bsc/pull/1964) consensus/parlia: hardfork block can be epoch block
* [\#1979](https://github.com/bnb-chain/bsc/pull/1979) fix: upgrade pebble and improve config
* [\#1980](https://github.com/bnb-chain/bsc/pull/1980) internal/ethapi: fix null effectiveGasPrice in GetTransactionReceipt

IMPROVEMENT
* [\#1977](https://github.com/bnb-chain/bsc/pull/1977) doc: add instructions for starting fullnode with pbss

## v1.3.4
BUGFIX
* fix: remove pipecommit in miner
* add a hard fork: Hertzfix

## v1.3.3
BUGFIX
* [\#1986](https://github.com/bnb-chain/bsc/pull/1986) fix(cmd): check pruneancient when creating db

IMPROVEMENT
* [\#2000](https://github.com/bnb-chain/bsc/pull/2000) cmd/utils: exit process if txlookuplimit flag is set

## v1.3.2
BUGFIX
* fix: remove sharedPool

IMPROVEMENT
* [\#2007](https://github.com/bnb-chain/bsc/pull/2007) consensus/parlia: increase size of snapshot cache in parlia
* [\#2008](https://github.com/bnb-chain/bsc/pull/2008) consensus/parlia: recover faster when snapshot of parlia is gone in disk

## v1.3.1
FEATURE
* [\#1881](https://github.com/bnb-chain/bsc/pull/1881) feat: active pbss
* [\#1882](https://github.com/bnb-chain/bsc/pull/1882) cmd/geth: add hbss to pbss convert tool
* [\#1916](https://github.com/bnb-chain/bsc/pull/1916) feat: cherry-pick pbss patch commits from eth repo in v1.13.2
* [\#1939](https://github.com/bnb-chain/bsc/pull/1939) dependency: go version to 1.20 and some dependencies in go.mod
* [\#1955](https://github.com/bnb-chain/bsc/pull/1955) eth, trie/triedb/pathdb: pbss patches
* [\#1962](https://github.com/bnb-chain/bsc/pull/1962) cherry pick pbss patches from go-ethereum

BUGFIX
* [\#1923](https://github.com/bnb-chain/bsc/pull/1923) consensus/parlia: fix nextForkHash in Extra filed of block header
* [\#1950](https://github.com/bnb-chain/bsc/pull/1950) fix: 2 APIs of get receipt related
* [\#1951](https://github.com/bnb-chain/bsc/pull/1951) txpool: fix a potential crash issue in shutdown;
* [\#1963](https://github.com/bnb-chain/bsc/pull/1963) fix: revert trie commited flag after delete statedb mpt cache

IMPROVEMENT
* [\#1948](https://github.com/bnb-chain/bsc/pull/1948) performance: commitTire concurrently
* [\#1949](https://github.com/bnb-chain/bsc/pull/1949) code: remove accountTrieCache and storageTrieCache
* [\#1954](https://github.com/bnb-chain/bsc/pull/1954) trie: keep trie prefetch during validation phase

## v1.3.0
#### RPC
* [internal/ethapi: add debug_getRawReceipts RPC method (#24773)](https://github.com/bnb-chain/bsc/pull/1840/commits/ae7d834bc752a2d94fef9d354ee78fcb9425f3d1)
* [node, rpc: add ReadHeaderTimeout config option (#25338)](https://github.com/bnb-chain/bsc/pull/1840/commits/9244f87dc1c8869a2632176f719e515217720a43)
* [rpc: check that "version" is "2.0" in request objects (#25570)](https://github.com/bnb-chain/bsc/pull/1840/commits/38e002f4641c2779c897ccaca575ec5ddeee9254)
* [rpc: support injecting HTTP headers through context (#26023)](https://github.com/bnb-chain/bsc/pull/1840/commits/add337e0f7bad02f3cf535c66cd31f252b0b5c99)
* [rpc: websocket should respect the "HTTP_PROXY" by default (#27264)](https://github.com/bnb-chain/bsc/pull/1840/commits/73697529994e14996b7740730481e926d5ec3e40)
* [rpc: change BlockNumber constant values to match ethclient (#27219)](https://github.com/bnb-chain/bsc/pull/1840/commits/9231770811cda0473a7fa4e2bccc95bf62aae634)
* [eth: make debug_StorageRangeAt take a block hash or number (#27328)](https://github.com/bnb-chain/bsc/pull/1840/commits/d789c68b667e13eb5cefd19d09ae84f7d016df6a)
* [eth,core: add api debug_getTrieFlushInterval (#27303)](https://github.com/bnb-chain/bsc/pull/1840/commits/0783cb7d91ad7b3cdf72ac6c6edaec8318673eb6)
* [rpc: add limit for batch request items and response size (#26681)](https://github.com/bnb-chain/bsc/pull/1840/commits/f3314bb6df4c86e650f0e47cbb5a21ca0616ac11)
* [core/types: support yParity field in JSON transactions (#27744)](https://github.com/bnb-chain/bsc/pull/1840/commits/bb148dd342ba03ce40cf04295e193c94b9dda322)
* [eth/filters: send rpctransactions in pending-subscription (#26126)](https://github.com/bnb-chain/bsc/pull/1840/commits/8c5ce1107b3110c7cb735d8dfa91c9c701393c85)
#### Flag
* [cmd/geth: rename --whitelist to --eth.requiredblocks (#24505)](https://github.com/bnb-chain/bsc/pull/1840/commits/dbfd3972624c1d82db21f5dfceab8fde7a1eee0a)
* [cmd: migrate to urfave/cli/v2 (#24751)](https://github.com/bnb-chain/bsc/pull/1840/commits/52ed3570c483693fdd6667add7e3050520ad3ba2)
* [cmd/utils: print warning when --metrics.port set without --metrics.ad…](https://github.com/bnb-chain/bsc/pull/1840/commits/8846c07d044f30dca8cd0db91c6245f71f4b24fa)
* [cmd/devp2p: add --extaddr flag (#26312)](https://github.com/bnb-chain/bsc/pull/1840/commits/b44abf56a966016cbb651648ac2d7b6705e80b11)
* [core,eth: adddebug_setTrieFlushInterval to change trie flush frequ](https://github.com/bnb-chain/bsc/pull/1840/commits/711afbc7fd76f1f206429e26f9aa5bf98bc7b43d)
* [miner, cmd, eth: require explicit etherbase address (#26413)](https://github.com/bnb-chain/bsc/pull/1840/commits/2b44ef5f93cc7479a77890917a29684b56e9167a)
* [cmd/geth: Add[--log.format] cli param (#27001)](https://github.com/bnb-chain/bsc/pull/1840/commits/2d1492821d058a3488b4da2c1f62906eaf6d7c95)
* [cmd/geth: rename --vmodule to --log.vmodule (#27071)](https://github.com/bnb-chain/bsc/pull/1840/commits/f2df2b1981fa1e014e4cb34cf9a8dd7b8519e0ac)
* [params, trie: add verkle fork management + upgrade go-verkle (#27464)](https://github.com/bnb-chain/bsc/pull/1840/commits/85b8d1c06c49342966cad2bbdc17d0dc28b66ffd)
#### GraphQL
* [graphql: fee history fields (#24452)](https://github.com/bnb-chain/bsc/pull/1840/commits/57cec892536270fc6dafae01ded2c528ffa370e9)
* [graphql: add rawReceipt field to transaction type (#24738)](https://github.com/bnb-chain/bsc/pull/1840/commits/d73df893a6fc528e69506397322205bd9258b6fa)
* [graphql: add raw fields to block and tx (#24816)](https://github.com/bnb-chain/bsc/pull/1840/commits/29a6b6bcac170ca7f8fceb242eba45ff15df17a1)
* [graphql: return correct logs for tx (#25612)](https://github.com/bnb-chain/bsc/pull/1840/commits/d0dc349fd36bd79f94516c866251783641ed12f1)
* [graphql: add query timeout (#26116)](https://github.com/bnb-chain/bsc/pull/1840/commits/ee9ff064694c445a3a6972001ccbce2cc5b9c3f2)
* [graphql, node, rpc: improve HTTP write timeout handling (#25457)](https://github.com/bnb-chain/bsc/pull/1840/commits/f20eba426a1a871f98d0d988bfd51767364650b7)
* [graphql: implement withdrawals (EIP-4895) (#27072)](https://github.com/bnb-chain/bsc/pull/1840/commits/fbe432fa1584bc976fe0242d999a7dd8903378b2)
#### Client
* [ethclient: add CallContractAtHash (#24355)](https://github.com/bnb-chain/bsc/pull/1700/commits/e98114da4feedf6dfb17b9839fc2c314cf1e5768)
* [ethclient: add PeerCount method (#24849)](https://github.com/bnb-chain/bsc/pull/1840/commits/f5ff022dbca2b14af59974154874537b5ed4cc5e) 
* [ethereum, ethclient: add FeeHistory support (#25403)](https://github.com/bnb-chain/bsc/pull/1840/commits/9ad508018e4790da0c1c00ac355f206fca12ab7c)
* [eth/filters, ethclient/gethclient: add fullTx option to pending tx fi…](https://github.com/bnb-chain/bsc/pull/1840/commits/5b1a04b9c749d804b51159fe12246c56de8515c1)
* [ethclient: include withdrawals in ethclient block responses (#26778)](https://github.com/bnb-chain/bsc/pull/1840/commits/e1b98f49a5075694c5022f5ec74425e40da415dd)
#### Tracer
* [eth/tracers/js: drop duktape engine (#24934)](https://github.com/bnb-chain/bsc/pull/1840/commits/ba47d800b13058885288c38bd174babb38560c89)
* [eth/tracers: add support for block overrides in debug_traceCall (#24871)](https://github.com/bnb-chain/bsc/pull/1840/commits/d8a2305565b1f97c451f8595e0f65358d6842714)
* [eth/tracers: add onlyTopCall option to callTracer (#25430)](https://github.com/bnb-chain/bsc/pull/1840/commits/86de2e516e5a4a2bbe1d29b46a0f460fbdde8303)
* [eth/tracers: remove revertReasonTracer, add revert reason to callTracer](https://github.com/bnb-chain/bsc/pull/1840/commits/ff1f49245d641a7268ade38cf512bdc7b26f9b7c)
* [eth/tracers: add diffMode to prestateTracer (#25422)](https://github.com/bnb-chain/bsc/pull/1840/commits/5d52a35931bba10f438ce4f41410442dd9cd396c)
* [eth/tracers: add multiplexing tracer (#26086)](https://github.com/bnb-chain/bsc/pull/1840/commits/53b624b56d4f36c90ebf8046bd1ca78c87a3b6df)
* [core/vm: set tracer-observable value of a delegatecall to match parent `value`](https://github.com/bnb-chain/bsc/pull/1840/commits/b0cd8c4a5c4f0f25011ed64235a3ea1280f03c51)
* [eth/tracers: add native flatCallTracer (aka parity style tracer) (#26…](https://github.com/bnb-chain/bsc/pull/1840/commits/2ad150d986dab085965be047c94af6b2952a9e24)
* [eth/tracers/native: set created address to nil in case of failure (#2…](https://github.com/bnb-chain/bsc/pull/1840/commits/41af42e97c9d62d303a883cc3c143f560867fa34)
* [eth/tracers: report correct gasLimit in call tracers (#27029)](https://github.com/bnb-chain/bsc/pull/1840/commits/0b76eb3708626fbd2eb9c1b58d7b4eac6a5eec15)
* [eth/tracers: addtxHashfield on txTraceResult (#27183)](https://github.com/bnb-chain/bsc/pull/1840/commits/604e215d1bb070dff98fb76aa965064c74e3633f)
* [eth/tracers: add ReturnData in the tracer's response (#27704)](https://github.com/bnb-chain/bsc/pull/1840/commits/1e069cf8026a9f71b5f7e80959465e4b273d5806)
#### Command
* [cmd/geth: inspect snapshot dangling storage (#24643)](https://github.com/bnb-chain/bsc/pull/1840/commits/92e3c56e7be26aac4a25859f55f234aadeec7dbf)
* [core/state/snapshot: detect and clean up dangling storage snapshot in generation](https://github.com/bnb-chain/bsc/pull/1840/commits/59ac229f87831bd74b4dc07d34f54137cca78095)
* [internal/ethapi: add db operations to api (#24739)](https://github.com/bnb-chain/bsc/pull/1840/commits/16701c51697e28986feebd122c6a491e4d9ac0e7)
* [cmd/geth: adddb check-state-contentto verify integrity of trie nodes (#24840)](https://github.com/bnb-chain/bsc/pull/1840/commits/e0a9752b965f243313f2c32a91d306600dc3863c)
* [ethdb/remotedb, cmd: add support for remote (readonly) databases](https://github.com/bnb-chain/bsc/pull/1840/commits/57192bd0dc545d921306f6a4d7566c0c70c764c5)
* [cmd/abigen: accept combined-json via stdin (#24960)](https://github.com/bnb-chain/bsc/pull/1840/commits/0287e1a7c00c1eaad1a99b4ea05d70f1ed685140)
* [cmd/geth: extend traverseRawState command (#24954)](https://github.com/bnb-chain/bsc/pull/1840/commits/a10660b7f8f4fa218ee62a7664b47eb6028fee84)
* [cmd/geth, core/state/snapshot: rework journal loading, implement account-check (#24765)](https://github.com/bnb-chain/bsc/pull/1840/commits/c375ee91e99cd9c072f2fe9b535c5cb780b5f8a0)
* [cmd/geth: add a verkle subcommand (#25718)](https://github.com/bnb-chain/bsc/pull/1840/commits/9d717167aaf27a48d56ad9d1a2c36f90eba1cc13)
* [cmd/geth, cmd/utils: geth attach with custom headers (#25829)](https://github.com/bnb-chain/bsc/pull/1840/commits/ea26fc8a6c44ebb48223f991048f41b2ec0a6414)
* [core/rawdb: refactor db inspector for extending multiple ancient storage](https://github.com/bnb-chain/bsc/pull/1840/commits/60e30a940bbba2c0d26de040195a5ccdb14d8c10)
* [cmd/clef: addlist-accountsandlist-walletsto CLI (#26080)](https://github.com/bnb-chain/bsc/pull/1840/commits/f3a005f176372ff291dfa7c02ee1c87d18e9c788)
* [cmd/clef: add importraw feature to clef (#26058)](https://github.com/bnb-chain/bsc/pull/1840/commits/17744639dafc5a54f21e220660bd39d765a09051)
* [cmd/devp2p: add more nodekey commands (#26129)](https://github.com/bnb-chain/bsc/pull/1840/commits/913973436bb88b652faffc10d8f97e4c19722883)
* [internal/web3ext: fix eth_call stateOverrides in console (#26265)](https://github.com/bnb-chain/bsc/pull/1840/commits/1325fef1025b9feb3342308265b6d1399614be30)
* [cmd/evm: add blocktest subcommand to evm (#26526)](https://github.com/bnb-chain/bsc/pull/1840/commits/90f15a0230be34a292c5d0574ee7910ee44267de)
#### HardFork
* [params: define cancun and prague as timestamp based forks (#26481)](https://github.com/bnb-chain/bsc/pull/1840/commits/f3a005f176372ff291dfa7c02ee1c87d18e9c788)
* [all: tie timestamp based forks to the passage of London (#27279)](https://github.com/bnb-chain/bsc/pull/1840/commits/85a4b82b3373fc5f3fa8b7c68061c55b0db0e9b7)
##### Shanghai
* [core/vm: implement EIP-3855: PUSH0 instruction (#24039)](https://github.com/bnb-chain/bsc/pull/1840/commits/3b967d16caf306ccf8eb78b3a68bec36fa2a52ee)
* [core: implement EIP-3651, warm coinbase (#25819)](https://github.com/bnb-chain/bsc/pull/1840/commits/ec2ec2d08e28571dc189903f743cc3931da254a9)
* [core/vm: implement EIP-3860: Limit and meter initcode (#23847)](https://github.com/bnb-chain/bsc/pull/1840/commits/793f0f9ec860f6f51e0cec943a268c10863097c7)
* [all: implement withdrawals (EIP-4895) (#26484)](https://github.com/bnb-chain/bsc/pull/1840/commits/2a2b0419fb966c54fb86b17bbccea743a45b4d2a)
##### CanCun (almost ready)
* [all: implement EIP-1153 transient storage (#26003)](https://github.com/bnb-chain/bsc/pull/1840/commits/b4ea2bf7dda9def5374ed3ab16a3dfd872eaa40a)
* [core: 4844 opcode and precompile (#27356)](https://github.com/bnb-chain/bsc/pull/1840/commits/c537ace249805903f068c4c66b90558848b49a2f)
* [core/vm: implement EIP-5656, mcopy instruction (#26181)](https://github.com/bnb-chain/bsc/pull/1840/commits/5c9cbc218a67ea6d71652f0d93f4c354a687a965)
* [core/state, core/vm: implement EIP 6780 (#27189)](https://github.com/bnb-chain/bsc/pull/1840/commits/988d84aa7caf8e71ce441fa65f80d44216d9e00e)
#### New Feature
* [eth: introduce eth67 protocol (#24093)](https://github.com/bnb-chain/bsc/pull/1840/commits/30602163d5d8321fbc68afdcbbaf2362b2641bde)
* [eth: implement eth/68 (#25980)](https://github.com/bnb-chain/bsc/pull/1840/commits/b0d44338bbcefee044f1f635a84487cbbd8f0538)
* [PBBS(ready to activate)](https://github.com/ethereum/go-ethereum/commits?author=rjl493456442)
#### P2P
* [eth/fetcher: throttle peers which deliver many invalid transactions (…](https://github.com/bnb-chain/bsc/pull/1840/commits/7f2890a9be1f91368582479f171248b972b45ae3)
#### Build
* [build/bot: add ppa-build.sh (#24919)](https://github.com/bnb-chain/bsc/pull/1840/commits/adcad1cd39ad2bc9ddab67b4bee3023b3e6c9873)
* [more checs in ci](https://github.com/bnb-chain/bsc/pull/1840/files#diff-6179837f7df53a6f05c522b6b7bb566d484d5465d9894fb04910dd08bb40dcc9)
#### Improvement
* [all: use 'embed' instead of go-bindata (#24744)](https://github.com/bnb-chain/bsc/pull/1840/commits/7ab15490e93e6384cfaa233238777ea88a88b8b6)
* [all: move genesis initialization to blockchain (#25523)](https://github.com/bnb-chain/bsc/pull/1840/commits/d10c28030944d1c32febba3f45ae8c175ab34063)
#### Clear Up
* [common/compiler, cmd/abigen: remove solc/vyper compiler integration](https://github.com/bnb-chain/bsc/pull/1840/commits/8541ddbd951370b2a42df8d82b0633ff0efeba12)
* [all: remove concept of public/private API definitions (#25053)](https://github.com/bnb-chain/bsc/pull/1840/commits/10dc5dce0871bf8c24bac41b04e47c3b9ad2b93e)
* [cmd/geth: drop geth js command (#25000)](https://github.com/bnb-chain/bsc/pull/1840/commits/f20a56926551ae91a349498f9ce97c8ee373d6bb)
* [core/genesis: remove calaverasAllocData (#25516)](https://github.com/bnb-chain/bsc/pull/1840/commits/141cd425310b503c5678e674a8c3872cf46b7086)
* [node: drop support for static & trusted node list files (#25610)](https://github.com/bnb-chain/bsc/pull/1840/commits/3630cafb34f7c48b9cc78cf736309275cbd70f74)
* [core: drop legacy receipt types (#26225)](https://github.com/bnb-chain/bsc/pull/1840/commits/10347c6b54d5b28a2e71d9c4993e7f44b0a359c3)
* [cmd/puppeth: remove puppeth](https://github.com/bnb-chain/bsc/pull/1840/commits/8ded6a9fcd883d7d96ef695f5b312c509eae3a0a)
* [cmd, eth, node: deprecate personal namespace (#26390)](https://github.com/bnb-chain/bsc/pull/1840/commits/d0a4989a8def7e6bad182d1513e8d4a093c1672d)
* [accounts, build, mobile: remove Andriod and iOS support](https://github.com/bnb-chain/bsc/pull/1840/commits/d9699c8238307d5c3081c12078f78527468d7dbc)
* [params: remove EIP150Hash from chainconfig (#27087)](https://github.com/bnb-chain/bsc/pull/1840/commits/5e4d726e2a05aee80a75e5f99fd699f220dd503e)
* [all: remove notion of trusted checkpoints in the post-merge world (#2…](https://github.com/bnb-chain/bsc/pull/1840/commits/1e556d220c3a40286dd90b37a08bb5fc659ee6ee)
* [all: remove ethash pow, only retain shims needed for consensus and te](https://github.com/bnb-chain/bsc/pull/1840/commits/dde2da0efb8e9a1812f470bc43254134cd1f8cc0)
* [cmd, core, eth, graphql, trie: no persisted clean trie cache file (#2…](https://github.com/bnb-chain/bsc/pull/1840/commits/59f7b289c329b5a56fa6f4e9acee64e504c4cc0d)
* [les: remove obsolete code related to PoW header syncing (#27737)](https://github.com/bnb-chain/bsc/pull/1840/commits/d4d88f9bce13ca9310bf28f5f26ea9f1915ba90d)
* remove diffsync
#### Others
* [accounts/usbwallet: support Ledger Nano S Plus and FTS (#25933)](https://github.com/bnb-chain/bsc/pull/1840/commits/7eafbec741d124bc53896f6bfc2408b70ab9a82a)
* [accounts/scwallet: fix keycard data signing error (#25331)](https://github.com/bnb-chain/bsc/pull/1840/commits/0c66d971e7f3557df297cbe450fe7fc7826017be)
* [core/state: replace fastcache code cache with gc-friendly structure (…](https://github.com/bnb-chain/bsc/pull/1840/commits/5fded040372784985265f83f33f15cb6a51bebdb)
* [internal/debug: add --log.file option (#26149)](https://github.com/bnb-chain/bsc/pull/1840/commits/5b4c149f97408ecefc7f440e86c12a30c4342620)
* [ci: disable coverage reporting in appveyor and travis](https://github.com/bnb-chain/bsc/pull/1840/commits/a0d63bc69a659009a3884f50c563a0e58483cdd0)
* [all: change chain head markers from block to header (#26777)](https://github.com/bnb-chain/bsc/pull/1840/commits/cd31f2dee2843776e485769ce85e0524716199bc)
* [core, miner: revert block gas counter in case of invalid transaction](https://github.com/bnb-chain/bsc/pull/1840/commits/77e33e5a49be99130a02dc72d6a0e4739fdd44d6)
* [accounts/usbwallet: mitigate ledger app chunking issue (#26773)](https://github.com/bnb-chain/bsc/pull/1840/commits/1e3177de220b1590704c96572fce820bfc87281e)
* [signer/core: accept all solidity primitive types for EIP-712 signing](https://github.com/bnb-chain/bsc/pull/1840/commits/02796f6bee7e014fd16ad39f0bcd3b665b51e0bb)
* [cmd/geth: enable log rotation (#26843)](https://github.com/bnb-chain/bsc/pull/1840/commits/7076ae00aa36ae250608455de928557ce4e5549f)
* [internal/ethapi: make EstimateGas use[latest] block by default (#24363)](https://github.com/bnb-chain/bsc/pull/1840/commits/0b66d47449f61e9ebaf9e1db3ed290b59844d4c1)
* [miner: suspend miner if node is syncing (#27218)](https://github.com/bnb-chain/bsc/pull/1840/commits/d4961881d7c92603f591f9cb8c705d00d8cbdfc0)
* [all: move main transaction pool into a subpool (#27463)](https://github.com/bnb-chain/bsc/pull/1840/commits/d40a255e973775575d8d16456252f93ac75c09f0)
* [core/txpool/blobpool: 4844 blob transaction pool (#26940)](https://github.com/bnb-chain/bsc/pull/1840/commits/1662228ac68325b4024e0cb6a4ce7dde27eb4c2d)
* [eth: send big transactions by announce/retrieve only (#27618)](https://github.com/bnb-chain/bsc/pull/1840/commits/f5d3d486e459dce29130576ae88f2324ad586b50)
* [core/rawdb: support freezer batch read with no size limit (#27687)](https://github.com/bnb-chain/bsc/pull/1840/commits/0b1f97e151e8b34a0a0d528a3472e27de1d12a9c)
* disable pipeCommit, break now

## v1.2.12
FEATURE
* [\#1852](https://github.com/bnb-chain/bsc/pull/1852) discov: add hardcoded bootnodes

BUGFIX
* [\#1844](https://github.com/bnb-chain/bsc/pull/1844) crypto: Update BLST to v0.3.11
* [\#1854](https://github.com/bnb-chain/bsc/pull/1854) fetcher: no import blocks before or equal to the finalized height
* [\#1855](https://github.com/bnb-chain/bsc/pull/1855) eth/tracers: trace system tx should add intrinsicGas

IMPROVEMENT
* [\#1839](https://github.com/bnb-chain/bsc/pull/1839) Update init-network command
* [\#1858](https://github.com/bnb-chain/bsc/pull/1858) vote: check consensus key match vote key before voting

## v1.2.11
FEATURE
* [\#1797](https://github.com/bnb-chain/bsc/pull/1797) client: add FinalizedHeader/Block to use the fast finality
* [\#1805](https://github.com/bnb-chain/bsc/pull/1805) vote: remove DisableBscProtocol and add flag to skip votes assmebling

BUGFIX
* [\#1829](https://github.com/bnb-chain/bsc/pull/1829) fix: lagging nodes failed to sync

## v1.2.10
FEATURE
* [\#1780](https://github.com/bnb-chain/bsc/pull/1780) log: reduce logs when receiving too much votes from a peer
* [\#1788](https://github.com/bnb-chain/bsc/pull/1788) metrics: add txpool config into metrics server
* [\#1789](https://github.com/bnb-chain/bsc/pull/1789) rpc: add GetFinalizedHeader/Block to simplify using the fast finality feature
* [\#1791](https://github.com/bnb-chain/bsc/pull/1791) finality: add more check to ensure result of assembleVoteAttestation
* [\#1795](https://github.com/bnb-chain/bsc/pull/1795) tool: add a tool extradump to parse extra data after luban

BUGFIX
* [\#1773](https://github.com/bnb-chain/bsc/pull/1773) discov: do not filter out bootnodes
* [\#1778](https://github.com/bnb-chain/bsc/pull/1778) vote: backup validator sync votes from corresponding mining validator
* [\#1784](https://github.com/bnb-chain/bsc/pull/1784) fix: exclude same votes when doing malicious voting check

## v1.2.9
FEATURE
* [\#1775](https://github.com/bnb-chain/bsc/pull/1775) upgrade: several hardfork block height on mainnet: Plato, Hertz(Berlin, London)

## v1.2.8
FEATURE
* [\#1626](https://github.com/bnb-chain/bsc/pull/1626) eth/filters, ethclient/gethclient: add fullTx option to pending tx filter
* [\#1726](https://github.com/bnb-chain/bsc/pull/1726) feat: support password flag when handling bls keys

BUGFIX
* [\#1734](https://github.com/bnb-chain/bsc/pull/1734) fix: avoid to block the chain when failed to send votes

## v1.2.7
FEATURE
* [\#1645](https://github.com/bnb-chain/bsc/pull/1645) lightclient: fix validator set change
* [\#1717](https://github.com/bnb-chain/bsc/pull/1717) feat: support creating a bls keystore from a specified private key
* [\#1720](https://github.com/bnb-chain/bsc/pull/1720) metrics: add counter for voting status of whole network

## v1.2.6
FEATURE
* [\#1697](https://github.com/bnb-chain/bsc/pull/1697) upgrade: block height of Hertz(London&Berlin) on testnet
* [\#1686](https://github.com/bnb-chain/bsc/pull/1686) eip3529tests: refactor tests
* [\#1676](https://github.com/bnb-chain/bsc/pull/1676) EIP-3529 (BEP-212) Unit tests for Parlia Config
* [\#1660](https://github.com/bnb-chain/bsc/pull/1660) feat: add a tool for submitting evidence of maliciousvoting
* [\#1623](https://github.com/bnb-chain/bsc/pull/1623) P2P: try to limit the connection number per IP address
* [\#1608](https://github.com/bnb-chain/bsc/pull/1608) feature: Enable Berlin EIPs
* [\#1597](https://github.com/bnb-chain/bsc/pull/1597) feature: add malicious vote monitor
* [\#1422](https://github.com/bnb-chain/bsc/pull/1422) core: port several London EIPs on BSC

IMPROVEMENT
* [\#1662](https://github.com/bnb-chain/bsc/pull/1662) consensus, core/rawdb, miner: downgrade logs
* [\#1654](https://github.com/bnb-chain/bsc/pull/1654) config: use default fork config if not specified in config.toml
* [\#1642](https://github.com/bnb-chain/bsc/pull/1642) readme: update the disk requirement to 2.5TB
* [\#1621](https://github.com/bnb-chain/bsc/pull/1621) upgrade: avoid to modify RialtoGenesisHash when testing in rialtoNet

BUGFIX
* [\#1682](https://github.com/bnb-chain/bsc/pull/1682) fix: set the signer of parlia to the most permissive one
* [\#1681](https://github.com/bnb-chain/bsc/pull/1681) fix: not double GasLimit of block upon London upgrade
* [\#1679](https://github.com/bnb-chain/bsc/pull/1679) fix: check integer overflow when decode crosschain payload
* [\#1671](https://github.com/bnb-chain/bsc/pull/1671) fix: voting can only be enabled when mining
* [\#1663](https://github.com/bnb-chain/bsc/pull/1663) fix: ungraceful shutdown caused by malicious Vote Monitor
* [\#1651](https://github.com/bnb-chain/bsc/pull/1651) fix: remove naturally finality
* [\#1641](https://github.com/bnb-chain/bsc/pull/1641) fix: support getFilterChanges after NewFinalizedHeaderFilter

## v1.2.5
BUGFIX
* [\#1675](https://github.com/bnb-chain/bsc/pull/1675) goleveldb: downgrade the version for performance

## v1.2.4
FEATURE
* [\#1636](https://github.com/bnb-chain/bsc/pull/1636) upgrade: block height of Luban on mainnet

## v1.2.3
FEATURE
* [\#1574](https://github.com/bnb-chain/bsc/pull/1574) upgrade: update PlatoUpgrade contracts code
* [\#1594](https://github.com/bnb-chain/bsc/pull/1594) upgrade: block height of Plato on testnet

IMPROVEMENT
* [\#866](https://github.com/bnb-chain/bsc/pull/866) code: x = append(y) is equivalent to x = y
* [\#1488](https://github.com/bnb-chain/bsc/pull/1488) eth/tracers, core/vm: remove `time` from trace output and tracing interface
* [\#1547](https://github.com/bnb-chain/bsc/pull/1547) fix: recently signed check when slashing unavailable validator
* [\#1573](https://github.com/bnb-chain/bsc/pull/1573) feat: remove supports for legacy proof type
* [\#1576](https://github.com/bnb-chain/bsc/pull/1576) fix: support golang 1.20 by upgrading prysm to v4
* [\#1578](https://github.com/bnb-chain/bsc/pull/1578) fix: output an error log when bsc extension fail to handshake
* [\#1583](https://github.com/bnb-chain/bsc/pull/1583) metrics: add a counter for validator to check work status of voting

BUGFIX
* [\#1566](https://github.com/bnb-chain/bsc/pull/1566) fix: config for VoteJournalDir and BLSWalletDir
* [\#1572](https://github.com/bnb-chain/bsc/pull/1572) fix: remove dynamic metric labels about fast finality
* [\#1575](https://github.com/bnb-chain/bsc/pull/1575) fix: make BLST PORTABLE for release binary
* [\#1590](https://github.com/bnb-chain/bsc/pull/1590) fix: fix snap flaky tests

## v1.2.2
It was skipped by a mistake, replaced by v1.2.3

## v1.2.1
IMPROVEMENT
* [\#1527](https://github.com/bnb-chain/bsc/pull/1527) log: revert a log back to trace level

## v1.2.0
FEATURE
* [\#936](https://github.com/bnb-chain/bsc/pull/936) BEP-126: Introduce Fast Finality Mechanism
* [\#1325](https://github.com/bnb-chain/bsc/pull/1325) genesis: add BEP174 changes to relayer contract
* [\#1357](https://github.com/bnb-chain/bsc/pull/1357) Integration API for EIP-4337 bundler with an L2 validator/sequencer
* [\#1463](https://github.com/bnb-chain/bsc/pull/1463) BEP-221: implement cometBFT light block validation
* [\#1493](https://github.com/bnb-chain/bsc/pull/1493) bep: update the bytecode of luban fork after the contract release

IMPROVEMENT
* [\#1486](https://github.com/bnb-chain/bsc/pull/1486) feature: remove diff protocol registration
* [\#1434](https://github.com/bnb-chain/bsc/pull/1434) fix: improvements after testing fast finality

BUGFIX
* [\#1430](https://github.com/bnb-chain/bsc/pull/1430) docker: upgrade alpine version & remove apks version
* [\#1458](https://github.com/bnb-chain/bsc/pull/1458) cmd/faucet: clear reqs list when reorg to lower nonce
* [\#1484](https://github.com/bnb-chain/bsc/pull/1484) fix: a deadlock caused by bsc protocol handeshake timeout

## v1.1.23
BUGFIX
* [\#1464](https://github.com/bnb-chain/bsc/pull/1464) fix: panic on using WaitGroup after it is freed

## v1.1.22
FEATURE
* [\#1361](https://github.com/bnb-chain/bsc/pull/1361) cmd/faucet: merge ipfaucet2 branch to develop

IMPROVEMENT
* [\#1412](https://github.com/bnb-chain/bsc/pull/1412) fix: init-network with config.toml without setting TimeFormat
* [\#1401](https://github.com/bnb-chain/bsc/pull/1401) log: support custom time format configuration
* [\#1382](https://github.com/bnb-chain/bsc/pull/1382) consnesus/parlia: abort sealing when block in the same height has updated
* [\#1383](https://github.com/bnb-chain/bsc/pull/1383) miner: no need to broadcast sidechain header mined by this validator

BUGFIX
* [\#1379](https://github.com/bnb-chain/bsc/pull/1379) UT: fix some flaky tests
* [\#1403](https://github.com/bnb-chain/bsc/pull/1403) Makefile: fix devtools install error
* [\#1381](https://github.com/bnb-chain/bsc/pull/1381) fix: snapshot generation issue after chain reinit from a freezer

## v1.1.21
FEATURE
* [\#1389](https://github.com/bnb-chain/bsc/pull/1389) upgrade: update the fork height of planck upgrade on mainnet

BUGFIX
* [\#1354](https://github.com/bnb-chain/bsc/pull/1354) fix: add some boundary check for security
* [\#1373](https://github.com/bnb-chain/bsc/pull/1373) tracer: enable withLog for TraceCall
* [\#1377](https://github.com/bnb-chain/bsc/pull/1377) miner: add fallthrough for switch cases

## v1.1.20
FEATURE
* [\#1322](https://github.com/bnb-chain/bsc/pull/1322) cmd/utils/flags.go: --diffsync flag is deprecate
* [\#1261](https://github.com/bnb-chain/bsc/pull/1261) tracer: port call tracer `withLog` to bsc

IMPROVEMENT
* [\#1337](https://github.com/bnb-chain/bsc/pull/1337) clean: Remove support for Ethereum testnet
* [\#1347](https://github.com/bnb-chain/bsc/pull/1347) upgrade: update the fork height of planck upgrade on testnet
* [\#1343](https://github.com/bnb-chain/bsc/pull/1343) upgrade: update system contracts' code of planck upgrade
* [\#1328](https://github.com/bnb-chain/bsc/pull/1328) upgrade: update system contracts' code of testnet
* [\#1162](https://github.com/bnb-chain/bsc/pull/1162) consensus: fix slash bug when validator set changing
* [\#1344](https://github.com/bnb-chain/bsc/pull/1344) consensus: fix delete the 1st validator from snapshot.recents list
* [\#1149](https://github.com/bnb-chain/bsc/pull/1149) feats: add ics23 proof support for cross chain packages
* [\#1333](https://github.com/bnb-chain/bsc/pull/1333) sec: add proof ops check and key checker

BUGFIX
* [\#1348](https://github.com/bnb-chain/bsc/pull/1348) core/txpool: implement additional DoS defenses

## v1.1.19
FEATURE
* [\#1199](https://github.com/bnb-chain/bsc/pull/1199) mointor: implement double sign monitor

IMPROVEMENT
* [\#1226](https://github.com/bnb-chain/bsc/pull/1226) eth, trie: sync with upstream v1.10.26 to solve snap sync issues
* [\#1212](https://github.com/bnb-chain/bsc/pull/1212) metrics: add miner info into metrics server
* [\#1240](https://github.com/bnb-chain/bsc/pull/1240) Add NewBatchWithSize API for db and use this API for BloomIndexer.Commit()
* [\#1254](https://github.com/bnb-chain/bsc/pull/1254) ci: update unmaintained tools to use maintained tools
* [\#1256](https://github.com/bnb-chain/bsc/pull/1256) ci: disable CGO_ENABLED when building binary
* [\#1274](https://github.com/bnb-chain/bsc/pull/1274) dep: bump the version of several important library
* [\#1294](https://github.com/bnb-chain/bsc/pull/1294) parlia : add a check for the length of extraData.
* [\#1298](https://github.com/bnb-chain/bsc/pull/1298) dep: update tendermint to v0.31.14

Document
* [\#1233](https://github.com/bnb-chain/bsc/pull/1233) doc: update readme
* [\#1245](https://github.com/bnb-chain/bsc/pull/1245) comments: add comments to clarify flags and byte codes
* [\#1266](https://github.com/bnb-chain/bsc/pull/1266) docs: update the readme to latest
* [\#1267](https://github.com/bnb-chain/bsc/pull/1267) docs: minor fix about the readme
* [\#1287](https://github.com/bnb-chain/bsc/pull/1287) docs: minor fix on geth links

BUGFIX
* [\#1233](https://github.com/bnb-chain/bsc/pull/1233) doc: update readme
* [\#1253](https://github.com/bnb-chain/bsc/pull/1253) fix comments: prune ancient compatibility, add prune ancient comments
* [\#1301](https://github.com/bnb-chain/bsc/pull/1301) fix: p2p sync with lagging peer
* [\#1302](https://github.com/bnb-chain/bsc/pull/1302) fix: eth fetcher re-queue issue

## v1.1.18
IMPROVEMENT
* [\#1209](https://github.com/bnb-chain/bsc/pull/1209) metrics: add build info into metrics server
* [\#1204](https://github.com/bnb-chain/bsc/pull/1204) worker: NewTxsEvent and triePrefetch reuse in mining task
* [\#1195](https://github.com/bnb-chain/bsc/pull/1195) hardfork: update Gibbs fork height and system contract code
* [\#1192](https://github.com/bnb-chain/bsc/pull/1192) all: sync with upstream v1.10.22
* [\#1186](https://github.com/bnb-chain/bsc/pull/1186) worker: improvement of the current block generation logic to get more rewards
* [\#1184](https://github.com/bnb-chain/bsc/pull/1184) worker: remove pre-seal empty block
* [\#1182](https://github.com/bnb-chain/bsc/pull/1182) Parlia: Some updates of the miner worker
* [\#1181](https://github.com/bnb-chain/bsc/pull/1181) all: sync with upstream v1.10.21
* [\#1177](https://github.com/bnb-chain/bsc/pull/1177) core/forkid: refactor nextForkHash function
* [\#1174](https://github.com/bnb-chain/bsc/pull/1174) worker: some code enhancement on work.go
* [\#1166](https://github.com/bnb-chain/bsc/pull/1166) miner: disable enforceTip when get txs from txpool

BUGFIX
* [\#1201](https://github.com/bnb-chain/bsc/pull/1201) worker: add double sign check for safety
* [\#1185](https://github.com/bnb-chain/bsc/pull/1185) worker: fix a bug of the delay timer

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
