package core

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/consensus"
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

// LaneState is one block's lane: the quota derived from the parent post-state, plus the payment
// total accumulated as the block executes.
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

// ResolveLaneState derives one block's lane. One implementation for the importer and the producer
// on purpose: nothing is committed, so both sides must reach the same quota independently.
func ResolveLaneState(config *params.ChainConfig, engine consensus.Engine, parent, header *types.Header, statedb *state.StateDB) (*LaneState, error) {
	if !config.IsJenner(parent.Number, parent.Time) {
		return &LaneState{}, nil
	}
	meta, err := paymentlanemeta.LoadMeta(config, header, statedb)
	if err != nil {
		return nil, err
	}
	return &LaneState{
		Budget:     paymentlane.Budget{PaymentLaneQuota: meta.Quota(header.GasLimit)},
		classifier: meta.NewClassifier(systemTxOracle(engine, header), statedb),
		state:      statedb,
		gasLimit:   header.GasLimit,
	}, nil
}

func systemTxOracle(engine consensus.Engine, header *types.Header) paymentlane.SystemTxOracle {
	posa, ok := engine.(consensus.PoSA)
	if !ok {
		return paymentlane.NoSystemTxs
	}
	return func(tx *types.Transaction) bool {
		isSystem, err := posa.IsSystemTransaction(tx, header)
		return err == nil && isSystem
	}
}

func (ls *LaneState) On() bool { return ls != nil && ls.classifier != nil }

// Classify must be called where the transaction is about to run: the code gate reads the live
// state, so producer and importer agree only if both ask at the same point in the sequence.
func (ls *LaneState) Classify(tx *types.Transaction) paymentlane.LaneType {
	if !ls.On() {
		return paymentlane.GeneralLane
	}
	return ls.classifier.Classify(tx)
}

// RecordUsedFrom books the gas the pool consumed since usedBefore.
func (ls *LaneState) RecordUsedFrom(laneType paymentlane.LaneType, gp *GasPool, usedBefore uint64) {
	if used := gp.Used(); ls.On() && used > usedBefore {
		ls.Budget.RecordUsed(laneType, used-usedBefore)
	}
}

// Admits takes shared as the shared remainder, i.e. gasPool.Gas().
func (ls *LaneState) Admits(shared uint64, laneType paymentlane.LaneType, txGasLimit uint64) bool {
	if !ls.On() {
		return true
	}
	return ls.Budget.Admits(shared, laneType, txGasLimit)
}

// VerifyPackedBid is the bid path's verdict on an environment it did not pack itself.
func (ls *LaneState) VerifyPackedBid(shared uint64) error {
	if !ls.On() {
		return nil
	}
	if idle := ls.Budget.IdleLane(); idle > shared {
		return fmt.Errorf("%w: idle lane %d exceeds the %d gas left in the pool", paymentlane.ErrViolated, idle, shared)
	}
	return nil
}

// Verify is the block validity rule over a finished block, for the producer's self-check and the importer's
// verdict alike. A failed state read is the local fault it is, not the peer's: StateDB answers
// such a read with the zero code hash - which classifies as payment - and holds the error until
// Commit, after every verdict here.
func (ls *LaneState) Verify(totalGasUsed uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.state.Error(); err != nil {
		return fmt.Errorf("%w: %w", paymentlane.ErrStateUnavailable, err)
	}
	return ls.Budget.Verify(ls.gasLimit, totalGasUsed)
}

func (ls *LaneState) recordImported() {
	if !ls.On() {
		return
	}
	paymentLaneImportedQuotaGauge.Update(int64(ls.Budget.PaymentLaneQuota))
	paymentLaneImportedGasUsedGauge.Update(int64(ls.Budget.PaymentLaneUsed))
	paymentLaneImportedIdleGauge.Update(int64(ls.Budget.IdleLane()))
}
