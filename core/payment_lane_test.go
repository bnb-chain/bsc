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

package core

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/stretchr/testify/require"
)

// Gas costs of the two transaction shapes this file builds. Protocol constants, not
// values read back from the implementation - the assertions would prove nothing if the
// expected buckets were derived from the buckets.
// 55M is mainnet's gas limit and, more to the point, the smallest round figure at which
// the accumulator is not degenerate. An expansion is only observable when
// floor + expandStep < ceiling, i.e. 2M + 2%*G < 8%*G, i.e. G > 33.3M; below that both
// the expand and the hold branch land on the ceiling and a test cannot tell them apart.
const (
	laneTestGasLimit = 55_000_000
	laneTestData     = 4 // non-zero calldata bytes on a general transaction
	paymentTxGas     = params.TxGas
	generalTxGas     = params.TxGas + laneTestData*params.TxDataNonZeroGasEIP2028
)

// laneGenesis builds a chain whose Gauss timestamp falls between block 1 (t=10) and
// block 2 (t=20), so block 2 is the activation block and block 3 is the first block the
// rules bind to.
//
// No fork-order boilerplate here, unlike consensus/parlia/payment_lane_test.go's harness:
// CheckConfigForkOrder returns early for a non-BSC config (IsInBSC is "Parlia != nil"),
// and this one is ethash. The two fixtures are deliberately not shared - see the note
// there.
//
// 0x2007 is allocated rather than installed by the Gauss upgrade, because GenerateChain
// cannot run that upgrade: chain_makers only calls TryUpdateBuildInSystemContract with
// atBlockBegin=true, and a test genesis hash resolves to defaultNet, whose gaussUpgrade
// entry is nil - a miss reported at Info level, so it would never turn a test red. (On a
// real BSC config the post-Feynman branch would skip it as well.) Substituting the
// allocation is faithful for lane purposes, because from Gauss+1 onwards the real chain
// also has the code in the parent post-state; what it cannot cover is the installation
// itself, which is TestGaussUpgradeApplies in core/systemcontracts.
//
// Engine is ethash, so there are no system transactions and systemGasUsed is zero -
// which is what lets these tests assert the buckets against block.GasUsed() directly.
// One artefact to know about rather than fix: ethash derives difficulty from
// "parent.UncleHash != EmptyUncleHash", so every lane block's child is scored as though
// its parent had uncles. It is symmetric between makeHeader and verifyHeader, so nothing
// diverges; it simply does not happen under parlia, where the lane actually runs.
func laneGenesis(t *testing.T) (*params.ChainConfig, *Genesis, *ecdsaKey) {
	t.Helper()
	code, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	require.NoError(t, err)

	config := *params.AllEthashProtocolChanges
	gaussTime := uint64(15)
	config.GaussTime = &gaussTime

	key := newKey(t)
	gspec := &Genesis{
		Config:   &config,
		GasLimit: laneTestGasLimit,
		Alloc: types.GenesisAlloc{
			paymentlane.ContractAddress: {Code: code, Balance: common.Big0},
			key.addr:                    {Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))},
		},
	}
	return &config, gspec, key
}

