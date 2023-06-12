package parlia_tests

import (
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
	"reflect"
	"testing"
)

func newGwei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(params.GWei))
}

// Test the gas used by running a transaction sent to a smart contract with given bytecode and storage.
func TestGasUsage(t *testing.T, config *params.ChainConfig, engine consensus.Engine, key *ecdsa.PrivateKey, gAlloc core.GenesisAlloc, txData types.TxData, expectedGasUsed uint64) {
	var (
		// Generate a canonical chain to act as the main dataset
		db = rawdb.NewMemoryDatabase()

		gspec = &core.Genesis{
			Config: config,
			Alloc:  gAlloc,
		}
		genesis = gspec.MustCommit(db)
	)

	blocks, _ := core.GenerateChain(gspec.Config, genesis, engine, db, 1, func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{1})
		// One transaction to 0xAAAA
		signer := types.LatestSigner(gspec.Config)
		txType := reflect.TypeOf(txData)
		var tx *types.Transaction
		if txType.String() == "*types.AccessListTx" {
			txData.(*types.AccessListTx).GasPrice = b.BaseFee()
			tx, _ = types.SignNewTx(key, signer, txData)
		} else {
			tx, _ = types.SignNewTx(key, signer, txData)
		}
		b.AddTx(tx)
	})

	// Import the canonical chain
	diskdb := rawdb.NewMemoryDatabase()
	gspec.MustCommit(diskdb)

	chain, err := core.NewBlockChain(diskdb, nil, gspec.Config, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create tester chain: %v", err)
	}
	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("block %d: failed to insert into chain: %v", n, err)
	}

	block := chain.GetBlockByNumber(1)

	if block.GasUsed() != expectedGasUsed {
		t.Fatalf("incorrect amount of gas spent: expected %d, got %d", expectedGasUsed, block.GasUsed())
	}
}
