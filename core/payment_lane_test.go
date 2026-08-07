package core

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
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
			PaymentGasUsed: tc.payment,
		}, got, "block %d (%s)", tc.number, tc.regime)
		// General gas is the residual, so the expected count is checked against it rather
		// than committed: tc.general comes from the protocol constants, not from the code
		// under test, so this still pins the transaction arithmetic.
		require.Equal(t, tc.general, block.GasUsed()-got.PaymentGasUsed, "block %d", tc.number)
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
			// The quota and the payment figure share the 32 bytes, so a producer that
			// wrote them in the wrong order commits a payment figure of 2M against a
			// replay of 21000. Caught as untruthful accounting, not as a bad quota,
			// because CheckQuota runs on the value in the laneSize slot and that one is
			// now the (correct) payment figure.
			name: "swapped fields",
			mutate: func(c paymentlane.Commitment) common.Hash {
				c.LaneSize, c.PaymentGasUsed = c.PaymentGasUsed, c.LaneSize
				return paymentlane.Encode(c)
			},
			wantErr: paymentlane.ErrQuotaMismatch,
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
			name:    "carrier left at the pre-activation empty-list hash",
			mutate:  func(paymentlane.Commitment) common.Hash { return types.EmptyUncleHash },
			wantErr: paymentlane.ErrBadCommitment,
		},
		{
			// The all-zero carrier is a well-formed commitment now - the version byte
			// that used to exclude it is gone - so what rejects a header that simply
			// never had one written is the quota comparison, not the framing. This is
			// the case that makes dropping the version byte safe.
			name:    "carrier left all zero",
			mutate:  func(paymentlane.Commitment) common.Hash { return common.Hash{} },
			wantErr: paymentlane.ErrQuotaMismatch,
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

// TestPaymentLaneClassifiesAgainstTheParentState pins the classifier's state binding, which
// is a security property rather than tidiness.
//
// Bound to the advancing StateDB instead of the parent post-state, a block producer could
// insert one cheap contract creation ahead of a batch of transfers and thereby choose whose
// transfers count as payments - deterministically, with every other test still green, and
// invisible until somebody used it. So a transfer to an address that gains code IN THIS BLOCK
// is still a payment, and only from the next block on is it general.
//
// That is also the leak recorded on Classify's gate 8: the transfer below executes the code
// deployed by the transaction before it and burns its whole limit inside the payment bucket.
// The burner is here to keep that concrete rather than theoretical - block 3 commits a payment
// total far above 21,000 - so anyone weighing the trade again has the number in front of them.
func TestPaymentLaneClassifiesAgainstTheParentState(t *testing.T) {
	config, gspec, key := laneGenesis(t)
	signer := types.LatestSigner(config)

	// Deploys `JUMPDEST; PUSH1 0; JUMP`, an infinite loop, so the transfer into it halts out
	// of gas having consumed its whole limit. burnGas clears the deployment's own ~80k so the
	// assertion can tell executed from not, and stays under the 2M floor so the block is not
	// quota-bound for a different reason.
	const (
		burnerInitCode = "0x635b6000566000526004601cf3"
		burnGas        = 1_000_000
	)
	created := crypto.CreateAddress(key.addr, 0)

	var nonce uint64
	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 4, func(i int, b *BlockGen) {
		switch i + 1 {
		case 3:
			deploy, err := types.SignNewTx(key.priv, signer, &types.LegacyTx{
				Nonce: nonce, Value: common.Big0, Gas: 100_000,
				GasPrice: big.NewInt(params.GWei), Data: common.FromHex(burnerInitCode),
			})
			require.NoError(t, err)
			b.AddTx(deploy)
			nonce++
			b.AddTx(key.sign(t, signer, nonce, created, big.NewInt(1), burnGas, nil))
			nonce++
		case 4:
			// The same transfer one block later: the code is in the parent post-state now, so
			// gate 8 makes it general.
			b.AddTx(key.sign(t, signer, nonce, created, big.NewInt(1), burnGas, nil))
			nonce++
		}
	})

	require.Len(t, blocks[2].Transactions(), 2, "block 3 must carry the deployment and the transfer")
	sameBlock, err := paymentlane.Decode(blocks[2].UncleHash())
	require.NoError(t, err)
	require.EqualValues(t, burnGas, sameBlock.PaymentGasUsed,
		"a transfer to an address created by this same block is still a payment, and it executed code")

	nextBlock, err := paymentlane.Decode(blocks[3].UncleHash())
	require.NoError(t, err)
	require.Zero(t, nextBlock.PaymentGasUsed,
		"once the code is in the parent post-state the same transfer is general")

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err)
}

