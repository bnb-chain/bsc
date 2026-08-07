package paymentlane

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// AccountReader is the only state capability the classifier needs.
type AccountReader interface {
	Account(addr common.Address) (*types.StateAccount, error)
}

// Classifier answers "payment or general" for the block whose parent post-state
// root is parentRoot.
type Classifier struct {
	parentRoot          common.Hash
	parentAccountReader AccountReader
	listed              map[common.Address]struct{}
	hasCode             map[common.Address]bool
	err                 error
}

// NewClassifier binds a classifier to one parent state.
func NewClassifier(parentRoot common.Hash, parentAccountReader AccountReader, listed map[common.Address]struct{}) *Classifier {
	return &Classifier{
		parentRoot:          parentRoot,
		parentAccountReader: parentAccountReader,
		listed:              listed,
		hasCode:             make(map[common.Address]bool),
	}
}

// Classify returns the lane class of a user transaction (never a Parlia system one): the
// BEP-703 section 3.2 gates, statically over declared fields plus the parent post-state, so
// nothing executes and the gates are ordered to touch state last. It fails shut to
// ClassGeneral. Deviations from the section are registered at the top of quota.go.
func (c *Classifier) Classify(tx *types.Transaction) (Class, error) {
	to := tx.To()
	if to == nil {
		return ClassGeneral, nil
	}
	switch tx.Type() {
	case types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType:
	default:
		return ClassGeneral, nil
	}
	if len(tx.AccessList()) != 0 {
		return ClassGeneral, nil
	}
	if _, ok := c.listed[*to]; ok {
		return ClassPayment, nil
	}
	if len(tx.Data()) != 0 {
		return ClassGeneral, nil
	}
	if tx.Value().Sign() == 0 {
		return ClassGeneral, nil
	}
	coded, err := c.destinationHasCode(*to)
	if err != nil {
		return ClassGeneral, err
	}
	if coded {
		return ClassGeneral, nil
	}
	return ClassPayment, nil
}

// Err reports the first state-read failure, sticky and never cleared by a later success.
// Once set, some transaction's class is untrustworthy and the block must not be produced -
// assert it alongside Budget.Verify before assembling.
func (c *Classifier) Err() error { return c.err }

// destinationHasCode reports whether addr has code in the parent post-state.
func (c *Classifier) destinationHasCode(addr common.Address) (bool, error) {
	if v, ok := c.hasCode[addr]; ok {
		return v, nil
	}
	acct, err := c.parentAccountReader.Account(addr)
	if err != nil {
		err = fmt.Errorf("%w: classify %x at parent root %x: %w", ErrStateUnavailable, addr, c.parentRoot, err)
		if c.err == nil {
			c.err = err
		}
		// Not memoised: the block is already doomed, and caching a guess would let
		// a later refactor that ignores the error produce a stable wrong answer.
		return false, err
	}
	coded := acct != nil && hasCodeHash(acct.CodeHash)
	c.hasCode[addr] = coded
	return coded, nil
}

// hasCodeHash reports whether a code hash denotes an account that has code.
func hasCodeHash(codeHash []byte) bool {
	return len(codeHash) != 0 && !bytes.Equal(codeHash, types.EmptyCodeHash.Bytes())
}
