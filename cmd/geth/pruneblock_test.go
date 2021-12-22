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
	"io/ioutil"
	"math/big"
	"os"
	"testing"

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
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
)

// So we can deterministically seed different blockchains
var (
	canonicalSeed = 1

	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)
)

func TestOfflineBlockPrune(t *testing.T) {
	datadir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("Failed to create temporary datadir: %v", err)
	}
	os.RemoveAll(datadir)

	chaindbPath := datadir + "/chaindata"
	oldAncientPath := chaindbPath + "/ancient"
	newAncientPath := chaindbPath + "/ancient_back"
	//create a database with ancient freezer
	db, err := rawdb.NewLevelDBDatabaseWithFreezer(chaindbPath, 512, utils.MakeDatabaseHandles(), oldAncientPath, "", false)

	if err != nil {
		t.Fatalf("failed to create database with ancient backend")
	}

	testBlockPruneForOffSetZero(t, true, db, newAncientPath, oldAncientPath, chaindbPath)

}

func testBlockPruneForOffSetZero(t *testing.T, full bool, db ethdb.Database, backFreezer, oldfreezer, chaindbPath string) error {
	// Make chain starting from genesis
	blockchain, n, err := newCanonical(t, db, ethash.NewFaker(), 92000, full, chaindbPath)
	if err != nil {
		t.Fatalf("failed to make new canonical chain: %v", err)
	}
	defer blockchain.Stop()

	testBlockPruner, err := pruner.NewBlockPruner(db, n, oldfreezer, backFreezer)
	if err != nil {
		t.Fatalf("failed to make new blockpruner: %v", err)
	}

	db.Close()
	if err := testBlockPruner.BlockPruneBackUp(chaindbPath, 512, utils.MakeDatabaseHandles(), "", false); err != nil {
		log.Error("Failed to back up block", "err", err)
		return err
	}

	return nil

}

// newCanonical creates a chain database, and injects a deterministic canonical
// chain. Depending on the full flag, if creates either a full block chain or a
// header only chain.
func newCanonical(t *testing.T, db ethdb.Database, engine consensus.Engine, height int, full bool, chaindbPath string) (*core.BlockChain, *node.Node, error) {

	var (
		key, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		address = crypto.PubkeyToAddress(key.PublicKey)
		funds   = big.NewInt(1000000000)
		gspec   = &core.Genesis{Config: params.TestChainConfig, Alloc: core.GenesisAlloc{address: {Balance: funds}}}
		genesis = gspec.MustCommit(db)
	)

	// Initialize a fresh chain with only a genesis block
	blockchain, _ := core.NewBlockChain(db, nil, params.AllEthashProtocolChanges, engine, vm.Config{}, nil, nil)

	// Full block-chain requested, same to GenerateChain func
	blocks := makeBlockChain(genesis, height, engine, db, canonicalSeed)
	_, err := blockchain.InsertChain(blocks)
	nd, _ := startEthService(t, gspec, blocks, chaindbPath)
	return blockchain, nd, err
}

// makeBlockChain creates a deterministic chain of blocks rooted at parent.
func makeBlockChain(parent *types.Block, n int, engine consensus.Engine, db ethdb.Database, seed int) []*types.Block {
	blocks, _ := core.GenerateChain(params.TestChainConfig, parent, engine, db, n, func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{0: byte(seed), 19: byte(i)})
	})
	return blocks
}

// startEthService creates a full node instance for testing.
func startEthService(t *testing.T, genesis *core.Genesis, blocks []*types.Block, chaindbPath string) (*node.Node, *eth.Ethereum) {
	t.Helper()

	n, err := node.New(&node.Config{DataDir: chaindbPath})
	if err != nil {
		t.Fatal("can't create node:", err)
	}

	ethcfg := &ethconfig.Config{Genesis: genesis, Ethash: ethash.Config{PowMode: ethash.ModeFake}}
	ethservice, err := eth.New(n, ethcfg)
	if err != nil {
		t.Fatal("can't create eth service:", err)
	}
	if err := n.Start(); err != nil {
		t.Fatal("can't start node:", err)
	}
	if _, err := ethservice.BlockChain().InsertChain(blocks); err != nil {
		n.Close()
		t.Fatal("can't import test blocks:", err)
	}
	ethservice.SetEtherbase(testAddr)

	return n, ethservice
}
