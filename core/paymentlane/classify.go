package paymentlane

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// SystemTxOracle is BEP-703 section 3.2's first gate. On a valid block the engine's system
// transactions are exactly the ones it built itself, so the oracle reads no state.
type SystemTxOracle func(tx *types.Transaction) bool

// NoSystemTxs is the oracle for an engine that appends none. That is a statement about the chain,
// not a stub.
func NoSystemTxs(*types.Transaction) bool { return false }

// CodeReader must be the LIVE state of the block being classified, not the parent's: against the
// parent, a transfer to an address this same block gave code to - by deployment or by an EIP-7702
// authorisation - counted as a payment and ran that code inside the quota.
type CodeReader interface {
	GetCodeHash(addr common.Address) common.Hash
}

// Classifier answers "payment lane or general lane" over two state views. listed comes from the
// PARENT post-state and only ever decides PaymentLane, stopping there; everything past it is
// decided against the LIVE state - so no transaction's lane type rests on both. Membership must be
// settled before the block runs, or whoever orders it decides who is on the list.
type Classifier struct {
	isSystemTx SystemTxOracle
	code       CodeReader
	listed     map[common.Address]struct{}
}

// NewClassifier panics on a nil oracle rather than defaulting one: a silently absent system-
// transaction gate is the one failure that books consensus gas against the payment quota.
func NewClassifier(isSystemTx SystemTxOracle, code CodeReader, listed map[common.Address]struct{}) *Classifier {
	if isSystemTx == nil {
		panic("payment lane classifier needs a system-transaction oracle; pass NoSystemTxs when the engine appends none")
	}
	return &Classifier{isSystemTx: isSystemTx, code: code, listed: listed}
}

// Classify runs BEP-703 section 3.2's gates in order, arranged to touch state last. Nothing is
// memoised: the code gate's answer changes within the block, which is the point.
func (c *Classifier) Classify(tx *types.Transaction) LaneType {
	if c.isSystemTx(tx) {
		return GeneralLane
	}
	to := tx.To()
	if to == nil {
		return GeneralLane
	}
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType:
	default:
		return GeneralLane
	}
	if _, ok := c.listed[*to]; ok {
		return PaymentLane
	}
	if tx.Value().Sign() == 0 {
		return GeneralLane
	}
	if hasCode(c.code.GetCodeHash(*to)) {
		return GeneralLane
	}
	return PaymentLane
}

// hasCode reads a code hash from StateDB, where the zero hash is the trap: it means the account
// does not exist, and it is NOT EmptyCodeHash. Read it as code and every transfer to a fresh
// account leaves the lane.
func hasCode(codeHash common.Hash) bool {
	return codeHash != (common.Hash{}) && codeHash != types.EmptyCodeHash
}
