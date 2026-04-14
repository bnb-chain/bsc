# Post-Quantum BSC — Implementation Plan

Phase 0 and Phase 1 are **complete**. Phases 2 and 3 are pending.

---

## Phase 1.5 — PQ Public Key Registry (pending)

> **Why:** ML-DSA-44 public key is 1312 bytes. Embedding it in every transaction inflates
> each tx by ~3732 bytes (pubkey + sig) vs 65 bytes for secp256k1 — a 57× increase.
> This is measured as the "worst case" baseline in the Phase 1 benchmark, then eliminated
> by the registry so the optimised path only carries the 2420-byte signature per tx.

### Design

A system-level key-value store mapping `address → PQPublicKey` written directly into
the MPT state trie (same mechanism as contract storage), keyed under a reserved system
address (e.g. `0x0000…PQRegistry`).

```
Registry state layout (per account slot):
  key:   keccak256(addr)          // 32-byte slot key
  value: PQPublicKey (1312 bytes) // stored as consecutive 32-byte slots (41 slots)
```

### 1.5.1 — Registration Transaction

A new one-time tx type **or** a system contract call that writes the sender's PQ public
key into the registry. Two sub-options:

**Option A — System contract (recommended):** Deploy a precompiled system contract at
`0x0000…PQRegistry`. Registration is a normal tx `to=0x…PQRegistry, data=pubkey`.
Gas cost covers the 41-slot write (~20k gas × 41 = ~820k gas, one-time per address).

**Option B — Dedicated tx type `0x51`:** A `PQRegisterTxData` tx that carries only the
pubkey and is processed at the protocol level, bypassing EVM.

Go with **Option A** — no new tx type needed, simpler tooling, compatible with existing
wallets once they know the contract ABI.

### 1.5.2 — Modified `PQTxData` (post-registry)

Remove `PQPublicKey` from the tx payload. `Sender()` looks up the registry:

```go
// Before (Phase 1):
type PQTxData struct {
    ...
    PQPublicKey []byte  // 1312 bytes — removed after Phase 1.5
    PQSignature []byte  // 2420 bytes — stays
}

// After (Phase 1.5):
type PQTxData struct {
    ...
    PQSignature []byte  // 2420 bytes only
}
```

`PQSigner.Sender(tx)` flow after registry:
1. Compute candidate address from `tx.inner.(*PQTxData)` — not possible without pubkey,
   so sender address must be derivable another way.
2. **Bootstrapping problem:** without the pubkey we cannot derive the address.
   Solution: keep a 20-byte `From` field in `PQTxData` (explicit sender), then:
   a. Look up `registry[From]` → `pubKey`
   b. Verify `mldsa.Verify(pubKey, sigHash, PQSignature)`
   c. Return `From` if valid, error otherwise

This shifts the trust model: sender is asserted (like EIP-3074) and verified via
registry lookup + signature check, rather than derived from the signature.

```go
type PQTxData struct {
    ChainID     *big.Int
    Nonce       uint64
    GasPrice    *big.Int
    Gas         uint64
    To          *common.Address
    Value       *big.Int
    Data        []byte
    From        common.Address  // explicit sender (20 bytes)
    PQSignature []byte          // 2420 bytes
}
```

**Net tx overhead vs secp256k1:** 2420 + 20 = 2440 bytes vs 65 bytes (~37× baseline).

### 1.5.3 — Registry Precompile

**Decision: Go precompile** (faster, no EVM overhead, consistent with other BSC crypto precompiles).

**File:** `core/vm/contracts.go` — add `pqKeyRegistry` at `0x70`.

```
pqKeyRegistry.Run(input, caller, statedb):
  if len(input) == 1312:           // register: caller → pubkey
    if slot_0(caller) != zero:     // write-once: reject if already registered
      return nil, ErrAlreadyRegistered
    write 41 × 32-byte slots under registry address, keyed by keccak256(caller)
    return []byte{1}, nil
  if len(input) == 20:             // lookup: query address → pubkey
    read 41 slots, return 1312-byte pubkey (or zeros if unregistered)
    return pubkey, nil
  return nil, ErrInvalidInput

Gas:
  register: params.SstoreSetGas * 41  (~820k, one-time)
  lookup:   params.SloadGas * 41      (~2.7k, per tx validation)
```

State storage: `statedb.SetState(registryAddr, slot, value)` / `GetState`.
Slot derivation: `slot_i = keccak256(addr ++ i)` for `i` in `0..40`.

