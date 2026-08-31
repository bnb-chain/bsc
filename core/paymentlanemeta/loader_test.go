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

const paymentContractsLenSlot = 8

func deployedContractState(t *testing.T) *state.StateDB {
	t.Helper()
	code, err := hex.DecodeString(strings.TrimSpace(jenner.RialtoPaymentLaneContract))
	require.NoError(t, err)

	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	require.NoError(t, err)
	statedb.SetCode(paymentlane.ContractAddress, code, tracing.CodeChangeSystemContractUpgrade)
	return statedb
}

func paramSlot(i int) common.Hash {
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

func TestLoadMetaReadsDefaults(t *testing.T) {
	loadMetaCache = metaCache{}
	statedb := deployedContractState(t)

	got, err := LoadMeta(params.BSCChainConfig, laneHeader(60_000_000), statedb)
	require.NoError(t, err)
	require.Equal(t, paymentlane.GovernanceParams{
		MinRatio:      200,
		MaxRatio:      800,
		ExpandTrigger: 8_000,
		ShrinkTrigger: 7_000,
		ExpandStep:    200,
		ShrinkStep:    50,
		MinGas:        2_000_000,
		MaxGas:        8_000_000,
	}, got.GovernanceParams())
	require.Nil(t, got.listed)
}

func TestLoadMetaPagesLongListsAndReadsGovernedParams(t *testing.T) {
	loadMetaCache = metaCache{}
	statedb := deployedContractState(t)
	wantGovernanceParams := paymentlane.GovernanceParams{
		MinRatio:      150,
		MaxRatio:      900,
		ExpandTrigger: 9_000,
		ShrinkTrigger: 2_500,
		ExpandStep:    300,
		ShrinkStep:    25,
		MinGas:        1_000_000,
		MaxGas:        9_000_000,
	}
	for i, v := range []uint64{
		wantGovernanceParams.MinRatio, wantGovernanceParams.MaxRatio, wantGovernanceParams.ExpandTrigger, wantGovernanceParams.ShrinkTrigger,
		wantGovernanceParams.ExpandStep, wantGovernanceParams.ShrinkStep, wantGovernanceParams.MinGas, wantGovernanceParams.MaxGas,
	} {
		statedb.SetState(paymentlane.ContractAddress, paramSlot(i), word(v))
	}
	listed := make([]common.Address, 300)
	statedb.SetState(paymentlane.ContractAddress, common.Hash{31: paymentContractsLenSlot}, word(uint64(len(listed))))
	for i := range listed {
		listed[i] = common.BigToAddress(new(big.Int).SetUint64(uint64(i + 0x10000)))
		statedb.SetState(paymentlane.ContractAddress, paymentContractSlot(uint64(i)), common.BytesToHash(listed[i][:]))
	}

	got, err := LoadMeta(params.BSCChainConfig, laneHeader(60_000_000), statedb)
	require.NoError(t, err)
	require.Equal(t, wantGovernanceParams, got.GovernanceParams())
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
	statedb.SetState(paymentlane.ContractAddress, common.Hash{31: paymentContractsLenSlot}, word(maxListedContracts+1))

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

func TestLoadGovernanceParamsForQuotaStaysOnParentRoot(t *testing.T) {
	db := state.NewDatabaseForTesting()
	statedb, err := state.New(types.EmptyRootHash, db)
	require.NoError(t, err)

	code, err := hex.DecodeString(strings.TrimSpace(jenner.RialtoPaymentLaneContract))
	require.NoError(t, err)
	statedb.SetCode(paymentlane.ContractAddress, code, tracing.CodeChangeSystemContractUpgrade)
	statedb.SetState(paymentlane.ContractAddress, paramSlot(6), word(3_000_000))

	root, err := statedb.Commit(1, false, false)
	require.NoError(t, err)

	live, err := state.New(root, db)
	require.NoError(t, err)
	live.SetState(paymentlane.ContractAddress, paramSlot(6), word(9_000_000))

	parent := laneHeader(60_000_000)
	parent.Root = root
	header := laneHeader(60_000_001)

	got, err := LoadGovernanceParamsForQuota(params.BSCChainConfig, parent, header, live)
	require.NoError(t, err)
	require.Equal(t, uint64(3_000_000), got.MinGas)
}

// Slot 1 is paymentLaneMaxRatio; a direct write is the only way past updateParam's validation.
func TestLoadMetaRejectsATupleViolatingTheGuards(t *testing.T) {
	statedb := deployedContractState(t)
	statedb.SetState(paymentlane.ContractAddress, paramSlot(1), word(paymentlane.MaxLaneRatio+1))

	_, err := LoadMeta(params.BSCChainConfig, laneHeader(1), statedb)
	require.ErrorIs(t, err, paymentlane.ErrCorruptConfig)
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
		statedb.SetState(paymentlane.ContractAddress, common.Hash{31: paymentContractsLenSlot}, word(pageSize))
		for i := uint64(0); i < pageSize; i++ {
			statedb.SetState(paymentlane.ContractAddress, paymentContractSlot(i), word(i+1))
		}

		evm := vm.NewEVM(blockContext(header), statedb, &cfg, vm.Config{NoBaseFee: true})
		budget := vm.NewGasBudget(getterGasLimit)
		_, left, err := evm.StaticCall(common.Address{}, paymentlane.ContractAddress, packGetPaymentContracts(0, pageSize), budget)
		evm.Release()
		require.NoError(t, err, tc.fork)

		used := left.Used(budget)
		require.EqualValues(t, 356_819, used, "%s: getter gas moved; confirm getterGasLimit still leaves room, then update this figure", tc.fork)
		require.Less(t, used*10, getterGasLimit, "%s: getterGasLimit no longer leaves an order of magnitude", tc.fork)
	}
}
