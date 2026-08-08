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
// the parameters.
type laneReader interface {
	paymentlane.AccountReader
	paymentlane.StorageReader
}

// laneHeaderReader resolves the header chain.
type laneHeaderReader interface {
	GetHeaderByHash(common.Hash) *types.Header
}

// LaneState is one block's lane state: the recursion inputs read from the parent, plus the
// payment total accumulated as the block executes.
// The zero value - and a nil pointer - mean the lane is off, and every method is safe in that
// state, so the only lane branch a call site needs is around the parent's commitment.
type LaneState struct {
	Budget   paymentlane.Budget
	cfg      paymentlane.Params
	signal   paymentlane.Signal
	class    *paymentlane.Classifier
	gasLimit uint64
}

// ResolveLaneState derives one block's lane inputs from the parent header, the grandparent and
// the parent post-state. One implementation for the importer and the producer on purpose: every
// input is a choice the two must make identically.
//
// reader must be bound to the parent post-state root, never the advancing StateDB - with the
// advancing state a producer inserts one cheap CREATE2 or SetCodeTx and thereby picks whose
// transfers enter the lane. Pass statedb.Reader(): pinned to originalRoot, and it inherits the
// reader dispatch the caller already got right, so archive, fastnode and UBT nodes are correct
// for free. Stateless execution is out of scope and fails closed, at the configuration read.
func ResolveLaneState(config *params.ChainConfig, hc laneHeaderReader, parent, header *types.Header, reader laneReader) (*LaneState, error) {
	if !config.IsGauss(parent.Number, parent.Time) {
		return &LaneState{}, nil
	}
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
func (ls *LaneState) SetQuota() {
	if !ls.On() {
		return
	}
	ls.Budget.LaneSize = ls.signal.NextLaneSize(ls.cfg, ls.gasLimit)
}

// CheckQuota verifies a committed quota against the parent derivation and adopts it, for the
// importing side.
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
func (ls *LaneState) Classify(tx *types.Transaction) (paymentlane.Class, error) {
	if !ls.On() {
		return paymentlane.ClassGeneral, nil
	}
	return ls.class.Classify(tx)
}

// RecordUsedFrom books the gas the pool consumed since usedBefore, for a payment transaction; a
// general one is a no-op, general gas being the header residual.
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
func (ls *LaneState) stickyErr() error { return ls.class.Err() }

// VerifyPackedBid is the bid path's verdict on an environment it did not pack: quota and sticky
// error together, because a caller that tested the quota alone would pass a bid whose bucket is
// unknown, and the miner would then decline to seal it after discarding a good local block.
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

// VerifyImported is the importer's verdict, and the only authoritative one.
func (ls *LaneState) VerifyImported(headerGasUsed, poolUsed uint64, c paymentlane.Commitment) error {
	if !ls.On() {
		return nil
	}
	return ls.Budget.VerifyCommitment(ls.gasLimit, headerGasUsed, poolUsed, c)
}

// WriteCommitment stamps the commitment onto an assembled block and self-checks it - the whole of
// the producing side's lane business after packing, which is what keeps core.AssembleBlock free of
// the lane.
func (ls *LaneState) WriteCommitment(block *types.Block, poolUsed uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.stickyErr(); err != nil {
		return err
	}
	if len(block.Uncles()) != 0 {
		return errors.New("payment lane and uncles cannot share the uncle hash slot")
	}
	block.SetUncleHash(paymentlane.Encode(paymentlane.Commitment{
		LaneSize:       ls.Budget.LaneSize,
		PaymentGasUsed: ls.Budget.PaymentUsed,
	}))
	if block.Hash() != block.Header().Hash() {
		return errors.New("block hash was cached before the commitment was written")
	}
	return ls.Budget.Verify(ls.gasLimit, block.GasUsed(), poolUsed)
}
