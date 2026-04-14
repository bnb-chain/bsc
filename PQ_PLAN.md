# Post-Quantum BSC — Implementation Plan

**Last updated:** 2026-04-13
**Branch:** `post_quantum_dev`
**Fork activation:** Timestamp-based via `PQForkTime` / `IsPQFork(num, time)`

---

## Status Overview

| Phase | Name | Status | Notes |
|-------|------|--------|-------|
| 0 | PQ Crypto Primitives | **Complete** | ML-DSA-44, ML-KEM-768, XMSS stubs |
| 1 | PQ Transaction Signing | **Complete** | Tx type `0x05`, `PQSigner`, `pqRecover` precompile |
| 1.5 | PQ Public Key Registry | **Complete** | `pqKeyRegistry` precompile at `0x70` |
| 2 | P2P Handshake: ML-KEM-768 | Pending | Design ready, not yet implemented |
| 3 | Fast Finality Voting: STARK Aggregation | **Complete** (placeholder prover) | STARK prover is a structural placeholder |
| 4 | Integration & Full Benchmark | Pending | Depends on production STARK prover |

---

## Phase 0 — PQ Crypto Primitives (Complete)

### Deliverables

| File | Description |
|------|-------------|
| `crypto/pq/mldsa/mldsa.go` | ML-DSA-44 (Dilithium): `GenerateKey`, `Sign`, `Verify`, `PublicKeyFromPrivate`, `PubKeyToAddress` |
| `crypto/pq/mldsa/mldsa_test.go` | Unit tests |
| `crypto/pq/mlkem/mlkem.go` | ML-KEM-768: `GenerateKey`, `Encapsulate`, `Decapsulate` |
| `crypto/pq/mlkem/mlkem_test.go` | Unit tests |
| `crypto/pq/xmss/xmss.go` | XMSS stubs (Sign, Aggregate, VerifyProof — returns "not implemented") |
| `crypto/crypto.go` | Top-level wrappers: `SignPQ`, `VerifyPQ`, `PQPubkeyToAddress` |
| `crypto/pq_signing_test.go` | Integration tests for crypto wrappers |

### Key Constants

| Parameter | Value |
|-----------|-------|
| ML-DSA-44 public key | 1312 bytes |
| ML-DSA-44 signature | 2420 bytes |
| ML-KEM-768 ciphertext | ~1088 bytes |

---

## Phase 1 — PQ Transaction Signing (Complete)

### Deliverables

| File | Description |
|------|-------------|
| `core/types/pq_transaction.go` | `PQTxData` (type `0x05`) with `From`, `PQSignature` fields |
| `core/types/transaction_signing_pq.go` | `PQSigner`, `pqDispatchSigner`, `SignPQTx` |
| `core/types/pq_transaction_test.go` | Sign/verify/sender recovery test |
| `core/pq_e2e_test.go` | Full chain E2E: genesis → PQ tx → block insert → balance check |
| `params/config.go` | `PQForkTime`, `IsPQFork()`, fork ordering |

### Design

- New tx type `0x05` (`PQTxData`) carries an explicit `From` field (20 bytes) and `PQSignature` (2420 bytes)
- `PQSigner.Sender()` looks up `From` in the PQ key registry, verifies ML-DSA-44 signature
- `pqDispatchSigner` wraps the base signer; dispatches PQ txs to `PQSigner`, delegates others to the base
- `MakeSigner()` and `LatestSigner()` auto-wrap with `pqDispatchSigner` when `IsPQFork` is active

### Precompile: `pqRecover` (`0x68`)

- Input: `[hash(32) || signature(2420) || pubkey(1312)]` → total 3764 bytes
- Output: address (32 bytes, zero-padded, address at bytes 12-31)
- Gas: 30,000
- In pre-PQFork maps: coexists with `doubleSignEvidence` via `pqRecoverCompat` (routes by input length)

---

## Phase 1.5 — PQ Public Key Registry (Complete)

### Precompile: `pqKeyRegistry` (`0x70`)

- **Register** (input = 1312 bytes): `caller → pubkey` write-once to state trie (41 x 32-byte slots)
  - Gas: `SstoreSetGas * 41` (~820k gas, one-time per address)
  - Rejects if already registered
- **Lookup** (input = 20 bytes): `query address → 1312-byte pubkey`
  - Gas: `SloadGas * 41` (~2.7k gas)
  - Returns zeros if unregistered
- State storage: `statedb.SetState(registryAddr, slot, value)` where `slot_i = keccak256(addr ++ i)` for `i in 0..40`
- Stateful precompile: implements `RunStateful(input, caller, stateDB, readOnly)`