// TestPaymentLaneVerifyPackedBidRefusesASwallowedClassification pins the half of
// VerifyPackedBid that has nothing to do with the quota.
//
// The miner's packing loop swallows a classification failure on purpose - refusing to
// build is not the loop's decision - so on the bid path the sticky error is all that is
// left of it. Checked only at seal time, the answer would arrive after the good local
// block had been discarded, which is the outcome the bid gate exists to avoid.
func TestPaymentLaneVerifyPackedBidRefusesASwallowedClassification(t *testing.T) {
	to := common.Address{0x44}
	ls := &LaneState{class: paymentlane.NewClassifier(common.Hash{}, failingAccountReader{}, nil)}

	// Non-zero on purpose: at LaneSize 0 the quota comparison is 0 > shared, false for every
	// argument, and deleting it would leave the whole tree green.
	ls.Budget.LaneSize = 100
	require.NoError(t, ls.VerifyPackedBid(100), "a quota that exactly fits is the accepting case")
	require.ErrorIs(t, ls.VerifyPackedBid(99), paymentlane.ErrViolated,
		"a bid that leaves less than the idle quota must be rejected")

	_, err := ls.Classify(types.NewTx(&types.LegacyTx{To: &to, Value: common.Big1, Gas: paymentTxGas}))
	require.ErrorIs(t, err, paymentlane.ErrStateUnavailable)
	require.ErrorIs(t, ls.VerifyPackedBid(0), paymentlane.ErrStateUnavailable,
		"a swallowed classification failure must reject the bid, not wait for the seal")
}

// --- helpers -------------------------------------------------------------------

// failingAccountReader makes every classification that reaches the parent state fail.
type failingAccountReader struct{}

func (failingAccountReader) Account(common.Address) (*types.StateAccount, error) {
	return nil, errors.New("no state")
}

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

// TestPaymentLaneAndUnclesCannotShareTheSlot covers WriteCommitment's refusal, which is
// load-bearing in a way that only shows up outside this package.
//
// The refusal sits where the slot is about to be overwritten, not at AddUncle where the
// mistake is made, because AddUncle and AssembleBlock are both upstream code and a lane
// check in either is a divergence to re-resolve on every merge. The price is that a
// malformed uncle crashes the engine first.
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
// docs/bep703-payment-lane.md leaves that cache out until the listed set grows; this is
// the measurement behind that. It covers exactly what a cache would remove: the grandparent header
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

// systemGasFaker is ethash plus a fixed amount of system-transaction gas per block.
//
// It exists because an ethash chain has no system transactions at all, and every other
// lane fixture in the tree therefore runs with systemGasUsed == 0 - a value at which the
// two candidate readings of generalGasUsed (with and without system gas) are numerically
// identical. Without this engine the entire question is untestable: the code can be got
// completely wrong and stay green.
//
// Finalize is the right seam because it is the one the two sides share. AssembleBlock
// passes &header.GasUsed to it on the producing side (ethash implements no
// FinalizeAndAssemble, so the else branch runs) and Process passes &gasUsed on the
// importing side, so a block built by this engine also imports under it.
type systemGasFaker struct {
	consensus.Engine
	systemGas uint64
}

