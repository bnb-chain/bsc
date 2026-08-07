// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// laneReader is the state capability BEP-703 needs: account presence for
// classification, storage words for the parameters. core/state.Reader satisfies it;
// taking the two paymentlane interfaces instead makes "the lane never loads code" a
// compile-time fact rather than a review comment. WHICH root the reader is bound to is
// a separate obligation, on ResolveLaneState's callers.
type laneReader interface {
	paymentlane.AccountReader
	paymentlane.StorageReader
}

// laneHeaderReader resolves the grandparent. Both *HeaderChain and *BlockChain
// satisfy it, which is what lets the importer and the miner share one resolver.
//
// One method, deliberately. The wrong way to resolve an ancestor is
// GetHeaderByNumber, which is canonical-only: on a reorg it answers with whatever
// header currently occupies that height rather than with this parent's parent, and the
// result is a divergence that no linear-chain test can see because every test chain is
// canonical. Narrowing the interface to the by-hash lookup makes that call unavailable
// instead of merely discouraged - a structural guard where a comment or a test would
// both be weaker.
type laneHeaderReader interface {
	GetHeaderByHash(common.Hash) *types.Header
}

// LaneState is everything BEP-703 needs for one block: the recursion inputs taken
// from the parent, plus the payment total accumulated as that block executes.
//
// Construct exactly one per block-building or block-processing attempt, via
// ResolveLaneState, and do not share it between goroutines - the classifier memo is a
// plain map. Sequential hand-off is fine and does happen, when the miner adopts a
// winning bid's environment.
//
// It deliberately does NOT store the derived quota alongside the inputs it came from.
// The producer calls SetQuota, the importer calls CheckQuota, and both resolve to the
// same LaneSize call over the same private inputs; precomputing it and having the
// importer compare against the stored copy would be a second implementation of
// CheckQuota with nothing keeping the two agreed. The gas limit is captured at
// construction for the same reason: it is the last input a call site could have chosen
// differently.
//
// The zero value - and a nil pointer - mean the lane does not apply, and every method
// here is safe to call in that state, so the only branch a call site needs is around the
// block-level prologue that decodes the parent's commitment.
//
// The importer's verdict and the producer's commitment write are methods rather than
// direct Budget calls for a second reason: WriteCommitment also drains the classifier's
// sticky error, which is the only thing that turns the packing loop's swallowed
// classification failure into a refusal to seal. Budget cannot see that error, so reaching
// through to it would pass on a block built with an unknown class.
type LaneState struct {
	// Budget accumulates this block's payment total. LaneSize is filled in by SetQuota
	// on the producing side and by CheckQuota on the importing side, never by hand.
	Budget paymentlane.Budget

	cfg      paymentlane.Params
	signal   paymentlane.Signal
	class    *paymentlane.Classifier
	gasLimit uint64
}

