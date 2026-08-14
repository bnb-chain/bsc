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
	"github.com/ethereum/go-ethereum/core/systemcontracts/jenner"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// laneGasLimit must stay above the system reservation so the lane remains active.
const laneGasLimit = 35_000_000

// newParliaLaneHarness builds a lane-active Parlia chain and preallocates 0x2007.
func newParliaLaneHarness(t *testing.T) (*Parlia, *core.BlockChain, *params.ChainConfig, *types.Block, func() *types.Header, func() *state.StateDB) {
	t.Helper()

	db, err := rawdb.Open(rawdb.NewMemoryDatabase(), rawdb.OpenOptions{Ancient: t.TempDir()})
	if err != nil {
		t.Fatalf("failed to create database with ancient backend: %v", err)
	}
	trieDB := triedb.NewDatabase(db, nil)
	t.Cleanup(func() { trieDB.Close() })

	laneCode, err := hex.DecodeString(strings.TrimSpace(jenner.RialtoPaymentLaneContract))
	if err != nil {
		t.Fatalf("failed to decode the PaymentLane blob: %v", err)
	}

	// Parlia configs must enable the earlier timestamp forks before Jenner.
	at0 := func() *uint64 { v := uint64(0); return &v }
	config := *params.ParliaTestChainConfig
	config.HaberTime, config.HaberFixTime = at0(), at0()
	config.BohrTime, config.PascalTime, config.PragueTime = at0(), at0(), at0()
	config.LorentzTime, config.MaxwellTime, config.FermiTime = at0(), at0(), at0()
	config.OsakaTime, config.MendelTime, config.PasteurTime = at0(), at0(), at0()
	jennerTime := uint64(5)
	config.JennerTime = &jennerTime
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
		if len(stateDB.GetCode(paymentlane.ContractAddress)) == 0 {
			t.Fatal("0x2007 carries no code: the parameters below would be defaults by accident")
		}
		stateDB.SetBalance(consensus.SystemAddress, uint256.NewInt(12345), tracing.BalanceChangeUnspecified)
		return stateDB
	}
	return engine, chain, &config, parent, newHeader, newState
}

// TestPaymentLaneCommitmentSurvivesParliaAssembly checks both Parlia assemblers stamp after assembly.
func TestPaymentLaneCommitmentSurvivesParliaAssembly(t *testing.T) {
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

			lane, err := core.ResolveLaneState(config, parent.Header(), header, stateDB)
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
			header.GasUsed = paymentUsed

			block, err := tc.assemble(engine, chain, header, stateDB)
			if err != nil {
				t.Fatalf("failed to assemble: %v", err)
			}
			if block.UncleHash() != types.EmptyUncleHash {
				t.Fatalf("the assembler must leave the uncle slot for the stamp, got %x", block.UncleHash())
			}
			if err := lane.WriteCommitmentAndVerify(block, paymentUsed); err != nil {
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
			if len(block.Uncles()) != 0 {
				t.Fatal("parlia must never produce uncles")
			}
		})
	}
}

// TestPaymentLaneRefusesAStaleBlockHash checks the cached-hash guard in WriteCommitmentAndVerify.
func TestPaymentLaneRefusesAStaleBlockHash(t *testing.T) {
	engine, chain, config, parent, newHeader, newState := newParliaLaneHarness(t)
	header, stateDB := newHeader(), newState()

	lane, err := core.ResolveLaneState(config, parent.Header(), header, stateDB)
	if err != nil {
		t.Fatalf("failed to resolve the lane state: %v", err)
	}
	lane.SetQuota()

	block, _, err := core.AssembleBlock(engine, chain, header, stateDB, &types.Body{}, nil)
	if err != nil {
		t.Fatalf("failed to assemble: %v", err)
	}
	stale := block.Hash()

	err = lane.WriteCommitmentAndVerify(block, 0)
	if err == nil {
		t.Fatal("WriteCommitmentAndVerify accepted a block whose hash was already cached")
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

// authorizeLaneValidator seeds recentSnaps for verifyCascadingFields.
func authorizeLaneValidator(engine *Parlia, parent *types.Header, validator common.Address) {
	engine.recentSnaps.Add(parent.Hash(), newSnapshot(engine.config, engine.signatures,
		parent.Number.Uint64(), parent.Hash(), []common.Address{validator}, nil, nil))
}

// laneVerifiableHeader fills the non-lane fields verifyCascadingFields expects.
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

// TestVerifyCascadingFieldsGatesTheLaneCommitment checks the header gate before execution.
func TestVerifyCascadingFieldsGatesTheLaneCommitment(t *testing.T) {
	engine, chain, config, laneParent, newHeader, _ := newParliaLaneHarness(t)
	base := newHeader()
	postJenner, preJenner := laneParent.Header(), chain.GetHeaderByNumber(0)
	authorizeLaneValidator(engine, postJenner, base.Coinbase)
	authorizeLaneValidator(engine, preJenner, base.Coinbase)
	require.False(t, config.IsJenner(preJenner.Number, preJenner.Time),
		"the genesis must be pre-Jenner, or the boundary cases below test one regime twice")

	for _, tc := range []struct {
		name      string
		parent    *types.Header
		gasUsed   uint64
		uncleHash common.Hash
		wantErr   error
	}{
		{
			name:      "a truthful commitment passes",
			parent:    postJenner,
			gasUsed:   1_000_000,
			uncleHash: paymentlane.Encode(paymentlane.Commitment{LaneSize: 2_000_000, PaymentGasUsed: 500_000}),
		},
		{
			name:      "an unstamped uncle slot is refused",
			parent:    postJenner,
			uncleHash: types.EmptyUncleHash,
			wantErr:   paymentlane.ErrBadCommitment,
		},
		{
			name:      "a commitment that breaks the block rule is refused",
			parent:    postJenner,
			gasUsed:   1_000_000,
			uncleHash: paymentlane.Encode(paymentlane.Commitment{LaneSize: laneGasLimit + 1}),
			wantErr:   paymentlane.ErrViolated,
		},
		{
			// The activation block is the only parent/header boundary case.
			name:      "the activation block still carries an empty uncle hash",
			parent:    preJenner,
			uncleHash: types.EmptyUncleHash,
		},
		{
			name:      "a commitment before activation is refused",
			parent:    preJenner,
			uncleHash: paymentlane.Encode(paymentlane.Commitment{LaneSize: 2_000_000}),
			wantErr:   errInvalidUncleHash,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := laneVerifiableHeader(base, tc.parent)
			header.GasUsed, header.UncleHash = tc.gasUsed, tc.uncleHash
			require.True(t, config.IsJenner(header.Number, header.Time), "every header here is at or past activation")

			err := engine.verifyCascadingFields(chain, header, nil)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
