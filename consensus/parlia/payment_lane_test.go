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
	"github.com/stretchr/testify/require"
)

// laneGasLimit must stay above params.SystemTxsGasHardLimit, or NextLaneSize's final safety
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
// types.NewBlock then re-derives it from the body, so LaneState.WriteCommitment's write
// only survives because it happens to the assembled BLOCK, afterwards. Writing it one
// step earlier is silently discarded and every block is rejected network-wide.
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
		// Not optional: untouched storage is byte for byte indistinguishable from an absent
		// account and LoadParams maps a zero word to its default, so every quota assertion
		// in this file would pass just as happily against an address where nothing was ever
		// installed. Mutation-checked in core's harness, which carries the same guard.
		if len(stateDB.GetCode(paymentlane.ContractAddress)) == 0 {
			t.Fatal("0x2007 carries no code: the parameters below would be defaults by accident")
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
	_, _, config, parent, _, _ := newParliaLaneHarness(t)

	if !config.IsGauss(parent.Number(), parent.Time()) {
		t.Fatalf("block %d must be a lane block: gauss=%d parent=(%d, %d)",
			parent.NumberU64()+1, *config.GaussTime, parent.NumberU64(), parent.Time())
	}
}

// TestPaymentLaneCommitmentSurvivesParliaAssembly is the ordering test no other test in
// the tree can perform, over both Parlia assemblers.
//
// The mutation it exists to kill: writing the commitment onto the header BEFORE
// finalizeAndAssemble - the obvious-looking place, right where parlia sets EmptyUncleHash -
// instead of onto the assembled block afterwards. That mutation loses the commitment
// silently, and only a Parlia assembly can see it, because the ethash path in core's tests
// never overwrites the uncle slot on the way.
//
// Both assemblers, in one table, because "they behave the same way" is the assertion:
// FinalizeAndAssembleBidBlock is called only from the builder binary, so nothing in this
// repository links the two halves of that sequence, and a divergence between the two
// assemblers would show up as builder blocks nobody accepts. What verifyCascadingFields does
// with an unstamped block from Gauss+1 is TestVerifyCascadingFieldsGatesTheLaneCommitment's job.
func TestPaymentLaneCommitmentSurvivesParliaAssembly(t *testing.T) {
	// Non-zero on purpose: the all-zero commitment is a legal value, so a dropped one would
	// be indistinguishable from a correct empty block.
	const paymentUsed = 42_000

	for _, tc := range []struct {
		name     string
		assemble func(*Parlia, *core.BlockChain, *types.Header, *state.StateDB) (*types.Block, error)
	}{
		{"the sealing path", func(e *Parlia, c *core.BlockChain, h *types.Header, sdb *state.StateDB) (*types.Block, error) {
			block, _, err := core.AssembleBlock(e, c, h, sdb, &types.Body{}, nil)
			return block, err
		}},
		{"the builder path", func(e *Parlia, c *core.BlockChain, h *types.Header, sdb *state.StateDB) (*types.Block, error) {
			block, _, err := e.FinalizeAndAssembleBidBlock(c, h, sdb, &types.Body{}, nil, nil)
			return block, err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			lane.Budget.PaymentUsed = paymentUsed
			// The gas the bucket claims must also show up in the block total, as it would
			// after a real apply; the mock engine's Finalize does not touch usedGas.
			header.GasUsed = paymentUsed

			block, err := tc.assemble(engine, chain, header, stateDB)
			if err != nil {
				t.Fatalf("failed to assemble: %v", err)
			}
			if block.UncleHash() != types.EmptyUncleHash {
				t.Fatalf("the assembler must leave the uncle slot for the stamp, got %x", block.UncleHash())
			}
			// poolUsed must cover the bucket: WriteCommitment self-checks, and the bucket can
			// only ever be a part of what the pool consumed. This block has no general gas,
			// so the two are equal.
			if err := lane.WriteCommitment(block, paymentUsed); err != nil {
				t.Fatalf("failed to write the commitment: %v", err)
			}
			got, err := paymentlane.Decode(block.UncleHash())
			if err != nil {
				t.Fatalf("the commitment does not decode after a parlia assembly: %v (uncle slot %x)", err, block.UncleHash())
			}
			want := paymentlane.Commitment{LaneSize: 2_000_000, PaymentGasUsed: paymentUsed}
			if got != want {
				t.Fatalf("commitment: got %+v, want %+v", got, want)
			}
			// No hash assertion: WriteCommitment made that comparison itself and froze the
			// cache, so repeating it could not fail. TestPaymentLaneRefusesAStaleBlockHash
			// covers it.
			if len(block.Uncles()) != 0 {
				t.Fatal("parlia must never produce uncles")
			}
		})
	}
}