// ResolveLaneState derives the per-block lane inputs from the parent header, the
// grandparent header and the parent post-state.
//
// One implementation for both the importer and the producer, on purpose. Every input
// is a choice the two sides must make identically - which header, which state root,
// which gas limit - and the recursion has memory, so a single block of disagreement
// offsets the accumulator forever instead of healing. Two call sites each making those
// choices is exactly how that happens.
//
// reader must be bound to the parent post-state root and must never be the advancing
// StateDB: with the advancing state a producer inserts one cheap CREATE2 or SetCodeTx
// ahead of a batch of transfers and thereby chooses whose transfers enter the lane.
// Pass statedb.Reader() - it is pinned to originalRoot, immune to writes made during
// this block, and inherits whatever reader dispatch the caller already got right, so
// archive, fastnode and UBT nodes are correct for free. Reaching for a
// blockchain-level State* helper instead reintroduces the MPT-hardcoded path.
//
// Witness/stateless execution is out of scope and fails closed here: a witness records
// only the parent header, so the grandparent lookup below returns ErrStateUnavailable on
// every lane block, and 0x2007's storage nodes are never in the witness either because
// Reader() bypasses the tries a witness observes. Supporting it needs a depth-2 witness
// format, which is a separate decision.
//
// CAVEAT on the activation predicate, and it is a real one. Installing the contract gates
// on IsOnGauss, which agrees with the predicate below on every chain whose Gauss timestamp
// falls after its London block, but not unconditionally: if LondonBlock is 0 and GaussTime
// is at or before the genesis timestamp, then IsGauss already holds at genesis, IsOnGauss
// never fires, the contract is never installed - and the lane switches on from block 1
// regardless. paymentlane.LoadParams cannot detect it, because an absent account and an
// untouched one are both all-zero storage, so the chain runs the lane against a code-less
// address on hardcoded defaults with governance unable to change them. Real networks are
// safe (mainnet's LondonBlock is 31,302,048 and the devnet template's is 8), so this
// constrains new chain configurations rather than being a live defect - but it must be
// checked when Gauss is scheduled. core/paymentlane's
// TestLaneCannotDetectAnUninstalledContract pins it.
func ResolveLaneState(config *params.ChainConfig, hc laneHeaderReader, parent, header *types.Header, reader laneReader) (*LaneState, error) {
	// The rules bind from Gauss+1, i.e. from the block whose PARENT is already Gauss:
	// post-Feynman the Gauss upgrade runs from Finalize/FinalizeAndAssemble, so while the
	// activation block executes the contract has no code and no parameters can be read.
	if !config.IsGauss(parent.Number, parent.Time) {
		return &LaneState{}, nil
	}
	// The grandparent decides only one thing - whether the parent was itself a lane block,
	// which is the same IsGauss test one level up - but that one bit selects between "read
	// the parent's commitment" and "seed from the floor", so getting it wrong at the
	// activation boundary splits the accumulator for good. See laneHeaderReader for why the
	// canonical-only lookup is not reachable from here.
	//
	// Genesis is passed through as a nil grandparent rather than special-cased:
	// ParentSignal distinguishes "the parent is genesis" from "the caller could not
	// resolve the grandparent" itself, and duplicating that judgement here is how the
	// two answers get conflated. The explicit number test is what keeps this from
	// reaching for GetHeader(hash, number-1), which underflows to MaxUint64 at genesis.
	var grandparent *types.Header
	if parent.Number.Sign() != 0 {
		if grandparent = hc.GetHeaderByHash(parent.ParentHash); grandparent == nil {
			// Local fault, not a bad block: this node may simply not have the header
			// yet. Reporting it as unavailable state is what stops a transient miss
			// from being blamed on whoever sent the block.
			return nil, fmt.Errorf("%w: grandparent %x of block %d", paymentlane.ErrStateUnavailable, parent.ParentHash, header.Number)
		}
	}
	cfg, err := paymentlane.LoadParams(reader)
	if err != nil {
		return nil, err
	}
	listed, err := paymentlane.LoadPaymentContracts(reader)
	if err != nil {
		return nil, err
	}
	signal, err := paymentlane.ParentSignal(config, grandparent, parent, parent.UncleHash)
	if err != nil {
		return nil, err
	}
	return &LaneState{
		cfg:      cfg,
		signal:   signal,
		class:    paymentlane.NewClassifier(parent.Root, reader, listed),
		gasLimit: header.GasLimit,
	}, nil
}

// On reports whether the lane binds this block. Nil-safe, so callers that never
// resolved a state can still ask.
func (ls *LaneState) On() bool { return ls != nil && ls.class != nil }

// SetQuota records the quota this block must reserve, for the producing side.
//
// The result must NOT be clamped by the caller. The only quantity available to clamp
// against on that side is the miner-local gas reservation, which the validator cannot
// see: clamping shrinks IdleLane, general gets over-packed, and the block is invalid -
// while the producer's own self-check uses the same clamped value and therefore cannot
// notice.
func (ls *LaneState) SetQuota() {
	if !ls.On() {
		return
	}
	ls.Budget.LaneSize = paymentlane.LaneSize(ls.cfg, ls.signal, ls.gasLimit)
}

