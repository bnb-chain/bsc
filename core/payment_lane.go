package core

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// laneReader is the lane's whole state capability: accounts for classification, storage words for
// the parameters. Deliberately no Code method - the lane must never load code - and *state.StateDB
// satisfies neither half, which is what keeps the advancing state out structurally. See
// ResolveLaneState.
type laneReader interface {
	paymentlane.AccountReader
	paymentlane.StorageReader
}

// laneHeaderReader resolves the grandparent, and has one method deliberately: *HeaderChain,
// *BlockChain and chainMaker all satisfy it, while canonical-only GetHeaderByNumber stays out of
// reach - on a reorg it answers with whatever header now occupies that height, a divergence no
// linear test chain can see. Do not widen it.
type laneHeaderReader interface {
	GetHeaderByHash(common.Hash) *types.Header
}

// LaneState is one block's lane state: the recursion inputs read from the parent, plus the
// payment total accumulated as the block executes. Build exactly one per block-building or
// block-processing attempt, via ResolveLaneState, and keep it on one goroutine - the classifier
// memo is a plain map. Sequential hand-off is fine and does happen, when the miner adopts a
// winning bid.
//
// The zero value - and a nil pointer - mean the lane is off, and every method is safe in that
// state, so the only lane branch a call site needs is around the parent's commitment.
type LaneState struct {
	// Budget accumulates this block's payment total. LaneSize is derived, never assigned by hand
	// in production code: SetQuota on the producing side, CheckQuota on the importing side, both
	// resolving to one paymentlane.Signal.NextLaneSize call over the same private inputs.
	Budget paymentlane.Budget

	cfg      paymentlane.Params
	signal   paymentlane.Signal
	class    *paymentlane.Classifier
	gasLimit uint64
}

