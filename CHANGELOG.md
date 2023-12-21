# Changelog
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