// CheckQuota verifies a committed quota and adopts it, for the importing side.
//
// Adopting the committed value rather than the derived one is safe precisely because
// they were just proved equal, and it keeps one value in play instead of two.
func (ls *LaneState) CheckQuota(committed uint64) error {
	if !ls.On() {
		return nil
	}
	if err := paymentlane.CheckLaneSize(committed, ls.cfg, ls.signal, ls.gasLimit); err != nil {
		return err
	}
	ls.Budget.LaneSize = committed
	return nil
}

// Classify returns tx's lane class, or ClassGeneral when the lane is off.
//
// It is safe to call more than once for the same transaction, and the producer does: once in
// the packing loop's band gate and once in applyTransaction. That holds only because the
// answer is a pure function of the transaction bytes and the parent state, with the single
// state read memoised per destination - so the two calls cannot disagree and the second is
// nearly free. The bidblock pre-seal ceiling is a third caller and relies on the same
// purity: it has no execution state, only the transaction list and the parent root.
//
// That is a property to preserve, not an invitation. The moment the answer depends on
// anything else - a per-block payment cap, a rate term, a count of what is already in
// the block - two calls can differ, the budget a transaction was admitted against stops
// matching the bucket it is booked into, and every importer rejects the block. Anything
// like that has to thread one decision through instead.
func (ls *LaneState) Classify(tx *types.Transaction) (paymentlane.Class, error) {
	if !ls.On() {
		return paymentlane.ClassGeneral, nil
	}
	return ls.class.Classify(tx)
}

// AccountFrom books the gas the pool has consumed since usedBefore, for a payment
// transaction; a general one is a no-op, general gas being the header residual. Call it
// once per apply, with the sample taken immediately before that apply.
//
// It takes the pool rather than a delta so that the only callable form is the correct
// one. receipt.GasUsed and GasPool.CumulativeUsed() are both plausible and both wrong:
// only Used() is the quantity that feeds header.GasUsed on the producing AND the
// importing side, which is what lets the importer compare this figure against its own
// replay at all. Differencing also makes rollback free - a reverted apply has
// already restored the pool from its snapshot, so the delta is zero - and it cancels the
// bid path's temporary PayBidTxGasLimit reservation, which would otherwise offset an
// absolute reading by exactly 25,000.
func (ls *LaneState) AccountFrom(class paymentlane.Class, gp *GasPool, usedBefore uint64) {
	if !ls.On() {
		return
	}
	ls.Budget.Account(class, gp.Used()-usedBefore)
}

// Admits reports whether a transaction of this class and gas limit may still be
// included. shared is the shared remainder, i.e. gasPool.Gas().
func (ls *LaneState) Admits(shared uint64, class paymentlane.Class, gasLimit uint64) bool {
	if !ls.On() {
		return true
	}
	return ls.Budget.Admits(shared, class, gasLimit)
}

// Err reports the first classifier state-read failure, if any.
//
// Sticky, and the backstop for the one site that swallows a classification error: the
// miner's packing loop drops that account and carries on, because refusing to build is
// not a decision the loop should take. Everywhere else the error is returned where it
// happens. WriteCommitment turns a swallowed one into a refusal to seal, and
// VerifyPackedBid into a rejected bid - the earlier and cheaper of the two.
func (ls *LaneState) Err() error {
	if !ls.On() {
		return nil
	}
	return ls.class.Err()
}

// VerifyPackedBid is the bid path's verdict on an environment it did not pack itself. The
// sticky error is checked here and not only at seal time because a caller that tested the
// quota alone would pass a bid whose bucket is unknown, and the miner would then decline to
// seal it after discarding a perfectly good local block.
//
// The quota half is the invariant the packing loop maintains one admission at a time,
// restated as a single end-of-block test for a path that cannot gate per transaction.
// With C the pool's capacity, r the miner's gas reservation and shared the remainder,
// IdleLane <= shared is exactly poolUsed + IdleLane <= C = GasLimit - r, which implies the
// block rule poolUsed + systemGasUsed + IdleLane <= GasLimit whenever the real system gas
// stays inside r. It is therefore a STRENGTHENING of the rule, not an equivalent of it: at
// C=1000, r=25, poolUsed=990, IdleLane=15 this refuses a block the rule permits. The
// packing loop maintains the same strengthened form, so the two sides agree, and what
// catches the real system gas overrunning r is WriteCommitment.
//
// Deliberately not a per-transaction gate on the MEV path: rejecting a builder's
// transaction because its declared LIMIT does not fit would refuse bids the rule permits,
// since the rule is about gas actually consumed and a bid is all-or-nothing anyway.
func (ls *LaneState) VerifyPackedBid(shared uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.Err(); err != nil {
		return err
	}
	if idle := ls.Budget.IdleLane(); idle > shared {
		return fmt.Errorf("%w: idle lane %d exceeds the %d gas left in the pool", paymentlane.ErrViolated, idle, shared)
	}
	return nil
}

