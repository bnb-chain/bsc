package core

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/systemcontracts/jenner"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// 55M matches mainnet, so the quota below is BEP-703 3.4.4's worked example.
const (
	laneTestGasLimit = 55_000_000
	laneTestQuota    = 2_750_000 // 500 * 55M / 10000, the unwritten-ratio default
)

// PaymentLane's storage: slot 1 is the listed set's array length.
var laneListedLenSlot = common.Hash{31: 1}

// laneGenesis builds a faker-backed BSC lane harness and preallocates 0x2007.
func laneGenesis(t testing.TB, gasLimit uint64) (*params.ChainConfig, *Genesis, *ecdsaKey) {
	t.Helper()
	code, err := hex.DecodeString(strings.TrimSpace(jenner.RialtoPaymentLaneContract))
	require.NoError(t, err)

	config := *params.ParliaTestChainConfig
	jennerTime := uint64(15)
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
	}

	key := newKey(t)
	gspec := &Genesis{
		Config:   &config,
		GasLimit: gasLimit,
		Alloc: types.GenesisAlloc{
			paymentlane.ContractAddress: {Code: code, Balance: common.Big0},
			key.addr:                    {Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))},
		},
	}
	return &config, gspec, key
}

// laneRecord is what one block's lane looked like once its transactions had run. Nothing is
// committed to the header any more, so the generator's own LaneState is the only place to read it.
type laneRecord struct {
	on                bool
	quota, paymentGas uint64
}

// recordLanes captures scalar payment usage for the Jenner regression tests.
func recordLanes(into map[uint64]laneRecord, gen func(int, *BlockGen)) func(int, *BlockGen) {
	return func(i int, b *BlockGen) {
		if gen != nil {
			gen(i, b)
		}
		into[b.header.Number.Uint64()] = laneRecord{
			on:         b.lane.On(),
			quota:      b.lane.Budget.PaymentLaneQuota,
			paymentGas: b.lane.Budget.PaymentLaneUsed,
		}
	}
}

func laneRequiredTxGas(t testing.TB, config *params.ChainConfig, data []byte) uint64 {
	t.Helper()
	rules := config.Rules(common.Big1, false, 1)
	cost, err := IntrinsicGas(data, nil, nil, common.Address{}, &common.Address{}, new(uint256.Int), rules)
	require.NoError(t, err)
	gas := cost
	if rules.IsPrague {
		floor, err := FloorDataGas(rules, common.Address{}, &common.Address{}, new(uint256.Int), data, nil)
		require.NoError(t, err)
		if floor > gas {
			gas = floor
		}
	}
	return gas
}