// TestPaymentLaneRoundTripsThroughAGeneratedChain is the end-to-end proof for the
// wiring: a producer writes the commitment, an importer replays the block and accepts
// it, and the quota moves through all three regimes of the recursion on the way.
//
// A test in which laneSize never leaves its floor would pass against an implementation
// that ignored the signal entirely - the zero signal maps to the floor and so does
// every quiet block - so the chain below is shaped to expand, then hold inside the
// hysteresis band, then shrink to a value that is neither the floor nor the ceiling.
// Likewise it carries payment-class transactions, because ClassGeneral is the zero
// value and a run with no payment traffic cannot tell a working classifier from one
// hard-coded to answer general.
func TestPaymentLaneRoundTripsThroughAGeneratedChain(t *testing.T) {
	config, gspec, key := laneGenesis(t)

	// Quota arithmetic at 55M under the factory defaults, worked out from the contract
	// constants rather than from the code under test:
	//   ceiling = min(8% * 55M, 8M) = 4.4M      floor = min(max(2% * 55M, 2M), 4.4M) = 2M
	//   expandStep = 2% * 55M = 1.1M            shrinkStep = 0.5% * 55M = 275k
	//   expand when parent general >= 80% of the parent gas limit, shrink below 70%
	const (
		wantFloor    = 2_000_000
		wantExpanded = wantFloor + 1_100_000 // 3.1M, strictly inside (floor, ceiling)
		shrinkStep   = 275_000
	)

	// Transaction counts chosen to land the signal in each regime.
	var (
		nExpand = int(laneTestGasLimit*8_000/10_000/generalTxGas) + 1   // >= 80% general
		nHold   = int(laneTestGasLimit * 7_500 / 10_000 / generalTxGas) // inside [70%, 80%)
		nPay    = 3
	)

	var nonce uint64
	signer := types.LatestSigner(config)
	general := func(b *BlockGen, n int) {
		for i := 0; i < n; i++ {
			b.AddTx(key.sign(t, signer, nonce, common.Address{0xaa}, big.NewInt(1), generalTxGas, []byte{1, 2, 3, 4}))
			nonce++
		}
	}
	payment := func(b *BlockGen, n int) {
		for i := 0; i < n; i++ {
			// Bare transfer to a fresh, code-less destination: category 1.
			b.AddTx(key.sign(t, signer, nonce, common.Address{byte(i + 1), 0xbb}, big.NewInt(1), paymentTxGas, nil))
			nonce++
		}
	}

	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 6, func(i int, b *BlockGen) {
		switch i + 1 { // block number
		case 3:
			general(b, nExpand) // drives an expansion in block 4
		case 4:
			general(b, nHold) // drives a hold in block 5
		case 5:
			payment(b, nPay) // payment only: general stays ~0, so block 6 shrinks
		}
	})

	// What each lane block must commit. Block 3 is the bootstrap: the lane did not
	// apply to its parent, so the signal is zero and the quota is the floor.
	for _, tc := range []struct {
		number   int
		laneSize uint64
		general  uint64
		payment  uint64
		regime   string
	}{
		{3, wantFloor, uint64(nExpand) * generalTxGas, 0, "bootstrap: the zero signal maps to the floor"},
		{4, wantExpanded, uint64(nHold) * generalTxGas, 0, "expand, unclamped - so it is not the ceiling"},
		{5, wantExpanded, 0, uint64(nPay) * paymentTxGas, "hold: neither branch taken, and both would differ"},
		{6, wantExpanded - shrinkStep, 0, 0, "shrink: neither floor nor ceiling"},
	} {
		block := blocks[tc.number-1]
		require.EqualValues(t, tc.number, block.NumberU64())
		got, err := paymentlane.Decode(block.UncleHash())
		require.NoError(t, err, "block %d (%s) carries no commitment", tc.number, tc.regime)
		require.Equal(t, paymentlane.Commitment{
			LaneSize:       tc.laneSize,
			GeneralGasUsed: tc.general,
			PaymentGasUsed: tc.payment,
		}, got, "block %d (%s)", tc.number, tc.regime)
		// No system transactions under ethash, so the buckets must account for all of it.
		require.Equal(t, tc.general+tc.payment, block.GasUsed(), "block %d", tc.number)
	}

	// The blocks the producer built must import: this is the half that proves the two
	// sides agree, rather than that each is self-consistent.
	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	n, err := chain.InsertChain(blocks)
	require.NoError(t, err, "inserted %d of %d", n, len(blocks))
	require.EqualValues(t, blocks[len(blocks)-1].NumberU64(), chain.CurrentBlock().Number.Uint64())

	// Last, and not optional: untouched storage is byte for byte indistinguishable from
	// an absent account and LoadParams maps a zero word to its default, so every quota
	// assertion above would pass just as happily against an address where nothing was
	// ever installed. Mutation-checked - moving the allocation to 0x2008 leaves the rest
	// of this test green.
	sdb, err := chain.State()
	require.NoError(t, err)
	require.NotEmpty(t, sdb.GetCode(paymentlane.ContractAddress))
}