// VerifyImported is the importer's verdict, and the only authoritative one; see
// paymentlane.Budget.VerifyCommitment for why.
//
// headerGasUsed must be the locally recomputed total - the value Finalize grew - and never
// block.GasUsed(), which is attacker-supplied.
func (ls *LaneState) VerifyImported(headerGasUsed, poolUsed uint64, c paymentlane.Commitment) error {
	if !ls.On() {
		return nil
	}
	return ls.Budget.VerifyCommitment(ls.gasLimit, headerGasUsed, poolUsed, c)
}

// WriteCommitment stamps the commitment onto an assembled block and self-checks it. It is
// the whole of the producing side's lane business after packing, which is what keeps
// core.AssembleBlock - upstream code - free of the lane.
//
// Every sealing producer must call it, and must call it before ANYTHING hashes the block:
// the block hash is cached on first read with no invalidation (see
// (*types.Block).SetUncleHash), so a later stamp leaves the block disagreeing with its own
// header. The check below catches that rather than trusting the ordering, but catching it
// costs a slot, so the call belongs next to AssembleBlock.
//
// Writing and checking are one method because forgetting either produces an invalid block,
// and because it makes the order unforgettable: check-before-stamp cannot compile into
// anything that works, since the commitment it would decode is not there yet. Failing here
// costs a slot; producing anyway is worse, because the payment total goes into the
// commitment, every importer replays it and rejects, and the producer has by then set its
// own head to the block and broadcast it.
//
// An error means DISCARD the block, never repair it: both checks run after the stamp - the
// stale-hash one cannot run before it without itself filling the cache - so the block it
// refuses is already stamped.
//
// It reads block.GasUsed() rather than the header the caller still holds, because the
// parlia commit path hands AssembleBlock a CopyHeader: that header keeps the
// user-transaction total forever while the assembled block carries the system-transaction
// gas Finalize added. That total is the main term of the rule, so reading the stale header
// here would check a different inequality than the importer will. The gas LIMIT comes from
// ls instead, the same value LaneSize was derived from, so the quota and the capacity it
// is checked against cannot come from two different headers.
func (ls *LaneState) WriteCommitment(block *types.Block, poolUsed uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.Err(); err != nil {
		return err
	}
	// The two uses of the uncle slot are mutually exclusive, and silently preferring the
	// commitment would emit a block whose uncle list can never be verified again.
	// Unreachable under parlia, which forbids uncles outright; reachable from
	// GenerateChain, which still offers BlockGen.AddUncle - and eth/downloader's test
	// chains use it on every fifth block, which is why a lane-active variant of that
	// harness is not a small change.
	if len(block.Uncles()) != 0 {
		return errors.New("payment lane and uncles cannot share the uncle hash slot")
	}
	block.SetUncleHash(paymentlane.Encode(paymentlane.Commitment{
		LaneSize:       ls.Budget.LaneSize,
		PaymentGasUsed: ls.Budget.PaymentUsed,
	}))
	// Hashing here also freezes the cache with the commitment already in, which is safe
	// because every later header change goes through a copy: Seal takes block.Header() and
	// returns WithSeal, and WithSidecars builds a new block.
	if block.Hash() != block.Header().Hash() {
		return errors.New("block hash was cached before the commitment was written")
	}
	return ls.Budget.Verify(ls.gasLimit, block.GasUsed(), poolUsed)
}