The precompile needs `StateDB` write access — it must be called as a stateful precompile
(same pattern as BSC's `tmHeaderValidate`). Register in all fork maps from `PQForkBlock`
onward alongside the existing BSC precompiles (0x64–0x69).

### 1.5.4 — `PQSigner` changes

- `core/types/transaction_signing_pq.go`: `Sender` takes a `StateDB` reader interface
  to look up the registry. This requires threading state into the signer — consistent
  with how `statedb` is already passed to `ApplyTransaction`.
- `SignPQTx` helper: before signing, assert `From` is registered (warn if not).

### 1.5.5 — Migration Path

| Step | Who | What |
|---|---|---|
| 1 | User | Send registration tx (one-time, ~820k gas) |
| 2 | Node | Write `addr → pubkey` into registry state |
| 3 | User | Send normal PQ txs (2440 bytes each, no pubkey) |
| 4 | Node | `Sender()` reads registry, verifies sig |

### 1.5.6 — Benchmark Comparison

Run Phase 1 benchmark twice:
- **Baseline:** `PQTxData` with embedded pubkey (3732 bytes overhead) — measures worst case
- **Optimised:** `PQTxData` with registry lookup (2440 bytes overhead) — measures target

Both answer: "at what TPS does block propagation exceed 0.45 s?"

### 1.5.7 — Decisions

| # | Decision |
|---|---|
| Key rotation | **Write-once:** if `registry[caller]` already exists, `Run` returns an error and reverts. No overwrite, no cooldown. Can be relaxed in a later version. |
| Genesis pre-registration | **Deferred:** skip for now; handle at local net deployment time. |

---

## Phase 2 — P2P Handshake: ML-KEM-768 (pending)

**Touch points:** `p2p/rlpx/rlpx.go`, `crypto/ecies/`

### 2.1 ML-KEM-768 Session Key Exchange

Replace ECDH inside `handshakeState` only. Everything above (AES-CTR framing, MAC,
multiplexing) is unchanged.

| Step | Current | Replacement |
|---|---|---|
| Initiator key | ephemeral secp256k1 keypair | ML-KEM-768 encapsulation key |
| Auth message | ECIES-encrypted token + sig | ML-KEM ciphertext (1088 bytes) + identity sig |
| Shared secret | `ECDH(eph_priv, eph_pub)` | `Decapsulate(ciphertext)` → 32-byte shared secret |
| Derived secrets | `aesSecret`, `macSecret` | Same HKDF derivation, same field names |

**Files to create/modify:**
- `p2p/rlpx/rlpx.go`: add `runInitiatorPQ` / `runRecipientPQ`; add `Conn.isPQ bool`;
  fallback to legacy handshake if peer does not advertise `pq-rlpx` capability.
- `crypto/pq/mlkem/mlkem.go`: already exists (Phase 0).

### 2.2 Node Identity
Node discovery (`p2p/enode/`) keeps secp256k1 node IDs — out of scope, unchanged.
Only the session key establishment is replaced.

### 2.3 Benchmark Gate — Phase 2
- Handshake latency: ML-KEM vs ECDH under simulated 45-peer mesh.
- Ciphertext size overhead on first-packet RTT.

---

## Phase 3 — Fast Finality Voting: leanXMSS / pqSNARK (pending)

**Touch points:** `core/vote/`, `core/types/vote.go`, `consensus/parlia/parlia.go`,
`core/vm/contracts.go`

### 3.1 Prerequisite: leanXMSS library
Replace the `crypto/pq/xmss/xmss.go` stub with a real implementation once the
leanVM/leanXMSS library is available. The stub interface is already in place.

### 3.2 Data Structure Changes — `core/types/vote.go`

```go
// Replace
type VoteEnvelope struct {
    VoteAddress types.BLSPublicKey   // 48 bytes  →  types.PQPublicKey (leanXMSS)
    Signature   types.BLSSignature   // 96 bytes  →  types.PQSignature (XMSS sig)
    Data        *VoteData
}

type VoteAttestation struct {
    VoteAddressSet uint64
    AggSignature   types.BLSSignature  // 96 bytes  →  AggProof []byte (pqSNARK proof)
    Extra          *VoteData
    VoteAddresses  []types.BLSPublicKey
}
```

### 3.3 Vote Signing — `core/vote/vote_signer.go`
- Replace `bls.Sign(key, data)` with `xmss.Sign(key, data)`.
- XMSS is stateful (one-time signatures per leaf); keystore must persist `TreeIndex uint64`.

### 3.4 Vote Aggregation — new `core/vote/pq_aggregator.go`
- Collect individual `VoteEnvelope` entries (same pool logic).
- Call `leanVM.Aggregate(sigs, pubkeys, msg) → proof`.
- Write `AggProof` into `VoteAttestation`.

### 3.5 Consensus Verification — `consensus/parlia/parlia.go`
- Replace `bls.FastAggregateVerify` with `leanVM.VerifyProof(proof, pubkeys, msg)`.
- May need to raise `extraVanity`/`extraSeal` header size cap for larger proof bytes.

### 3.6 Precompile Update — `core/vm/contracts.go`
- Add `pqAttestation` precompile at `0x69`.
  - Input: `proof || pubkeys || msg`
  - Gas: calibrate after benchmarks.

### 3.7 Benchmark Gate — Phase 3
- **Target:** end-to-end latency from 45 validators signing → pool collects → aggregate
  proof generated → `VerifyProof` completes.
- **Key unknown:** what is the leanXMSS aggregation latency across 45 validators?

---

## Phase 4 — Integration & Full Benchmark (pending)

### 4.1 Combined hardfork activation
- Gate all three changes behind `PQForkBlock` (already added to `params/config.go`).
- Devnet: `PQForkBlock = 0`. Testnet: future block.

### 4.2 Combined benchmark suite
Run all three active simultaneously on devnet:
- TPS ceiling (block propagation stays < 0.45 s).
- Finality latency (vote collect + aggregate + verify round-trip).
- Handshake overhead (time-to-first-byte vs baseline).

### 4.3 Open questions
| # | Question | Notes |
|---|---|---|
| 1 | leanXMSS library: proof size, verify time? | Blocks Phase 3 entirely |
| 2 | XMSS tree state management — one-time-key reuse risk | Security review needed |
| 3 | Gas repricing for `pqAttestation` precompile | After Phase 3 benchmarks |
