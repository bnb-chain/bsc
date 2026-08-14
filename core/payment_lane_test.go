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
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// 55M matches mainnet and keeps expansion observable in the harness.
const (
	laneTestGasLimit = 55_000_000
	laneTestData     = 4 // non-zero calldata bytes on a general transaction
	paymentTxGas     = params.TxGas
	generalTxGas     = params.TxGas + laneTestData*params.TxDataNonZeroGasEIP2028
)

// laneGenesis builds the ethash-backed lane harness and preallocates 0x2007.
func laneGenesis(t testing.TB) (*params.ChainConfig, *Genesis, *ecdsaKey) {
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

// TestPaymentLaneRoundTripsThroughAGeneratedChain checks write, replay, and quota evolution together.
func TestPaymentLaneRoundTripsThroughAGeneratedChain(t *testing.T) {
	config, gspec, key := laneGenesis(t)

	const (
		wantFloor    = 2_000_000
		wantExpanded = wantFloor + 1_100_000 // 3.1M, strictly inside (floor, ceiling)
		shrinkStep   = 275_000
	)

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
		require.Equal(t, tc.general, block.GasUsed()-got.PaymentGasUsed, "block %d", tc.number)
	}

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	n, err := chain.InsertChain(blocks)
	require.NoError(t, err, "inserted %d of %d", n, len(blocks))
	require.EqualValues(t, blocks[len(blocks)-1].NumberU64(), chain.CurrentBlock().Number.Uint64())

	sdb, err := chain.State()
	require.NoError(t, err)
	require.NotEmpty(t, sdb.GetCode(paymentlane.ContractAddress))
}

// TestPaymentLaneImportRejectsATamperedCommitment checks importer-side commitment replay.
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
			name: "quota above the derivation",
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

func TestPaymentLaneFastNodeSkipsImportClassificationReplay(t *testing.T) {
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
	require.EqualValues(t, params.TxGas, sound.PaymentGasUsed)

	fastCfg := DefaultConfig()
	fastCfg.NoTries = true

	for _, tc := range []struct {
		name    string
		mutate  func(paymentlane.Commitment) common.Hash
		wantErr error
	}{
		{
			name: "understated payment gas",
			mutate: func(c paymentlane.Commitment) common.Hash {
				c.PaymentGasUsed--
				return paymentlane.Encode(c)
			},
		},
		{
			name: "quota above derivation",
			mutate: func(c paymentlane.Commitment) common.Hash {
				c.LaneSize += 150_000
				return paymentlane.Encode(c)
			},
			wantErr: paymentlane.ErrQuotaMismatch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forged := types.NewBlockWithHeader(honest.Header()).WithBody(*honest.Body())
			forged.SetUncleHash(tc.mutate(sound))

			chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), fastCfg)
			require.NoError(t, err)
			defer chain.Stop()

			_, err = chain.InsertChain(append(append(types.Blocks{}, blocks[:2]...), forged))
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestPaymentLaneClassifiesAgainstTheLiveState is the attack the live-state gate closes, run
// through the real EVM and a real import. burnerInitCode deploys `5b600056` - JUMPDEST, PUSH1
// 0, JUMP - which loops until its gas is gone; the transfer behind it, one nonce later so that
// it packs second, used to book that loop as payment gas against the parent post-state.
func TestPaymentLaneClassifiesAgainstTheLiveState(t *testing.T) {
	config, gspec, key := laneGenesis(t)
	signer := types.LatestSigner(config)

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
			b.AddTx(key.sign(t, signer, nonce, created, big.NewInt(1), burnGas, nil))
			nonce++
		}
	})

	require.Len(t, blocks[2].Transactions(), 2, "block 3 must carry the deployment and the transfer")
	require.Greater(t, blocks[2].GasUsed(), uint64(burnGas),
		"premise: the loop really ran, so this is the attack and not a transfer that executed nothing")

	sameBlock, err := paymentlane.Decode(blocks[2].UncleHash())
	require.NoError(t, err)
	require.Zero(t, sameBlock.PaymentGasUsed,
		"the destination holds code by the time the transfer runs, so the loop it executes is general gas")

	nextBlock, err := paymentlane.Decode(blocks[3].UncleHash())
	require.NoError(t, err)
	require.Zero(t, nextBlock.PaymentGasUsed,
		"and it stays general once the code is older than the block")

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err)
}

// The bid path's only lane verdict on a re-executed environment: whatever the quota still holds
// idle must fit in what the pool has left.
func TestPaymentLaneVerifyPackedBidChecksTheIdleQuota(t *testing.T) {
	ls := &LaneState{class: paymentlane.NewClassifier(liveState{}, nil), state: liveState{}}

	ls.Budget.LaneSize = 100
	require.NoError(t, ls.VerifyPackedBid(100), "a quota that exactly fits is the accepting case")
	require.ErrorIs(t, ls.VerifyPackedBid(99), paymentlane.ErrViolated,
		"a bid that leaves less than the idle quota must be rejected")

	ls.Budget.PaymentUsed = 40
	require.NoError(t, ls.VerifyPackedBid(60), "payment gas already booked shrinks the idle quota one for one")
}

