package paymentlane

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

// TODO: ABI + Cache

// ContractAddress is the PaymentLane system contract, installed by the Gauss fork.
var ContractAddress = common.HexToAddress("0x0000000000000000000000000000000000002007")

// The consensus surface of the PaymentLane contract is its STORAGE LAYOUT, not its ABI:
// the slots are read directly, never the getters. An EVM call is not an option anyway -
// it would pull in node-local input (gas cap, timeout, chain rules) and internal/ethapi,
// which imports core.
//
// Layout, pinned against the deployed bytecode by TestLayoutMatchesDeployedBytecode:
//
//	slot 0..7  the eight governable parameters, in declaration order
//	slot 8     the payment-contract array's length, element i at keccak256(bytes32(8)) + i
//	slot 9     its EnumerableSet index mapping - never read
//
// Inserting or reordering a contract field shifts every slot after it and leaves every
// Solidity getter and Foundry test green, so that test is the only thing that catches it.
const (
	paymentContractsLenSlot = 8
	numParams               = 8
)

// What an unwritten slot reads as, mirroring PaymentLane's DEFAULT_* constants: the
// contract has no initializer, so all-zero storage already IS the shipped configuration.
// A one-digit slip here diverges silently from other clients, which is what
// TestDefaultsMatchDeployedBytecode catches - never change these alone.
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

// Params is the eight governable values of BEP-703 section 3.6 as they read at one state
// root; LoadParams is the only producer in production. The first six are parts per
// RatioDenom, MinGas and MaxGas absolute gas.
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

// StorageReader is the state capability the configuration loader needs. It must be bound to
// the PARENT block's post-state root.
type StorageReader interface {
	Storage(addr common.Address, slot common.Hash) (common.Hash, error)
}

// paramSlot returns the storage slot of parameter i.
func paramSlot(i int) common.Hash {
	return common.Hash{31: byte(i)}
}

// paymentContractSlot returns the storage slot of element i of the payment-contract array:
// keccak256(bytes32(paymentContractsLenSlot)) + i, added modulo 2^256 as the EVM does.
func paymentContractSlot(i uint64) common.Hash {
	base := new(uint256.Int).SetBytes32(crypto.Keccak256(common.Hash{31: paymentContractsLenSlot}.Bytes()))
	return base.AddUint64(base, i).Bytes32()
}

// word64 converts a storage word to uint64, reporting false above 2^64-1. The contract
// bounds every parameter well below that, so a wide word means the layout shifted and this
// is really part of a mapping - a tripwire, never clamped, and unreachable by governance.
func word64(w common.Hash) (uint64, bool) {
	for _, b := range w[:24] {
		if b != 0 {
			return 0, false
		}
	}
	return new(uint256.Int).SetBytes32(w[:]).Uint64(), true
}

// LoadParams reads the eight parameters from the state r is bound to. Two failure classes,
// told apart by the error: ErrCorruptConfig is deterministic, so the caller must reject the
// block and must NOT substitute a value; anything else is local (pruned state, stale
// snapshot, disk) and must be propagated and retried, because falling back to a default
// where another node read the real value IS a chain split.
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

// LoadPaymentContracts reads the BEP-703 section 3.7 payment-contract set as a membership
// map; nil means genuinely empty, the activation-day state, and never a failed read.
func LoadPaymentContracts(r StorageReader) (map[common.Address]struct{}, error) {
	w, err := r.Storage(ContractAddress, common.Hash{31: paymentContractsLenSlot})
	if err != nil {
		return nil, fmt.Errorf("%w: payment-contract count: %w", ErrStateUnavailable, err)
	}
	n, ok := word64(w)
	if !ok {
		return nil, fmt.Errorf("%w: payment-contract count = %#x", ErrCorruptConfig, w)
	}
	if n == 0 {
		return nil, nil
	}
	set := make(map[common.Address]struct{})
	for i := uint64(0); i < n; i++ {
		w, err := r.Storage(ContractAddress, paymentContractSlot(i))
		if err != nil {
			return nil, fmt.Errorf("%w: payment contract %d: %w", ErrStateUnavailable, i, err)
		}
		// An address leaves the top 12 bytes zero; anything there means this is not the array.
		for _, b := range w[:12] {
			if b != 0 {
				return nil, fmt.Errorf("%w: payment contract %d = %#x", ErrCorruptConfig, i, w)
			}
		}
		addr := common.BytesToAddress(w[12:])
		// An EnumerableSet cannot hold a duplicate, so a repeat proves this is not that array -
		// which is what keeps the unbounded loop safe.
		if _, dup := set[addr]; dup {
			return nil, fmt.Errorf("%w: payment contract %d = %x is a duplicate", ErrCorruptConfig, i, addr)
		}
		set[addr] = struct{}{}
	}
	return set, nil
}
