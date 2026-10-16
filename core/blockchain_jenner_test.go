package core

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/systemcontracts/jenner"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

// jennerTestChainConfig returns a Parlia config with every fork up to Pasteur
// active at genesis and Jenner activating at the given timestamp.
func jennerTestChainConfig(jennerTime uint64) *params.ChainConfig {
	config := *params.ParliaTestChainConfig
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
		Osaka:  params.DefaultOsakaBlobConfigBSC,
	}
	return &config
}

// TestChainOverridesJenner verifies that the --override.jenner plumbing
// (ChainOverrides.apply) actually lands on ChainConfig.JennerTime and that
// the fork-ordering invariant still guards overridden values.
func TestChainOverridesJenner(t *testing.T) {
	// All forks up to Pasteur must be scheduled for Jenner to be settable
	// (CheckConfigForkOrder enforces this — see TestChainOverridesJenner's
	// rejection case below for the guard itself).
	cfg := *jennerTestChainConfig(0)
	cfg.JennerTime = nil
	jennerTime := uint64(1_800_000_000)
	o := &ChainOverrides{OverrideJenner: &jennerTime}
	if err := o.apply(&cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if cfg.JennerTime == nil || *cfg.JennerTime != jennerTime {
		t.Fatalf("OverrideJenner must set JennerTime, got %v", cfg.JennerTime)
	}

	// Overriding Jenner to a time earlier than an already-scheduled fork must
	// be rejected by the CheckConfigForkOrder call at the end of apply().
	chapel := *params.ChapelChainConfig
	early := *chapel.PasteurTime - 1
	if err := (&ChainOverrides{OverrideJenner: &early}).apply(&chapel); err == nil {
		t.Fatalf("apply must reject a Jenner override earlier than Pasteur")
	}
}

// TestJennerForkTransition runs a real chain across the Jenner activation
// boundary and checks the BEP-706 precompile behavior end to end:
//   - before activation, a contract staticcalling 0x70 sees a successful call
//     with empty return data (0x70 behaves like an empty account);
//   - after activation, the same call returns the block's millisecond
//     timestamp (Header.MilliTimestamp());
//   - across several post-activation blocks the value keeps tracking each
//     block's own header (it is not cached at activation).
//
// The caller contract is the bytecode equivalent of the Solidity example in
// BEP-706 §4.5:
//
//	success := staticcall(gas(), 0x70, 0, 0, 0, 0x20)  // out -> mem[0:32]
//	pop(success)                                       // empty-account calls also succeed
//	sstore(1, returndatasize() + 1)                    // marker: proves the call ran
//	sstore(0, mload(0))                                // the returned word (0 if no data)
func TestJennerForkTransition(t *testing.T) {
	const jennerTime = uint64(25) // block times are 10, 20, 30, ... => blocks 1-2 pre-fork, 3+ post-fork

	callerCode := []byte{
		0x60, 0x20, // PUSH1 32   (retSize)
		0x60, 0x00, // PUSH1 0    (retOffset)
		0x60, 0x00, // PUSH1 0    (argSize)
		0x60, 0x00, // PUSH1 0    (argOffset)
		0x60, 0x70, // PUSH1 0x70 (the BEP-706 precompile address)
		0x5a,       // GAS
		0xfa,       // STATICCALL
		0x50,       // POP (success flag: 1 in both the empty-account and precompile case)
		0x3d,       // RETURNDATASIZE
		0x60, 0x01, // PUSH1 1
		0x01,       // ADD
		0x60, 0x01, // PUSH1 1
		0x55,       // SSTORE slot1 = returndatasize + 1
		0x60, 0x00, // PUSH1 0
		0x51,       // MLOAD
		0x60, 0x00, // PUSH1 0
		0x55, // SSTORE slot0 = mload(0)
		0x00, // STOP
	}
	code, err := hex.DecodeString(strings.TrimSpace(jenner.RialtoPaymentLaneContract))
	if err != nil {
		t.Fatalf("decode payment lane contract: %v", err)
	}

	var (
		caller = common.HexToAddress("0x000000000000000000000000000000000000aaaa")
		db     = rawdb.NewMemoryDatabase()

		// Generate a fresh key per run: the test only needs some funded account
		// to sign transactions, not a specific address, so no key is hardcoded.
		key, _ = crypto.GenerateKey()
		sender = crypto.PubkeyToAddress(key.PublicKey)

		gspec = &Genesis{
			Config: jennerTestChainConfig(jennerTime),
			Alloc: types.GenesisAlloc{
				sender:                      {Balance: big.NewInt(params.Ether)},
				caller:                      {Code: callerCode, Balance: big.NewInt(0)},
				paymentlane.ContractAddress: {Code: code},
			},
		}
		genesis = gspec.MustCommit(db, triedb.NewDatabase(db, nil))
		signer  = types.LatestSigner(gspec.Config)
	)

	const numBlocks = 5
	blocks, _ := GenerateChain(gspec.Config, genesis, ethash.NewFullFaker(), db, numBlocks, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{1})
		tx, err := types.SignNewTx(key, signer, &types.LegacyTx{
			Nonce:    uint64(i),
			To:       &caller,
			Gas:      100_000,
			GasPrice: new(big.Int).Mul(big.NewInt(5), big.NewInt(params.GWei)),
		})
		if err != nil {
			t.Fatalf("sign tx: %v", err)
		}
		b.AddTx(tx)
	})

	diskdb := rawdb.NewMemoryDatabase()
	gspec.MustCommit(diskdb, triedb.NewDatabase(diskdb, nil))
	chain, err := NewBlockChain(diskdb, gspec, ethash.NewFullFaker(), nil)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	defer chain.Stop()
	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("block %d: failed to insert into chain: %v", n, err)
	}

	var prevMilli uint64
	for _, block := range blocks {
		header := chain.GetHeaderByNumber(block.NumberU64())
		statedb, err := chain.StateAt(header)
		if err != nil {
			t.Fatalf("block %d: state: %v", block.NumberU64(), err)
		}
		// Every transaction (pre- and post-fork) must have succeeded.
		receipts := chain.GetReceiptsByHash(block.Hash())
		if len(receipts) != 1 || receipts[0].Status != types.ReceiptStatusSuccessful {
			t.Fatalf("block %d: caller tx must succeed, receipts: %v", block.NumberU64(), receipts)
		}
		marker := statedb.GetState(caller, common.BigToHash(big.NewInt(1))).Big().Uint64()
		word := statedb.GetState(caller, common.Hash{}).Big().Uint64()

		if header.Time < jennerTime {
			// Pre-activation: the staticcall ran (marker == 0 + 1) and saw an
			// empty-account call: zero return data.
			if marker != 1 {
				t.Fatalf("block %d (pre-fork): returndatasize must be 0, marker=%d", block.NumberU64(), marker)
			}
			if word != 0 {
				t.Fatalf("block %d (pre-fork): 0x70 must return no data, got %d", block.NumberU64(), word)
			}
			continue
		}
		// Post-activation: 32 bytes of return data carrying the block's own
		// millisecond timestamp.
		if marker != 33 {
			t.Fatalf("block %d (post-fork): returndatasize must be 32, marker=%d", block.NumberU64(), marker)
		}
		want := header.MilliTimestamp()
		if want != header.Time*1000 {
			t.Fatalf("block %d: test setup: generated headers must carry no ms remainder", block.NumberU64())
		}
		if word != want {
			t.Fatalf("block %d (post-fork): 0x70 returned %d, want %d", block.NumberU64(), word, want)
		}
		if word <= prevMilli {
			t.Fatalf("block %d (post-fork): value must increase per block, got %d after %d", block.NumberU64(), word, prevMilli)
		}
		prevMilli = word
	}
	if prevMilli == 0 {
		t.Fatalf("test must cover at least one post-fork block")
	}
}
