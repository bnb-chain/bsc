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
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// ContractAddress is the PaymentLane system contract, installed by the Gauss fork.
//
// Spelled out rather than derived from systemcontracts.PaymentLaneContract so that
// this package stays a leaf over common/core/types/params: core/systemcontracts
// transitively pulls in core/state, core/vm, trie and triedb, which would make
// paymentlane unimportable by anything below those. The duplication is closed by
// TestContractAddressMatchesSystemContracts, which imports the constant in the test
// binary only.
var ContractAddress = common.HexToAddress("0x0000000000000000000000000000000000002007")

// The consensus-relevant surface of the PaymentLane contract is its STORAGE
// LAYOUT, not its ABI: the client reads the slots directly and never calls the
// getters. Reading through the EVM is not an option - the only in-tree mechanism
// for it, parlia's ethAPI.Call, lives in internal/ethapi, which imports core, so
// core/state_processor.go could not then import this package. Direct slot reads
// are also the only form with no node-local input at all: no gas cap, no timeout,
// no chain rules, no ABI, so two honest nodes cannot disagree.
//
// Layout, verified empirically against the deployed Gauss bytecode by planting
// sentinels and reading them back through the getters (see
// TestLayoutMatchesDeployedBytecode):
//
//	slot 0..7  the eight governable parameters, in declaration order
//	slot 8     _paymentContracts._inner._values.length
//	           element i at keccak256(bytes32(8)) + i
//	slot 9     _paymentContracts._inner._indexes - NOT read here
//
// Slots 8 and 9 are OpenZeppelin EnumerableSet internals (v4.9.6: _values and
// _indexes; v5 renames the latter to _positions but keeps the order). Only slot 8
// is read - enumerating the array is what a per-block snapshot needs, and a
// membership-by-key read would both depend on slot 9's layout and cost one hashed
// storage read per distinct recipient instead of one per listed contract.
//
// Inserting or reordering a field in the contract shifts every slot after it,
// leaves every Solidity getter and every Foundry test green, and silently breaks
// this file. TestLayoutMatchesDeployedBytecode is the only thing that catches it.
const (
	paymentContractsLenSlot = 8
	numParams               = 8
)

// MaxPaymentContracts mirrors PaymentLane.MAX_PAYMENT_CONTRACTS, for the assertion
// in TestConstantsMatchDeployedBytecode. The read path bounds itself with
// maxPaymentContractsRead instead - see there for why the two differ.
const MaxPaymentContracts = 256

// maxPaymentContractsRead bounds the enumeration loop, so a corrupt length word
// cannot become an unbounded allocation.
//
// Deliberately far above the contract's own bound, which is the OPPOSITE policy
// from maxLaneRatio, and the asymmetry is the point. Were this an exact mirror, a
// later fork raising the contract's limit - a plausible contract-only change, and
// one the contract's own comment invites by noting lookup is O(1) at any size -
// would leave every Go assertion green, and then HALT THE CHAIN the moment
// governance added the 257th contract: LoadPaymentContracts is a pure function of
// the parent root, so every candidate block would fail identically and forever with
// ErrCorruptConfig. Slack turns that into a no-op. The claim that a deterministic
// tripwire "cannot halt the chain on a governance action" holds for the parameter
// words, whose every contract bound is at most 1e9, and does NOT transfer to a
// length bounded only by a mirrored constant.
const maxPaymentContractsRead = 4096

// The value an unwritten slot reads as, mirroring PaymentLane's DEFAULT_*
// constants. The contract has no initializer: all-zero storage already IS the
// shipped configuration, so these are a live fallback rather than a genesis seed.
//
// These are the only GOVERNANCE-SETTABLE values duplicated across the language
// boundary, and therefore the sharpest silent cross-client divergence surface in
// this package: a one-digit slip here still clamps, still steps, and passes every
// Go test. (Four protocol constants are mirrored too - RatioDenom, maxLaneRatio,
// maxReservedAddress, MaxPaymentContracts - and TestConstantsMatchDeployedBytecode
// pins those.)
// TestDefaultsMatchDeployedBytecode closes it by running the deployed blob in a
// real EVM against empty storage and comparing. Do not change these without
// changing the contract, and do not add a value here that the contract's
// validator would reject.
const (
	defaultMinRatio      = 200       // 2%
	defaultMaxRatio      = 800       // 8%
	defaultExpandTrigger = 8_000     // 80%
	defaultShrinkTrigger = 7_000     // 70%
	defaultExpandStep    = 200       // 2%
	defaultShrinkStep    = 50        // 0.5%
	defaultMinGas        = 2_000_000 // gas
	defaultMaxGas        = 8_000_000 // gas
)