### `PQSigner` Integration

`PQSigner.Sender(tx)` calls `pqKeyRegistryLookup(From)` to retrieve the pubkey, then verifies the ML-DSA-44 signature against the tx hash.

### Migration Path

1. User sends registration tx to `0x70` (one-time, ~820k gas)
2. Node writes `addr → pubkey` into registry state
3. User sends PQ txs (`PQTxData`) — no embedded pubkey, only 2440 bytes overhead vs 65 for secp256k1

---

## Phase 2 — P2P Handshake: ML-KEM-768 (Pending)

**Touch points:** `p2p/rlpx/rlpx.go`, `crypto/pq/mlkem/`

### 2.1 ML-KEM-768 Session Key Exchange

Replace ECDH inside `handshakeState` only. AES-CTR framing, MAC, multiplexing unchanged.

| Step | Current | Replacement |
|------|---------|-------------|
| Initiator key | ephemeral secp256k1 keypair | ML-KEM-768 encapsulation key |
| Auth message | ECIES-encrypted token + sig | ML-KEM ciphertext (1088 bytes) + identity sig |
| Shared secret | `ECDH(eph_priv, eph_pub)` | `Decapsulate(ciphertext)` → 32-byte shared secret |
| Derived secrets | `aesSecret`, `macSecret` | Same HKDF derivation |

**Files to create/modify:**
- `p2p/rlpx/rlpx.go`: add `runInitiatorPQ` / `runRecipientPQ`; `Conn.isPQ bool`; fallback to legacy if peer does not advertise `pq-rlpx`
- `crypto/pq/mlkem/mlkem.go`: already exists (Phase 0)

### 2.2 Node Identity

Node discovery (`p2p/enode/`) keeps secp256k1 node IDs — unchanged. Only session key establishment is replaced.

### 2.3 Benchmark Gate

- Handshake latency: ML-KEM vs ECDH under simulated 45-peer mesh
- Ciphertext size overhead on first-packet RTT

---

## Phase 3 — Fast Finality Voting: STARK Aggregation (Complete — Placeholder Prover)

**Touch points:** `core/vote/`, `core/types/vote.go`, `consensus/parlia/`, `core/vm/contracts.go`, `crypto/pq/proofs/`

### 3.1 STARK Proof System

| File | Description |
|------|-------------|
| `crypto/pq/proofs/stark_prover.go` | `STARKProver`, `GenerateSTARKProof`, `VerifySTARKProof`, Merkle tree utilities |

**Current state:** Placeholder implementation that builds Merkle commitments, FRI layer stubs, and query responses, but does **NOT** perform real STARK proving (no polynomial interpolation, no FRI commitment, no constraint evaluation). Marked with `TODO: Replace with a production STARK/leanVM prover`.

### 3.2 Data Structure Changes — `core/types/vote.go`

| Type | Description |
|------|-------------|
| `PQPublicKey [1312]byte` | ML-DSA-44 public key type |
| `PQSignature [2420]byte` | ML-DSA-44 signature type |
| `PQVoteEnvelope` | Individual validator PQ vote (VoteAddress, Signature, Data) |
| `PQVoteAttestation` | Aggregated PQ attestation (VoteAddressSet, AggProof, Data, Extra) |
| `PQVoteEnvelope.Verify()` | Verifies ML-DSA-44 signature against `Data.Hash()` |
| `PQVoteEnvelope.Hash()` | Computes envelope hash |

All existing BLS types (`VoteEnvelope`, `VoteAttestation`, `BLSPublicKey`, `BLSSignature`) are preserved for backward compatibility.

### 3.3 Vote Signing — `core/vote/pq_vote_signer.go`

| Function | Description |
|----------|-------------|
| `NewPQVoteSigner(pqKeyPath)` | Creates signer from private key file |
| `NewPQVoteSignerFromRawKey(privKey)` | Creates signer from raw bytes (for testing) |
| `SignVote(vote *PQVoteEnvelope)` | Signs with ML-DSA-44, populates VoteAddress + Signature |

### 3.4 STARK Signature Aggregation — `consensus/parlia/pq_stark_aggregation.go`