// TestPaymentLaneQuotaAndClassification runs the quota derivation, the activation boundary and
// both classification rules through a generated chain, then imports it.
func TestPaymentLaneQuotaAndClassification(t *testing.T) {
	config, gspec, key := laneGenesis(t, laneTestGasLimit)

	// A listed destination is a payment transaction whatever it is called with - here a
	// zero-value call to an address that holds code, so only the list can make it one. The
	// client mirrors the set through getPaymentContracts, i.e. the EnumerableSet's array.
	listed := common.Address{0xcc}
	gspec.Alloc[listed] = types.Account{Code: []byte{0x00}, Balance: common.Big0} // STOP
	laneContract := gspec.Alloc[paymentlane.ContractAddress]
	laneContract.Storage = map[common.Hash]common.Hash{
		laneListedLenSlot:                       common.BigToHash(common.Big1),
		enumerableSetSlot(laneListedLenSlot, 0): common.BytesToHash(listed[:]),
	}
	gspec.Alloc[paymentlane.ContractAddress] = laneContract

	transferGas := laneRequiredTxGas(t, config, nil)
	signer := types.LatestSigner(config)

	var nonce uint64
	records := map[uint64]laneRecord{}
	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFullFaker(), 6, recordLanes(records, func(i int, b *BlockGen) {
		// A bare transfer (payment), a zero-value call to a codeless account (general, no
		// value), and a zero-value call to the listed contract (payment, by the list).
		b.AddTx(key.sign(t, signer, nonce, common.Address{0xaa}, common.Big1, transferGas, nil))
		nonce++
		b.AddTx(key.sign(t, signer, nonce, common.Address{0xbb}, common.Big0, transferGas, nil))
		nonce++
		b.AddTx(key.sign(t, signer, nonce, listed, common.Big0, transferGas, nil))
		nonce++
	}))

	// The lane binds a block if and only if its parent is at or after activation, so it starts
	// exactly one block later than the fork itself.
	var firstLaneBlock uint64
	for _, b := range blocks {
		if config.IsJenner(b.Number(), b.Time()) {
			firstLaneBlock = b.NumberU64() + 1
			break
		}
	}
	require.NotZero(t, firstLaneBlock, "fixture must straddle JennerTime")
	require.Less(t, firstLaneBlock, uint64(len(blocks)), "fixture must contain blocks the lane binds in")

	for _, b := range blocks {
		got, ok := records[b.NumberU64()]
		require.True(t, ok)
		if b.NumberU64() < firstLaneBlock {
			require.Falsef(t, got.on, "block %d is outside the mechanism", b.NumberU64())
			continue
		}
		require.Truef(t, got.on, "block %d must be inside the mechanism", b.NumberU64())
		require.EqualValuesf(t, laneTestQuota, got.quota, "block %d quota", b.NumberU64())
		require.EqualValuesf(t, 2*transferGas, got.paymentGas,
			"block %d: the bare transfer and the listed call are payment gas, the zero-value call is not", b.NumberU64())
	}

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFullFaker(), DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	n, err := chain.InsertChain(blocks)
	require.NoError(t, err, "inserted %d of %d", n, len(blocks))
	require.EqualValues(t, blocks[len(blocks)-1].NumberU64(), chain.CurrentBlock().Number.Uint64())
}

// enumerableSetSlot is the storage slot of element i of an EnumerableSet whose array length lives
// at lenSlot.
func enumerableSetSlot(lenSlot common.Hash, i uint64) common.Hash {
	base := new(big.Int).SetBytes(crypto.Keccak256(lenSlot.Bytes()))
	return common.BigToHash(base.Add(base, new(big.Int).SetUint64(i)))
}

// TestPaymentLaneRejectsABlockThatBreaksTheRule is the importer's verdict. The chain is generated
// with the lane switched off - a producer that honours the lane cannot build the block - and then
// imported by a node the lane binds for.
func TestPaymentLaneRejectsABlockThatBreaksTheRule(t *testing.T) {
	// Small enough to fill: the quota is 5% of it, so the general side must clear 95%.
	const gasLimit = 2_000_000
	config, gspec, key := laneGenesis(t, gasLimit)
	*config.JennerTime = ^uint64(0) >> 1 // off while the chain is produced

	generalTxGas := laneRequiredTxGas(t, config, nil)
	nGeneral := int((gasLimit - paymentlane.Quota(500, gasLimit) + generalTxGas) / generalTxGas)

	signer := types.LatestSigner(config)
	var nonce uint64
	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFullFaker(), 2, func(i int, b *BlockGen) {
		if i != 1 {
			return
		}
		for n := 0; n < nGeneral; n++ {
			// Zero value: general gas, so none of it counts towards the quota.
			b.AddTx(key.sign(t, signer, nonce, common.Address{0xaa}, common.Big0, generalTxGas, nil))
			nonce++
		}
	})
	require.Greater(t, blocks[1].GasUsed(), gasLimit-paymentlane.Quota(500, gasLimit),
		"the fixture must leave less room than the quota reserves, or there is nothing to reject")

	*config.JennerTime = 0 // and on for every importer below

	for _, tc := range []struct {
		name    string
		cfg     *BlockChainConfig
		wantErr error
	}{
		{"a node that replays classification rejects it", DefaultConfig(), paymentlane.ErrViolated},
		{"a no-tries node cannot classify and does not judge", fastNodeConfig(), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFullFaker(), tc.cfg)
			require.NoError(t, err)
			defer chain.Stop()

			_, err = chain.InsertChain(blocks)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func fastNodeConfig() *BlockChainConfig {
	cfg := DefaultConfig()
	cfg.NoTries = true
	return cfg
}

// TestPaymentLaneClassifiesAgainstTheLiveState is the attack the live-state gate closes, run
// through the real EVM and a real import. burnerInitCode deploys `5b600056` - JUMPDEST, PUSH1
// 0, JUMP - which loops until its gas is gone; the transfer behind it, one nonce later so that
// it packs second, would book that loop as payment gas against the parent post-state.
func TestPaymentLaneClassifiesAgainstTheLiveState(t *testing.T) {
	config, gspec, key := laneGenesis(t, laneTestGasLimit)
	signer := types.LatestSigner(config)

	const (
		burnerInitCode = "0x635b6000566000526004601cf3"
		burnGas        = 1_000_000
	)
	created := crypto.CreateAddress(key.addr, 0)

	var nonce uint64
	records := map[uint64]laneRecord{}
	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFullFaker(), 4, recordLanes(records, func(i int, b *BlockGen) {
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
	}))

	require.Len(t, blocks[2].Transactions(), 2, "block 3 must carry the deployment and the transfer")
	require.Greater(t, blocks[2].GasUsed(), uint64(burnGas),
		"premise: the loop really ran, so this is the attack and not a transfer that executed nothing")

	require.True(t, records[3].on && records[4].on, "both blocks must be inside the mechanism")
	require.Zero(t, records[3].paymentGas,
		"the destination holds code by the time the transfer runs, so the loop it executes is general gas")
	require.Zero(t, records[4].paymentGas, "and it stays general once the code is older than the block")

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFullFaker(), DefaultConfig())
	require.NoError(t, err)
	defer chain.Stop()
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err)
}

