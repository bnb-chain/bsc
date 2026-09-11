package paymentlanemeta

import (
	"encoding/hex"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts/jenner"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/params/forks"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// Storage layout of the new PaymentLane: slot 0 is _paymentLaneRatio, slot 1 is the
// EnumerableSet's _values array (its length here, its elements at keccak(1)+i).
const (
	ratioSlot               = 0
	paymentContractsLenSlot = 1
)

func deployedContractState(t *testing.T) *state.StateDB {
	t.Helper()
	code, err := hex.DecodeString(strings.TrimSpace(jenner.RialtoPaymentLaneContract))
	require.NoError(t, err)

	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	statedb.SetCode(paymentlane.ContractAddress, code, tracing.CodeChangeSystemContractUpgrade)
	return statedb
}

func slot(i int) common.Hash {
	return common.Hash{31: byte(i)}
}

func paymentContractSlot(i uint64) common.Hash {
	base := new(uint256.Int).SetBytes32(crypto.Keccak256(common.Hash{31: paymentContractsLenSlot}.Bytes()))
	return base.AddUint64(base, i).Bytes32()
}

func word(v uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(v))
}

func laneHeader(number uint64) *types.Header {
	return &types.Header{
		Number:     new(big.Int).SetUint64(number),
		GasLimit:   55_000_000,
		Time:       1_800_000_000 + number,
		Difficulty: common.Big1,
		BaseFee:    common.Big0,
	}
}

func TestLoadMetaReadsTheUnwrittenDefault(t *testing.T) {
	loadMetaCache = metaCache{}
	statedb := deployedContractState(t)

	got, err := LoadMeta(params.BSCChainConfig, laneHeader(60_000_000), statedb)
	require.NoError(t, err)
	require.EqualValues(t, 500, got.ratio, "an unwritten slot must read as the contract's default, not as zero")
	require.EqualValues(t, 2_750_000, got.Quota(55_000_000))
	require.Nil(t, got.listed)
}

func TestLoadMetaPagesLongListsAndReadsTheGovernedRatio(t *testing.T) {
	loadMetaCache = metaCache{}
	statedb := deployedContractState(t)
	statedb.SetState(paymentlane.ContractAddress, slot(ratioSlot), word(800))

	listed := make([]common.Address, 300)
	statedb.SetState(paymentlane.ContractAddress, slot(paymentContractsLenSlot), word(uint64(len(listed))))
	for i := range listed {
		listed[i] = common.BigToAddress(new(big.Int).SetUint64(uint64(i + 0x10000)))
		statedb.SetState(paymentlane.ContractAddress, paymentContractSlot(uint64(i)), common.BytesToHash(listed[i][:]))
	}

	got, err := LoadMeta(params.BSCChainConfig, laneHeader(60_000_000), statedb)
	require.NoError(t, err)
	require.EqualValues(t, 800, got.ratio)
	require.Len(t, got.listed, len(listed))
	for _, addr := range listed {
		require.Contains(t, got.listed, addr)
	}
}

func TestLoadMetaReusesCachedMeta(t *testing.T) {
	loadMetaCache = metaCache{}
	statedb := deployedContractState(t)

	got1, err := LoadMeta(params.BSCChainConfig, laneHeader(60_000_000), statedb)
	require.NoError(t, err)
	got2, err := LoadMeta(params.BSCChainConfig, laneHeader(60_000_001), statedb)
	require.NoError(t, err)
	require.Same(t, got1, got2)
}

func TestLoadMetaRejectsListedSetAboveContractLimit(t *testing.T) {
	loadMetaCache = metaCache{}
	statedb := deployedContractState(t)
	statedb.SetState(paymentlane.ContractAddress, slot(paymentContractsLenSlot), word(paymentlane.MaxListedContracts+1))

	_, err := LoadMeta(params.BSCChainConfig, laneHeader(60_000_000), statedb)
	require.ErrorIs(t, err, paymentlane.ErrCorruptConfig)
	require.Contains(t, err.Error(), "exceeds limit")
}

