package miner

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"runtime"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/systemcontracts/jenner"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// laneMinerChain builds an Ethash-driven BSC lane harness and preallocates 0x2007.
func laneMinerChain(t *testing.T, corruptRatio bool) (*worker, *params.ChainConfig, *types.Header, *types.Header, *ecdsa.PrivateKey) {
	t.Helper()

	code, err := hex.DecodeString(strings.TrimSpace(jenner.RialtoPaymentLaneContract))
	require.NoError(t, err)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	config := *params.ParliaTestChainConfig
	jennerTime := uint64(15)
	config.HaberTime = new(uint64)
	config.HaberFixTime = new(uint64)
	config.BohrTime = new(uint64)
	config.PascalTime = new(uint64)
	config.PragueTime = new(uint64)
	config.LorentzTime = new(uint64)
	config.MaxwellTime = new(uint64)
	config.FermiTime = new(uint64)
	config.OsakaTime = new(uint64)
	config.MendelTime = new(uint64)
	config.PasteurTime = new(uint64)
	config.JennerTime = &jennerTime
	config.BlobScheduleConfig = &params.BlobScheduleConfig{
		Cancun: params.DefaultCancunBlobConfig,
		Prague: params.DefaultPragueBlobConfigBSC,
	}

	lane := types.Account{Code: code, Balance: common.Big0}
	if corruptRatio {
		// Slot 0 is _paymentLaneRatio; 2^248 is far outside section 3.6.1's guard, and a direct
		// write is the only way past updateParam's own check.
		lane.Storage = map[common.Hash]common.Hash{{}: {0: 1}}
	}
	gspec := &core.Genesis{
		Config:   &config,
		GasLimit: 55_000_000,
		Alloc: types.GenesisAlloc{
			paymentlane.ContractAddress:           lane,
			crypto.PubkeyToAddress(key.PublicKey): {Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))},
		},
	}
	db, blocks, _ := core.GenerateChainWithGenesis(gspec, ethash.NewFullFaker(), 2, nil)
	chain, err := core.NewBlockChain(db, gspec, ethash.NewFullFaker(), core.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(chain.Stop)
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err)

	parent := blocks[len(blocks)-1].Header()
	require.True(t, config.IsJenner(parent.Number, parent.Time),
		"the candidate must be a lane block, or every assertion built on it is vacuous")

	return &worker{chain: chain, chainConfig: &config, engine: ethash.NewFullFaker()}, &config, parent, &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   parent.GasLimit,
		Time:       parent.Time + 10,
		Difficulty: common.Big0,
		BaseFee:    common.Big0,
	}, key
}

// Lane resolution is the only makeEnv state read that can fail, and it runs after StartPrefetcher so
// that a witness build records the getters; its error path must therefore stop the prefetcher itself.
func TestMakeEnvLeavesNoPrefetcherWhenTheLaneFails(t *testing.T) {
	w, _, parent, header, _ := laneMinerChain(t, true)

	// One leaked prefetcher per failure would show up well above this threshold.
	const iterations = 20
	before := runtime.NumGoroutine()
	for i := 0; i < iterations; i++ {
		env, err := w.makeEnv(parent, header, common.Address{}, nil, false)
		require.ErrorIs(t, err, paymentlane.ErrCorruptConfig)
		require.Nil(t, env)
	}
	runtime.Gosched()
	require.Less(t, runtime.NumGoroutine()-before, iterations/2,
		"makeEnv leaked prefetcher goroutines on the lane error path")
}

// Failed bid-path transactions must not book lane gas even if buyGas already ran.
func TestBidCommitTransactionBooksNothingForAFailedTransaction(t *testing.T) {
	w, config, parent, header, key := laneMinerChain(t, false)

	env, err := w.makeEnv(parent, header, common.Address{}, nil, false)
	require.NoError(t, err)
	require.True(t, env.lane.On(), "the lane must bind, or the assertion below is vacuous")

	// 20,000 gas buys successfully but fails intrinsic gas.
	tx, err := types.SignTx(types.NewTransaction(0, common.Address{0xaa}, big.NewInt(1), 20_000, common.Big0, nil),
		types.LatestSigner(config), key)
	require.NoError(t, err)
	require.Equal(t, paymentlane.PaymentLane, env.lane.Classify(tx), "the fixture must be a payment transaction")

	r := &BidRuntime{env: env}
	require.ErrorIs(t, r.commitTransaction(w.chain, config, tx, false), core.ErrIntrinsicGas)
	require.NotZero(t, env.gasPool.Used(), "buyGas must have drawn from the pool, or the test proves nothing")
	require.Zero(t, env.lane.Budget.PaymentLaneUsed, "a failed transaction must not book lane gas")
}

