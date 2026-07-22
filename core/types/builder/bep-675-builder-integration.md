This document describes the concrete builder-side integration details for the BidBlock path: API call chain, payload assembly, signature, permission lifecycle, error handling, and timing.

For the protocol design and high-level workflow, see [BEP-675: Builder-Proposed Block with Validator Blind Signing](https://github.com/bnb-chain/BEPs/blob/master/BEPs/BEP-675.md). This document does not restate the rationale covered there.

## Workflow

The builder's end-to-end flow for producing and submitting a BidBlock:

```
parent header / state
  → initialize header skeleton
  → Parlia.PrepareForBidBlock
        fills Coinbase / Difficulty / Time / MixDigest / Extra / Nonce
        (Extra will be overwritten by the validator)

  → select and execute user txs
        produces body.Transactions / receipts / updated state

  → Parlia.FinalizeAndAssembleBidBlock
        executes and appends unsigned system tx
        returns the complete block

  → block → builder.BidBlock
        Header       = block.Header()
        Transactions = tx.MarshalBinary()
        Sidecars     = block.Sidecars()

  → Sign BidBlock.Hash()
        produces builder.BidBlockArgs

  → mev_getBidBlockPermission(builder)
        (cached; if !allowed, fall back to mev_sendBid)

  → mev_sendBidBlock
```

## 1. Prepare the Header Skeleton

The builder first fills in the locally known fields, then calls `PrepareForBidBlock` to complete the Parlia consensus fields:

```go
header := &types.Header{
    ParentHash: parent.Hash(),
    Number:     new(big.Int).Add(parent.Number, common.Big1),
    GasLimit:   gasLimit,
    BaseFee:    baseFee,
}
err := parliaEngine.PrepareForBidBlock(chain, header)
```

`PrepareForBidBlock` writes `Coinbase` / `Difficulty` / `Time` / `MixDigest` / `Extra` / `Nonce`. `Coinbase` is taken from the parent snapshot's `inturnValidator()`, not the local `p.val` — the builder does not have a validator key.

## 2. Execute User Transactions

Transaction selection and EVM execution are entirely builder-driven; this specification does not constrain them. The builder runs selected user transactions against the parent state and maintains `state` / `receipts` / `body.Transactions` / `sidecars`.

## 3. Finalize (generate unsigned system tx)

```go
block, receipts, err := parliaEngine.FinalizeAndAssembleBidBlock(
    chain, header, state, body, receipts, tracer,
)
```

**Input:**

- The `header` / `state` / `body` / `receipts` after user txs have already been executed.

**Execution:**

1. Enters `finalizeAndAssemble(..., systemTxPacking)`.
2. Generates and executes the trailing system txs per Parlia rules:
   - `deposit` (always)
   - `distributeFinalityReward` (every 200 blocks)
   - `updateValidatorSetV2` (breathe blocks only)
3. The system tx genuinely executes on the EVM:
   - updates state
   - increases `header.GasUsed`
   - appends a receipt
   - affects `Root` / `ReceiptHash` / `Bloom` / `GasUsed`
4. However, in `systemTxPacking` mode the transactions are **not** signed with the validator key:
   - the system txs appended to the block are unsigned (v/r/s = 0, gasPrice = 0)
5. Computes the final state root and assembles the block from `header` + `body` + `receipts`.

**Output:**

- the complete block (with unsigned trailing system txs)
- the complete receipts

Signing does not affect EVM state transitions, so the execution results are identical whether the system transactions are signed (validator-mining path) or unsigned (builder packing path). The validator bind-signs these unsigned system txs at seal time and recomputes `TxHash`.

`GasFee` is not a wire field of `BidBlock`. The validator derives it from the `value` of the trailing `deposit` system transaction and uses it to rank competing BidBlocks for the same parent.

## 4. Assemble the BidBlock Payload

```go
txBytes := make([]hexutil.Bytes, len(block.Transactions()))
for i, tx := range block.Transactions() {
    enc, _ := tx.MarshalBinary()
    txBytes[i] = enc
}
bidBlock := &builder.BidBlock{
    Header:       block.Header(),
    Transactions: txBytes,
    Sidecars:     block.Sidecars(),
}
```

**Hard ordering constraint:** user txs come first, unsigned system txs come last.

## 5. Signing

```go
sig, _ := crypto.Sign(bidBlock.Hash().Bytes(), builderKey)
args := &builder.BidBlockArgs{BidBlock: bidBlock, Signature: sig}
```

A bare keccak digest, with no EIP-191/712 prefix, consistent with the existing `mev_sendBid`. The validator recovers the address using `args.EcrecoverSender()`.

## 6. Send and Fallback

### Check permission

Builders poll `mev_getBidBlockPermission` to determine whether the BidBlock path is currently open for them on a given validator, and fall back to legacy `mev_sendBid` when it is not.

The RPC does not surface permission denial through a JSON-RPC error; state is carried in the `allowed` field of the result. When `allowed` is false, `reason` identifies why. Current values:

- `insertchain_failed` — the last sealed BidBlock from this builder failed validator-side `InsertChain` (e.g. invalid state root, mismatched receipt hash, KZG proof failure).
- `gasprice_too_low` — the sealed BidBlock imported successfully, but its average gas price (excluding system transactions) was below the validator's configured minimum.
- `manual` — admin revoke via `admin_setBidBlockPermission`.

`mev_getBidBlockPermission` response:

```jsonc
{
  "allowed": false,
  "reason": "insertchain_failed",
  "blockHash": "0x...",
  "blockNumber": "0x123",
  "revokedAt": "2026-05-22T...",       // when the revoke happened
  "resetAt":   "2026-05-23T00:00:00Z"  // when the revoke expires (RPC returns the lockout expiry; builder may treat as informational)
}
```

### `mev_sendBidBlock` error mapping

The main BidBlock failure modes have dedicated JSON-RPC codes; match by code where possible. Code `-38001` remains a catch-all for parameter / pre-check errors, so still inspect the message for those.

| Error message contains | JSON-RPC code | Builder action |
| --- | --- | --- |
| `BidBlock disabled, fallback to SendBid` | -38001 | fallback to `mev_sendBid` |
| `builder BidBlock permission revoked, fallback to SendBid` | -38006 | permission not allowed; fallback to `mev_sendBid` |
| `pre-seal verify failed: ...` | -38007 | **fix build logic; do NOT retry the same BidBlock** |
| `too late, expected before ...` | -38008 | dropped; next slot |
| `the validator stop accepting bids ...` | -38003 | validator paused; retry later |
| `the validator is working on too many bids ...` | -38004 | validator busy/overloaded (admission timed out); retry later |
| `the validator is not in-turn ...` | -38005 | next slot or try another validator |
| `too many bids: exceeded limit of N bids per builder per block` | _(no dedicated code; plain JSON-RPC error)_ | per-builder quota hit (`mev_params.MaxBidsPerBuilder`); retry next slot or fall back to `mev_sendBid` |

**Recommended practice:**

- Poll `mev_getBidBlockPermission` once every 5–10 seconds and cache the current validator's BidBlock permission for that builder.
- For each validator, periodically query `mev_params` to check whether that validator has BidBlock enabled (`BidBlockEnabled` field). When disabled, treat it the same as permission denied and fall back to legacy `mev_sendBid`.

### BidBlock send window

The BidBlock path skips validator-side simulation, so the receive deadline is:

```
BidMustBefore = parent.MilliTimestamp + BlockInterval - DelayLeftOver  // 15ms
```

As the validator still needs µs-level time for signature recovery, tx decoding, pre-seal verification, and `Extra` overwrite before sealing, arrivals **exactly at** `BidMustBefore` may still miss the seal. We recommend builders leave a buffer of ≈100µs–1ms before `BidMustBefore`.

The transmission latency on the wire is not constant: the number of transactions and the number of blobs in the BidBlock both increase the payload size and therefore the time it takes to reach the validator. A larger BidBlock arrives later, so this transmission cost must be factored into the send-window control logic (e.g. estimate the on-wire delay from the current tx/blob count and bring the send time forward accordingly), rather than assuming a fixed offset before `BidMustBefore`.

## Summary

1. GasFee does not need to be provided explicitly; the validator derives it from the trailing deposit system tx.
2. The validator overwrites `Extra` and recomputes `TxHash` after bind-signing the system tx; all other header fields are used as-is from the builder.
3. The BidBlock send window is later than legacy `mev_sendBid` because no validator-side simulation is needed (see [BidBlock send window](#bidblock-send-window)).
4. BidBlock and the legacy bid share the same per-block quota, exposed via `mev_params.MaxBidsPerBuilder`.
5. Permission must be polled continuously (every 5–10 seconds is recommended); the cache is also invalidated whenever `mev_sendBidBlock` returns "permission revoked". When `mev_params.BidBlockEnabled == false`, treat it the same as permission denied.
6. The builder must handle BidBlock failure paths: (1) `mev_sendBidBlock` may return a direct error; (2) permission may be revoked, with the reason exposed by `mev_getBidBlockPermission`; (3) validator admin or local policy changes may later restore or revoke permission.
7. **Send the BidBlock as close to `BidMustBefore` as possible** (leaving the ≈100µs buffer noted above) — a later send leaves more time for transaction selection and execution, maximizing the value packed into the block.
