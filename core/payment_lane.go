package core

import (
	"errors"
	"fmt"
	"math/bits"

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
	paymentLaneImportedQuotaGauge         = metrics.NewRegisteredGauge("paymentlane/imported/paymentLaneQuota", nil)
	paymentLaneImportedGasUsedGauge       = metrics.NewRegisteredGauge("paymentlane/imported/paymentGasUsed", nil)
	paymentLaneImportedIdleGauge          = metrics.NewRegisteredGauge("paymentlane/imported/paymentLaneIdle", nil)
	paymentLaneImportedExecutionUsedGauge = metrics.NewRegisteredGauge("paymentlane/imported/paymentExecutionUsed", nil)
	paymentLaneImportedStateUsedGauge     = metrics.NewRegisteredGauge("paymentlane/imported/paymentStateUsed", nil)
	paymentLaneImportedExecutionIdleGauge = metrics.NewRegisteredGauge("paymentlane/imported/paymentExecutionIdle", nil)
	paymentLaneImportedStateIdleGauge     = metrics.NewRegisteredGauge("paymentlane/imported/paymentStateIdle", nil)

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
	Budget     paymentlane.Budget // quota and payment usage
	classifier *paymentlane.Classifier
	state      laneStateDB
	gasLimit   uint64
	amsterdam  bool
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
		amsterdam:  config.IsAmsterdam(header.Number, header.Time),
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

// RecordUsedFrom books payment resources consumed since the pool snapshot.
func (ls *LaneState) RecordUsedFrom(laneType paymentlane.LaneType, gp, before *GasPool) {
	if !ls.On() || laneType != paymentlane.PaymentLane {
		return
	}
	if !ls.amsterdam {
		if used, previous := gp.Used(), before.Used(); used > previous {
			ls.Budget.RecordUsed(laneType, used-previous)
		}
		return
	}
	execution, state := gp.CumulativeExecution(), gp.CumulativeState()
	if execution < before.CumulativeExecution() || state < before.CumulativeState() {
		return
	}
	ls.Budget.RecordUsedAmsterdam(laneType, execution-before.CumulativeExecution(), state-before.CumulativeState())
}

// Admits leaves each resource's unused payment quota available for payments.
func (ls *LaneState) Admits(gp *GasPool, laneType paymentlane.LaneType, txGasLimit uint64) bool {
	if !ls.On() {
		return true
	}
	if !ls.amsterdam {
		return ls.Budget.Admits(gp.Available(false), laneType, txGasLimit)
	}
	var idleExecution, idleState uint64
	if laneType != paymentlane.PaymentLane {
		idleExecution, idleState = ls.Budget.IdleLaneAmsterdam()
	}
	execution, executionCarry := bits.Add64(min(txGasLimit, params.MaxTxGas), idleExecution, 0)
	state, stateCarry := bits.Add64(txGasLimit, idleState, 0)
	return executionCarry == 0 && stateCarry == 0 && gp.CheckGasAmsterdam(execution, state) == nil
}

// VerifyPackedBid checks that the re-executed bid leaves the unused quota in the pool.
func (ls *LaneState) VerifyPackedBid(gp *GasPool) error {
	if !ls.On() {
		return nil
	}
	if ls.amsterdam {
		execution, state := ls.Budget.IdleLaneAmsterdam()
		if err := gp.CheckGasAmsterdam(execution, state); err != nil {
			return fmt.Errorf("%w: idle payment quota (execution %d, state %d): %v", paymentlane.ErrViolated, execution, state, err)
		}
		return nil
	}
	if idle, shared := ls.Budget.IdleLane(), gp.Available(false); idle > shared {
		return fmt.Errorf("%w: idle lane %d exceeds the %d gas left in the pool", paymentlane.ErrViolated, idle, shared)
	}
	return nil
}

// Verify is the block validity rule over a finished block, for the producer's self-check and the importer's
// verdict alike. A failed state read is the local fault it is, not the peer's: StateDB answers
// such a read with the zero code hash - which classifies as payment - and holds the error until
// Commit, after every verdict here.
func (ls *LaneState) Verify(totalGasUsed uint64, gp *GasPool) error {
	if !ls.On() {
		return nil
	}
	if err := ls.state.Error(); err != nil {
		return fmt.Errorf("%w: %w", paymentlane.ErrStateUnavailable, err)
	}
	if !ls.amsterdam {
		return ls.Budget.Verify(ls.gasLimit, totalGasUsed)
	}
	userGas := max(gp.CumulativeExecution(), gp.CumulativeState())
	if totalGasUsed < userGas || totalGasUsed > ls.gasLimit {
		return fmt.Errorf("%w: total gas %d user gas %d limit %d", paymentlane.ErrViolated, totalGasUsed, userGas, ls.gasLimit)
	}
	// Parlia adds system gas after the user pool's maximum, so it reduces both budgets.
	limit := ls.gasLimit - (totalGasUsed - userGas)
	return ls.Budget.VerifyAmsterdam(limit, gp.CumulativeExecution(), gp.CumulativeState())
}

func (ls *LaneState) recordImported() {
	var quota, used, idle, executionUsed, stateUsed, executionIdle, stateIdle uint64
	if ls.On() {
		quota = ls.Budget.PaymentLaneQuota
		used, idle = ls.Budget.PaymentLaneUsed, ls.Budget.IdleLane()
		if ls.amsterdam {
			executionUsed, stateUsed = ls.Budget.PaymentExecutionUsed, ls.Budget.PaymentStateUsed
			executionIdle, stateIdle = ls.Budget.IdleLaneAmsterdam()
			used, idle = max(executionUsed, stateUsed), max(executionIdle, stateIdle)
		}
	}
	paymentLaneImportedQuotaGauge.Update(int64(quota))
	paymentLaneImportedGasUsedGauge.Update(int64(used))
	paymentLaneImportedIdleGauge.Update(int64(idle))
	paymentLaneImportedExecutionUsedGauge.Update(int64(executionUsed))
	paymentLaneImportedStateUsedGauge.Update(int64(stateUsed))
	paymentLaneImportedExecutionIdleGauge.Update(int64(executionIdle))
	paymentLaneImportedStateIdleGauge.Update(int64(stateIdle))
}
