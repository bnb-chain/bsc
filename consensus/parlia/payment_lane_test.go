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

package parlia

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

// laneGasLimit must stay above params.SystemTxsGasHardLimit, or LaneSize's final safety
// clamp - min(size, gasLimit-SystemTxsGasHardLimit) - takes the quota to zero and every
// assertion below passes against a lane that is doing nothing. 35M is the devnet's limit
// and gives floor 2,000,000 / ceiling 2,800,000 under the factory defaults.
const laneGasLimit = 35_000_000

// newParliaLaneHarness builds a Parlia chain whose NEXT block is a payment lane block,
// and hands back the pieces needed to assemble it.
//
// This exists because nothing else in the tree exercises the lane under Parlia: every
// other lane test runs on ethash through GenerateChain. The gap matters for one ordering
// in particular - finalizeAndAssemble writes EmptyUncleHash into the header and
// types.NewBlock then re-derives it from the body, so core.AssembleBlock's commitment
// write only survives because it happens to the assembled BLOCK, afterwards. Writing it
// one step earlier is silently discarded and every block is rejected network-wide.
//
// Fork timing: genesis is at t=0 and GenerateChain fixes the block interval at 10s, so a
// Gauss timestamp of 5 makes block 1 the activation block (IsOnGauss fires there) and
// block 2 the first block the rules bind to. 0x2007 goes into the genesis allocation for
// the same reason it does in core's tests - GenerateChain cannot run the system-contract
// upgrade - which is faithful from Gauss+1 onwards, where this harness operates.
func newParliaLaneHarness(t *testing.T) (*Parlia, *core.BlockChain, *params.ChainConfig, *types.Block, func() *types.Header, func() *state.StateDB) {
	t.Helper()

	db, err := rawdb.Open(rawdb.NewMemoryDatabase(), rawdb.OpenOptions{Ancient: t.TempDir()})
	if err != nil {
		t.Fatalf("failed to create database with ancient backend: %v", err)
	}
	trieDB := triedb.NewDatabase(db, nil)
	t.Cleanup(func() { trieDB.Close() })

	laneCode, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	if err != nil {
		t.Fatalf("failed to decode the PaymentLane blob: %v", err)
	}

	// CheckConfigForkOrder rejects a config that enables gauss while any earlier timestamp
	// fork is nil - it tests enabled-ness, not values - so everything between Cancun (where
	// ParliaTestChainConfig stops) and Pasteur has to be switched on at 0.
	//
	// core/payment_lane_test.go's laneGenesis needs none of this, and the difference is
	// essential rather than accidental: CheckConfigForkOrder opens with
	// "if c.IsNotInBSC() { return nil }" and IsInBSC is "Parlia != nil", so an ethash
	// config skips the whole check. That is also why these two fixtures are deliberately
	// NOT shared - a common helper would have to carry this branch inside it, which is
	// worse than two explicit setups. Same for the Gauss timestamps: 5 here and 15 there,
	// each tied to its own harness's block interval and to which block must be the
	// activation block.
	at0 := func() *uint64 { v := uint64(0); return &v }
	config := *params.ParliaTestChainConfig
	config.HaberTime, config.HaberFixTime = at0(), at0()
	config.BohrTime, config.PascalTime, config.PragueTime = at0(), at0(), at0()
	config.LorentzTime, config.MaxwellTime, config.FermiTime = at0(), at0(), at0()
	config.OsakaTime, config.MendelTime, config.PasteurTime = at0(), at0(), at0()
	gaussTime := uint64(5)
	config.GaussTime = &gaussTime
	// Enabling prague/osaka above makes their blobSchedule entries mandatory.
	config.BlobScheduleConfig = &params.BlobScheduleConfig{
		Cancun: params.DefaultCancunBlobConfig,
		Prague: params.DefaultPragueBlobConfig,
		Osaka:  params.DefaultOsakaBlobConfig,
	}

	gspec := &core.Genesis{
		Config:   &config,
		GasLimit: laneGasLimit,
		Alloc: types.GenesisAlloc{
			testAddr:                    {Balance: new(big.Int).SetUint64(10 * params.Ether)},
			paymentlane.ContractAddress: {Code: laneCode, Balance: common.Big0},
		},
	}
	mockEngine := &mockParlia{}
	genesisBlock := gspec.MustCommit(db, trieDB)
	chain, _ := core.NewBlockChain(db, gspec, mockEngine, nil)
	t.Cleanup(chain.Stop)

	parents, _ := core.GenerateChain(&config, genesisBlock, mockEngine, db, 1, nil)
	parent := parents[0]
	rawdb.WriteBlock(db, parent)

	engine := New(&config, db, nil, genesisBlock.Hash())
	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	validator := crypto.PubkeyToAddress(validatorKey.PublicKey)
	engine.Authorize(validator, nil, func(account accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
		return types.SignTx(tx, types.LatestSigner(&config), validatorKey)
	})

	newHeader := func() *types.Header {
		return &types.Header{
			ParentHash: parent.Hash(),
			Number:     new(big.Int).Add(parent.Number(), common.Big1),
			Coinbase:   validator,
			Difficulty: new(big.Int).Set(diffInTurn),
			GasLimit:   laneGasLimit,
			Time:       parent.Time() + 1,
		}
	}
	newState := func() *state.StateDB {
		stateDB, err := state.New(parent.Root(), state.NewDatabase(trieDB, nil))
		if err != nil {
			t.Fatalf("failed to create stateDB: %v", err)
		}
		stateDB.SetBalance(consensus.SystemAddress, uint256.NewInt(12345), tracing.BalanceChangeUnspecified)
		return stateDB
	}
	return engine, chain, &config, parent, newHeader, newState
}

