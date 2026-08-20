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

func recordLaneImported(c paymentlane.Commitment) {
	paymentLaneImportedQuotaGauge.Update(int64(c.PaymentLaneQuota))
	paymentLaneImportedGasUsedGauge.Update(int64(c.PaymentGasUsed))
	paymentLaneImportedIdleGauge.Update(int64(paymentlane.Budget{
		PaymentLaneQuota: c.PaymentLaneQuota,
		PaymentLaneUsed:  c.PaymentGasUsed,
	}.IdleLane()))
}

func laneReject(err error) error {
	if errors.Is(err, paymentlane.ErrStateUnavailable) {
		laneUnavailableCounter.Inc(1)
	} else {
		laneRejectedCounter.Inc(1)
	}
	return err
}

// LaneState is one block's lane state: the recursion inputs read from the parent, plus the
// payment total accumulated as the block executes.
//
// The zero value and a nil pointer both mean the lane is off, and every method is safe in that
// state, so no call site needs a fork branch. Reading the Budget field is not: do that only
// where the caller constructed the lane itself.
type LaneState struct {
	Budget           paymentlane.Budget
	governanceParams paymentlane.GovernanceParams
	signal           paymentlane.Signal
	classifier       *paymentlane.Classifier
	state            laneStateDB
	gasLimit         uint64
}

// laneStateDB is the live state: what the classifier reads, and whether reading it worked.
type laneStateDB interface {
	paymentlane.CodeReader
	Error() error
}

// ResolveLaneState derives one block's lane. One implementation for the importer and
// the producer on purpose: every input is a choice the two must make identically.
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
	signal, err := paymentlane.NewSignalFromParent(parent)
	if err != nil {
		return nil, err
	}
	return &LaneState{
		governanceParams: meta.GovernanceParams(),
		signal:           signal,
		classifier:       meta.NewClassifier(statedb),
		state:            statedb,
		gasLimit:         header.GasLimit,
	}, nil
}

// checkState reports a failed state read as the local fault it is, not the peer's: StateDB
// answers such a read with the zero code hash - which classifies as payment - and holds the
// error until Commit, after every verdict below.
func (ls *LaneState) checkState() error {
	if err := ls.state.Error(); err != nil {
		return fmt.Errorf("%w: %w", paymentlane.ErrStateUnavailable, err)
	}
	return nil
}

// VerifyHeaderQuota adjudicates a committed quota against its parent derivation - the whole of
// the lane that is settled before any transaction runs. It exists for BEP-675's blind-seal path,
// which has no execution state for the block it is about to sign and so cannot classify. statedb
// is used only to rebuild a parent-root-bound read-only StateDB for the governance params read.
func VerifyHeaderQuota(config *params.ChainConfig, parent, header *types.Header, statedb *state.StateDB) error {
	if !config.IsJenner(parent.Number, parent.Time) {
		return nil
	}
	c, err := paymentlane.Decode(header.UncleHash)
	if err != nil {
		return err
	}
	governanceParams, err := paymentlanemeta.LoadGovernanceParamsForQuota(config, parent, header, statedb)
	if err != nil {
		return err
	}
	signal, err := paymentlane.NewSignalFromParent(parent)
	if err != nil {
		return err
	}
	return signal.CheckNextLaneQuota(c.PaymentLaneQuota, governanceParams, header.GasLimit)
}

// On reports whether the lane binds this block.
func (ls *LaneState) On() bool { return ls != nil && ls.classifier != nil }

// SetQuota records the quota this block must reserve, for the producing side.
func (ls *LaneState) SetQuota() {
	if !ls.On() {
		return
	}
	ls.Budget.PaymentLaneQuota = ls.signal.NextLaneQuota(ls.governanceParams, ls.gasLimit)
}

// CheckQuota verifies a committed quota against the parent derivation and adopts it, for the
// importing side.
func (ls *LaneState) CheckQuota(committed uint64) error {
	if !ls.On() {
		return nil
	}
	if err := ls.signal.CheckNextLaneQuota(committed, ls.governanceParams, ls.gasLimit); err != nil {
		return err
	}
	ls.Budget.PaymentLaneQuota = committed
	return nil
}

func (ls *LaneState) Bounds() (floor, ceiling, safetyCap uint64) {
	if !ls.On() {
		return 0, 0, 0
	}
	return paymentlane.Bounds(ls.governanceParams, ls.gasLimit)
}

// GovernanceParams returns the tuple this node read from 0x2007 for this block.
func (ls *LaneState) GovernanceParams() paymentlane.GovernanceParams {
	if !ls.On() {
		return paymentlane.GovernanceParams{}
	}
	return ls.governanceParams
}

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

// VerifyImported is the importer's replay verdict on nodes that classify against trie-backed
// state; modes that skip that replay can still settle the committed quota exactly.
func (ls *LaneState) VerifyImported(totalGasUsed, poolUsed uint64, c paymentlane.Commitment) error {
	if !ls.On() {
		return nil
	}
	if err := ls.checkState(); err != nil {
		return err
	}
	return ls.Budget.VerifyCommitment(ls.gasLimit, totalGasUsed, poolUsed, c)
}

// WriteCommitmentAndVerify stamps the commitment onto an assembled block, then checks the block
// rule over it. It refuses a block that carries uncles, or whose hash was cached before the
// stamp - the hash is memoised on first read and never invalidated.
func (ls *LaneState) WriteCommitmentAndVerify(block *types.Block, poolUsed uint64) error {
	if !ls.On() {
		return nil
	}
	if len(block.Uncles()) != 0 {
		return errors.New("payment lane and uncles cannot share the uncle hash slot")
	}
	if err := ls.checkState(); err != nil {
		return err
	}
	block.SetUncleHash(paymentlane.Encode(paymentlane.Commitment{
		PaymentLaneQuota: ls.Budget.PaymentLaneQuota,
		PaymentGasUsed:   ls.Budget.PaymentLaneUsed,
	}))
	if block.Hash() != block.Header().Hash() {
		return errors.New("block hash was cached before the commitment was written")
	}
	return ls.Budget.Verify(ls.gasLimit, block.GasUsed(), poolUsed)
}