func TestAppendPageRejectsPageLargerThanPageSize(t *testing.T) {
	page := make([]common.Address, pageSize+1)
	err := appendPage(make(map[common.Address]struct{}), 0, page, uint64(len(page)))
	require.ErrorIs(t, err, paymentlane.ErrCorruptConfig)
	require.Contains(t, err.Error(), "returned 129 entries")
}

func TestAppendPageRejectsOverflowingPageLength(t *testing.T) {
	page := make([]common.Address, 2)
	err := appendPage(make(map[common.Address]struct{}), math.MaxUint64-1, page, math.MaxUint64)
	require.ErrorIs(t, err, paymentlane.ErrCorruptConfig)
	require.Contains(t, err.Error(), "length 2 exceeds totalLength")
}

// The one governance value a node reads, and the only way past updateParam's own guard is a
// direct write - which is exactly the corruption 3.6.4 says must reject the block.
func TestLoadMetaRejectsARatioOutsideTheGuard(t *testing.T) {
	for _, bad := range []uint64{paymentlane.MaxLaneRatio + 1, math.MaxUint64} {
		loadMetaCache = metaCache{}
		statedb := deployedContractState(t)
		statedb.SetState(paymentlane.ContractAddress, slot(ratioSlot), word(bad))

		_, err := LoadMeta(params.BSCChainConfig, laneHeader(1), statedb)
		require.ErrorIsf(t, err, paymentlane.ErrCorruptConfig, "ratio %d", bad)
	}
}

// A full page is the most one getter call can cost: the walk pages at pageSize, so this figure
// does not grow with the list. Every rule set the lane can run under has to leave room for it.
func TestPageGasStaysFarBelowTheGetterBudget(t *testing.T) {
	zero := uint64(0)
	fromJenner := []struct {
		fork     forks.Fork
		activate func(*params.ChainConfig)
	}{
		{forks.Jenner, func(c *params.ChainConfig) { c.JennerTime = &zero }},
		{forks.BPO1, func(c *params.ChainConfig) { c.BPO1Time = &zero }},
		{forks.BPO2, func(c *params.ChainConfig) { c.BPO2Time = &zero }},
		{forks.BPO3, func(c *params.ChainConfig) { c.BPO3Time = &zero }},
		{forks.BPO4, func(c *params.ChainConfig) { c.BPO4Time = &zero }},
		{forks.BPO5, func(c *params.ChainConfig) { c.BPO5Time = &zero }},
		{forks.Amsterdam, func(c *params.ChainConfig) { c.AmsterdamTime = &zero }},
	}
	last := fromJenner[len(fromJenner)-1].fork
	require.Equal(t, "Unknown fork", (last + 1).String(), "a fork was added after %s; append it here", last)

	cfg := *params.BSCChainConfig
	for _, tc := range fromJenner {
		tc.activate(&cfg)
		header := laneHeader(60_000_000)
		require.Equal(t, tc.fork, cfg.LatestFork(header.Time))

		statedb := deployedContractState(t)
		statedb.SetState(paymentlane.ContractAddress, slot(paymentContractsLenSlot), word(pageSize))
		for i := uint64(0); i < pageSize; i++ {
			statedb.SetState(paymentlane.ContractAddress, paymentContractSlot(i), word(i+1))
		}

		evm := vm.NewEVM(blockContext(header), statedb, &cfg, vm.Config{NoBaseFee: true})
		budget := vm.NewGasBudget(getterGasLimit)
		_, left, err := evm.StaticCall(common.Address{}, paymentlane.ContractAddress, packGetPaymentContracts(0, pageSize), budget)
		evm.Release()
		require.NoError(t, err, tc.fork)

		used := left.Used(budget)
		require.EqualValues(t, 356_774, used, "%s: getter gas moved; confirm getterGasLimit still leaves room, then update this figure", tc.fork)
		require.Less(t, used*10, getterGasLimit, "%s: getterGasLimit no longer leaves an order of magnitude", tc.fork)
	}
}
