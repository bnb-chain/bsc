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

package paymentlane

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// maxReservedAddress mirrors PaymentLane.MAX_RESERVED_ADDRESS.
//
// Every precompile in this tree (0x01-0x11, BSC's own 0x64-0x69, and 0x100
// p256Verify) and every Parlia system contract (up to 0x3000) is at or below it,
// so one range test covers all of them. Note isReserved hard-codes the two-byte
// window rather than reading this constant, so the coupling between the two is
// enforced by test (TestReservedRangeIsExact) rather than by the compiler, and the
// agreement with the contract by TestConstantsMatchDeployedBytecode.
//
// A monotone address range is also why the classifier needs neither params.Rules
// nor core/vm: vm.ActivePrecompiledContracts clones a ~25-entry map on every call,
// its contents depend on the fork AND on rules.IsInBSC, and it grows a branch every
// fork - none of which a fixed range does.
//
// The contract enforces the same bound when an address is listed, and
// TestConstantsMatchDeployedBytecode asserts the two stay equal - so a raise has to
// happen on both sides in one change. LOWERING it, or dropping the check there, is the
// silently dangerous direction: the contract would accept a listing every client
// ignores forever, with PaymentContractAdded emitted and no error anywhere.
const maxReservedAddress = 0xFFFF

// AccountReader is the only state capability the classifier needs.
//
// One method, and deliberately not core/state.StateReader: it makes "classifying
// never reads storage and never loads code" a compile-time fact rather than a
// review comment. Reading code size would load the whole code blob in witness
// mode, and reading storage is not classification's business at all. Both
// state.Reader and state.StateReader satisfy this structurally.
type AccountReader interface {
	Account(addr common.Address) (*types.StateAccount, error)
}

// Classifier answers "payment or general" for the block whose parent post-state
// root is parentRoot.
//
// Construct exactly one per block-building or block-processing attempt. It is
// single-goroutine by contract and must not be used concurrently: the memo is a
// plain map, and although the readers underneath are thread-safe, two goroutines
// classifying through one Classifier is a concurrent map write.
//
// Sequential hand-off between goroutines is fine and does happen - the miner adopts
// a winning bid's environment, and therefore its Classifier, from the bid
// simulator's goroutine after that bid has finished. What must not happen is two
// goroutines classifying at once.
//
// The reader must be bound to the parent state root, never to the advancing
// StateDB. That is a security requirement, not tidiness. With the advancing state,
// a block producer - or a bid builder - inserts one cheap CREATE2 or SetCodeTx
// ahead of a batch of transfers and thereby decides which users' transfers enter
// the lane; installing or removing a delegation on a busy deposit address is an
// admission switch costing one transaction. It stays deterministic, so every test
// passes and the manipulation is invisible until someone uses it.
type Classifier struct {
	parentRoot common.Hash                 // diagnostics only; parent is already pinned to it
	parent     AccountReader               // bound to parentRoot
	listed     map[common.Address]struct{} // section 3.7 set; caller-owned, read-only
	hasCode    map[common.Address]bool     // per-block memo, dies with this Classifier
	err        error                       // sticky: the first read failure, never cleared
}

// NewClassifier binds a classifier to one parent state.
//
// listed may be nil, which means the payment-contract set is empty - the state on
// activation day. It must never be nil because a read failed; see LoadPaymentContracts.
// The map is not copied: the caller must not mutate it for this block.
func NewClassifier(parentRoot common.Hash, parent AccountReader, listed map[common.Address]struct{}) *Classifier {
	return &Classifier{
		parentRoot: parentRoot,
		parent:     parent,
		listed:     listed,
		hasCode:    make(map[common.Address]bool),
	}
}

