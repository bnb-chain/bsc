package core

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/paymentlanemeta"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
)

// Reported from the import path, the only one every node type shares.
var (
	paymentLaneImportedQuotaGauge   = metrics.NewRegisteredGauge("paymentlane/imported/paymentLaneQuota", nil)
	paymentLaneImportedGasUsedGauge = metrics.NewRegisteredGauge("paymentlane/imported/paymentGasUsed", nil)
	paymentLaneImportedIdleGauge    = metrics.NewRegisteredGauge("paymentlane/imported/paymentLaneIdle", nil)

	laneRejectedCounter    = metrics.NewRegisteredCounter("paymentlane/rejected", nil)
	laneUnavailableCounter = metrics.NewRegisteredCounter("paymentlane/stateUnavailable", nil)
)

func laneReject(err error) error {
	if errors.Is(err, paymentlane.ErrStateUnavailable) {
		laneUnavailableCounter.Inc(1)
	} else {
		laneRejectedCounter.Inc(1)
	}
	return err
}

// LaneState is one block's lane state: the quota derived from the parent post-state, plus the
// payment total accumulated as the block executes.
//
// The zero value and a nil pointer both mean the lane is off, and every method is safe in that
// state, so no call site needs a fork branch. Reading the Budget field is not: do that only
// where the caller constructed the lane itself.
type LaneState struct {
	Budget     paymentlane.Budget
	classifier *paymentlane.Classifier
	state      laneStateDB
	gasLimit   uint64
}

// laneStateDB is the live state: what the classifier reads, and whether reading it worked.
type laneStateDB interface {
	paymentlane.CodeReader
	Error() error
}

// ResolveLaneState derives one block's lane. One implementation for the importer and the
// producer on purpose: the quota is a pure function of the parent post-state's ratio and this
// block's gas limit, so both sides must reach the same value with no field to compare.
//
// The lane binds a block if and only if its parent is at or after activation (BEP-703 3.4.3):
// the fork installs 0x2007 while the activation block executes, so its post-state is the first
// to hold a ratio.
//
// statedb must be the block's own state, opened on the parent root and not yet advanced: the
// metadata read has to land on the witness-visible path, and classification then follows the same
// StateDB as it advances.
func ResolveLaneState(config *params.ChainConfig, parent, header *types.Header, statedb *state.StateDB) (*LaneState, error) {
	if !config.IsJenner(parent.Number, parent.Time) {
		return &LaneState{}, nil
	}
	meta, err := paymentlanemeta.LoadMeta(config, header, statedb)
	if err != nil {
		return nil, err
	}
	return &LaneState{
		Budget:     paymentlane.Budget{PaymentLaneQuota: meta.Quota(header.GasLimit)},
		classifier: meta.NewClassifier(statedb),
		state:      statedb,
		gasLimit:   header.GasLimit,
	}, nil
}

// On reports whether the lane binds this block.
func (ls *LaneState) On() bool { return ls != nil && ls.classifier != nil }

// Classify returns tx's lane type, or GeneralLane when the lane is off. Call it where the
// transaction is about to run: the code gate reads the live state, so producer and importer agree
// only if both ask at the same point in the sequence.
func (ls *LaneState) Classify(tx *types.Transaction) paymentlane.LaneType {
	if !ls.On() {
		return paymentlane.GeneralLane
	}
	return ls.classifier.Classify(tx)
}

// RecordUsedFrom books the gas the pool consumed since usedBefore, for a payment transaction; a
// general one is a no-op, general gas being the header residual.
func (ls *LaneState) RecordUsedFrom(laneType paymentlane.LaneType, gp *GasPool, usedBefore uint64) {
	if !ls.On() {
		return
	}
	ls.Budget.RecordUsed(laneType, gp.Used()-usedBefore)
}

// Admits reports whether this transaction may still be included, and admits everything while the
// lane is off. shared is the shared remainder, i.e. gasPool.Gas().
func (ls *LaneState) Admits(shared uint64, laneType paymentlane.LaneType, txGasLimit uint64) bool {
	if !ls.On() {
		return true
	}
	return ls.Budget.Admits(shared, laneType, txGasLimit)
}

// VerifyPackedBid is the bid path's verdict on an environment it did not pack itself. Sound
// only because that environment was re-executed locally, so the payment total is this node's
// own classification rather than the builder's word.
func (ls *LaneState) VerifyPackedBid(shared uint64) error {
	if !ls.On() {
		return nil
	}
	if idle := ls.Budget.IdleLane(); idle > shared {
		return fmt.Errorf("%w: idle lane %d exceeds the %d gas left in the pool", paymentlane.ErrViolated, idle, shared)
	}
	return nil
}

// Verify is BEP-703 3.3 over a finished block, for the producer's self-check and the importer's
// verdict alike. A failed state read is reported as the local fault it is, not the peer's:
// StateDB answers such a read with the zero code hash - which classifies as payment - and holds
// the error until Commit, after every verdict here.
func (ls *LaneState) Verify(totalGasUsed uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.state.Error(); err != nil {
		return fmt.Errorf("%w: %w", paymentlane.ErrStateUnavailable, err)
	}
	return ls.Budget.Verify(ls.gasLimit, totalGasUsed)
}

// recordImported publishes what this node replayed, once the block is judged valid.
func (ls *LaneState) recordImported() {
	if !ls.On() {
		return
	}
	paymentLaneImportedQuotaGauge.Update(int64(ls.Budget.PaymentLaneQuota))
	paymentLaneImportedGasUsedGauge.Update(int64(ls.Budget.PaymentLaneUsed))
	paymentLaneImportedIdleGauge.Update(int64(ls.Budget.IdleLane()))
}