| Type/Function | Description |
|---------------|-------------|
| `PQVoteData` | Per-vote input: target/source block info + PQ sig/pubkey + validator index |
| `STARKSignatureAggregation` | Output: AggregateProof + CommitteeRoot + VoteDataHash + NumValidators |
| `STARKSignatureAggregator` | Stateful aggregator with mutex-protected prover |
| `Aggregate(votes, voteDataHash)` | Builds execution trace (7 columns), generates STARK proof, computes committee root |
| `Verify(agg, pubkeys, expectedVoteDataHash)` | Checks vote data hash binding → verifies STARK proof → validates committee root |
| `MarshalSTARKAggregation(agg)` | Binary serialization for header storage |
| `UnmarshalSTARKAggregation(data)` | Deserialization with bounds checks (H3 fix: numValidators≤1000, numFRI≤64, numQ≤1024, numAuth≤64) |
| `computeCommitteeRoot(pubkeys)` | SHA-256 Merkle tree over validator public keys |
| `hashSignatureData(sig, pubkey)` | SHA-256 commitment over signature + public key |

### 3.5 Vote Attestation Assembly & Verification — `consensus/parlia/pq_vote_attestation.go`

| Function | Description |
|----------|-------------|
| `pqAssembleVoteAttestation(chain, header)` | Collects PQ votes from pool, validates quorum (2/3), STARK aggregates, RLP-encodes into header Extra |
| `pqVerifyVoteAttestation(chain, header, parents)` | Extracts PQVoteAttestation from header, validates source/target blocks, checks quorum, verifies STARK proof |
| `getPQVoteAttestationFromHeader(header, chainConfig, epochLength)` | Decodes PQVoteAttestation from header Extra (handles epoch and non-epoch blocks) |

### 3.6 Consensus Fork Gating — `consensus/parlia/parlia.go`

Three fork-gated integration points:

| Line | Context | Logic |
|------|---------|-------|
| 439 | `getVoteAttestationFromHeader` | Post-PQFork: tries PQ decode first, converts `PQVoteAttestation → VoteAttestation` for downstream consumers |
| 765 | `verifyHeader` | `IsPQFork` → `pqVerifyVoteAttestation()` else `verifyVoteAttestation()` |
| 1770 | `Seal` (block production) | `IsPQFork` → `pqAssembleVoteAttestation()` else `assembleVoteAttestation()` |

### 3.7 Precompile: `pqAttestationVerify` (`0x6a`)

- Input: `[proof_len(4)] [proof_bytes] [vote_data_hash(32)] [num_pubkeys(4)] [{pubkey(1312)}...]`
- Full STARK verification: unmarshal → verify STARK proof → validate committee root → check vote data hash binding
- Registered in Hertz+ fork maps (not in Luban/Plato early fork maps)

### 3.8 Tests

| File | Tests |
|------|-------|
| `consensus/parlia/pq_stark_aggregation_test.go` | 8 unit tests + 1 benchmark: Basic, Verify, MismatchedPubkeys, EmptyVotes, MarshalUnmarshal, SingleVote, NilVerify, VoteDataHashMismatch, BenchmarkSTARKAggregation_21Validators |
| `consensus/parlia/pq_e2e_test.go` | 4 E2E tests (10 cases): FullFlow (21 validators full pipeline), NegativeCases (6 sub-tests), CommitteeRootDeterminism, IndividualSignatureVerification |

### 3.9 Review Fixes Applied

| ID | Issue | Fix |
|----|-------|-----|
| C3 | VoteDataHash not verified during Verify | Added `expectedVoteDataHash` param, enforced binding check (replay protection) |
| C4 | Non-deterministic committee root | Assembly now uses `snap.validators()` sorted order |
| C5 | Downstream consumers break post-fork | `getVoteAttestationFromHeader` converts PQVoteAttestation → VoteAttestation |
| H1 | Epoch blocks skipped in header extraction | Added epoch block handling mirroring legacy function |
| H2 | `BytesToHash` truncation for vote address | Changed to `crypto.Keccak256Hash` |
| H3 | Unmarshal OOM DoS | Added upper bounds on all array lengths |
| H4 | Precompile only checked committee root | Rewrote with full STARK verify flow |
| H5 | Precompile in early fork maps | Removed from Luban/Plato maps |

### 3.10 Known Limitations

- **STARK prover is a placeholder.** Structural correctness is ensured (Merkle commitments, data flow) but not cryptographic soundness. Must be replaced with a production STARK/leanVM prover before any public network deployment.
- **PQ assembly path consumes BLS-typed `[]*VoteEnvelope`.** The `VotePool` and `VoteManager` still deal in BLS-typed envelopes (`VoteEnvelope` with 48-byte pubkey / 96-byte sig). `pqAssembleVoteAttestation` adapts by reading `.Signature[:]` and `.VoteAddress[:]` — this works for the current bridge because the byte slicing is size-agnostic, but a full PQ vote pipeline should switch `VotePool`/`VoteManager` to `PQVoteEnvelope`.
- **XMSS not used.** The original plan mentioned leanXMSS; the current implementation uses ML-DSA-44 for vote signing (same as transaction signing) with STARK aggregation, which is a simpler and more practical approach.