// TestPaymentLaneAppliesToTheBlockAfterParliaActivation pins the harness itself: if the
// next block were not a lane block, the two tests below would assert nothing and stay
// green forever.
func TestPaymentLaneAppliesToTheBlockAfterParliaActivation(t *testing.T) {
	_, _, config, parent, newHeader, _ := newParliaLaneHarness(t)
	header := newHeader()

	if paymentlane.Applies(config, nil, parent.Header()) {
		t.Fatal("genesis cannot be a lane block")
	}
	if paymentlane.Applies(config, parent.Header(), header) != true {
		t.Fatalf("block %d must be a lane block: gauss=%d parent.Time=%d header.Time=%d",
			header.Number, *config.GaussTime, parent.Time(), header.Time)
	}
	if got := paymentlane.Applies(config, nil, header); got {
		t.Fatal("Applies must answer false for an unresolved parent")
	}
}

// TestPaymentLaneCommitmentSurvivesParliaAssembly is the ordering test that no other
// test in the tree can perform.
//
// The mutation it exists to kill: writing the commitment onto the header BEFORE
// finalizeAndAssemble - the obvious-looking place, right where parlia sets
// EmptyUncleHash - instead of onto the assembled block afterwards. That mutation loses
// the commitment silently, and only a Parlia assembly can see it, because the ethash
// path in core's tests never overwrites the uncle slot on the way.
func TestPaymentLaneCommitmentSurvivesParliaAssembly(t *testing.T) {
	engine, chain, config, parent, newHeader, newState := newParliaLaneHarness(t)
	header, stateDB := newHeader(), newState()

	lane, err := core.ResolveLaneState(config, chain, parent.Header(), header, stateDB.Reader())
	if err != nil {
		t.Fatalf("failed to resolve the lane state: %v", err)
	}
	if !lane.On() {
		t.Fatal("the lane must apply to this block, or this test asserts nothing")
	}
	lane.SetQuota()
	if want := uint64(2_000_000); lane.Budget.LaneSize != want {
		t.Fatalf("bootstrap quota: got %d, want the floor %d at a %d gas limit",
			lane.Budget.LaneSize, want, laneGasLimit)
	}

	// Non-zero, distinct buckets on purpose: an all-zero commitment is exactly what a
	// dropped one decodes to for the two gas figures, so equal-to-zero would not
	// distinguish success from failure for them.
	lane.Budget.GeneralUsed = 21_000
	lane.Budget.PaymentUsed = 42_000

	block, _, err := core.AssembleBlock(engine, chain, header, stateDB, &types.Body{}, nil, lane)
	if err != nil {
		t.Fatalf("failed to assemble: %v", err)
	}
	got, err := paymentlane.Decode(block.UncleHash())
	if err != nil {
		t.Fatalf("parlia assembly clobbered the commitment: %v (uncle slot %x)", err, block.UncleHash())
	}
	want := paymentlane.Commitment{LaneSize: 2_000_000, GeneralGasUsed: 21_000, PaymentGasUsed: 42_000}
	if got != want {
		t.Fatalf("commitment: got %+v, want %+v", got, want)
	}
	// The block hash must already account for the commitment. SetUncleHash mutates a
	// cached-hash struct, so anything that computed the hash between assembly and the
	// write would leave the two disagreeing here.
	if block.Hash() != block.Header().Hash() {
		t.Fatal("block hash was cached before the commitment was written")
	}
	if len(block.Uncles()) != 0 {
		t.Fatal("parlia must never produce uncles")
	}
}

// TestPaymentLaneRefusesBidBlockAssembly pins the fence in FinalizeAndAssembleBidBlock.
//
// That path is the one production assembler that does NOT go through core.AssembleBlock,
// so it structurally cannot stamp a commitment: it would emit EmptyUncleHash on every
// block, and handleBidBlockResult signs and broadcasts before InsertChain verifies. The
// refusal lives in consensus code rather than only at the miner's RPC gate so that a
// future reopening fails here instead of relying on someone reading a comment.
func TestPaymentLaneRefusesBidBlockAssembly(t *testing.T) {
	engine, chain, _, _, newHeader, newState := newParliaLaneHarness(t)

	if _, _, err := engine.FinalizeAndAssembleBidBlock(chain, newHeader(), newState(), &types.Body{}, nil, nil); err == nil {
		t.Fatal("BidBlock assembly must be refused while the payment lane applies")
	} else if !strings.Contains(err.Error(), "payment lane commitment") {
		t.Fatalf("unexpected refusal reason: %v", err)
	}
	// The ordinary path must still work on identical inputs, or the fence is just a
	// broken assembler rather than a targeted refusal.
	if _, _, err := engine.FinalizeAndAssemble(chain, newHeader(), newState(), &types.Body{}, nil, nil); err != nil {
		t.Fatalf("the signed path must still assemble: %v", err)
	}
}
