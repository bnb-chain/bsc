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

// ABI selectors for cross-checks against the deployed contract.
const (
	selGetPaymentLaneParams = "ff620147" // getPaymentLaneParams()
	selGetPaymentContracts  = "08fcc45a" // getPaymentContracts(uint256,uint256)
	selRatioDenom           = "7c86be0d" // RATIO_DENOM()
	selMaxLaneRatio         = "4df7c732" // MAX_LANE_RATIO()
	selTriggerGapMin        = "170da6d4" // TRIGGER_GAP_MIN()
	selRatioGapMin          = "2bbf97d7" // RATIO_GAP_MIN()
	selMinExpandTriggerRat  = "04b9bb05" // MIN_EXPAND_TRIGGER_RATIO()
	selMinShrinkTriggerRat  = "393c718f" // MIN_SHRINK_TRIGGER_RATIO()
	selMaxStepRatio         = "b5fe5373" // MAX_STEP_RATIO()
	selMinLaneGas           = "a7c083f6" // MIN_LANE_GAS()
	selMaxLaneGas           = "33eab21e" // MAX_LANE_GAS()
)

// lenSlot is where the payment-contract array's length lives.
var lenSlot = common.Hash{31: paymentContractsLenSlot}

// mapReader reads literal slots; missing slots read as zero.
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

// deployedContract loads the exact bytecode installed by Gauss.
func deployedContract(t *testing.T) *state.StateDB {
	t.Helper()
	code, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	require.NoError(t, err)
	// The three networks currently ship identical bytecode.
	require.Equal(t, strings.TrimSpace(gauss.MainnetPaymentLaneContract), strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	require.Equal(t, strings.TrimSpace(gauss.ChapelPaymentLaneContract), strings.TrimSpace(gauss.RialtoPaymentLaneContract))

	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	statedb.SetCode(ContractAddress, code, tracing.CodeChangeSystemContractUpgrade)
	return statedb
}

// callContract executes a read-only call and returns 32-byte words.
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

// Tripwire for slot-layout drift between Go's slot math and the deployed bytecode.
func TestLayoutMatchesDeployedBytecode(t *testing.T) {
	t.Run("params occupy slots 0..7 in declaration order", func(t *testing.T) {
		statedb := deployedContract(t)
		// Non-zero sentinels keep the default fallback from masking a slot mismatch.
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
		statedb.SetState(ContractAddress, lenSlot, word(uint64(len(addrs))))
		for i, a := range addrs {
			statedb.SetState(ContractAddress, paymentContractSlot(uint64(i)), common.BytesToHash(a[:]))
		}
		// getPaymentContracts(0, 0) returns the full list.
		got := callContract(t, statedb, selGetPaymentContracts, make([]byte, 64)...)
		require.Len(t, got, 3+len(addrs))
		require.Equal(t, uint64(len(addrs)), got[1].Uint64(), "totalLength")
		require.Equal(t, uint64(len(addrs)), got[2].Uint64(), "page length")
		for i, a := range addrs {
			require.Equal(t, a, common.BigToAddress(got[3+i]),
				"element %d is not backed by paymentContractSlot(%d)", i, i)
		}
	})

}

// Keep Go's mirrored defaults aligned with the deployed contract.
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

	// The production read path must return the same tuple.
	params, err := LoadParams(mapReader{})
	require.NoError(t, err)
	require.Equal(t, Params{
		MinRatio: want[0], MaxRatio: want[1], ExpandTrigger: want[2], ShrinkTrigger: want[3],
		ExpandStep: want[4], ShrinkStep: want[5], MinGas: want[6], MaxGas: want[7],
	}, params)
}

