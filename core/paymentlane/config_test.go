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
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// ABI selectors of the PaymentLane getters. Only the tests use these: the
// production read path never goes through the ABI.
const (
	selGetPaymentLaneParams = "ff620147" // getPaymentLaneParams()
	selGetPaymentContracts  = "2b4af15b" // getPaymentContracts()
	selMaxReservedAddress   = "7bbeedf6" // MAX_RESERVED_ADDRESS()
	selMaxPaymentContracts  = "daaff2c4" // MAX_PAYMENT_CONTRACTS()
	selRatioDenom           = "7c86be0d" // RATIO_DENOM()
	selMaxLaneRatio         = "4df7c732" // MAX_LANE_RATIO()
	selTriggerGapMin        = "170da6d4" // TRIGGER_GAP_MIN()
	selRatioGapMin          = "2bbf97d7" // RATIO_GAP_MIN()
)

// mapReader is a StorageReader over a literal slot map. Absent slots read as zero,
// which is exactly what state.Reader guarantees for an unwritten slot.
type mapReader struct {
	slots map[common.Hash]common.Hash
	err   error // when non-nil, every read fails: the nondeterministic case
}

func (r mapReader) Storage(addr common.Address, slot common.Hash) (common.Hash, error) {
	if r.err != nil {
		return common.Hash{}, r.err
	}
	if addr != ContractAddress {
		return common.Hash{}, nil
	}
	return r.slots[slot], nil
}

func word(v uint64) common.Hash { return common.BigToHash(new(big.Int).SetUint64(v)) }