// Classify returns the lane class of a user transaction.
//
// Never call it for a Parlia system transaction: those are split out before
// accounting and their gas is the separate systemGasUsed term. If one ever leaks
// through, gate 4 makes it general, because every system contract is inside the
// reserved range.
//
// On a state-read failure it returns (ClassGeneral, err) and records err
// stickily. ClassGeneral is the fail-shut value in both directions: a producer
// that ignored the error would under-fill the lane, which costs revenue but still
// yields a valid block, whereas ClassPayment would shrink IdleLane, widen general
// headroom and over-pack. An importer that ignored it would reject the block,
// which is a correct refusal rather than acceptance of an invalid one.
//
// The gates are ordered so that no state is touched until every free test has
// passed, and so that each one dominates the next for a reason:
//
//	1 to == nil            CREATE has no destination to classify.
//	2 type allowlist       Not a blacklist. SetCodeTx (0x04) installs its
//	                       authorizations BEFORE the top-level call and does not
//	                       revoke them, so 320 of them are 8.02M gas of pure state
//	                       writes at lane price; BlobTx (0x03) buys the separate
//	                       DA dimension for 21000 gas of execution. An allowlist
//	                       also makes every future transaction type general by
//	                       default, which a blacklist would not.
//	3 empty access list    Access-list entries are intrinsic gas with no code
//	                       execution: to=EOA, data empty, 3300 addresses is ~8M
//	                       gas. AccessList() is a free slice-header read, so this
//	                       is cheaper than gate 4.
//	4 not reserved         Precompiles have no code in state but do execute, and a
//	                       precompile returning a non-revert error burns all the
//	                       gas handed to it - so to=0x65 with empty data and the
//	                       maximum gas would be payment class and would exhaust the
//	                       whole lane in one transaction. Placed above gate 5 so a
//	                       mis-governed reserved listing cannot reclassify
//	                       anything, which is why this file neither copies nor
//	                       validates the caller's set.
//	5 listed               Categories 2 and 3. Must be below 2 to 4 - a BlobTx to
//	                       a listed token must not be payment - and above 6 and 7,
//	                       because a real transfer() call has calldata and zero
//	                       value, so hoisting either of those makes the entire
//	                       payment-contract list dead code.
//	6 empty calldata       Category 1 is a bare value transfer. Free slice-header
//	                       read, so it precedes gate 7.
//	7 non-zero value       A zero-value bare transfer is not a payment. Last of the
//	                       static gates because Value() allocates a big.Int.
//	8 destination has no   Category 1 proper, decided against the PARENT
//	  code in parent state post-state. An absent account has no code. An EIP-7702
//	                       delegation designator is classified general for free: its
//	                       code hash is keccak(0xef0100||target), so it is not the
//	                       empty-code hash.
func (c *Classifier) Classify(tx *types.Transaction) (Class, error) {
	to := tx.To() // allocates; call it once
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
	if isReserved(*to) {
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

// Err reports the first state-read failure, if any.
//
// Sticky by design, and never cleared by a later success. Classify already
// returns the error so each call site handles it where the decision is made; this
// is the backstop that makes a call site which does not still unable to produce a
// block. Assert it alongside Budget.Verify before assembling.
func (c *Classifier) Err() error { return c.err }

// isReserved reports whether addr is at or below maxReservedAddress, i.e. whether
// bytes 0..17 of the 20-byte address are all zero.
func isReserved(addr common.Address) bool {
	for _, b := range addr[:common.AddressLength-2] {
		if b != 0 {
			return false
		}
	}
	return true
}

// destinationHasCode reports whether addr has code in the parent post-state.
func (c *Classifier) destinationHasCode(addr common.Address) (bool, error) {
	if v, ok := c.hasCode[addr]; ok {
		return v, nil
	}
	acct, err := c.parent.Account(addr)
	if err != nil {
		err = fmt.Errorf("%w: classify %x at parent root %x: %w", ErrStateUnavailable, addr, c.parentRoot, err)
		if c.err == nil {
			c.err = err
		}
		// Not memoised: the block is already doomed, and caching a guess would let
		// a later refactor that ignores the error produce a stable wrong answer.
		return false, err
	}
	// acct == nil means the account does not exist, which means it has no code.
	// Writing this as GetCodeHash(to) == types.EmptyCodeHash instead would
	// misclassify every transfer to a brand-new address - first deposits and new
	// wallets, the lane's core use case - with no error, no invalid block and no
	// failing test. That comparison shape is already in the tree (legacypool tests
	// GetCodeHash(from) == EmptyCodeHash, for 7702 authorisations), which is exactly
	// what makes it easy to reach for here by reflex.
	coded := acct != nil && hasCodeHash(acct.CodeHash)
	c.hasCode[addr] = coded
	return coded, nil
}

// hasCodeHash reports whether a code hash denotes an account that has code.
//
// The len check is load-bearing, not decoration. flatReader and historicStateReader
// normalise an omitted code hash back to EmptyCodeHash, and mptTrieReader returns the
// consensus-format leaf, whose code hash is always 32 bytes. ubtTrieReader does NOT
// normalise: it passes bintrie.BinaryTrie.GetAccount's value through, and that assigns
// acc.CodeHash from the CodeHash leaf unmodified, so a missing leaf yields nil. That
// path is unreachable only because UBTTime is nil in every shipped config - i.e. it
// goes live at the UBT fork, not never.
//
// Dropping the check is silent and permissive: an account whose reader omitted the
// code hash would compare unequal to EmptyCodeHash, look coded, and its transfers
// would leave the lane - visible only as demand that never materialised.
func hasCodeHash(codeHash []byte) bool {
	return len(codeHash) != 0 && !bytes.Equal(codeHash, types.EmptyCodeHash.Bytes())
}