// ResolveLaneState derives one block's lane inputs from the parent header, the grandparent and
// the parent post-state. One implementation for the importer and the producer on purpose: every
// input is a choice the two must make identically, and the recursion has memory, so one block of
// disagreement offsets the accumulator forever instead of healing.
//
// reader must be bound to the parent post-state root, never the advancing StateDB - with the
// advancing state a producer inserts one cheap CREATE2 or SetCodeTx and thereby picks whose
// transfers enter the lane. Pass statedb.Reader(): pinned to originalRoot, and it inherits the
// reader dispatch the caller already got right, so archive, fastnode and UBT nodes are correct
// for free. Stateless execution is out of scope and fails closed, at the configuration read.
//
// Scheduling Gauss on a NEW chain has a config-level trap - the lane can bind before the contract
// is installed, undetectably. docs/bep703-payment-lane.md section 4 has it, and
// TestLaneCannotDetectAnUninstalledContract pins it.
func ResolveLaneState(config *params.ChainConfig, hc laneHeaderReader, parent, header *types.Header, reader laneReader) (*LaneState, error) {
	// The rules bind from Gauss+1, the block whose PARENT is already Gauss: post-Feynman the Gauss
	// upgrade runs from Finalize, so while the activation block executes 0x2007 has no code yet.
	if !config.IsGauss(parent.Number, parent.Time) {
		return &LaneState{}, nil
	}
	// The grandparent decides one bit - whether the parent was itself a lane block - but that bit
	// selects between reading the parent's commitment and seeding from the floor, so getting it
	// wrong at the activation boundary splits the accumulator for good. Genesis passes through as
	// a nil grandparent, which NewSignalFromParent tells apart from an unresolved one; the
	// explicit number test is also what avoids GetHeader(hash, number-1) underflowing there.
	var grandparent *types.Header
	if parent.Number.Sign() != 0 {
		if grandparent = hc.GetHeaderByHash(parent.ParentHash); grandparent == nil {
			// Local fault, not a bad block: this node may simply not have the header yet.
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
	signal, err := paymentlane.NewSignalFromParent(config, grandparent, parent, parent.UncleHash)
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

// On reports whether the lane binds this block.
func (ls *LaneState) On() bool { return ls != nil && ls.class != nil }

// SetQuota records the quota this block must reserve, for the producing side.
//
// The result must NOT be clamped by the caller. The only quantity available to clamp against is
// the miner-local gas reservation, and the importer cannot see it: it derives the unclamped quota
// and rejects the block with ErrQuotaMismatch, while the producer's own self-check, run on the
// same clamped value, cannot notice.
func (ls *LaneState) SetQuota() {
	if !ls.On() {
		return
	}
	ls.Budget.LaneSize = ls.signal.NextLaneSize(ls.cfg, ls.gasLimit)
}

// CheckQuota verifies a committed quota against the parent derivation and adopts it, for the
// importing side. Adopting keeps one quota in play instead of two.
func (ls *LaneState) CheckQuota(committed uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.signal.CheckNextLaneSize(committed, ls.cfg, ls.gasLimit); err != nil {
		return err
	}
	ls.Budget.LaneSize = committed
	return nil
}

// Classify returns tx's lane class, or ClassGeneral when the lane is off.
//
// Safe to call twice for one transaction, and the producer does - the packing loop's band gate,
// then applyTransaction - because the answer is a pure function of the transaction bytes and the
// parent state, with the single state read memoised per destination once it succeeds. A failed
// read is deliberately not memoised, and the sticky error is what keeps that from mattering.
//
// Preserve that purity. The moment the answer depends on block-local state - a payment cap, a
// rate term, a count of what is already packed - two calls can differ, the budget a transaction
// was admitted against stops matching the bucket it is booked into, and every importer rejects.
func (ls *LaneState) Classify(tx *types.Transaction) (paymentlane.Class, error) {
	if !ls.On() {
		return paymentlane.ClassGeneral, nil
	}
	return ls.class.Classify(tx)
}

// RecordUsedFrom books the gas the pool consumed since usedBefore, for a payment transaction; a
// general one is a no-op, general gas being the header residual. Call it once per apply, with the
// sample taken immediately before that apply.
//
// It takes the pool rather than a delta so that the only callable form is the correct one:
// receipt.GasUsed and GasPool.CumulativeUsed() are both plausible and both wrong, and only Used()
// is the quantity that feeds header.GasUsed on the producing AND the importing side. Differencing
// also books zero for a reverted apply, which has already restored the pool, and cancels the bid
// path's temporary PayBidTxGasLimit reservation.
func (ls *LaneState) RecordUsedFrom(class paymentlane.Class, gp *GasPool, usedBefore uint64) {
	if !ls.On() {
		return
	}
	ls.Budget.RecordUsed(class, gp.Used()-usedBefore)
}

// Admits reports whether this transaction may still be included, and admits everything while the
// lane is off. shared is the shared remainder, i.e. gasPool.Gas().
func (ls *LaneState) Admits(shared uint64, class paymentlane.Class, txGasLimit uint64) bool {
	if !ls.On() {
		return true
	}
	return ls.Budget.Admits(shared, class, txGasLimit)
}

// stickyErr reports the first classifier state-read failure, and is the backstop for the one site
// that swallows a classification error: the miner's packing loop, which drops that account and
// carries on. Everywhere else the error is returned where it happens. VerifyPackedBid and
// WriteCommitment turn a swallowed one into a rejected bid and a refusal to seal.
//
// Unexported so nothing outside can pull the error out and decide to ignore it. Callers must
// already be past On().
func (ls *LaneState) stickyErr() error { return ls.class.Err() }

// VerifyPackedBid is the bid path's verdict on an environment it did not pack: quota and sticky
// error together, because a caller that tested the quota alone would pass a bid whose bucket is
// unknown, and the miner would then decline to seal it after discarding a good local block.
//
// IdleLane <= shared STRENGTHENS the block rule rather than restating it - it spends the miner's
// gas reservation, which the rule does not - and it is the same strengthened form the packing
// loop maintains one admission at a time, so the two sides agree. What catches the real system
// gas overrunning that reservation is WriteCommitment; docs/bep703-payment-lane.md carries the
// derivation.
//
// Deliberately not a per-transaction gate on the MEV path: refusing a builder's transaction
// because its declared LIMIT does not fit would reject bids the rule permits, the rule being
// about gas actually consumed and a bid being all-or-nothing anyway.
func (ls *LaneState) VerifyPackedBid(shared uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.stickyErr(); err != nil {
		return err
	}
	if idle := ls.Budget.IdleLane(); idle > shared {
		return fmt.Errorf("%w: idle lane %d exceeds the %d gas left in the pool", paymentlane.ErrViolated, idle, shared)
	}
	return nil
}

// VerifyImported is the importer's verdict, and the only authoritative one; see
// paymentlane.Budget.VerifyCommitment for why. headerGasUsed must be the locally recomputed
// total - the value Finalize grew - and never block.GasUsed(), which is attacker-supplied.
func (ls *LaneState) VerifyImported(headerGasUsed, poolUsed uint64, c paymentlane.Commitment) error {
	if !ls.On() {
		return nil
	}
	return ls.Budget.VerifyCommitment(ls.gasLimit, headerGasUsed, poolUsed, c)
}

// WriteCommitment stamps the commitment onto an assembled block and self-checks it - the whole of
// the producing side's lane business after packing, which is what keeps core.AssembleBlock free of
// the lane. An error means DISCARD the block, never repair it: the hash and inequality checks can
// only run after the stamp, so the block they refuse already carries it.
//
// Every sealing producer must call it, and must call it before ANYTHING hashes the block: the
// block hash is cached on first read with no invalidation ((*types.Block).Hash), so a later stamp
// leaves the block disagreeing with its own header. The hash check catches that rather than
// trusting the ordering, but catching it costs a slot, so the call belongs next to AssembleBlock.
//
// It reads block.GasUsed(), not the header the caller still holds, because the parlia commit path
// hands AssembleBlock a CopyHeader: that header keeps the user-transaction total while the
// assembled block carries the system-transaction gas Finalize added - the main term of the rule.
// The gas LIMIT comes from ls, the same value LaneSize was derived from, so the quota and the
// capacity it is checked against cannot come from two different headers.
func (ls *LaneState) WriteCommitment(block *types.Block, poolUsed uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.stickyErr(); err != nil {
		return err
	}
	// The two uses of the uncle slot are mutually exclusive, and preferring the commitment would
	// emit a block whose uncle list can never be verified again. Unreachable under parlia, which
	// forbids uncles outright; reachable from GenerateChain, whose downloader test chains add an
	// uncle every fifth block.
	if len(block.Uncles()) != 0 {
		return errors.New("payment lane and uncles cannot share the uncle hash slot")
	}
	block.SetUncleHash(paymentlane.Encode(paymentlane.Commitment{
		LaneSize:       ls.Budget.LaneSize,
		PaymentGasUsed: ls.Budget.PaymentUsed,
	}))
	// Hashing here freezes the cache with the commitment already in, which is safe because nothing
	// changes the header afterwards: Seal works on a copy (WithSeal), and WithSidecars only
	// aliases it.
	if block.Hash() != block.Header().Hash() {
		return errors.New("block hash was cached before the commitment was written")
	}
	return ls.Budget.Verify(ls.gasLimit, block.GasUsed(), poolUsed)
}
