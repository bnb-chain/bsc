// Copyright 2025 The go-ethereum Authors
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

package eth

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/blobpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	"github.com/ethereum/go-ethereum/core/txpool/locals"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

var (
	key, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address = crypto.PubkeyToAddress(key.PublicKey)
	funds   = big.NewInt(1000_000_000_000_000)
	gspec   = &core.Genesis{
		Config: params.MergedTestChainConfig,
		Alloc: types.GenesisAlloc{
			address: {Balance: funds},
		},
		Difficulty: common.Big0,
		BaseFee:    big.NewInt(params.InitialBaseFee),
	}
	signer = types.LatestSignerForChainID(gspec.Config.ChainID)
)

func initBackend(withLocal bool) *EthAPIBackend {
	var (
		// Create a database pre-initialize with a genesis block
		db     = rawdb.NewMemoryDatabase()
		engine = beacon.New(ethash.NewFaker())
	)
	chain, _ := core.NewBlockChain(db, gspec, engine, nil)

	txconfig := legacypool.DefaultConfig
	txconfig.Journal = "" // Don't litter the disk with test journals

	blobPool := blobpool.New(blobpool.Config{Datadir: ""}, chain, nil)
	legacyPool := legacypool.New(txconfig, chain)
	txpool, _ := txpool.New(txconfig.PriceLimit, chain, []txpool.SubPool{legacyPool, blobPool})

	eth := &Ethereum{
		blockchain: chain,
		txPool:     txpool,
	}
	if withLocal {
		eth.localTxTracker = locals.New("", time.Minute, gspec.Config, txpool)
	}
	return &EthAPIBackend{
		eth: eth,
	}
}

func makeTx(nonce uint64, gasPrice *big.Int, amount *big.Int, key *ecdsa.PrivateKey) *types.Transaction {
	if gasPrice == nil {
		gasPrice = big.NewInt(params.GWei)
	}
	if amount == nil {
		amount = big.NewInt(1000)
	}
	tx, _ := types.SignTx(types.NewTransaction(nonce, common.Address{0x00}, amount, params.TxGas, gasPrice, nil), signer, key)
	return tx
}

type unsignedAuth struct {
	nonce uint64
	key   *ecdsa.PrivateKey
}

func pricedSetCodeTx(nonce uint64, gaslimit uint64, gasFee, tip *uint256.Int, key *ecdsa.PrivateKey, unsigned []unsignedAuth) *types.Transaction {
	var authList []types.SetCodeAuthorization
	for _, u := range unsigned {
		auth, _ := types.SignSetCode(u.key, types.SetCodeAuthorization{
			ChainID: *uint256.MustFromBig(gspec.Config.ChainID),
			Address: common.Address{0x42},
			Nonce:   u.nonce,
		})
		authList = append(authList, auth)
	}
	return pricedSetCodeTxWithAuth(nonce, gaslimit, gasFee, tip, key, authList)
}

func pricedSetCodeTxWithAuth(nonce uint64, gaslimit uint64, gasFee, tip *uint256.Int, key *ecdsa.PrivateKey, authList []types.SetCodeAuthorization) *types.Transaction {
	return types.MustSignNewTx(key, signer, &types.SetCodeTx{
		ChainID:    uint256.MustFromBig(gspec.Config.ChainID),
		Nonce:      nonce,
		GasTipCap:  tip,
		GasFeeCap:  gasFee,
		Gas:        gaslimit,
		To:         common.Address{},
		Value:      uint256.NewInt(100),
		Data:       nil,
		AccessList: nil,
		AuthList:   authList,
	})
}

func TestSendTx(t *testing.T) {
	testSendTx(t, false)
	testSendTx(t, true)
}

func TestSendTxEIP2681(t *testing.T) {
	b := initBackend(false)

	// Test EIP-2681: nonce overflow should be rejected
	tx := makeTx(uint64(math.MaxUint64), nil, nil, key) // max uint64 nonce
	err := b.SendTx(context.Background(), tx)
	if err == nil {
		t.Fatal("Expected EIP-2681 nonce overflow error, but transaction was accepted")
	}
	if !errors.Is(err, core.ErrNonceMax) {
		t.Errorf("Expected core.ErrNonceMax, got: %v", err)
	}

	// Test normal case: should succeed
	normalTx := makeTx(0, nil, nil, key)
	err = b.SendTx(context.Background(), normalTx)
	if err != nil {
		t.Errorf("Normal transaction should succeed, got error: %v", err)
	}
}

