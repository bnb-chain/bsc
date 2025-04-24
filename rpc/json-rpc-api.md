## RPC API Reference

This document provides a comprehensive list of JSON-RPC API methods supported. Each API is listed with its required parameters and their formats.

### API Categories

| Command                                         | Parameters                                       |
|-------------------------------------------------|--------------------------------------------------|
| **Admin API**                                   |                                                  |
| admin_nodeInfo()                                | -                                                |
| admin_peers()                                   | -                                                |
| admin_addPeer(url)                              | `String`                                         |
|                                                 |                                                  |
| **Web3 API**                                    |                                                  |
| web3_clientVersion()                            | -                                                |
| web3_sha3(data)                                 | `String`                                         |
|                                                 |                                                  |
| **Network API**                                 |                                                  |
| net_listening()                                 | -                                                |
| net_peerCount()                                 | -                                                |
| net_version()                                   | -                                                |
|                                                 |                                                  |
| **Ethereum API (Chain State)**                  |                                                  |
| eth_blockNumber()                               | -                                                |
| eth_chainID/eth_chainId()                       | -                                                |
| eth_protocolVersion()                           | -                                                |
| eth_syncing()                                   | -                                                |
| eth_gasPrice()                                  | -                                                |
| eth_maxPriorityFeePerGas()                      | -                                                |
| eth_feeHistory()                                | -                                                |
|                                                 |                                                  |
| **Ethereum API (Blocks)**                       |                                                  |
| eth_getBlockByHash(...)                         | `BlockHash`, `Boolean`                           |
| eth_getBlockByNumber(...)                       | `BlockNumber\|Tag`, `Boolean`                    |
| eth_getBlockTransactionCountByHash(...)         | `BlockHash`                                      |
| eth_getBlockTransactionCountByNumber(...)       | `BlockNumber\|Tag`                               |
| eth_getUncleByBlockHashAndIndex(...)            | `BlockHash`, `Integer`                           |
| eth_getUncleByBlockNumberAndIndex(...)          | `BlockNumber\|Tag`, `Integer`                    |
| eth_getUncleCountByBlockHash(...)               | `BlockHash`                                      |
| eth_getUncleCountByBlockNumber(...)             | `BlockNumber\|Tag`                               |
|                                                 |                                                  |
| **Ethereum API (Transactions)**                 |                                                  |
| eth_getTransactionByHash(...)                   | `TxHash`                                         |
| eth_getRawTransactionByHash(...)                | `TxHash`                                         |
| eth_getTransactionByBlockHashAndIndex(...)      | `BlockHash`, `Integer`                           |
| eth_retRawTransactionByBlockHashAndIndex(...)   | `BlockHash`, `Integer`                           |
| eth_getTransactionByBlockNumberAndIndex(...)    | `BlockNumber\|Tag`, `Integer`                    |
| eth_retRawTransactionByBlockNumberAndIndex(...) | `BlockNumber\|Tag`, `Integer`                    |
| eth_getTransactionReceipt(...)                  | `TxHash`                                         |
| eth_getBlockReceipts(...)                       | `BlockNumber\|Tag`                               |
|                                                 |                                                  |
| **Ethereum API (State Reading)**                |                                                  |
| eth_estimateGas(...)                            | `TxCallObject`, `BlockNumber\|Tag`               |
| eth_getBalance(...)                             | `Address`, `BlockNumber\|Tag`                    |
| eth_getCode(...)                                | `Address`, `BlockNumber\|Tag`                    |
| eth_getTransactionCount(...)                    | `Address`, `BlockNumber\|Tag`                    |
| eth_getStorageAt(...)                           | `Address`, `Integer`, `BlockNumber\|Tag`         |
| eth_call(...)                                   | `TxCallObject`, `BlockNumber\|Tag`               |
| eth_createAccessList(...)                       | `TxCallObject`, `BlockNumber\|Tag`               |
| eth_simulateV1(...)                             | `BlockStateCalls`, `BlockNumber\|Tag`            |
|                                                 |                                                  |
| **Ethereum API (Filters)**                      |                                                  |
| eth_newFilter(...)                              | `FilterOptions`                                  |
| eth_newBlockFilter()                            | -                                                |
| eth_newPendingTransactionFilter()               | -                                                |
| eth_getFilterLogs(...)                          | `FilterId`                                       |
| eth_getFilterChanges(...)                       | `FilterId`                                       |
| eth_uninstallFilter(...)                        | `FilterId`                                       |
| eth_getLogs(...)                                | `FilterOptions`                                  |
|                                                 |                                                  |
| **Ethereum API (Account Operations)**           |                                                  |
| eth_accounts()                                  | -                                                |
| eth_sendRawTransaction(...)                     | `SignedTxData`                                   |
| eth_sendTransaction(...)                        | `TransactionObject`                              |
| eth_signTransaction(...)                        | `TransactionObject`                              |
| eth_signTypedData(...)                          | `Address`, `TypedData`                           |
| eth_getProof(...)                               | `Address`, `Array<StorageKey>`,`BlockNumber\|Tag`|
|                                                 |                                                  |
| **Ethereum API (Mining)**                       |                                                  |
| eth_mining()                                    | -                                                |
| eth_coinbase()                                  | -                                                |
| eth_hashrate()                                  | -                                                |
| eth_submitHashrate(...)                         | `HashRate`, `ClientID`                           |
| eth_getWork()                                   | -                                                |
| eth_submitWork(...)                             | `Nonce`, `PowHash`, `Digest`                     |
|                                                 |                                                  |
| **Ethereum API (Pub/Sub)**                      |                                                  |
| eth_subscribe(...)                              | `String`, `Object`                               |
| eth_unsubscribe(...)                            | `SubscriptionId`                                 |
|                                                 |                                                  |
| **Engine API**                                  |                                                  |
| engine_newPayloadV1(...)                        | `ExecutionPayloadV1`                             |
| engine_newPayloadV2(...)                        | `ExecutionPayloadV2`                             |
| engine_newPayloadV3(...)                        | `ExecutionPayloadV3`                             |
| engine_forkchoiceUpdatedV1(...)                 | `ForkchoiceState`, `PayloadAttributes`           |
| engine_forkchoiceUpdatedV2(...)                 | `ForkchoiceState`, `PayloadAttributes`           |
| engine_forkchoiceUpdatedV3(...)                 | `ForkchoiceState`, `PayloadAttributes`           |
| engine_getPayloadV1(...)                        | `PayloadId`                                      |
| engine_getPayloadV2(...)                        | `PayloadId`                                      |
| engine_getPayloadV3(...)                        | `PayloadId`                                      |
|                                                 |                                                  |
| **Debug API**                                   |                                                  |
| debug_getRawReceipts(...)                       | `BlockNumber\|Tag`                               |
| debug_accountRange(...)                         | `BlockNumber\|Tag`, `AccountKey`, `Integer`, `Boolean` |
| debug_accountAt(...)                            | `BlockNumber\|Tag`, `AccountIndex`               |
| debug_getModifiedAccountsByNumber(...)          | `StartBlock`, `EndBlock`                         |
| debug_getModifiedAccountsByHash(...)            | `StartHash`, `EndHash`                           |
| debug_storageRangeAt(...)                       | `BlockHash`, `TxIndex`, `Address`, `StartKey`, `Integer` |
| debug_traceBlockByHash(...)                     | `BlockHash`, `TraceConfig`                       |
| debug_traceBlockByNumber(...)                   | `BlockNumber\|Tag`, `TraceConfig`                |
| debug_traceTransaction(...)                     | `TxHash`, `TraceConfig`                          |
| debug_traceCall(...)                            | `TxCallObject`, `BlockNumber\|Tag`, `TraceConfig`|
|                                                 |                                                  |
| **Transaction Pool API**                        |                                                  |
| txpool_content()                                | -                                                |
| txpool_contentFrom(...)                         | `Address`                                        |
| txpool_status()                                 | -                                                |
|                                                 |                                                  |
| **BSC-Specific APIs**                           |                                                  |
| eth_getHeaderByNumber(...)                      | `BlockNumber\|Tag` with "finalized"              |
| eth_getBlockByNumber(...)                       | `BlockNumber\|Tag` with "finalized", `Boolean`   |
| eth_newFinalizedHeaderFilter()                  | -                                                |
| eth_getFinalizedHeader(...)                     | `VerifiedValidatorNum`                           |
| eth_getFinalizedBlock(...)                      | `VerifiedValidatorNum`, `Boolean`                |
|                                                 |                                                  |
| eth_getBlobSidecarByTxHash(...)                 | `TxHash`, `Boolean`                              |
| eth_getBlobSidecars(...)                        | `BlockNumber\|Tag\|BlockHash`, `Boolean`         |
|                                                 |                                                  |
| eth_health()                                    | -                                                |
| eth_getTransactionsByBlockNumber(...)           | `BlockNumber\|Tag`                               |
| eth_getTransactionDataAndReceipt(...)           | `TxHash`                                         |
|                                                 |                                                  |

### Parameter Types

- `BlockNumber` - Hexadecimal block number
- `Tag` - String tag like "latest", "earliest", "pending", "safe", "finalized"
- `BlockHash` - 32-byte hash of a block
- `TxHash` - 32-byte hash of a transaction
- `Address` - 20-byte Ethereum address
- `Boolean` - true or false
- `Integer` - Numeric value
- `String` - Text string
- `TxCallObject` - Transaction call object with from/to/gas/value/data
- `FilterOptions` - Options for event filtering
- `FilterId` - ID of a previously created filter
- `VerifiedValidatorNum` - Number of validators that must verify a block
- `TraceConfig` - Configuration options for tracing
- `BlockStateCalls` - Series of transactions to simulate with optional state and block overrides

### Notes

- Methods that show (...) have parameters that were simplified for readability.
- APIs marked with "BSC-Specific" are only available on BSC nodes.
- Parameters in backticks (`) represent the expected data type.
- Some methods accept multiple parameter formats; consult detailed documentation for complete usage.

