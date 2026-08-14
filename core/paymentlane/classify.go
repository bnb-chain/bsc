package paymentlane

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// CodeReader must be the LIVE state of the block being classified, not the parent's: against
// the parent, a transfer to an address this same block gave code to - by deployment or by an
// EIP-7702 authorisation - counted as a payment and ran that code inside the quota.
type CodeReader interface {
	GetCodeHash(addr common.Address) common.Hash
}

// Classifier answers "payment or general" for one block's transactions, over two state views.
// listed is derived from the PARENT post-state and only ever decides ClassPayment, stopping
// there; everything past it is decided by code against the LIVE state - so no transaction's
// class rests on both. Membership must be settled before the block runs, or whoever orders it
// decides who is on the list.
type Classifier struct {
	code   CodeReader
	listed map[common.Address]struct{}
}

func NewClassifier(code CodeReader, listed map[common.Address]struct{}) *Classifier {
	return &Classifier{code: code, listed: listed}
}

// Classify returns the lane class of a user transaction (never a Parlia system one): the
// BEP-703 section 3.2 gates in order, arranged to touch state last. Nothing is executed to
// decide it, and nothing is memoised - the code gate's answer changes within the block, which
// is the point.
func (c *Classifier) Classify(tx *types.Transaction) Class {
	to := tx.To()
	if to == nil {
		return ClassGeneral
	}
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType:
	default:
		return ClassGeneral
	}
	if len(tx.AccessList()) != 0 {
		return ClassGeneral
	}
	if _, ok := c.listed[*to]; ok {
		return ClassPayment
	}
	if len(tx.Data()) != 0 {
		return ClassGeneral
	}
	if tx.Value().Sign() == 0 {
		return ClassGeneral
	}
	if hasCode(c.code.GetCodeHash(*to)) {
		return ClassGeneral
	}
	return ClassPayment
}

// hasCode reports whether a code hash denotes an account that holds code. The zero hash is the
// trap: StateDB.GetCodeHash returns it for an account that does not exist, and it is NOT
// EmptyCodeHash. Read it as code and every transfer to a fresh account leaves the lane.
func hasCode(codeHash common.Hash) bool {
	return codeHash != (common.Hash{}) && codeHash != types.EmptyCodeHash
}
