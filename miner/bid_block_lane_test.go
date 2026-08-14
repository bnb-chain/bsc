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
	"github.com/ethereum/go-ethereum/core/systemcontracts/jenner"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// laneMinerChain builds the ethash-backed lane harness and preallocates 0x2007.
func laneMinerChain(t *testing.T, corruptParams bool) (*worker, *params.ChainConfig, *types.Header, *types.Header, *ecdsa.PrivateKey) {
	t.Helper()

	code, err := hex.DecodeString(strings.TrimSpace(jenner.RialtoPaymentLaneContract))
	require.NoError(t, err)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	config := *params.AllEthashProtocolChanges
	jennerTime := uint64(15)
	config.JennerTime = &jennerTime

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
	require.True(t, config.IsJenner(parent.Number, parent.Time),
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

// bidBlockWith wraps a bare header as admission would leave it: no transactions, so no trailing
// unsigned system txs either.
func bidBlockWith(header *types.Header) *buildertypes.DecodedBidBlock {
	return &buildertypes.DecodedBidBlock{Header: header}
}

// verifyBidBlockLaneQuota reads only the header, so every case here is a header the validator
// either accepts or refuses before blind-signing.
func TestVerifyBidBlockLaneQuota(t *testing.T) {
	w, header, local, laneSize := laneBidBlockHarness(t)

	for _, tc := range []struct {
		name       string
		commitment common.Hash
		wantErr    error
	}{
		{
			name:       "a truthful commitment is accepted",
			commitment: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: params.TxGas}),
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
			name:       "paymentGasUsed is not examined here, however wrong it is",
			commitment: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: laneSize}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := types.CopyHeader(header)
			h.UncleHash = tc.commitment

			err := w.verifyBidBlockLaneQuota(bidBlockWith(h), local)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
