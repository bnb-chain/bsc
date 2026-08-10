package miner

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// laneMinerChain builds the ethash-backed lane harness and preallocates 0x2007.
func laneMinerChain(t *testing.T, corruptParams bool) (*worker, *params.ChainConfig, *types.Header, *types.Header, *ecdsa.PrivateKey) {
	t.Helper()

	code, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	require.NoError(t, err)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	config := *params.AllEthashProtocolChanges
	gaussTime := uint64(15)
	config.GaussTime = &gaussTime

	lane := types.Account{Code: code, Balance: common.Big0}
	if corruptParams {
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
	db, blocks, _ := core.GenerateChainWithGenesis(gspec, ethash.NewFaker(), 2, nil)
	chain, err := core.NewBlockChain(db, gspec, ethash.NewFaker(), core.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(chain.Stop)
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err)

	parent := blocks[len(blocks)-1].Header()
	require.True(t, config.IsGauss(parent.Number, parent.Time),
		"the candidate must be a lane block, or every assertion built on it is vacuous")

	return &worker{chain: chain, chainConfig: &config}, &config, parent, &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   parent.GasLimit,
		Time:       parent.Time + 10,
		Difficulty: common.Big0,
		BaseFee:    common.Big0,
	}, key
}

// laneBidBlockHarness adds the local validator environment and expected quota.
func laneBidBlockHarness(t *testing.T) (*worker, *types.Header, *environment, uint64) {
	t.Helper()

	w, _, parent, header, _ := laneMinerChain(t, false)
	parentState, err := w.chain.StateAt(parent)
	require.NoError(t, err)
	require.NotEmpty(t, parentState.GetCode(paymentlane.ContractAddress),
		"0x2007 must carry code, or the parameters are defaults by accident")

	local := &environment{header: types.CopyHeader(header), state: parentState}

	const wantLaneSize = 2_000_000
	return w, header, local, wantLaneSize
}

// bidBlockWith wraps a header and transactions as admission would leave them.
func bidBlockWith(header *types.Header, txs ...*types.Transaction) *buildertypes.DecodedBidBlock {
	return &buildertypes.DecodedBidBlock{
		Header:        header,
		Txs:           txs,
		SystemTxStart: len(txs), // no trailing unsigned system txs
	}
}

// TestVerifyBidBlockLaneQuota checks the cheap validator-side quota gate.
func TestVerifyBidBlockLaneQuota(t *testing.T) {
	w, header, local, laneSize := laneBidBlockHarness(t)

	stranger := common.Address{0xbe, 0xef}
	transfer := types.NewTx(&types.LegacyTx{To: &stranger, Value: common.Big1, Gas: params.TxGas})

	for _, tc := range []struct {
		name       string
		commitment common.Hash
		gasUsed    uint64
		txs        []*types.Transaction
		wantErr    error
	}{
		{
			name:       "a truthful commitment is accepted",
			commitment: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: params.TxGas}),
			gasUsed:    laneSize,
			txs:        []*types.Transaction{transfer},
		},
		{
			name:       "an unstamped uncle slot is refused",
			commitment: types.EmptyUncleHash,
			wantErr:    paymentlane.ErrBadCommitment,
		},
		{
			name:       "a quota the recursion does not derive is refused",
			commitment: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize - 1}),
			wantErr:    paymentlane.ErrQuotaMismatch,
		},
		{
			name:       "more payment gas than these transactions can consume is refused",
			commitment: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: 2 * params.TxGas}),
			gasUsed:    laneSize,
			txs:        []*types.Transaction{transfer},
			wantErr:    paymentlane.ErrUntruthy,
		},
		{
			name:       "understated payment gas is beyond what this check can see",
			commitment: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize}),
			gasUsed:    params.TxGas,
			txs:        []*types.Transaction{transfer},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := types.CopyHeader(header)
			h.UncleHash = tc.commitment
			h.GasUsed = tc.gasUsed

			err := w.verifyBidBlockLaneQuota(bidBlockWith(h, tc.txs...), local)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestVerifyBidBlockLaneQuotaBoundsByDeclaredLimits checks the declared-gas ceiling.
func TestVerifyBidBlockLaneQuotaBoundsByDeclaredLimits(t *testing.T) {
	w, header, local, laneSize := laneBidBlockHarness(t)

	stranger := common.Address{0xbe, 0xef}
	fat := types.NewTx(&types.LegacyTx{To: &stranger, Value: common.Big1, Gas: 5_000_000})

	h := types.CopyHeader(header)
	h.GasUsed = 6_000_000
	h.UncleHash = paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: 5_000_000})
	require.NoError(t, w.verifyBidBlockLaneQuota(bidBlockWith(h, fat), local),
		"a payment total up to the declared limit must be accepted, or honest blocks are refused")

	h.UncleHash = paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: 5_000_001})
	require.ErrorIs(t, w.verifyBidBlockLaneQuota(bidBlockWith(h, fat), local), paymentlane.ErrUntruthy,
		"one gas past every declared limit in the block is still refused")
}

// TestVerifyBidBlockLaneQuotaSkipsTheSystemTxRegion ignores the system-tx suffix.
func TestVerifyBidBlockLaneQuotaSkipsTheSystemTxRegion(t *testing.T) {
	w, header, local, laneSize := laneBidBlockHarness(t)

	stranger := common.Address{0xbe, 0xef}
	transfer := types.NewTx(&types.LegacyTx{To: &stranger, Value: common.Big1, Gas: params.TxGas})
	trailing := types.NewTx(&types.LegacyTx{To: &stranger, Nonce: 1, Value: common.Big1, Gas: params.TxGas})

	h := types.CopyHeader(header)
	h.GasUsed = laneSize
	h.UncleHash = paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: params.TxGas})

	decoded := &buildertypes.DecodedBidBlock{
		Header:        h,
		Txs:           []*types.Transaction{transfer, trailing},
		SystemTxStart: 1,
	}
	require.NoError(t, w.verifyBidBlockLaneQuota(decoded, local),
		"one user transfer permits exactly 21,000 of payment gas")

	h.UncleHash = paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: 2 * params.TxGas})
	require.ErrorIs(t, w.verifyBidBlockLaneQuota(decoded, local), paymentlane.ErrUntruthy,
		"the system-tx region must not raise the ceiling")
}

// TestVerifyBidBlockLaneQuotaRequiresTheLocalParent checks the local-parent precondition.
func TestVerifyBidBlockLaneQuotaRequiresTheLocalParent(t *testing.T) {
	w, header, local, _ := laneBidBlockHarness(t)

	h := types.CopyHeader(header)
	h.ParentHash = common.Hash{0x99}

	err := w.verifyBidBlockLaneQuota(bidBlockWith(h), local)
	require.ErrorContains(t, err, "is not the parent the local state is open on")
}

// TestVerifyBidBlockLaneQuotaSkipsAPreActivationHeader keeps pre-Gauss blocks untouched.
func TestVerifyBidBlockLaneQuotaSkipsAPreActivationHeader(t *testing.T) {
	w, header, local, _ := laneBidBlockHarness(t)

	preGauss := *w.chainConfig
	preGauss.GaussTime = nil
	w.chainConfig = &preGauss

	h := types.CopyHeader(header)
	h.UncleHash = types.EmptyUncleHash
	require.NoError(t, w.verifyBidBlockLaneQuota(bidBlockWith(h), local))
}