// Params is the eight governable values of BEP-703 section 3.6, as they read at
// one state root.
//
// In production the only producer is LoadParams. The fields are exported for
// logging, metrics and tests; a hand-built Params cannot break anything because
// LaneSize is total, but it is not the configuration any chain is running.
//
// The four ratio fields and the two trigger/step pairs are parts per RatioDenom;
// MinGas and MaxGas are absolute gas.
type Params struct {
	MinRatio      uint64
	MaxRatio      uint64
	ExpandTrigger uint64
	ShrinkTrigger uint64
	ExpandStep    uint64
	ShrinkStep    uint64
	MinGas        uint64
	MaxGas        uint64
}

func (p Params) String() string {
	return fmt.Sprintf("minRatio %d maxRatio %d expandTrigger %d shrinkTrigger %d expandStep %d shrinkStep %d minGas %d maxGas %d",
		p.MinRatio, p.MaxRatio, p.ExpandTrigger, p.ShrinkTrigger, p.ExpandStep, p.ShrinkStep, p.MinGas, p.MaxGas)
}

// StorageReader is the state capability the configuration loader needs.
//
// One method, and deliberately not core/state.StateReader: it documents and
// enforces that configuration loading reads storage and nothing else, and it
// keeps this package free of a core/state import. Both state.Reader and
// state.StateReader satisfy it structurally.
//
// The reader must be bound to the PARENT block's post-state root. Reading the
// advancing state instead would let a governance transaction inside the block
// change the configuration the same block is judged by.
type StorageReader interface {
	Storage(addr common.Address, slot common.Hash) (common.Hash, error)
}

// paramSlot returns the storage slot of parameter i.
func paramSlot(i int) common.Hash {
	return common.Hash{31: byte(i)}
}

// paymentContractSlot returns the storage slot of element i of the payment-contract
// array: keccak256(bytes32(paymentContractsLenSlot)) + i.
//
// The addition is modulo 2^256, matching the EVM's unchecked add, so it agrees with
// Solidity even in the astronomically unlikely case that the keccak base is within
// maxPaymentContractsRead of wrapping.
func paymentContractSlot(i uint64) common.Hash {
	base := new(uint256.Int).SetBytes32(crypto.Keccak256(common.Hash{31: paymentContractsLenSlot}.Bytes()))
	return base.AddUint64(base, i).Bytes32()
}

// word64 converts a storage word to uint64.
//
// A parameter above 2^64-1 is not something governance can produce - every bound
// in the contract's validator is at most 1e9 - so it can only mean the slot
// layout has shifted and this word is really part of a mapping or an array. That
// is deterministic (every node reading this root sees it), so it is safe to make
// it a hard error, and it is unreachable, so it cannot halt the chain on a
// governance action. It is a layout tripwire, not a fallback: a clamp here would
// squash the garbage into a legal-looking value and hide exactly this failure.
func word64(w common.Hash) (uint64, bool) {
	for _, b := range w[:24] {
		if b != 0 {
			return 0, false
		}
	}
	return new(uint256.Int).SetBytes32(w[:]).Uint64(), true
}