// The bid path's only lane verdict on a re-executed environment: whatever the quota still holds
// idle must fit in what the pool has left.
func TestPaymentLaneVerifyPackedBidChecksTheIdleQuota(t *testing.T) {
	ls := &LaneState{classifier: paymentlane.NewClassifier(paymentlane.NoSystemTxs, liveState{}, nil), state: liveState{}}

	ls.Budget.PaymentLaneQuota = 100
	require.NoError(t, ls.VerifyPackedBid(NewGasPool(100)), "a quota that exactly fits is the accepting case")
	require.ErrorIs(t, ls.VerifyPackedBid(NewGasPool(99)), paymentlane.ErrViolated,
		"a bid that leaves less than the idle quota must be rejected")

	ls.Budget.PaymentLaneUsed = 40
	ls.Budget.RecordUsedAmsterdam(paymentlane.PaymentLane, 100, 100)
	require.NoError(t, ls.VerifyPackedBid(NewGasPool(60)), "payment gas already booked shrinks the idle quota one for one")
}

// TestPaymentLaneReportsAFailedReadAsLocal pins the error category, not the rejection: a failed
// read classifies as payment, so the verdict would otherwise be that the block is invalid - and
// peers pay for this node's missing state.
func TestPaymentLaneReportsAFailedReadAsLocal(t *testing.T) {
	broken := errors.New("missing trie node")
	live := liveState{err: broken}
	ls := &LaneState{
		Budget:     paymentlane.Budget{PaymentLaneQuota: laneTestGasLimit},
		classifier: paymentlane.NewClassifier(paymentlane.NoSystemTxs, live, nil),
		state:      live,
		gasLimit:   laneTestGasLimit,
	}

	err := ls.Verify(params.TxGas, NewGasPool(laneTestGasLimit))
	require.ErrorIs(t, err, paymentlane.ErrStateUnavailable, "a failed read is this node's fault, not the block's")
	require.ErrorIs(t, err, broken, "the cause has to survive for whoever reads the log")
	require.NotErrorIs(t, err, paymentlane.ErrViolated, "calling a good block invalid is what costs peers")
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

// posaFaker answers only the one PoSA method the lane uses; the embedded nil interface makes it a
// consensus.Engine without implementing thirty methods that are never called.
type posaFaker struct {
	consensus.PoSA
	system map[common.Hash]struct{}
	err    error
}

func (f posaFaker) IsSystemTransaction(tx *types.Transaction, _ *types.Header) (bool, error) {
	_, ok := f.system[tx.Hash()]
	return ok, f.err
}

// The lane's first gate comes from the engine, so this pins the wiring: a PoSA engine's verdict
// reaches the classifier, and an engine that appends no system transactions answers no.
func TestSystemTxOracleComesFromTheEngine(t *testing.T) {
	config, _, key := laneGenesis(t, laneTestGasLimit)
	tx := key.sign(t, types.LatestSigner(config), 0, common.Address{0xaa}, common.Big1, params.TxGas, nil)
	header := &types.Header{Number: common.Big1}

	require.False(t, systemTxOracle(ethash.NewFullFaker(), header)(tx),
		"a non-PoSA engine appends no system transactions")
	require.True(t, systemTxOracle(posaFaker{system: map[common.Hash]struct{}{tx.Hash(): {}}}, header)(tx))
	require.False(t, systemTxOracle(posaFaker{system: map[common.Hash]struct{}{tx.Hash(): {}},
		err: errors.New("UnAuthorized transaction")}, header)(tx),
		"a sender that will not recover is a failing transaction, not a system one")
}

// Applying a transaction can restore a gas-pool snapshot on failure. A negative
// usage delta must not fill the payment quota with phantom gas.
func TestRecordUsedFromIgnoresARolledBackPool(t *testing.T) {
	ls := &LaneState{classifier: paymentlane.NewClassifier(paymentlane.NoSystemTxs, liveState{}, nil), state: liveState{}}
	gp := NewGasPool(1000)
	snapshot := *gp
	require.NoError(t, gp.CheckGasLegacy(100))
	require.NoError(t, gp.ChargeGasLegacy(0, 100))
	usedBefore := gp.Snapshot()

	require.NoError(t, gp.CheckGasLegacy(40))
	require.NoError(t, gp.ChargeGasLegacy(0, 40))
	ls.RecordUsedFrom(paymentlane.PaymentLane, gp, usedBefore)
	require.EqualValues(t, 40, ls.Budget.PaymentLaneUsed)

	*gp = snapshot // back past usedBefore
	require.Less(t, gp.Used(), usedBefore.Used())
	ls.RecordUsedFrom(paymentlane.PaymentLane, gp, usedBefore)
	require.EqualValues(t, 40, ls.Budget.PaymentLaneUsed, "a rolled-back pool books nothing rather than wrapping")
}

func TestPaymentLaneAmsterdamAccounting(t *testing.T) {
	// Legacy usage must not affect Amsterdam accounting or idle quota.
	ls := &LaneState{amsterdam: true, Budget: paymentlane.Budget{PaymentLaneQuota: 10, PaymentLaneUsed: 10}, classifier: paymentlane.NewClassifier(paymentlane.NoSystemTxs, liveState{}, nil), state: liveState{}}
	gp := NewGasPool(100)
	require.NoError(t, gp.ChargeGasAmsterdam(60, 0, 60))
	before := gp.Snapshot()
	require.NoError(t, gp.ChargeGasAmsterdam(0, 40, 40))
	ls.RecordUsedFrom(paymentlane.PaymentLane, gp, before)
	require.EqualValues(t, 0, ls.Budget.PaymentExecutionUsed)
	require.EqualValues(t, 40, ls.Budget.PaymentStateUsed)
	executionIdle, stateIdle := ls.Budget.IdleLaneAmsterdam()
	require.EqualValues(t, 10, executionIdle, "state payments do not release execution quota")
	require.Zero(t, stateIdle)
	before = gp.Snapshot()
	require.NoError(t, gp.ChargeGasAmsterdam(5, 2, 7))
	gp.Set(before)
	ls.RecordUsedFrom(paymentlane.PaymentLane, gp, before)
	require.EqualValues(t, 0, ls.Budget.PaymentExecutionUsed)
	require.EqualValues(t, 40, ls.Budget.PaymentStateUsed)
	require.EqualValues(t, 10, ls.Budget.PaymentLaneUsed)
}

func TestPaymentLaneImportedMetricsReset(t *testing.T) {
	ls := &LaneState{classifier: paymentlane.NewClassifier(paymentlane.NoSystemTxs, liveState{}, nil)}
	names := []string{"paymentLaneQuota", "paymentGasUsed", "paymentLaneIdle",
		"paymentExecutionUsed", "paymentStateUsed", "paymentExecutionIdle", "paymentStateIdle"}
	for _, tc := range []struct {
		name      string
		amsterdam bool
		budget    paymentlane.Budget
		want      []int64
	}{
		{"Amsterdam", true, paymentlane.Budget{PaymentLaneQuota: 10, PaymentLaneUsed: 9, PaymentExecutionUsed: 4, PaymentStateUsed: 12}, []int64{10, 12, 6, 4, 12, 6, 0}},
		{"legacy reorg", false, paymentlane.Budget{PaymentLaneQuota: 10, PaymentLaneUsed: 3, PaymentExecutionUsed: 4, PaymentStateUsed: 12}, []int64{10, 3, 7, 0, 0, 0, 0}},
		{"Amsterdam again", true, paymentlane.Budget{PaymentLaneQuota: 10, PaymentExecutionUsed: 12, PaymentStateUsed: 4}, []int64{10, 12, 6, 12, 4, 0, 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ls.Budget, ls.amsterdam = tc.budget, tc.amsterdam
			ls.recordImported()
			for i, name := range names {
				gauge := metrics.DefaultRegistry.Get("paymentlane/imported/" + name).(*metrics.Gauge)
				require.Equal(t, tc.want[i], gauge.Snapshot().Value(), name)
			}
		})
	}
	ls.classifier = nil
	ls.recordImported()
	for _, name := range names {
		gauge := metrics.DefaultRegistry.Get("paymentlane/imported/" + name).(*metrics.Gauge)
		require.Zero(t, gauge.Snapshot().Value(), name)
	}
}

func TestPaymentLaneAmsterdamAdmission(t *testing.T) {
	ls := &LaneState{amsterdam: true, Budget: paymentlane.Budget{PaymentLaneQuota: 2_750_000}, classifier: paymentlane.NewClassifier(paymentlane.NoSystemTxs, liveState{}, nil)}
	gp := NewGasPool(55_000_000)
	require.NoError(t, gp.ChargeGasAmsterdam(38_000_000, 2_750_000, 0))
	require.False(t, ls.Admits(gp, paymentlane.GeneralLane, 30_000_000))
	require.True(t, ls.Admits(gp, paymentlane.PaymentLane, 30_000_000))
	ls.Budget.PaymentExecutionUsed = ls.Budget.PaymentLaneQuota
	require.True(t, ls.Admits(gp, paymentlane.GeneralLane, 30_000_000))
	require.NoError(t, gp.ReserveGasAmsterdam(250_000, 250_000))
	require.False(t, ls.Admits(gp, paymentlane.PaymentLane, 30_000_000))
	require.NoError(t, gp.ReleaseGasAmsterdam(250_000, 250_000))
	require.True(t, ls.Admits(gp, paymentlane.PaymentLane, 30_000_000))
}

func TestPaymentLaneAmsterdamVerify(t *testing.T) {
	for _, tc := range []struct {
		name                                                     string
		execution, state, paymentExecution, paymentState, system uint64
		invalid                                                  bool
	}{
		{"empty", 0, 0, 0, 0, 0, false},
		{"execution quota", 100, 90, 0, 10, 0, true},
		{"state quota", 90, 100, 10, 0, 0, true},
		{"both quotas filled", 100, 100, 10, 10, 0, false},
		{"system boundary", 85, 85, 0, 0, 5, false},
		{"system overflow", 85, 85, 0, 0, 6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ls := &LaneState{amsterdam: true, gasLimit: 100, Budget: paymentlane.Budget{PaymentLaneQuota: 10, PaymentExecutionUsed: tc.paymentExecution, PaymentStateUsed: tc.paymentState}, classifier: paymentlane.NewClassifier(paymentlane.NoSystemTxs, liveState{}, nil), state: liveState{}}
			gp := NewGasPool(100)
			require.NoError(t, gp.ChargeGasAmsterdam(tc.execution, tc.state, 0))
			packedErr := ls.VerifyPackedBid(gp)
			finalErr := ls.Verify(gp.Used()+tc.system, gp)
			if tc.invalid {
				require.ErrorIs(t, finalErr, paymentlane.ErrViolated)
			} else {
				require.NoError(t, finalErr)
			}
			if tc.system == 0 {
				require.Equal(t, tc.invalid, errors.Is(packedErr, paymentlane.ErrViolated))
			}
		})
	}
}
