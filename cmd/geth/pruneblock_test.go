// Copyright 2016 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state/pruner"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	canonicalSeed               = 1
	blockPruneBackUpBlockNumber = 128
	key, _                      = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address                     = crypto.PubkeyToAddress(key.PublicKey)
	balance                     = big.NewInt(100000000000000000)
	gspec                       = &core.Genesis{Config: params.TestChainConfig, Alloc: core.GenesisAlloc{address: {Balance: balance}}, BaseFee: big.NewInt(params.InitialBaseFee)}
	signer                      = types.LatestSigner(gspec.Config)
	config                      = &core.CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		TrieTimeLimit:  5 * time.Minute,
		SnapshotLimit:  0, // Disable snapshot
		TriesInMemory:  128,
	}
	engine = ethash.NewFullFaker()
)

func TestOfflineBlockPrune(t *testing.T) {
	//Corner case for 0 remain in ancinetStore.
	testOfflineBlockPruneWithAmountReserved(t, 0)
	//General case.
	testOfflineBlockPruneWithAmountReserved(t, 100)
}

func testOfflineBlockPruneWithAmountReserved(t *testing.T, amountReserved uint64) {
	datadir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("Failed to create temporary datadir: %v", err)
	}
	os.RemoveAll(datadir)

	chaindbPath := filepath.Join(datadir, "chaindata")
	oldAncientPath := filepath.Join(chaindbPath, "ancient")
	newAncientPath := filepath.Join(chaindbPath, "ancient_back")

	db, blocks, blockList, receiptsList, externTdList, startBlockNumber, _ := BlockchainCreator(t, chaindbPath, oldAncientPath, amountReserved)
	node, _ := startEthService(t, gspec, blocks, chaindbPath)
	defer node.Close()

	//Initialize a block pruner for pruning, only remain amountReserved blocks backward.
	testBlockPruner := pruner.NewBlockPruner(db, node, oldAncientPath, newAncientPath, amountReserved)
	if err != nil {
		t.Fatalf("failed to make new blockpruner: %v", err)
	}
	if err := testBlockPruner.BlockPruneBackUp(chaindbPath, 512, utils.MakeDatabaseHandles(), "", false, false); err != nil {
		t.Fatalf("Failed to back up block: %v", err)
	}

	dbBack, err := rawdb.NewLevelDBDatabaseWithFreezer(chaindbPath, 0, 0, newAncientPath, "", false, true, false, false, true)
	if err != nil {
		t.Fatalf("failed to create database with ancient backend")
	}
	defer dbBack.Close()

	//check against if the backup data matched original one
	for blockNumber := startBlockNumber; blockNumber < startBlockNumber+amountReserved; blockNumber++ {
		blockHash := rawdb.ReadCanonicalHash(dbBack, blockNumber)
		block := rawdb.ReadBlock(dbBack, blockHash, blockNumber)

		if block.Hash() != blockHash {
			t.Fatalf("block data did not match between oldDb and backupDb")
		}
		if blockList[blockNumber-startBlockNumber].Hash() != blockHash {
			t.Fatalf("block data did not match between oldDb and backupDb")
		}

		receipts := rawdb.ReadRawReceipts(dbBack, blockHash, blockNumber)
		if err := checkReceiptsRLP(receipts, receiptsList[blockNumber-startBlockNumber]); err != nil {
			t.Fatalf("receipts did not match between oldDb and backupDb")
		}
		// // Calculate the total difficulty of the block
		td := rawdb.ReadTd(dbBack, blockHash, blockNumber)
		if td == nil {
			t.Fatalf("Failed to ReadTd: %v", consensus.ErrUnknownAncestor)
		}
		if td.Cmp(externTdList[blockNumber-startBlockNumber]) != 0 {
			t.Fatalf("externTd did not match between oldDb and backupDb")
		}
	}

	//check if ancientDb freezer replaced successfully
	testBlockPruner.AncientDbReplacer()
	if _, err := os.Stat(newAncientPath); err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("ancientDb replaced unsuccessfully")
		}
	}
	if _, err := os.Stat(oldAncientPath); err != nil {
		t.Fatalf("ancientDb replaced unsuccessfully")
	}
}