func TestPaymentLaneWorkerRollback(t *testing.T) {
	for _, amsterdam := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "amsterdam"}[amsterdam], func(t *testing.T) {
			w, config, parent, header, key := laneMinerChain(t, false)
			if amsterdam {
				config.AmsterdamTime = &header.Time
			}
			env, err := w.makeEnv(parent, header, common.Address{}, nil, false)
			require.NoError(t, err)
			defer env.discard()
			require.True(t, env.lane.On())
			sender := crypto.PubkeyToAddress(key.PublicKey)
			balance := env.state.GetBalance(sender).ToBig()
			if amsterdam {
				require.NoError(t, env.gasPool.ReserveGasAmsterdam(25_000, 300_000))
			}
			before := env.gasPool.Snapshot()
			pool := env.gasPool.String()
			tx, err := types.SignTx(types.NewTransaction(0, common.Address{0xaa}, big.NewInt(1), 1, big.NewInt(1), nil), types.LatestSigner(config), key)
			require.NoError(t, err)
			require.Equal(t, paymentlane.PaymentLane, env.lane.Classify(tx))
			_, err = w.applyTransaction(env, tx)
			require.ErrorIs(t, err, core.ErrIntrinsicGas)
			require.Equal(t, pool, env.gasPool.String())
			require.Equal(t, before, env.gasPool)
			require.Equal(t, balance, env.state.GetBalance(sender).ToBig())
			require.Zero(t, env.lane.PaymentGasUsed())
			require.Equal(t, env.lane.Budget.PaymentLaneQuota, env.lane.IdleGas())
		})
	}
}

func TestBidPaymentGasReservation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		amsterdam bool
		gas       uint64
	}{
		{"legacy", false, params.PayBidTxGasLimit},
		{"amsterdam", true, params.PayBidTxGasLimit},
		{"amsterdam-state-creation", true, 300_000},
		{"amsterdam-above-execution-cap", true, 30_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, config, parent, header, key := laneMinerChain(t, false)
			if tc.amsterdam {
				zero := uint64(0)
				config.AmsterdamTime = &zero
				slot := header.Number.Uint64()
				header.SlotNumber = &slot
			}
			header.ExcessBlobGas = new(uint64)
			header.BlobGasUsed = new(uint64)
			env, err := w.makeEnv(parent, header, common.Address{}, nil, false)
			require.NoError(t, err)
			defer env.discard()
			sender := crypto.PubkeyToAddress(key.PublicKey)
			recipient := common.Address{0xaa}
			payee := sender
			if tc.gas == 300_000 {
				payee = recipient
			}
			pay, err := types.SignTx(types.NewTransaction(1, payee, big.NewInt(1), tc.gas, common.Big0, nil), types.LatestSigner(config), key)
			require.NoError(t, err)
			if tc.amsterdam {
				require.NoError(t, env.gasPool.ReserveGasAmsterdam(min(pay.Gas(), params.MaxTxGas), pay.Gas()))
			} else {
				require.NoError(t, env.gasPool.CheckGasLegacy(params.PayBidTxGasLimit))
			}
			if tc.amsterdam {
				require.Zero(t, env.gasPool.Used())
				require.Zero(t, env.lane.PaymentGasUsed())
			}
			// The preceding payment must be booked even while the final payment has a reservation.
			tx, err := types.SignTx(types.NewTransaction(0, sender, big.NewInt(1), 25_000, common.Big0, nil), types.LatestSigner(config), key)
			require.NoError(t, err)
			r := &BidRuntime{env: env, bid: &buildertypes.Bid{}}
			require.NoError(t, r.commitTransaction(w.chain, config, tx, true))
			if tc.amsterdam {
				require.Equal(t, uint64(12_000), env.lane.PaymentGasUsed())
			} else {
				require.Equal(t, uint64(21_000), env.lane.PaymentGasUsed())
			}
			before := env.gasPool.Snapshot()
			if tc.amsterdam {
				require.NoError(t, env.gasPool.ReleaseGasAmsterdam(min(pay.Gas(), params.MaxTxGas), pay.Gas()))
			} else {
				require.NoError(t, env.gasPool.AddGas(params.PayBidTxGasLimit))
			}
			require.Equal(t, before.CumulativeUsed(), env.gasPool.CumulativeUsed())
			require.NoError(t, r.commitTransaction(w.chain, config, pay, true))
			require.Equal(t, types.ReceiptStatusSuccessful, env.receipts[1].Status)
			require.NoError(t, env.lane.VerifyPackedBid(env.gasPool))
			if tc.amsterdam {
				require.Equal(t, max(env.gasPool.CumulativeExecution(), env.gasPool.CumulativeState()), env.lane.PaymentGasUsed())
				require.Equal(t, env.receipts[0].GasUsed+env.receipts[1].GasUsed, env.gasPool.CumulativeUsed())
				if payee == recipient {
					require.Equal(t, uint64(params.AccountCreationSize)*env.evm.Context.CostPerStateByte, env.gasPool.CumulativeState())
				}
			} else {
				require.Equal(t, uint64(42_000), env.gasPool.Used())
				require.Equal(t, uint64(42_000), env.lane.PaymentGasUsed())
			}
		})
	}
}