func (e *systemGasFaker) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state vm.StateDB,
	txs *[]*types.Transaction, uncles []*types.Header, withdrawals []*types.Withdrawal,
	receipts *[]*types.Receipt, systemTxs *[]*types.Transaction, usedGas *uint64, tracer *tracing.Hooks) error {
	if err := e.Engine.Finalize(chain, header, state, txs, uncles, withdrawals, receipts, systemTxs, usedGas, tracer); err != nil {
		return err
	}
	if usedGas != nil {
		*usedGas += e.systemGas
	}
	return nil
}

// TestPaymentLaneSignalCountsSystemTransactionGas pins which gas the congestion signal
// counts, on the only chain in the tree where the answer is observable.
//
// The block it turns on carries 33M of user general gas and 12.16M of system gas - the
// largest system-transaction cost parlia records on mainnet, a breathe block's
// validator-set update. At a 55M gas limit the two readings fall on opposite sides of
// BOTH triggers:
//
//	user general alone  33.00M  <  shrink 38.5M   -> the quota shrinks
//	general + system    45.16M  >= expand 44.0M   -> the quota expands
//
// so a single assertion separates them, and it separates them by a full step in each
// direction rather than by a rounding difference.
func TestPaymentLaneSignalCountsSystemTransactionGas(t *testing.T) {
	config, gspec, key := laneGenesis(t)

	// 12_160_000 is parlia's recorded maximum for a validator-set update; see
	// EstimateGasReservedForSystemTxs. Anything smaller than 11M would leave the two
	// readings on the same side of the expand trigger and the test would prove nothing.
	const systemGas = 12_160_000
	engine := &systemGasFaker{Engine: ethash.NewFaker(), systemGas: systemGas}

	// 33M of general gas: below the 38.5M shrink trigger on its own, above the 44M
	// expand trigger once system gas joins it.
	const wantGeneral = 33_000_000
	nGeneral := int(wantGeneral / generalTxGas)

	var nonce uint64
	signer := types.LatestSigner(config)
	_, blocks, _ := GenerateChainWithGenesis(gspec, engine, 5, func(i int, b *BlockGen) {
		if i+1 != 3 {
			return
		}
		for n := 0; n < nGeneral; n++ {
			b.AddTx(key.sign(t, signer, nonce, common.Address{0xaa}, big.NewInt(1), generalTxGas, []byte{1, 2, 3, 4}))
			nonce++
		}
	})

	// Block 3 is the first block the rules bind to, and its parent is not a lane block,
	// so its own quota is the bootstrap floor whichever reading is in force. Block 4 is
	// where the readings part.
	general := uint64(nGeneral) * generalTxGas
	require.Greater(t, general+systemGas, uint64(44_000_000), "fixture must clear the expand trigger with system gas")
	require.Less(t, general, uint64(38_500_000), "fixture must fall under the shrink trigger without it")

	block3, block4 := blocks[2], blocks[3]
	require.EqualValues(t, general+systemGas, block3.GasUsed(), "the faker must actually inject system gas")

	c3, err := paymentlane.Decode(block3.UncleHash())
	require.NoError(t, err)
	require.EqualValues(t, 2_000_000, c3.LaneSize, "block 3 is the bootstrap floor")

	c4, err := paymentlane.Decode(block4.UncleHash())
	require.NoError(t, err)
	// The signal counts the block's whole gas, so block 3 reads as 45.16M of 55M - past
	// the expand trigger - and block 4 gets one expansion step. Counting user general gas
	// alone would read 33M, fall under the shrink trigger, and hold block 4 at the floor;
	// that is what this number distinguishes.
	require.EqualValues(t, 3_100_000, c4.LaneSize,
		"signal must count system gas too: user general alone is %d", general)

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, engine, DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err, "a chain with non-zero system gas must import")
}