func testSendTx(t *testing.T, withLocal bool) {
	b := initBackend(withLocal)

	txA := pricedSetCodeTx(0, 250000, uint256.NewInt(params.GWei), uint256.NewInt(params.GWei), key, []unsignedAuth{{nonce: 0, key: key}})
	if err := b.SendTx(context.Background(), txA); err != nil {
		t.Fatalf("Failed to submit tx: %v", err)
	}
	for {
		pending, _ := b.TxPool().ContentFrom(address)
		if len(pending) == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	txB := makeTx(1, nil, nil, key)
	err := b.SendTx(context.Background(), txB)

	if withLocal {
		if err != nil {
			t.Fatalf("Unexpected error sending tx: %v", err)
		}
	} else {
		if !errors.Is(err, txpool.ErrInflightTxLimitReached) {
			t.Fatalf("Unexpected error, want: %v, got: %v", txpool.ErrInflightTxLimitReached, err)
		}
	}
}

func TestStateAtSystemTransactionGasBudget(t *testing.T) {
	config := *params.ParliaTestChainConfig
	config.Parlia = &params.ParliaConfig{}
	config.HaberTime, config.HaberFixTime, config.BohrTime, config.PascalTime = new(uint64), new(uint64), new(uint64), new(uint64)
	config.PragueTime, config.LorentzTime, config.MaxwellTime, config.FermiTime = new(uint64), new(uint64), new(uint64), new(uint64)
	config.OsakaTime, config.MendelTime, config.PasteurTime = new(uint64), new(uint64), new(uint64)
	forkTime := uint64(2)
	config.AmsterdamTime = &forkTime
	config.BlobScheduleConfig = params.BSCChainConfig.BlobScheduleConfig
	to := common.HexToAddress("0x1000")
	genesis := &core.Genesis{Config: &config, Alloc: types.GenesisAlloc{
		address: {Balance: big.NewInt(100)},
		// The first transaction records whether more than 16M execution gas is available.
		to: {Code: common.FromHex("0x63010000005a1160005500")},
	}}
	chain := newTestBlockChain(t, 0, genesis, nil)
	defer chain.Stop()
	parent := chain.GetBlockByNumber(0)
	engine := parlia.New(&config, nil, nil, parent.Hash())
	defer engine.Close()
	backend := &Ethereum{blockchain: chain, engine: engine}
	var txs []*types.Transaction
	for nonce := uint64(0); nonce < 2; nonce++ {
		txs = append(txs, types.MustSignNewTx(key, types.LatestSigner(&config), &types.LegacyTx{
			Nonce: nonce, To: &to, Gas: math.MaxUint64 / 2, GasPrice: common.Big0,
		}))
	}
	for _, timestamp := range []uint64{1, 2} {
		t.Run(new(big.Int).SetUint64(timestamp).String(), func(t *testing.T) {
			header := parent.Header()
			header.Number, header.ParentHash = big.NewInt(1), parent.Hash()
			header.Time, header.Coinbase, header.GasLimit = timestamp, address, 30_000_000
			header.BaseFee, header.Difficulty = common.Big0, common.Big1
			if timestamp >= forkTime {
				slot := uint64(1)
				header.SlotNumber = &slot
			}
			block := types.NewBlockWithHeader(header).WithBody(types.Body{Transactions: txs})
			tx, _, statedb, release, err := backend.stateAtTransaction(context.Background(), block, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if tx.Hash() != txs[1].Hash() || statedb.GetNonce(address) != 1 {
				t.Fatal("incorrect transaction or nonce after system transaction replay")
			}
			if have := statedb.GetState(to, common.Hash{}); have != common.BigToHash(common.Big1) {
				t.Fatalf("incorrect system transaction prestate: have %s, want 1", have)
			}
		})
	}
}