// LoadParams reads the eight parameters from the state r is bound to.
//
// Failure taxonomy - the two classes arrive on different return values, which is
// what makes the discipline structural rather than something to remember:
//
//	ErrCorruptConfig  deterministic: same root, same verdict on every node.
//	                  The caller must reject the block and must NOT substitute a
//	                  value.
//	any other error   nondeterministic: pruned state, snapshot stale, disk. The
//	                  caller must propagate and retry. Falling back to a default
//	                  IS a chain split - node A reads 5%, node B times out and
//	                  uses 2%, and they disagree about the same block.
//
// StorageReader.Storage returns (zero, nil) for an unwritten slot and an error
// only for an unexpected condition, and it takes no context - so the most common
// nondeterministic failure of an eth_call-based read, a node-local timeout, is
// absent by construction here rather than handled.
//
// There is deliberately no client-side sanitizer. The contract writes all eight
// slots on every accepted update and every one of its stage-one bounds excludes
// zero, so storage is either all-zero or all strictly positive - "partially
// written" is unreachable. And the default tuple itself satisfies all eight
// bounds and all six coupled invariants. So after the zero substitution below,
// the tuple is either exactly the defaults or exactly a tuple that passed the
// contract's full validator; a clamp could never alter a single value. A branch
// on a consensus path that no input can reach is worse than no branch: it is
// untested by construction and it makes the illegal case look handled.
func LoadParams(r StorageReader) (Params, error) {
	var raw [numParams]uint64
	for i := range raw {
		slot := paramSlot(i)
		w, err := r.Storage(ContractAddress, slot)
		if err != nil {
			return Params{}, fmt.Errorf("%w: params slot %d: %w", ErrStateUnavailable, i, err)
		}
		v, ok := word64(w)
		if !ok {
			return Params{}, fmt.Errorf("%w: params slot %d = %#x", ErrCorruptConfig, i, w)
		}
		raw[i] = v
	}
	// Mirror PaymentLane._loadParams: the fallback is applied per field.
	return Params{
		MinRatio:      orDefault(raw[0], defaultMinRatio),
		MaxRatio:      orDefault(raw[1], defaultMaxRatio),
		ExpandTrigger: orDefault(raw[2], defaultExpandTrigger),
		ShrinkTrigger: orDefault(raw[3], defaultShrinkTrigger),
		ExpandStep:    orDefault(raw[4], defaultExpandStep),
		ShrinkStep:    orDefault(raw[5], defaultShrinkStep),
		MinGas:        orDefault(raw[6], defaultMinGas),
		MaxGas:        orDefault(raw[7], defaultMaxGas),
	}, nil
}

func orDefault(stored, fallback uint64) uint64 {
	if stored == 0 {
		return fallback
	}
	return stored
}

// LoadPaymentContracts reads the BEP-703 section 3.7 payment-contract set from the
// state r is bound to, as a membership map.
//
// Enumerated once per block rather than queried once per transaction: a
// per-transaction membership read hashes a distinct slot for every distinct
// recipient, so it cannot be memoised and a congested block pays thousands of trie
// descents instead of the count plus one per listed contract.
//
// A nil result means the set is genuinely empty, which is the state on activation
// day - the contract has no initializer, so nothing can have been listed before
// its code existed. Callers must never use nil to signal a failed read: a failed
// read has to abort the block before any classification happens.
//
// No ordering, capacity or address-range guarantee is offered to the caller and
// none is needed. The classifier's reserved-range gate runs above its whitelist
// lookup, so a mis-governed reserved address in this set cannot reclassify
// anything, which removes three validation obligations from both sides.
func LoadPaymentContracts(r StorageReader) (map[common.Address]struct{}, error) {
	w, err := r.Storage(ContractAddress, common.Hash{31: paymentContractsLenSlot})
	if err != nil {
		return nil, fmt.Errorf("%w: payment-contract count: %w", ErrStateUnavailable, err)
	}
	n, ok := word64(w)
	if !ok || n > maxPaymentContractsRead {
		return nil, fmt.Errorf("%w: payment-contract count = %#x", ErrCorruptConfig, w)
	}
	if n == 0 {
		return nil, nil
	}
	set := make(map[common.Address]struct{}, n)
	for i := uint64(0); i < n; i++ {
		w, err := r.Storage(ContractAddress, paymentContractSlot(i))
		if err != nil {
			return nil, fmt.Errorf("%w: payment contract %d: %w", ErrStateUnavailable, i, err)
		}
		// The element is an address, so the top 12 bytes are padding. Anything
		// there means the array is not where this file thinks it is.
		for _, b := range w[:12] {
			if b != 0 {
				return nil, fmt.Errorf("%w: payment contract %d = %#x", ErrCorruptConfig, i, w)
			}
		}
		set[common.BytesToAddress(w[12:])] = struct{}{}
	}
	return set, nil
}