// Keep mirrored constants aligned with the deployed contract.
func TestConstantsMatchDeployedBytecode(t *testing.T) {
	statedb := deployedContract(t)
	for _, tc := range []struct {
		name     string
		selector string
		want     uint64
	}{
		{"MAX_LANE_RATIO", selMaxLaneRatio, maxLaneRatio},
		{"RATIO_DENOM", selRatioDenom, RatioDenom},
		{"TRIGGER_GAP_MIN", selTriggerGapMin, triggerGapMin},
		{"RATIO_GAP_MIN", selRatioGapMin, ratioGapMin},
		{"MIN_EXPAND_TRIGGER_RATIO", selMinExpandTriggerRat, minExpandTriggerRat},
		{"MIN_SHRINK_TRIGGER_RATIO", selMinShrinkTriggerRat, minShrinkTriggerRat},
		{"MAX_STEP_RATIO", selMaxStepRatio, maxStepRatio},
		{"MIN_LANE_GAS", selMinLaneGas, minLaneGas},
		{"MAX_LANE_GAS", selMaxLaneGas, maxLaneGas},
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
		// Unreachable on chain, but the reader must still mirror _loadParams.
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
		// Callers handle config corruption differently from state-read failures.
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

	// A garbage length must fail fast instead of looping forever.
	t.Run("a repeated element is a layout error", func(t *testing.T) {
		r := mapReader{slots: map[common.Hash]common.Hash{lenSlot: word(1_000_000_000)}}
		_, err := LoadPaymentContracts(r)
		require.ErrorIs(t, err, ErrCorruptConfig)
		require.ErrorContains(t, err, "duplicate")
	})

	t.Run("a read failure propagates", func(t *testing.T) {
		boom := errors.New("missing trie node")
		_, err := LoadPaymentContracts(mapReader{err: boom})
		require.ErrorIs(t, err, ErrStateUnavailable)
		require.ErrorIs(t, err, boom)
	})
}

// Keep the duplicated address in sync without importing systemcontracts in production.
func TestContractAddress(t *testing.T) {
	require.Equal(t, common.HexToAddress("0x0000000000000000000000000000000000002007"), ContractAddress)
	require.Equal(t, common.HexToAddress(systemcontracts.PaymentLaneContract), ContractAddress)
}

// Byte 23 is the first byte above uint64.
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

// Byte 11 is the last padding byte above the 20-byte address.
func TestPaymentContractPaddingBoundaryByte(t *testing.T) {
	for _, byteIdx := range []int{0, 11} {
		var elem common.Hash
		elem[byteIdx] = 0xff
		copy(elem[12:], common.HexToAddress("0xdead000000000000000000000000000000000001").Bytes())
		_, err := LoadPaymentContracts(mapReader{slots: map[common.Hash]common.Hash{
			lenSlot: word(1), paymentContractSlot(0): elem,
		}})
		require.ErrorIs(t, err, ErrCorruptConfig, "padding byte %d must be rejected", byteIdx)
	}
	// Byte 12 is the first address byte.
	var elem common.Hash
	elem[12] = 0xff
	got, err := LoadPaymentContracts(mapReader{slots: map[common.Hash]common.Hash{
		lenSlot: word(1), paymentContractSlot(0): elem,
	}})
	require.NoError(t, err)
	require.Contains(t, got, common.BytesToAddress(elem[12:]))
}

// Exercise the production reader against a committed trie and a non-empty whitelist.
func TestReadPathThroughARealTrie(t *testing.T) {
	db := state.NewDatabaseForTesting()
	statedb, err := state.New(types.EmptyRootHash, db)
	require.NoError(t, err)

	// Non-default values make an accidental fallback visible.
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
	statedb.SetState(ContractAddress, lenSlot, word(uint64(len(listed))))
	for i, a := range listed {
		statedb.SetState(ContractAddress, paymentContractSlot(uint64(i)), common.BytesToHash(a[:]))
	}

	// A reader taken before the writes stays pinned to the old root.
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

	// Sanity-check the resulting quota through the consumer path.
	const gasLimit = 55_000_000
	require.Equal(t, laneFloor(gotParams, gasLimit), Signal{}.NextLaneSize(gotParams, gasLimit))
	require.Equal(t, uint64(1_000_000), laneFloor(gotParams, gasLimit), "minRatio 150 of 55M is 825k, so minGas 1M is the binding floor")
}

// Large but valid lists must still load.
func TestNoLengthCeilingIsReintroduced(t *testing.T) {
	for _, n := range []uint64{257, 1025, 4097} {
		slots := map[common.Hash]common.Hash{lenSlot: word(n)}
		for i := uint64(0); i < n; i++ {
			a := common.BigToAddress(new(big.Int).SetUint64(i + 0x10000))
			slots[paymentContractSlot(i)] = common.BytesToHash(a[:])
		}
		got, err := LoadPaymentContracts(mapReader{slots: slots})
		require.NoError(t, err, "a %d-entry list must load", n)
		require.Len(t, got, int(n))
	}
}
