This document describes how builders submit BEP-675 BidBlocks over gRPC: the API, payload encoding, signature, and response handling.

For BidBlock construction, permission checks, and the send window, see [Builder Integration](./bep-675-builder-integration.md). These rules also apply to gRPC submissions.

## 1. API

Submit requests to the gRPC endpoint provided by the MEV sentry operator:

```
builder → MEV sentry → validator
```

The gRPC port supports only `SendBidBlock` among the MEV APIs, through `/mev.v1.BidBlockService/SendBidBlock`. All other MEV calls, including `mev_params`, `mev_getBidBlockPermission`, and legacy `mev_sendBid`, must use the JSON-RPC endpoint. The existing JSON-RPC `mev_sendBidBlock` endpoint remains supported.

The protobuf definition and generated Go bindings are available in [mevpb](./mevpb/mev.proto):

```proto
service BidBlockService {
  rpc SendBidBlock(BidBlockRequest) returns (BidBlockResponse);
}

message BidBlockRequest {
  bytes bid_block_rlp = 1;
  bytes signature = 2;
  string validator_host_name = 3;
  reserved 4 to 10;
}

message BidBlockResponse {
  bytes bid_hash = 1;
}
```

## 2. Encode and Sign

Build the same `builder.BidBlock` used by `mev_sendBidBlock`, then RLP-encode it and sign its hash:

```go
payload, err := rlp.EncodeToBytes(bidBlock)
// Handle err before sending.
sig, err := crypto.Sign(bidBlock.Hash().Bytes(), builderKey)
// Handle err before sending.
```

`bid_block_rlp` contains the RLP encoding of the `BidBlock`, including its header, transactions, and sidecars. The signature is the same 65-byte signature used by JSON-RPC, with no EIP-191/712 prefix. Both fields carry raw bytes rather than hex strings.

## 3. Send

Using the generated `mevpb.BidBlockServiceClient` connected to the sentry endpoint:

```go
resp, err := client.SendBidBlock(ctx, &mevpb.BidBlockRequest{
    BidBlockRlp:       payload,
    Signature:         sig,
    ValidatorHostName: validatorHostName,
})
```

On success, `resp.BidHash` contains the 32-byte bid hash. A successful submission does not guarantee that the bid will be selected or included on-chain.

## 4. Errors

MEV business errors carry the original JSON-RPC error code in `google.rpc.ErrorInfo`:

```
Domain: "mev.bnbchain.org"
Reason: "-38008"           // example: BidBlock arrived too late
```

Use this code with the [existing error handling rules](./bep-675-builder-integration.md#mev_sendbidblock-error-mapping). The gRPC status message carries the error description. Transport failures may have no `ErrorInfo` and should be handled as gRPC errors.