func BlockchainCreator(t *testing.T, chaindbPath, AncientPath string, blockRemain uint64) (ethdb.Database, []*types.Block, []*types.Block, []types.Receipts, []*big.Int, uint64, *core.BlockChain) {
	//create a database with ancient freezer
	db, err := rawdb.NewLevelDBDatabaseWithFreezer(chaindbPath, 0, 0, AncientPath, "", false, false, false, false, true)
	if err != nil {
		t.Fatalf("failed to create database with ancient backend")
	}
	defer db.Close()
	genesis := gspec.MustCommit(db)
	// Initialize a fresh chain with only a genesis block
	blockchain, err := core.NewBlockChain(db, config, gspec.Config, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}

	// Make chain starting from genesis
	blocks, _ := core.GenerateChain(gspec.Config, genesis, ethash.NewFaker(), db, 500, func(i int, block *core.BlockGen) {
		block.SetCoinbase(common.Address{0: byte(canonicalSeed), 19: byte(i)})
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(address), common.Address{0x00}, big.NewInt(1000), params.TxGas, big.NewInt(params.InitialBaseFee), nil), signer, key)
		if err != nil {
			panic(err)
		}
		block.AddTx(tx)
		block.SetDifficulty(big.NewInt(1000000))
	})
	if _, err := blockchain.InsertChain(blocks); err != nil {
		t.Fatalf("Failed to import canonical chain start: %v", err)
	}

	// Force run a freeze cycle
	type freezer interface {
		Freeze(threshold uint64) error
		Ancients() (uint64, error)
	}
	db.(freezer).Freeze(10)

	frozen, err := db.Ancients()
	//make sure there're frozen items
	if err != nil || frozen == 0 {
		t.Fatalf("Failed to import canonical chain start: %v", err)
	}
	if frozen < blockRemain {
		t.Fatalf("block amount is not enough for pruning: %v", err)
	}

	oldOffSet := rawdb.ReadOffSetOfCurrentAncientFreezer(db)
	// Get the actual start block number.
	startBlockNumber := frozen - blockRemain + oldOffSet
	// Initialize the slice to buffer the block data left.
	blockList := make([]*types.Block, 0, blockPruneBackUpBlockNumber)
	receiptsList := make([]types.Receipts, 0, blockPruneBackUpBlockNumber)
	externTdList := make([]*big.Int, 0, blockPruneBackUpBlockNumber)
	// All ancient data within the most recent 128 blocks write into memory buffer for future new ancient_back directory usage.
	for blockNumber := startBlockNumber; blockNumber < frozen+oldOffSet; blockNumber++ {
		blockHash := rawdb.ReadCanonicalHash(db, blockNumber)
		block := rawdb.ReadBlock(db, blockHash, blockNumber)
		blockList = append(blockList, block)
		receipts := rawdb.ReadRawReceipts(db, blockHash, blockNumber)
		receiptsList = append(receiptsList, receipts)
		// Calculate the total difficulty of the block
		td := rawdb.ReadTd(db, blockHash, blockNumber)
		if td == nil {
			t.Fatalf("Failed to ReadTd: %v", consensus.ErrUnknownAncestor)
		}
		externTdList = append(externTdList, td)
	}

	return db, blocks, blockList, receiptsList, externTdList, startBlockNumber, blockchain
}

func checkReceiptsRLP(have, want types.Receipts) error {
	if len(have) != len(want) {
		return fmt.Errorf("receipts sizes mismatch: have %d, want %d", len(have), len(want))
	}
	for i := 0; i < len(want); i++ {
		rlpHave, err := rlp.EncodeToBytes(have[i])
		if err != nil {
			return err
		}
		rlpWant, err := rlp.EncodeToBytes(want[i])
		if err != nil {
			return err
		}
		if !bytes.Equal(rlpHave, rlpWant) {
			return fmt.Errorf("receipt #%d: receipt mismatch: have %s, want %s", i, hex.EncodeToString(rlpHave), hex.EncodeToString(rlpWant))
		}
	}
	return nil
}

// startEthService creates a full node instance for testing.
func startEthService(t *testing.T, genesis *core.Genesis, blocks []*types.Block, chaindbPath string) (*node.Node, *eth.Ethereum) {
	t.Helper()
	n, err := node.New(&node.Config{DataDir: chaindbPath})
	if err != nil {
		t.Fatal("can't create node:", err)
	}

	if err := n.Start(); err != nil {
		t.Fatal("can't start node:", err)
	}

	return n, nil
}