// TestPaymentLaneImportRejectsATamperedCommitment covers the checks that make the
// commitment worth anything: a producer can always be self-consistent, so only the
// importer's replay can catch a lie.
func TestPaymentLaneImportRejectsATamperedCommitment(t *testing.T) {
	config, gspec, key := laneGenesis(t)
	signer := types.LatestSigner(config)

	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 3, func(i int, b *BlockGen) {
		if i+1 == 3 {
			b.AddTx(key.sign(t, signer, 0, common.Address{0xbb}, big.NewInt(1), params.TxGas, nil))
		}
	})
	honest := blocks[2]
	sound, err := paymentlane.Decode(honest.UncleHash())
	require.NoError(t, err)
	require.EqualValues(t, params.TxGas, sound.PaymentGasUsed, "the tampering below is only meaningful if the block has payment gas")

	for _, tc := range []struct {
		name    string
		mutate  func(paymentlane.Commitment) common.Hash
		wantErr error
	}{
		{
			name: "swapped buckets",
			mutate: func(c paymentlane.Commitment) common.Hash {
				c.GeneralGasUsed, c.PaymentGasUsed = c.PaymentGasUsed, c.GeneralGasUsed
				return paymentlane.Encode(c)
			},
			wantErr: paymentlane.ErrUntruthy,
		},
		{
			name: "understated payment gas",
			mutate: func(c paymentlane.Commitment) common.Hash {
				c.PaymentGasUsed--
				return paymentlane.Encode(c)
			},
			wantErr: paymentlane.ErrUntruthy,
		},
		{
			name: "quota one step above the derivation",
			mutate: func(c paymentlane.Commitment) common.Hash {
				c.LaneSize += 150_000
				return paymentlane.Encode(c)
			},
			wantErr: paymentlane.ErrQuotaMismatch,
		},
		{
			name:    "carrier left empty",
			mutate:  func(paymentlane.Commitment) common.Hash { return types.EmptyUncleHash },
			wantErr: paymentlane.ErrBadCommitment,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forged := types.NewBlockWithHeader(honest.Header()).WithBody(*honest.Body())
			forged.SetUncleHash(tc.mutate(sound))

			chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
			require.NoError(t, err)
			defer chain.Stop()
			_, err = chain.InsertChain(append(append(types.Blocks{}, blocks[:2]...), forged))
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// --- helpers -------------------------------------------------------------------

type ecdsaKey struct {
	priv *ecdsa.PrivateKey
	addr common.Address
}

func newKey(t *testing.T) *ecdsaKey {
	t.Helper()
	priv, err := crypto.GenerateKey()
	require.NoError(t, err)
	return &ecdsaKey{priv: priv, addr: crypto.PubkeyToAddress(priv.PublicKey)}
}

func (k *ecdsaKey) sign(t *testing.T, signer types.Signer, nonce uint64, to common.Address, value *big.Int, gas uint64, data []byte) *types.Transaction {
	t.Helper()
	tx, err := types.SignNewTx(k.priv, signer, &types.LegacyTx{
		Nonce:    nonce,
		To:       &to,
		Value:    value,
		Gas:      gas,
		GasPrice: big.NewInt(params.GWei),
		Data:     data,
	})
	require.NoError(t, err)
	return tx
}

// TestPaymentLaneClassifiesAgainstTheParentState pins the classifier's state binding,
// which is a security property rather than tidiness.
//
// Bound to the advancing StateDB instead of the parent post-state, a block producer
// could insert one cheap contract creation ahead of a batch of transfers and thereby
// choose whose transfers count as payments - deterministically, with every other test
// still green, and invisible until somebody used it. So a transfer to an address that
// gains code IN THIS BLOCK is still a payment, and only from the next block on is it
// general.
func TestPaymentLaneClassifiesAgainstTheParentState(t *testing.T) {
	config, gspec, key := laneGenesis(t)
	signer := types.LatestSigner(config)

	// Init code returning one byte, so the deployed account has a non-empty code hash.
	deployGas := uint64(100_000)
	created := crypto.CreateAddress(key.addr, 0)

	var nonce uint64
	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 4, func(i int, b *BlockGen) {
		switch i + 1 {
		case 3:
			// Deploy, then pay INTO the address created by this very block.
			tx, err := types.SignNewTx(key.priv, signer, &types.LegacyTx{
				Nonce: nonce, Value: common.Big0, Gas: deployGas,
				GasPrice: big.NewInt(params.GWei), Data: common.FromHex("0x60016000f3"),
			})
			require.NoError(t, err)
			b.AddTx(tx)
			nonce++
			b.AddTx(key.sign(t, signer, nonce, created, big.NewInt(1), paymentTxGas, nil))
			nonce++
		case 4:
			// Same transfer, one block later: now the code is in the parent post-state.
			b.AddTx(key.sign(t, signer, nonce, created, big.NewInt(1), paymentTxGas, nil))
			nonce++
		}
	})

	require.NotEmpty(t, blocks[2].Transactions(), "block 3 must actually carry the deployment")
	sameBlock, err := paymentlane.Decode(blocks[2].UncleHash())
	require.NoError(t, err)
	require.EqualValues(t, paymentTxGas, sameBlock.PaymentGasUsed,
		"a transfer to an address created by this same block must still be a payment")
	require.EqualValues(t, blocks[2].GasUsed()-paymentTxGas, sameBlock.GeneralGasUsed)

	nextBlock, err := paymentlane.Decode(blocks[3].UncleHash())
	require.NoError(t, err)
	require.Zero(t, nextBlock.PaymentGasUsed,
		"once the code is in the parent post-state the same transfer is general")
	require.EqualValues(t, blocks[3].GasUsed(), nextBlock.GeneralGasUsed)

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err)
}