// TestPaymentLaneRefusesAStaleBlockHash is the only test of the guard that keeps the
// commitment write correct at a distance from assembly.
//
// It hashes the block first, exactly as a stray log line or an early sidecar loop would,
// and requires WriteCommitment to refuse rather than emit a block whose cached hash no
// longer matches its own header. It cannot be folded into the test above, which calls
// WriteCommitment before ever reading block.Hash() and so can never fail this way.
func TestPaymentLaneRefusesAStaleBlockHash(t *testing.T) {
	engine, chain, config, parent, newHeader, newState := newParliaLaneHarness(t)
	header, stateDB := newHeader(), newState()

	lane, err := core.ResolveLaneState(config, chain, parent.Header(), header, stateDB.Reader())
	if err != nil {
		t.Fatalf("failed to resolve the lane state: %v", err)
	}
	lane.SetQuota()

	block, _, err := core.AssembleBlock(engine, chain, header, stateDB, &types.Body{}, nil)
	if err != nil {
		t.Fatalf("failed to assemble: %v", err)
	}
	stale := block.Hash() // the mistake: caching the hash before the commitment is in

	err = lane.WriteCommitment(block, 0)
	if err == nil {
		t.Fatal("WriteCommitment accepted a block whose hash was already cached")
	}
	if !strings.Contains(err.Error(), "cached before the commitment") {
		t.Fatalf("unexpected refusal reason: %v", err)
	}
	if block.Hash() == block.Header().Hash() {
		t.Fatal("this test proves nothing unless the cached hash really is stale")
	}
	if block.Hash() != stale {
		t.Fatal("Block.Hash must be cached on first read, or the guard is unnecessary")
	}
}

// authorizeLaneValidator lets verifyCascadingFields past its snapshot checks for the block
// after parent. Injected into recentSnaps, which p.snapshot consults first, because the
// harness chain carries no epoch header to derive a validator set from.
func authorizeLaneValidator(engine *Parlia, parent *types.Header, validator common.Address) {
	engine.recentSnaps.Add(parent.Hash(), newSnapshot(engine.config, engine.signatures,
		parent.Number.Uint64(), parent.Hash(), []common.Address{validator}, nil, nil))
}

// laneVerifiableHeader completes the fields verifyCascadingFields checks after the lane gate,
// so a truthful commitment yields nil and the accept cases below are not vacuous.
func laneVerifiableHeader(base, parent *types.Header) *types.Header {
	h := types.CopyHeader(base)
	h.ParentHash = parent.Hash()
	h.Number = new(big.Int).Add(parent.Number, common.Big1)
	h.Time = parent.Time + 15 // clears BlockInterval + backOffTime in blockTimeVerifyForRamanujanFork
	h.BaseFee = common.Big0
	zero := uint64(0)
	h.BlobGasUsed, h.ExcessBlobGas = &zero, &zero
	wh := types.EmptyWithdrawalsHash
	h.WithdrawalsHash = &wh
	return h
}

// TestVerifyCascadingFieldsGatesTheLaneCommitment is the only test of the header gate itself.
// Everything else in this file stops at assembly, and the gate sits behind p.snapshot, so no
// GenerateChain-based fixture reaches it.
//
// The gate is where a forged commitment is refused before any body is executed, and it is also
// where the fork boundary is decided: the parent's Gauss status, not the header's.
func TestVerifyCascadingFieldsGatesTheLaneCommitment(t *testing.T) {
	engine, chain, config, laneParent, newHeader, _ := newParliaLaneHarness(t)
	base := newHeader()
	postGauss, preGauss := laneParent.Header(), chain.GetHeaderByNumber(0)
	authorizeLaneValidator(engine, postGauss, base.Coinbase)
	authorizeLaneValidator(engine, preGauss, base.Coinbase)
	require.False(t, config.IsGauss(preGauss.Number, preGauss.Time),
		"the genesis must be pre-Gauss, or the boundary cases below test one regime twice")

	for _, tc := range []struct {
		name      string
		parent    *types.Header
		gasUsed   uint64
		uncleHash common.Hash
		wantErr   error
	}{
		{
			// GasUsed well under the limit and a non-zero bucket on purpose: it makes the
			// argument order load-bearing, since CheckHeaderBounds(GasLimit, GasUsed) would
			// then read the 2M quota as exceeding a 1M limit.
			name:      "a truthful commitment passes",
			parent:    postGauss,
			gasUsed:   1_000_000,
			uncleHash: paymentlane.Encode(paymentlane.Commitment{LaneSize: 2_000_000, PaymentGasUsed: 500_000}),
		},
		{
			name:      "an unstamped uncle slot is refused",
			parent:    postGauss,
			uncleHash: types.EmptyUncleHash,
			wantErr:   paymentlane.ErrBadCommitment,
		},
		{
			name:      "a commitment that breaks the block rule is refused",
			parent:    postGauss,
			gasUsed:   1_000_000,
			uncleHash: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneGasLimit + 1}),
			wantErr:   paymentlane.ErrViolated,
		},
		{
			// The activation block is the only place the parent's and the header's Gauss
			// answers differ, so it is the only place that pins which one the gate asks.
			name:      "the activation block still carries an empty uncle hash",
			parent:    preGauss,
			uncleHash: types.EmptyUncleHash,
		},
		{
			name:      "a commitment before activation is refused",
			parent:    preGauss,
			uncleHash: paymentlane.Encode(paymentlane.Commitment{LaneSize: 2_000_000}),
			wantErr:   errInvalidUncleHash,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := laneVerifiableHeader(base, tc.parent)
			header.GasUsed, header.UncleHash = tc.gasUsed, tc.uncleHash
			require.True(t, config.IsGauss(header.Number, header.Time), "every header here is at or past activation")

			err := engine.verifyCascadingFields(chain, header, nil)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