// TestPaymentLaneReportsAFailedReadAsLocal pins the error CLASS, not the rejection: a failed read
// classifies as payment, so the verdict would otherwise be that the block lied - and peers pay.
func TestPaymentLaneReportsAFailedReadAsLocal(t *testing.T) {
	broken := errors.New("missing trie node")
	live := liveState{err: broken}
	ls := &LaneState{
		class:    paymentlane.NewClassifier(live, nil),
		state:    live,
		gasLimit: laneTestGasLimit,
	}
	// The flip itself: the block booked this gas as general, the failed read books it as payment.
	ls.Budget.PaymentUsed = paymentTxGas

	err := ls.VerifyImported(paymentTxGas, paymentTxGas, paymentlane.Commitment{})
	require.ErrorIs(t, err, paymentlane.ErrStateUnavailable, "a failed read is this node's fault, not the block's")
	require.ErrorIs(t, err, broken, "the cause has to survive for whoever reads the log")
	require.NotErrorIs(t, err, paymentlane.ErrUntruthy, "calling a good block untruthful is what costs peers")

	require.ErrorIs(t, ls.WriteCommitmentAndVerify(types.NewBlockWithHeader(&types.Header{}), 0),
		paymentlane.ErrStateUnavailable, "nor may the producing side sign over a dirty classification")
}

// --- helpers -------------------------------------------------------------------

// liveState fakes the block's live state, every destination codeless. A non-nil err is a read
// that failed the way StateDB reports one: the zero hash now, the error only later.
type liveState struct{ err error }

func (liveState) GetCodeHash(common.Address) common.Hash { return common.Hash{} }
func (s liveState) Error() error                         { return s.err }

type ecdsaKey struct {
	priv *ecdsa.PrivateKey
	addr common.Address
}

func newKey(t testing.TB) *ecdsaKey {
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

// TestPaymentLaneAndUnclesCannotShareTheSlot checks WriteCommitmentAndVerify's shared-slot refusal.
func TestPaymentLaneAndUnclesCannotShareTheSlot(t *testing.T) {
	_, gspec, _ := laneGenesis(t)

	var caught any
	func() {
		defer func() { caught = recover() }()
		GenerateChainWithGenesis(gspec, ethash.NewFaker(), 3, func(i int, b *BlockGen) {
			if i+1 == 3 {
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

	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 3, nil)
	if _, err := paymentlane.Decode(blocks[2].UncleHash()); err != nil {
		t.Fatalf("the uncle-free chain must still carry a commitment: %v", err)
	}
}

// systemGasFaker injects fixed system gas into the shared Finalize path.
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

// TestPaymentLaneSignalCountsSystemTransactionGas checks that the signal includes system gas.
func TestPaymentLaneSignalCountsSystemTransactionGas(t *testing.T) {
	config, gspec, key := laneGenesis(t)

	const systemGas = 12_160_000
	engine := &systemGasFaker{Engine: ethash.NewFaker(), systemGas: systemGas}

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
	require.EqualValues(t, 3_100_000, c4.LaneSize,
		"signal must count system gas too: user general alone is %d", general)

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, engine, DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err, "a chain with non-zero system gas must import")
}

// TestPaymentLaneActivatesFromGenesis checks the genesis-grandparent boundary.
func TestPaymentLaneActivatesFromGenesis(t *testing.T) {
	config, gspec, key := laneGenesis(t)
	zero := uint64(0)
	config.GaussTime = &zero
	require.True(t, config.IsGauss(common.Big0, gspec.Timestamp),
		"the lane must bind from block 1, or the grandparent is never the genesis block")

	signer := types.LatestSigner(config)
	var nonce uint64
	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 3, func(i int, b *BlockGen) {
		b.AddTx(key.sign(t, signer, nonce, common.Address{0xaa}, big.NewInt(1), params.TxGas, nil))
		nonce++
	})
	require.Len(t, blocks, 3)
	for _, b := range blocks {
		c, err := paymentlane.Decode(b.UncleHash())
		require.NoError(t, err, "block %d must carry a commitment", b.NumberU64())
		require.EqualValues(t, 2_000_000, c.LaneSize, "block %d holds at the floor", b.NumberU64())
		require.EqualValues(t, params.TxGas, c.PaymentGasUsed, "block %d", b.NumberU64())
	}

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err, "a chain whose lane binds from block 1 must import")
}
