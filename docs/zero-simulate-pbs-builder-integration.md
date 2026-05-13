# Zero-Simulate PBS Builder 集成说明

本文说明 BSC 仓库为 builder 构造 BidBlock 已提供的支持，以及 builder 仓库后续需要完成的集成工作。

## 目标

builder 需要在本地构造完整 BidBlock，并通过 `mev_sendBidBlock` 发送给 validator。validator 接收后不重新执行交易，只做结构校验、system tx 补签、seal，并在后插入路径验证执行结果。

目标调用链：

```text
builder worker
-> Parlia.PrepareForBuilder
-> 执行 builder 选中的用户交易
-> Parlia.FinalizeAndAssembleBidBlock
-> builder 封装 types.BidBlock
-> builder 签名 BidBlock.Hash()
-> validatorclient.SendBidBlock
```

## BSC 侧已提供的能力

### Header 准备

```go
func (p *Parlia) PrepareForBuilder(chain consensus.ChainHeaderReader, header *types.Header) error
```

作用：

- 复用 Parlia `Prepare` 的 header 推导逻辑。
- `Coinbase` 使用当前 parent snapshot 的 in-turn validator，而不是本地 `p.val`。
- 填充 `Difficulty / Time / MixDigest / Extra / Nonce` 等共识字段。
- 不修改 `consensus.Engine` 接口。

builder 侧需要在调用前准备：

- `header.Number`
- `header.ParentHash`
- `header.GasLimit`
- `header.BaseFee`

### unsigned system tx finalize

```go
func (p *Parlia) FinalizeAndAssembleBidBlock(
    chain consensus.ChainHeaderReader,
    header *types.Header,
    state *state.StateDB,
    body *types.Body,
    receipts []*types.Receipt,
    tracer *tracing.Hooks,
) (*types.Block, *big.Int, []*types.Receipt, error)
```

作用：

- 走完整 Parlia finalize 流程。
- 生成并执行 system tx，但不使用 validator key 签名。
- 返回包含 unsigned system tx 的 block。
- 返回 `actualGasFee`，作为 BidBlock `GasFee` 的诚实声明依据。

对应内部控制：

```text
FinalizeAndAssembleBidBlock
-> FinalizeAndAssembleWithOpts(SignSystemTx=false)
-> applyTransaction(mining=true, signSystemTx=false)
```

普通本地出块路径仍然是：

```text
FinalizeAndAssemble
-> FinalizeAndAssembleWithOpts(SignSystemTx=true)
```

### Validator 接收侧

validator 收到 BidBlock 后的关键路径：

```text
worker.commitWork
-> verifyBidBlockSystemTxs
-> commitBidBlock
-> bindSignBidBlockSystemTxs
-> Parlia.SignSystemTx
-> recompute TxHash
-> Seal
```

`commitBidBlock` 只覆盖 validator 负责的字段：

- `Extra`
- `UncleHash`
- `TxHash`

builder 执行结果字段会保留：

- `Root`
- `ReceiptHash`
- `Bloom`
- `GasUsed`
- `Time`
- `MixDigest`

## Builder 侧需要完成的工作

### Task 1：同步 BidBlock 基础类型和 Parlia helper

同步 BSC 仓库中的最小必要代码到 builder 仓库：

- `core/types.BidBlock`
- `core/types.BidBlockArgs`
- `consensus/parlia.PrepareForBuilder`
- `consensus/parlia.FinalizeAndAssembleBidBlock`
- `consensus/parlia.FinalizeAndAssembleWithOpts`
- `FinalizeOpts.SignSystemTx`
- `applyTransaction(... signSystemTx bool ...)`

建议保留核心单测：

- `FinalizeAndAssembleBidBlock` 生成 unsigned system tx。
- `SignSystemTx=true/false` 的 state root 一致。
- `PrepareForBuilder` 和 `Prepare` 在 in-turn validator 场景下关键字段一致。

### Task 2：在 builder worker 中生成 BidBlock

当前 builder 老路径大致是：

```text
worker.FinalizeAndAssemble
-> bidder.setBestWork
-> RawBid
-> validatorclient.SendBid
```

BidBlock 路径建议并行新增，不直接替换老路径：

```text
worker PrepareForBuilder
-> 执行用户交易，生成 state / receipts / sidecars
-> FinalizeAndAssembleBidBlock
-> builder 封装 BidBlock payload
-> bidder 保存 BidBlockArgs
```

注意点：

- `GasFee` 使用 `FinalizeAndAssembleBidBlock` 返回值。
- `Transactions` 必须是用户交易在前、unsigned system tx 在后。
- `Transactions` 需要用 `tx.MarshalBinary()` 编码成 `[]hexutil.Bytes`。
- `Header` 保留 finalize 后的执行结果字段。
- `Sidecars` 从 assembled block 或 work 环境带入。
- 旧 `RawBid` 路径先保留，用于 fallback。

### Task 3：签名和发送 BidBlock

在 builder 侧封装：

```text
BidBlock.Hash()
-> builder key ECDSA sign
-> types.BidBlockArgs{BidBlock, Signature}
```

新增 validator client：

```go
func (c *Client) SendBidBlock(ctx context.Context, args *types.BidBlockArgs) (common.Hash, error)
```

RPC method：

```text
mev_sendBidBlock
```

建议发送策略：

```text
优先 SendBidBlock
-> 如果返回 permission revoked / unsupported / 网络错误
-> fallback SendBid
```

permission 状态查询可以后续再接：

```text
mev_getBidBlockPermission
```

第一版不需要复杂状态机，只要保证 BidBlock 被拒时能降级到老 SendBid。

## 不建议放在 BSC 侧的封装

以下能力不建议继续放到 BSC consensus/miner 中：

- `SignBidBlock(block, builderKey)`：builder 私钥签名属于 builder 侧。
- `SendBidBlockClient`：RPC client 属于 builder 侧。
- `BuilderNonceTracker`：system tx nonce 已由 state 自然推进，单独 tracker 容易产生双轨状态。
- `ReadDistributionBasis(state)`：`FinalizeAndAssembleBidBlock` 已返回 `actualGasFee`。
- 大而全的 `BuildBidBlock`：等 builder 调用链稳定后再抽象更稳。

## 最小集成范围

第一版建议只完成：

```text
types + parlia helper 同步
builder worker 生成 BidBlock
builder 封装 BidBlock payload
builder 签名
SendBidBlock RPC
失败 fallback SendBid
```

预计 builder 仓库改动量约 300-500 行，主要风险集中在 Parlia unsigned system tx finalize 路径。其它部分多是 payload 封装和 RPC 调用。
