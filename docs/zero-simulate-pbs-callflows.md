# Zero-Simulate PBS Call Flows

本文只记录关键函数链路和每个新增 helper 的职责。大图容易空，后续 review 主要看这些调用边界。

## Summary

| 场景 | 调用链 | 结果 |
| --- | --- | --- |
| 本地出块 / simBid | `worker.commit` -> `engine.FinalizeAndAssemble` -> `FinalizeAndAssembleWithOpts(SignSystemTx=true)` -> `applyTransaction(mining=true, signSystemTx=true)` -> `signTxFn` | 生成并追加 signed system tx |
| Builder 准备 header | `PrepareForBuilder` -> `snapshot(parent)` -> `snap.inturnValidator()` -> `prepareHeader` | 得到 BidBlock header 骨架 |
| Builder 构造 BidBlock | `FinalizeAndAssembleBidBlock` -> `FinalizeAndAssembleWithOpts(SignSystemTx=false)` -> `applyTransaction(mining=true, signSystemTx=false)` | 执行 system tx，追加 unsigned system tx，返回 `block + actualGasFee` |
| Validator 接收 BidBlock | `commitBidBlock` -> `verifyBidBlockSystemTxs` -> `bindSignBidBlockSystemTxs` -> `Parlia.SignSystemTx` -> `TxHash` -> `Seal` | 补签 builder 的 unsigned system tx 并 seal |
| 导入验证 | `StateProcessor.Process` -> `Engine.Finalize` -> `applyTransaction(mining=false)` | 从 block 内读取 system tx，与本地 expected tx 比对 |

## Builder Helpers

### `PrepareForBuilder`

用途：builder 没有 validator key，不能依赖 `Prepare()` 里的 `p.val`，但又需要正确推导 in-turn validator、difficulty、timestamp 和 extra。

```text
Parlia.PrepareForBuilder(chain, header)
-> snapshot(chain, header.Number-1, header.ParentHash)
-> validator = snap.inturnValidator()
-> header.Coinbase = validator
-> header.Nonce = types.BlockNonce{}
-> prepareHeader(chain, header, snap, validator, number)
```

`prepareHeader` 复用 `Prepare()` 的公共逻辑：

```text
prepareHeader
-> header.Difficulty = CalcDifficulty(snap, validator)
-> parent = chain.GetHeader(header.ParentHash, number-1)
-> blockTime = blockTimeForRamanujanFork(snap, header, parent)
-> header.Time / MixDigest
-> nextForkHash
-> prepareValidators
-> prepareTurnLength
-> append extraSeal placeholder
```

差异点只有一个：

| 函数 | Coinbase / Difficulty 使用的 validator |
| --- | --- |
| `Prepare` | `p.val` |
| `PrepareForBuilder` | `snap.inturnValidator()` |

调用方仍负责提前设置：

- `header.Number`
- `header.ParentHash`
- `header.GasLimit`
- `header.BaseFee`

### `FinalizeAndAssembleBidBlock`

用途：builder 复用 Parlia finalize 流程，获得与 validator 本地出块一致的 state/result，但 system tx 不签名。

```text
Parlia.FinalizeAndAssembleBidBlock
-> gasFee = state.GetBalance(SystemAddress)
-> FinalizeAndAssembleWithOpts(SignSystemTx=false)
-> system tx wrappers
-> applyTransaction(mining=true, signSystemTx=false)
-> execute EVM
-> append unsigned system tx
-> return block, gasFee, receipts
```

关键性质：

- `SignSystemTx=false` 只跳过签名，不跳过 EVM 执行。
- `Root / ReceiptHash / Bloom / GasUsed` 应与 `SignSystemTx=true` 路径一致。
- `GasFee` 使用 finalize 前的 `SystemAddress` 余额，作为 BidBlock 声明值来源。

## Validator Paths

### 本地出块 / simBid

```text
worker.commit
-> engine.FinalizeAndAssemble
-> Parlia.FinalizeAndAssembleWithOpts(SignSystemTx=true)
-> system tx wrappers
-> applyTransaction(mining=true, signSystemTx=true)
-> signTxFn
-> execute EVM
-> append signed system tx
-> Seal
```

这条路径保持原语义：validator 自己生成并签名 system tx。

### BidBlock 补签

```text
worker.commitBidBlock
-> verifyBidBlockSystemTxs
   -> locate trailing unsigned system txs
   -> IsSignableSystemTx whitelist
   -> ExpectedSystemTxShape / VerifySystemTxShape
-> bindSignBidBlockSystemTxs
   -> Parlia.SignSystemTx
-> recompute TxHash
-> types.NewBlock(builder header, signed txs)
-> Seal
```

这里才是 BEP-675 语义里的 bind/blind sign：validator 给 builder 提供的 unsigned system tx 补签。它不执行 EVM，也不调用 `FinalizeAndAssemble`。

### 导入验证

```text
StateProcessor.Process
-> execute common txs
-> Engine.Finalize
-> applyTransaction(mining=false)
-> take actual system tx from receivedTxs
-> compare signer.Hash(expectedTx) with signer.Hash(actualTx)
```

导入验证不走 `FinalizeAndAssembleWithOpts`，也不会生成新签名。它只校验 block 已携带的 system tx 是否等于本地重算的 expected tx。

## Legacy SendBid

Builder 侧：

```text
worker strategy builds environment
-> bidder.newWork
-> Bidder.setBestWork
-> serialize work.txs into RawBid.Txs
-> RawBid{BlockNumber, ParentHash, GasUsed, GasFee}
-> Bidder.signBid
-> validatorclient.SendBid
-> RPC mev_sendBid
```

Validator 侧：

```text
MevAPI.SendBid
-> Miner.SendBid
-> Ecrecover builder
-> ExistBuilder
-> CheckPending
-> RawBid.ToBid
-> bidSimulator.sendBid
-> simBid executes txs
-> cache best simulated bid
-> worker.commitWork selection
```

`SendBid` 仍保留 validator-side simulation，是 `SendBidBlock` revoke 后的 fallback。

## Builder Integration Point

新旧路径建议在 builder 的 best work 发送阶段分叉：

```text
Bidder.getBestWork
-> query / cache BidBlock permission
-> allowed:
     PrepareForBuilder
     FinalizeAndAssembleBidBlock
     sign rlpHash(BidBlock)
     validatorclient.SendBidBlock
-> revoked / unsupported:
     build legacy BidArgs
     validatorclient.SendBid
```

这个分叉保持 BEP-675 的定位：`SendBidBlock` 是 `SendBid` 的增强补充，不替代老路径。