// TestPaymentLaneAndUnclesCannotShareTheSlot covers AssembleBlock's refusal, which is
// load-bearing in a way that only shows up outside this package.
//
// The two uses of the uncle slot are mutually exclusive, and silently preferring the
// commitment would emit a block whose uncle list can never be verified again. Parlia
// forbids uncles outright so this is unreachable in production - but GenerateChain still
// offers AddUncle, and at least one existing harness relies on it:
// eth/downloader/testchain_test.go's generate() attaches an uncle to every fifth block.
// That is exactly why a lane-active variant of the downloader's chain is not a small
// change, and it is worth knowing that this error is what one would hit.
func TestPaymentLaneAndUnclesCannotShareTheSlot(t *testing.T) {
	_, gspec, _ := laneGenesis(t)

	var caught any
	func() {
		defer func() { caught = recover() }()
		// Block 3 is the first lane block; attach an uncle to it.
		GenerateChainWithGenesis(gspec, ethash.NewFaker(), 3, func(i int, b *BlockGen) {
			if i+1 == 3 {
				// AddUncle resolves the parent by ParentHash and would nil-deref without
				// it; same shape eth/downloader's harness uses.
				b.AddUncle(&types.Header{
					ParentHash: b.PrevBlock(i - 2).Hash(),
					Number:     new(big.Int).Sub(b.Number(), big.NewInt(1)),
				})
			}
		})
	}()
	if caught == nil {
		t.Fatal("assembling a lane block with an uncle must fail, not silently drop one of the two")
	}
	if msg := fmt.Sprint(caught); !strings.Contains(msg, "uncle hash slot") {
		t.Fatalf("unexpected failure: %v", msg)
	}

	// The same chain without the uncle must still assemble, or the assertion above would
	// pass for the wrong reason.
	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 3, nil)
	if _, err := paymentlane.Decode(blocks[2].UncleHash()); err != nil {
		t.Fatalf("the uncle-free chain must still carry a commitment: %v", err)
	}
}