// deployedContract loads the Gauss bytecode into a fresh StateDB at the real
// PaymentLane address, so every test below runs against the blob the fork actually
// installs rather than against a recompile.
func deployedContract(t *testing.T) *state.StateDB {
	t.Helper()
	code, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	require.NoError(t, err)
	// All three networks ship byte-identical code; assert that here so this test
	// keeps covering mainnet and chapel too, and fails loudly the day they diverge.
	require.Equal(t, strings.TrimSpace(gauss.MainnetPaymentLaneContract), strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	require.Equal(t, strings.TrimSpace(gauss.ChapelPaymentLaneContract), strings.TrimSpace(gauss.RialtoPaymentLaneContract))

	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	statedb.SetCode(ContractAddress, code, tracing.CodeChangeSystemContractUpgrade)
	return statedb
}

// callContract does a read-only call into the deployed contract and returns the
// return data split into 32-byte words.
func callContract(t *testing.T, statedb *state.StateDB, selector string, args ...byte) []*big.Int {
	t.Helper()
	input, err := hex.DecodeString(selector)
	require.NoError(t, err)
	input = append(input, args...)

	blockCtx := vm.BlockContext{
		CanTransfer: func(vm.StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(vm.StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		BlockNumber: big.NewInt(60_000_000),
		Time:        1_800_000_000,
		Difficulty:  common.Big1,
		BaseFee:     common.Big0,
		BlobBaseFee: common.Big0,
		GasLimit:    55_000_000,
	}
	evm := vm.NewEVM(blockCtx, statedb, params.BSCChainConfig, vm.Config{NoBaseFee: true})
	defer evm.Release()

	ret, _, err := evm.StaticCall(common.Address{}, ContractAddress, input, vm.NewGasBudget(50_000_000))
	require.NoError(t, err, "selector %s", selector)

	out := make([]*big.Int, 0, len(ret)/32)
	for i := 0; i+32 <= len(ret); i += 32 {
		out = append(out, new(big.Int).SetBytes(ret[i:i+32]))
	}
	return out
}

// TestLayoutMatchesDeployedBytecode is the layout tripwire, and the only thing
// standing between a contract-side field insertion and every Go client silently
// reading the wrong slot.
//
// It plants sentinels using this package's OWN slot arithmetic and reads them back
// through the contract's getters, so it pins the production functions rather than
// re-deriving the layout. Inserting or reordering a field in PaymentLane.sol shifts
// every Solidity read together - leaving all Solidity getters and all Foundry
// tests green - and breaks only this.
func TestLayoutMatchesDeployedBytecode(t *testing.T) {
	t.Run("params occupy slots 0..7 in declaration order", func(t *testing.T) {
		statedb := deployedContract(t)
		// Distinct, non-zero sentinels so the contract's DEFAULT_* fallback cannot
		// mask a wrong slot by coincidence. The getter does no validation, so the
		// values need not satisfy any invariant.
		for i := 0; i < numParams; i++ {
			statedb.SetState(ContractAddress, paramSlot(i), word(uint64(1000+i)))
		}
		got := callContract(t, statedb, selGetPaymentLaneParams)
		require.Len(t, got, numParams)
		for i, w := range got {
			require.Equal(t, uint64(1000+i), w.Uint64(),
				"field %d of getPaymentLaneParams() is not backed by paramSlot(%d)", i, i)
		}
	})

	t.Run("whitelist array length at slot 8, elements at keccak(8)+i", func(t *testing.T) {
		statedb := deployedContract(t)
		addrs := []common.Address{
			common.HexToAddress("0x1111111111111111111111111111111111111111"),
			common.HexToAddress("0x2222222222222222222222222222222222222222"),
			common.HexToAddress("0x3333333333333333333333333333333333333333"),
		}
		statedb.SetState(ContractAddress, common.Hash{31: paymentContractsLenSlot}, word(uint64(len(addrs))))
		for i, a := range addrs {
			statedb.SetState(ContractAddress, paymentContractSlot(uint64(i)), common.BytesToHash(a[:]))
		}
		// getPaymentContracts() returns (offset, length, elems...).
		got := callContract(t, statedb, selGetPaymentContracts)
		require.Len(t, got, 2+len(addrs))
		require.Equal(t, uint64(len(addrs)), got[1].Uint64())
		for i, a := range addrs {
			require.Equal(t, a, common.BigToAddress(got[2+i]),
				"element %d is not backed by paymentContractSlot(%d)", i, i)
		}
	})

}

// TestDefaultsMatchDeployedBytecode closes the one place a value is duplicated
// across the language boundary.
//
// A one-digit slip in the Go DEFAULT_* mirror still clamps, still steps, and
// passes every other test in this package - while a second client reading only the
// Solidity gets the right number, so the two split on the first block where the
// affected parameter binds. Nothing but a comparison against the real blob catches
// that.
func TestDefaultsMatchDeployedBytecode(t *testing.T) {
	statedb := deployedContract(t) // storage untouched: every slot reads as zero
	got := callContract(t, statedb, selGetPaymentLaneParams)
	require.Len(t, got, numParams)

	want := []uint64{
		defaultMinRatio, defaultMaxRatio, defaultExpandTrigger, defaultShrinkTrigger,
		defaultExpandStep, defaultShrinkStep, defaultMinGas, defaultMaxGas,
	}
	for i, w := range want {
		require.Equal(t, w, got[i].Uint64(), "Go default %d disagrees with the deployed contract", i)
	}

	// And the same tuple must come out of the production read path, which is what
	// actually runs. This is the end-to-end version of the check above.
	params, err := LoadParams(mapReader{})
	require.NoError(t, err)
	require.Equal(t, Params{
		MinRatio: want[0], MaxRatio: want[1], ExpandTrigger: want[2], ShrinkTrigger: want[3],
		ExpandStep: want[4], ShrinkStep: want[5], MinGas: want[6], MaxGas: want[7],
	}, params)
}

// TestConstantsMatchDeployedBytecode pins the constants this package mirrors
// from the contract. Each has a distinct failure mode if it drifts:
//
//	MAX_RESERVED_ADDRESS  lowering it contract-side would let governance list an
//	                      address every client ignores forever, with the event
//	                      emitted and no error anywhere.
//	MAX_LANE_RATIO        it is what bounds laneSize to GasLimit/5, which is the
//	                      whole no-reachable-halt argument.
//	RATIO_DENOM           every ratio in the package is parts per this.
//	MAX_PAYMENT_CONTRACTS it bounds the enumeration loop.
func TestConstantsMatchDeployedBytecode(t *testing.T) {
	statedb := deployedContract(t)
	for _, tc := range []struct {
		name     string
		selector string
		want     uint64
	}{
		{"MAX_RESERVED_ADDRESS", selMaxReservedAddress, maxReservedAddress},
		{"MAX_LANE_RATIO", selMaxLaneRatio, maxLaneRatio},
		{"RATIO_DENOM", selRatioDenom, RatioDenom},
		{"MAX_PAYMENT_CONTRACTS", selMaxPaymentContracts, MaxPaymentContracts},
		{"TRIGGER_GAP_MIN", selTriggerGapMin, triggerGapMin},
		{"RATIO_GAP_MIN", selRatioGapMin, ratioGapMin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := callContract(t, statedb, tc.selector)
			require.Len(t, got, 1)
			require.Equal(t, tc.want, got[0].Uint64())
		})
	}
}

func TestLoadParams(t *testing.T) {
	t.Run("governance values are used verbatim", func(t *testing.T) {
		r := mapReader{slots: map[common.Hash]common.Hash{
			paramSlot(0): word(100), paramSlot(1): word(700),
			paramSlot(2): word(9000), paramSlot(3): word(3000),
			paramSlot(4): word(500), paramSlot(5): word(10),
			paramSlot(6): word(21_000), paramSlot(7): word(9_000_000),
		}}
		got, err := LoadParams(r)
		require.NoError(t, err)
		require.Equal(t, Params{100, 700, 9000, 3000, 500, 10, 21_000, 9_000_000}, got)
	})

	t.Run("the fallback is per field, mirroring _loadParams", func(t *testing.T) {
		// Unreachable on chain - the contract writes all eight slots on every
		// accepted update - but the read path must still mirror the contract rather
		// than assume the reachable states.
		r := mapReader{slots: map[common.Hash]common.Hash{paramSlot(4): word(999)}}
		got, err := LoadParams(r)
		require.NoError(t, err)
		require.Equal(t, uint64(999), got.ExpandStep)
		require.Equal(t, uint64(defaultMinRatio), got.MinRatio)
		require.Equal(t, uint64(defaultMaxGas), got.MaxGas)
	})

	t.Run("a word above 2^64-1 is a deterministic layout error", func(t *testing.T) {
		var big common.Hash
		big[0] = 1 // bit 255: far above any value governance can store
		r := mapReader{slots: map[common.Hash]common.Hash{paramSlot(3): big}}
		_, err := LoadParams(r)
		require.ErrorIs(t, err, ErrCorruptConfig)
		// It must NOT be reported as a state problem: the caller's handling differs.
		require.NotErrorIs(t, err, ErrStateUnavailable)
	})

	t.Run("a read failure is nondeterministic and must not become a default", func(t *testing.T) {
		boom := errors.New("snapshot stale")
		_, err := LoadParams(mapReader{err: boom})
		require.ErrorIs(t, err, ErrStateUnavailable)
		require.ErrorIs(t, err, boom)
		require.NotErrorIs(t, err, ErrCorruptConfig)
	})
}

func TestLoadPaymentContracts(t *testing.T) {
	lenSlot := common.Hash{31: paymentContractsLenSlot}

	t.Run("empty means empty, not failed", func(t *testing.T) {
		got, err := LoadPaymentContracts(mapReader{})
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("enumerates the array", func(t *testing.T) {
		addrs := []common.Address{
			common.HexToAddress("0xaaaa000000000000000000000000000000000001"),
			common.HexToAddress("0xbbbb000000000000000000000000000000000002"),
		}
		slots := map[common.Hash]common.Hash{lenSlot: word(uint64(len(addrs)))}
		for i, a := range addrs {
			slots[paymentContractSlot(uint64(i))] = common.BytesToHash(a[:])
		}
		got, err := LoadPaymentContracts(mapReader{slots: slots})
		require.NoError(t, err)
		require.Len(t, got, len(addrs))
		for _, a := range addrs {
			require.Contains(t, got, a)
		}
	})

	t.Run("a count above the read bound is a layout error, not an allocation", func(t *testing.T) {
		r := mapReader{slots: map[common.Hash]common.Hash{lenSlot: word(maxPaymentContractsRead + 1)}}
		_, err := LoadPaymentContracts(r)
		require.ErrorIs(t, err, ErrCorruptConfig)
	})

	t.Run("a count above the contract bound but below ours is NOT an error", func(t *testing.T) {
		// The asymmetry that keeps a contract-side raise of MAX_PAYMENT_CONTRACTS from
		// halting the chain. See maxPaymentContractsRead.
		require.Greater(t, uint64(maxPaymentContractsRead), uint64(MaxPaymentContracts))
		slots := map[common.Hash]common.Hash{lenSlot: word(MaxPaymentContracts + 1)}
		for i := 0; i <= MaxPaymentContracts; i++ {
			a := common.BigToAddress(big.NewInt(int64(i) + 0x10000))
			slots[paymentContractSlot(uint64(i))] = common.BytesToHash(a[:])
		}
		got, err := LoadPaymentContracts(mapReader{slots: slots})
		require.NoError(t, err)
		require.Len(t, got, MaxPaymentContracts+1)
	})

	t.Run("exactly the contract bound is accepted", func(t *testing.T) {
		slots := map[common.Hash]common.Hash{lenSlot: word(MaxPaymentContracts)}
		for i := 0; i < MaxPaymentContracts; i++ {
			a := common.BigToAddress(big.NewInt(int64(i) + 0x10000))
			slots[paymentContractSlot(uint64(i))] = common.BytesToHash(a[:])
		}
		got, err := LoadPaymentContracts(mapReader{slots: slots})
		require.NoError(t, err)
		require.Len(t, got, MaxPaymentContracts)
	})

	t.Run("padding bytes in an element are a layout error", func(t *testing.T) {
		dirty := common.BytesToHash(common.HexToAddress("0xdead000000000000000000000000000000000001").Bytes())
		dirty[0] = 0xff // above the 20 address bytes
		r := mapReader{slots: map[common.Hash]common.Hash{
			lenSlot: word(1), paymentContractSlot(0): dirty,
		}}
		_, err := LoadPaymentContracts(r)
		require.ErrorIs(t, err, ErrCorruptConfig)
	})

	t.Run("a read failure propagates", func(t *testing.T) {
		boom := errors.New("missing trie node")
		_, err := LoadPaymentContracts(mapReader{err: boom})
		require.ErrorIs(t, err, ErrStateUnavailable)
		require.ErrorIs(t, err, boom)
	})
}

// TestContractAddressIsTheLiteralOne guards the one-digit typo that would point
// the whole feature at a different system contract. Comparing against
// common.HexToAddress(systemcontracts.PaymentLaneContract) would be tautological;
// 0x2006 is Timelock.
func TestContractAddressIsTheLiteralOne(t *testing.T) {
	require.Equal(t, common.HexToAddress("0x0000000000000000000000000000000000002007"), ContractAddress)
}

// TestContractAddressMatchesSystemContracts closes the one duplication introduced by
// keeping this package a leaf: ContractAddress is spelled out in config.go rather
// than derived from systemcontracts.PaymentLaneContract, so that core/systemcontracts
// (and through it core/state, core/vm, trie, triedb) stays out of the production
// import graph. The constant is imported here, in the test binary only.
func TestContractAddressMatchesSystemContracts(t *testing.T) {
	require.Equal(t, common.HexToAddress(systemcontracts.PaymentLaneContract), ContractAddress)
}

// TestWordTripwireBoundaryByte covers the highest byte the tripwire must reject.
//
// Byte 23 is the boundary: bits 64 to 71, the first bits that do not fit in a
// uint64. A check written as w[:23] accepts a word of exactly 2^64+200 and silently
// truncates it to 200 - which is precisely the shifted-layout reading the tripwire
// exists to catch, and no other test plants a value there.
func TestWordTripwireBoundaryByte(t *testing.T) {
	for _, tc := range []struct {
		name    string
		byteIdx int
		ok      bool
	}{
		{"byte 24 is the low uint64", 24, true},
		{"byte 23 is one bit too high", 23, false},
		{"byte 0 is far too high", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w common.Hash
			w[tc.byteIdx] = 1
			got, ok := word64(w)
			require.Equal(t, tc.ok, ok)
			if ok {
				require.Equal(t, uint64(1)<<((31-tc.byteIdx)*8), got)
			}
		})
	}
}

// TestPaymentContractPaddingBoundaryByte covers byte 11, the last padding byte above
// the 20 address bytes. A check written as w[:11] would let it through.
func TestPaymentContractPaddingBoundaryByte(t *testing.T) {
	lenSlot := common.Hash{31: paymentContractsLenSlot}
	for _, byteIdx := range []int{0, 11} {
		var elem common.Hash
		elem[byteIdx] = 0xff
		copy(elem[12:], common.HexToAddress("0xdead000000000000000000000000000000000001").Bytes())
		_, err := LoadPaymentContracts(mapReader{slots: map[common.Hash]common.Hash{
			lenSlot: word(1), paymentContractSlot(0): elem,
		}})
		require.ErrorIs(t, err, ErrCorruptConfig, "padding byte %d must be rejected", byteIdx)
	}
	// Byte 12 is the first address byte, so it must be accepted.
	var elem common.Hash
	elem[12] = 0xff
	got, err := LoadPaymentContracts(mapReader{slots: map[common.Hash]common.Hash{
		lenSlot: word(1), paymentContractSlot(0): elem,
	}})
	require.NoError(t, err)
	require.Contains(t, got, common.BytesToAddress(elem[12:]))
}

// TestReadPathThroughARealTrie exercises the production read path against a
// committed state trie rather than a map, with a non-empty payment-contract set.
//
// This closes the one gap neither the other unit tests nor the devnet cover. The
// mapReader tests drive the slot arithmetic but not the reader; the devnet drives a
// real trie but its set is empty, so the enumeration loop never runs there. Here the
// state is written, COMMITTED, and reopened at the resulting root - so the reader is
// the same multiStateReader production uses, and the values come back through the
// trie encoding.
//
// It also demonstrates the property the classifier's security argument rests on: a
// reader taken before the writes does not observe them.
func TestReadPathThroughARealTrie(t *testing.T) {
	db := state.NewDatabaseForTesting()
	statedb, err := state.New(types.EmptyRootHash, db)
	require.NoError(t, err)

	// A governance-written configuration, deliberately not the defaults, so a read
	// that silently fell back would be visible.
	want := Params{
		MinRatio: 150, MaxRatio: 900, ExpandTrigger: 9_000, ShrinkTrigger: 2_500,
		ExpandStep: 300, ShrinkStep: 25, MinGas: 1_000_000, MaxGas: 9_000_000,
	}
	for i, v := range []uint64{
		want.MinRatio, want.MaxRatio, want.ExpandTrigger, want.ShrinkTrigger,
		want.ExpandStep, want.ShrinkStep, want.MinGas, want.MaxGas,
	} {
		statedb.SetState(ContractAddress, paramSlot(i), word(v))
	}
	listed := []common.Address{
		common.HexToAddress("0x00000000000000000000000000000000000a0001"),
		common.HexToAddress("0x00000000000000000000000000000000000a0002"),
		common.HexToAddress("0x00000000000000000000000000000000000a0003"),
		common.HexToAddress("0x00000000000000000000000000000000000a0004"),
		common.HexToAddress("0x00000000000000000000000000000000000a0005"),
	}
	statedb.SetState(ContractAddress, common.Hash{31: paymentContractsLenSlot}, word(uint64(len(listed))))
	for i, a := range listed {
		statedb.SetState(ContractAddress, paymentContractSlot(uint64(i)), common.BytesToHash(a[:]))
	}

	// A reader taken now is pinned to the pre-write root and must see none of it -
	// this is what makes handing the classifier a parent-root reader safe even while
	// the block advances.
	stale := statedb.Reader()
	staleParams, err := LoadParams(stale)
	require.NoError(t, err)
	require.Equal(t, defaultParams(), staleParams, "a reader pinned to the parent root must not observe in-block writes")

	root, err := statedb.Commit(1, false, false)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyRootHash, root)

	committed, err := state.New(root, db)
	require.NoError(t, err)
	r := committed.Reader()

	gotParams, err := LoadParams(r)
	require.NoError(t, err)
	require.Equal(t, want, gotParams)

	gotSet, err := LoadPaymentContracts(r)
	require.NoError(t, err)
	require.Len(t, gotSet, len(listed))
	for _, a := range listed {
		require.Contains(t, gotSet, a)
	}

	// And the quota computed from that configuration, through the same path a
	// consumer would use.
	const gasLimit = 55_000_000
	require.Equal(t, Floor(gotParams, gasLimit), LaneSize(gotParams, Signal{}, gasLimit))
	require.Equal(t, uint64(1_000_000), Floor(gotParams, gasLimit), "minRatio 150 of 55M is 825k, so minGas 1M is the binding floor")
}

// TestPaymentContractCountAboveUint64 covers the !ok arm of the count check, which no
// other test reaches. Dropping it turns a shifted storage layout from a loud
// ErrCorruptConfig into a silently EMPTY payment-contract set.
func TestPaymentContractCountAboveUint64(t *testing.T) {
	var huge common.Hash
	huge[0] = 1 // bit 255
	_, err := LoadPaymentContracts(mapReader{slots: map[common.Hash]common.Hash{
		common.Hash{31: paymentContractsLenSlot}: huge,
	}})
	require.ErrorIs(t, err, ErrCorruptConfig)
}

// TestReadBoundHasRealSlack pins the MAGNITUDE of the slack, not just its direction.
// Asserting only MaxPaymentContracts < maxPaymentContractsRead lets the slack be
// shrunk to +1, which re-creates the halt-at-258 hazard the slack exists to prevent.
func TestReadBoundHasRealSlack(t *testing.T) {
	require.GreaterOrEqual(t, uint64(maxPaymentContractsRead), uint64(4*MaxPaymentContracts),
		"the read bound must leave room for the contract's bound to be raised several times")
}
