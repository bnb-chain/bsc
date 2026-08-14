package paymentlanemeta

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts/jenner"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
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
	require.Equal(t, paymentlane.Params{
		MinRatio:      200,
		MaxRatio:      800,
		ExpandTrigger: 8_000,
		ShrinkTrigger: 7_000,
		ExpandStep:    200,
		ShrinkStep:    50,
		MinGas:        2_000_000,
		MaxGas:        8_000_000,
	}, got.Params())
	require.Nil(t, got.listed)
}

func TestLoadMetaPagesLongListsAndReadsGovernedParams(t *testing.T) {
	loadMetaCache = metaCache{}
	statedb := deployedContractState(t)
	wantParams := paymentlane.Params{
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
		wantParams.MinRatio, wantParams.MaxRatio, wantParams.ExpandTrigger, wantParams.ShrinkTrigger,
		wantParams.ExpandStep, wantParams.ShrinkStep, wantParams.MinGas, wantParams.MaxGas,
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
	require.Equal(t, wantParams, got.Params())
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

func TestLoadParamsForQuotaStaysOnParentRoot(t *testing.T) {
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

	got, err := LoadParamsForQuota(params.BSCChainConfig, parent, header, live)
	require.NoError(t, err)
	require.Equal(t, uint64(3_000_000), got.MinGas)
}