// BenchmarkResolveLaneState measures what the deliberately-absent params cache costs.
//
// docs/bep703-wiring-plan.md defers that cache with "measure first"; this is the
// measurement. It covers exactly what a cache would remove: the grandparent header
// lookup, LoadParams' 8 slot reads, LoadPaymentContracts' 1+N reads, and building the
// classifier. It does NOT cover per-transaction classification, which no cache would
// help - that is a per-destination state read, memoised within the block already.
func BenchmarkResolveLaneState(b *testing.B) {
	t := &testing.T{}
	config, gspec, _ := laneGenesis(t)
	db, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 4, nil)
	parent, header := blocks[2], blocks[3].Header()

	sdb := state.NewDatabase(triedb.NewDatabase(db, triedb.HashDefaults), nil)
	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
	if err != nil {
		b.Fatal(err)
	}
	defer chain.Stop()
	if _, err := chain.InsertChain(blocks); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A fresh StateDB each iteration: reusing one would warm its reader's caches and
		// measure the second read, not the first, which is the one a block actually pays.
		statedb, err := state.New(parent.Root(), sdb)
		if err != nil {
			b.Fatal(err)
		}
		lane, err := ResolveLaneState(config, chain, parent.Header(), header, statedb.Reader())
		if err != nil {
			b.Fatal(err)
		}
		if !lane.On() {
			b.Fatal("the benchmark must exercise an active lane")
		}
	}
}

// BenchmarkResolveLaneStateFullList is the same measurement at the largest list the
// contract permits, MAX_PAYMENT_CONTRACTS = 256, so the cost of the 1+N reads is measured
// rather than extrapolated from the N=0 case.
//
// The genesis allocation writes the OpenZeppelin EnumerableSet layout directly: slot 8
// holds _values.length and element i lives at keccak256(bytes32(8))+i. That is the same
// layout core/paymentlane/config.go reads, and config_test.go pins it against the
// deployed blob - so writing it by hand here is reading the same contract, not inventing
// one.
func BenchmarkResolveLaneStateFullList(b *testing.B) {
	code, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	if err != nil {
		b.Fatal(err)
	}
	config := *params.AllEthashProtocolChanges
	gaussTime := uint64(15)
	config.GaussTime = &gaussTime

	const n = 256
	storage := map[common.Hash]common.Hash{
		common.BytesToHash([]byte{8}): common.BigToHash(big.NewInt(n)),
	}
	base := new(big.Int).SetBytes(crypto.Keccak256(common.BytesToHash([]byte{8}).Bytes()))
	for i := 0; i < n; i++ {
		slot := common.BigToHash(new(big.Int).Add(base, big.NewInt(int64(i))))
		storage[slot] = common.BytesToHash(common.BigToAddress(big.NewInt(int64(0x10000 + i))).Bytes())
	}
	gspec := &Genesis{
		Config:   &config,
		GasLimit: laneTestGasLimit,
		Alloc: types.GenesisAlloc{
			paymentlane.ContractAddress: {Code: code, Balance: common.Big0, Storage: storage},
		},
	}
	db, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 4, nil)
	parent, header := blocks[2], blocks[3].Header()

	sdb := state.NewDatabase(triedb.NewDatabase(db, triedb.HashDefaults), nil)
	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
	if err != nil {
		b.Fatal(err)
	}
	defer chain.Stop()
	if _, err := chain.InsertChain(blocks); err != nil {
		b.Fatal(err)
	}
	// Prove the fixture: if the list did not load, this would measure the N=0 case again.
	statedb, err := state.New(parent.Root(), sdb)
	if err != nil {
		b.Fatal(err)
	}
	listed, err := paymentlane.LoadPaymentContracts(statedb.Reader())
	if err != nil {
		b.Fatal(err)
	}
	if len(listed) != n {
		b.Fatalf("fixture did not take: %d listed, want %d", len(listed), n)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statedb, err := state.New(parent.Root(), sdb)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := ResolveLaneState(&config, chain, parent.Header(), header, statedb.Reader()); err != nil {
			b.Fatal(err)
		}
	}
}
