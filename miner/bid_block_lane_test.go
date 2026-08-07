package miner

import (
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

// laneBidBlockHarness builds a chain whose head is a lane block's parent, and returns the
// pieces verifyBidBlockLaneQuota needs: a worker, a header for the block under test, a
// local environment standing in for the validator's own build, and the quota that header
// must commit.
//
// The worker and the environment are filled in by hand rather than started: the function
// under test reads the chain, the config, the local header's parent and the local state's
// reader, and a real miner would drag in a Parlia-formatted genesis for no gain. The lane
// runs on ethash for the same reason every core-side lane test does - GenerateChain cannot
// run the system-contract upgrade, so 0x2007 goes into the genesis allocation instead.
//
// Fork timing: genesis is at t=0 with a 10s block interval, so Gauss at 15 makes block 2
// the activation block and block 3 the first block the rules bind. The chain is two blocks
// long and the header under test is block 3.
//
// 55M matches core's harness, so the derived floor is the same 2M. Nothing here exercises
// expand or shrink, for which anything above 33.3M would do.
func laneBidBlockHarness(t *testing.T) (*worker, *types.Header, *environment, uint64) {
	t.Helper()

	code, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	require.NoError(t, err)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	config := *params.AllEthashProtocolChanges
	gaussTime := uint64(15)
	config.GaussTime = &gaussTime

	gspec := &core.Genesis{
		Config:   &config,
		GasLimit: 55_000_000,
		Alloc: types.GenesisAlloc{
			paymentlane.ContractAddress:           {Code: code, Balance: common.Big0},
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
	parentState, err := chain.StateAt(parent)
	require.NoError(t, err)

	w := &worker{chain: chain, chainConfig: &config}
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   parent.GasLimit,
		Time:       parent.Time + 10,
		// Header.Size() dereferences both, and the packing loop's size accounting reads it.
		Difficulty: common.Big0,
		BaseFee:    common.Big0,
	}
	require.True(t, config.IsGauss(parent.Number, parent.Time),
		"the header under test must be a lane block, or every assertion below is vacuous")
	// LoadParams maps an unwritten word to the factory default, so an allocation at the
	// wrong address derives the same quota and every assertion below stays green.
	require.NotEmpty(t, parentState.GetCode(paymentlane.ContractAddress),
		"0x2007 must carry code, or the parameters are defaults by accident")

	// The local build the validator would fall back to: same parent, same state root.
	local := &environment{header: types.CopyHeader(header), state: parentState}

	// The floor at a 55M limit under the factory defaults, derived from the contract
	// constants rather than from the code under test: min(max(2%*55M, 2M), min(8%*55M, 8M)).
	const wantLaneSize = 2_000_000
	return w, header, local, wantLaneSize
}

// bidBlockWith wraps a header and transactions the way admission would have left them.
func bidBlockWith(header *types.Header, txs ...*types.Transaction) *buildertypes.DecodedBidBlock {
	return &buildertypes.DecodedBidBlock{
		Header:        header,
		Txs:           txs,
		SystemTxStart: len(txs), // no trailing unsigned system txs
	}
}

// TestVerifyBidBlockLaneQuota covers the last point at which a builder-authored commitment
// can be refused for free, which matters because a BidBlock header is adopted verbatim and
// handleBidBlockResult broadcasts before InsertChain.
//
// Each case is a commitment a validator must not sign, plus the truthful one it must.
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
			// gasUsed is a plausible block total rather than the transfer's own gas: at
			// gasUsed == 21000 the ceiling would reach the clamp on its first step and the
			// case would prove only that the clamp exists.
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
			// The profitable lie: claiming the lane was spent collapses the reserved term
			// and frees its gas for general traffic.
			name:       "more payment gas than these transactions can consume is refused",
			commitment: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneSize, PaymentGasUsed: 2 * params.TxGas}),
			gasUsed:    laneSize,
			txs:        []*types.Transaction{transfer},
			wantErr:    paymentlane.ErrUntruthy,
		},
		{
			// Under-stating is not caught here and cannot be: no cheap lower bound on the
			// bucket exists, since any transaction can install code at any address. Such a
			// block IS invalid - the importer demands exact equality - so this pins a known
			// residual exposure, not a property.
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

// TestVerifyBidBlockLaneQuotaBoundsByDeclaredLimits pins what the ceiling is made of, and
// therefore how weak it is: each payment-class transaction's declared gas limit.
//
// Bounding by intrinsic gas would be tighter and is what an earlier version did, but it is
// not sound - a payment-class transfer whose destination gains code mid-block executes it and
// really does consume its limit (the leak recorded on Classify's gate 8), so an intrinsic-gas
// ceiling would refuse honest blocks. The mutation this kills is that tightening.
func TestVerifyBidBlockLaneQuotaBoundsByDeclaredLimits(t *testing.T) {
	w, header, local, laneSize := laneBidBlockHarness(t)

	// One bare transfer declaring far more than a transfer can use. 5M is past the 4.4M
	// ceiling, so nothing else in the block bounds it either.
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

// TestVerifyBidBlockLaneQuotaSkipsTheSystemTxRegion pins the slice bound on the ceiling
// loop, which every other fixture leaves at the full transaction list.
//
// Everything at or after SystemTxStart is gas the importer never classifies - it splits
// system transactions out before the loop that books buckets - so counting it would let a
// builder commit payment gas no transaction of its block can produce.
func TestVerifyBidBlockLaneQuotaSkipsTheSystemTxRegion(t *testing.T) {
	w, header, local, laneSize := laneBidBlockHarness(t)

	stranger := common.Address{0xbe, 0xef}
	transfer := types.NewTx(&types.LegacyTx{To: &stranger, Value: common.Big1, Gas: params.TxGas})
	// Deliberately NOT addressed to a system contract: one that is would be general anyway
	// by the reserved-range gate, so widening the slice would not raise the ceiling and the
	// mutation would survive. This is the shape that makes the slice bound load-bearing.
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

// TestVerifyBidBlockLaneQuotaRequiresTheLocalParent pins the precondition the whole check
// rests on: the local state must be open on the bid's own parent, or the classifier and the
// parameters come from a different block's post-state than the importer will use, and every
// honest bid is refused with no test able to see why.
func TestVerifyBidBlockLaneQuotaRequiresTheLocalParent(t *testing.T) {
	w, header, local, _ := laneBidBlockHarness(t)

	h := types.CopyHeader(header)
	h.ParentHash = common.Hash{0x99}

	err := w.verifyBidBlockLaneQuota(bidBlockWith(h), local)
	require.ErrorContains(t, err, "is not the parent the local state is open on")
}

// TestVerifyBidBlockLaneQuotaSkipsAPreActivationHeader keeps the check from answering for
// blocks the lane does not bind, where EmptyUncleHash is the correct carrier value and a
// refusal would close the channel for every pre-Gauss block.
func TestVerifyBidBlockLaneQuotaSkipsAPreActivationHeader(t *testing.T) {
	w, header, local, _ := laneBidBlockHarness(t)

	preGauss := *w.chainConfig
	preGauss.GaussTime = nil
	w.chainConfig = &preGauss

	h := types.CopyHeader(header)
	h.UncleHash = types.EmptyUncleHash
	require.NoError(t, w.verifyBidBlockLaneQuota(bidBlockWith(h), local))
}