---

## Phase 4 — Integration & Full Benchmark (Pending)

### 4.1 Prerequisites

- [ ] Production STARK prover (replace placeholder in `crypto/pq/proofs/stark_prover.go`)
- [ ] Full PQ vote pipeline: switch `VotePool` / `VoteManager` from `VoteEnvelope` to `PQVoteEnvelope`
- [ ] Phase 2 (P2P handshake) implementation

### 4.2 Combined Hardfork Activation

Gate all changes behind `PQForkTime` (already added to `params/config.go`).
- Devnet: `PQForkTime = 0` (already active)
- Testnet: future timestamp

### 4.3 Combined Benchmark Suite

Run all phases active simultaneously on devnet:

| Metric | Target |
|--------|--------|
| TPS ceiling | Block propagation stays < 0.45 s |
| Finality latency | Vote collect + aggregate + verify round-trip |
| Handshake overhead | Time-to-first-byte vs baseline |
| Tx overhead | 2440 bytes (with registry) vs 65 bytes (secp256k1) |

### 4.4 Open Questions

| # | Question | Notes |
|---|----------|-------|
| 1 | Production STARK prover: proof size, verify time? | Blocks Phase 4 |
| 2 | VotePool/VoteManager PQ migration strategy | Backward compat during fork transition |
| 3 | Gas repricing for `pqAttestationVerify` precompile | After production prover benchmarks |
| 4 | ML-KEM-768 handshake: discovery compatibility | Peer capability negotiation needed |

---

## File Inventory

### New Files

| File | Phase | Lines |
|------|-------|-------|
| `crypto/pq/mldsa/mldsa.go` | 0 | ML-DSA-44 primitives |
| `crypto/pq/mldsa/mldsa_test.go` | 0 | Tests |
| `crypto/pq/mlkem/mlkem.go` | 0 | ML-KEM-768 primitives |
| `crypto/pq/mlkem/mlkem_test.go` | 0 | Tests |
| `crypto/pq/xmss/xmss.go` | 0 | Stubs |
| `crypto/pq_signing_test.go` | 0 | Wrapper tests |
| `core/types/pq_transaction.go` | 1 | PQTxData type 0x05 |
| `core/types/transaction_signing_pq.go` | 1 | PQSigner, pqDispatchSigner |
| `core/types/pq_transaction_test.go` | 1 | Tests |
| `core/pq_e2e_test.go` | 1 | Chain-level E2E test |
| `core/vote/pq_vote_signer.go` | 3 | PQ vote signing |
| `crypto/pq/proofs/stark_prover.go` | 3 | STARK proof system (placeholder) |
| `consensus/parlia/pq_stark_aggregation.go` | 3 | STARK signature aggregation |
| `consensus/parlia/pq_vote_attestation.go` | 3 | PQ vote attestation assembly/verification |
| `consensus/parlia/pq_stark_aggregation_test.go` | 3 | STARK aggregation unit tests |
| `consensus/parlia/pq_e2e_test.go` | 3 | STARK aggregation E2E tests |
| `core/vm/pq_precompile_test.go` | 1 | pqRecover precompile tests |

### Modified Files

| File | Changes |
|------|---------|
| `params/config.go` | `PQForkTime`, `IsPQFork()`, fork ordering, `BuildBlockContext.IsPQ` |
| `core/types/vote.go` | PQ types: `PQPublicKey`, `PQSignature`, `PQVoteEnvelope`, `PQVoteAttestation` |
| `core/vm/contracts.go` | 3 precompiles: `pqRecover` (0x68), `pqAttestationVerify` (0x6a), `pqKeyRegistry` (0x70) |
| `consensus/parlia/parlia.go` | Fork gating at lines 439, 765, 1770 |
| `crypto/crypto.go` | `SignPQ`, `VerifyPQ`, `PQPubkeyToAddress` wrappers |
| `core/genesis.go` | `PQForkTime` genesis override |
| `eth/backend.go` | `PQForkTime` backend config override |

---

## Precompile Address Map

| Address | Name | Phase | Description |
|---------|------|-------|-------------|
| `0x68` | `pqRecoverCompat` | 1 | ML-DSA-44 signature → address recovery (coexists with doubleSignEvidence) |
| `0x6a` | `pqAttestationVerify` | 3 | STARK aggregate proof verification |
| `0x70` | `pqKeyRegistry` | 1.5 | PQ public key registration and lookup |
