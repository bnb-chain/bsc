// Copyright 2014 The go-ethereum Authors
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

package vote

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prysmaticlabs/prysm/crypto/bls"
	"github.com/prysmaticlabs/prysm/validator/accounts"
	"github.com/prysmaticlabs/prysm/validator/accounts/iface"
	"github.com/prysmaticlabs/prysm/validator/accounts/wallet"
	"github.com/prysmaticlabs/prysm/validator/keymanager"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
)

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")

	// testAddr is the Ethereum address of the tester account.
	testAddr = crypto.PubkeyToAddress(testKey.PublicKey)

	password = "secretPassword"

	timeThreshold = 30
)

func (pool *VotePool) verifyStructureSizeOfVotePool(receivedVotes, curVotes, futureVotes, curVotesPq, futureVotesPq int) bool {
	for i := 0; i < timeThreshold; i++ {
		time.Sleep(1 * time.Second)
		if pool.receivedVotes.Cardinality() == receivedVotes && len(pool.curVotes) == curVotes && len(pool.futureVotes) == futureVotes && pool.curVotesPq.Len() == curVotesPq && pool.futureVotesPq.Len() == futureVotesPq {
			return true
		}
	}
	return false

}

func (journal *VoteJournal) verifyJournal(size, lastLatestVoteNumber int) bool {
	for i := 0; i < timeThreshold; i++ {
		time.Sleep(1 * time.Second)
		lastIndex, _ := journal.walLog.LastIndex()
		firstIndex, _ := journal.walLog.FirstIndex()
		if journal.latestVote.Data.TargetNumber == uint64(lastLatestVoteNumber) && int(lastIndex)-int(firstIndex)+1 == size {
			return true
		}
	}
	return false

}

func TestVotePool(t *testing.T) {
	walletPasswordDir, walletDir := setUpKeyManager(t)

	// Create a database pre-initialize with a genesis block
	db := rawdb.NewMemoryDatabase()
	(&core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  core.GenesisAlloc{testAddr: {Balance: big.NewInt(1000000)}},
	}).MustCommit(db)
	chain, _ := core.NewBlockChain(db, nil, params.TestChainConfig, ethash.NewFullFaker(), vm.Config{}, nil, nil)

	mux := new(event.TypeMux)

	// Create vote pool
	votePool := NewVotePool(params.TestChainConfig, chain, ethash.NewFaker())

	// Create vote manager
	// Create a temporary file for the votes journal
	file, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatalf("failed to create temporary file path: %v", err)
	}
	journal := file.Name()
	defer os.Remove(journal)

	// Clean up the temporary file, we only need the path for now
	file.Close()
	os.Remove(journal)

	voteManager, err := NewVoteManager(mux, params.TestChainConfig, chain, votePool, journal, walletPasswordDir, walletDir)
	if err != nil {
		t.Fatalf("failed to create vote managers")
	}

	voteJournal := voteManager.journal

	// Send the done event of downloader
	time.Sleep(10 * time.Millisecond)
	mux.Post(downloader.DoneEvent{})

	bs, _ := core.GenerateChain(params.TestChainConfig, chain.Genesis(), ethash.NewFaker(), db, 16, nil)
	// Insert a batch of 16 blocks in chain, expect only 11 amount of blocks: Block6 ~ Block16 be voted!
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}

	if !votePool.verifyStructureSizeOfVotePool(11, 11, 0, 11, 0) {
		t.Fatalf("put vote failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(11, 16) {
		t.Fatalf("journal failed")
	}

	bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}

	if !votePool.verifyStructureSizeOfVotePool(12, 12, 0, 12, 0) {
		t.Fatalf("put vote failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(12, 17) {
		t.Fatalf("journal failed")
	}

	for i := 0; i < 256; i++ {
		bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
		if _, err := chain.InsertChain(bs); err != nil {
			panic(err)
		}
	}

	// Verify journal
	if !voteJournal.verifyJournal(256, 273) {
		t.Fatalf("journal failed")
	}

	// currently chain size is 273, and blockNumber before 18 in votePool should be pruned, so vote pool size should be 256!
	if !votePool.verifyStructureSizeOfVotePool(256, 256, 0, 256, 0) {
		t.Fatalf("put vote failed")
	}

	// Test invalid vote whose number larger than latestHeader + 11
	invalidVote := &types.VoteEnvelope{
		Data: &types.VoteData{
			TargetNumber: 1000,
		},
	}
	voteManager.pool.PutVote(invalidVote)

	if !votePool.verifyStructureSizeOfVotePool(256, 256, 0, 256, 0) {
		t.Fatalf("put vote failed")
	}

	votes := votePool.GetVotes()
	if len(votes) != 256 {
		t.Fatalf("get votes failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(256, 273) {
		t.Fatalf("journal failed")
	}

	// Test future votes scenario: votes number within latestBlockHeader ~ latestBlockHeader + 11
	futureVote := &types.VoteEnvelope{
		Data: &types.VoteData{
			TargetNumber: 282,
		},
	}
	voteManager.pool.PutVote(futureVote)

	if !votePool.verifyStructureSizeOfVotePool(257, 256, 1, 256, 1) {
		t.Fatalf("put vote failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(256, 273) {
		t.Fatalf("journal failed")
	}

	// Test duplicate vote case, shouldn'd be put into vote pool
	duplicateVote := &types.VoteEnvelope{
		Data: &types.VoteData{
			TargetNumber: 282,
		},
	}
	voteManager.pool.PutVote(duplicateVote)

	if !votePool.verifyStructureSizeOfVotePool(257, 256, 1, 256, 1) {
		t.Fatalf("put vote failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(256, 273) {
		t.Fatalf("journal failed")
	}

	// Test future votes larger than latestBlockNumber + 11 should be rejected
	futureVote = &types.VoteEnvelope{
		Data: &types.VoteData{
			TargetNumber: 285,
		},
	}
	voteManager.pool.PutVote(futureVote)
	if !votePool.verifyStructureSizeOfVotePool(257, 256, 1, 256, 1) {
		t.Fatalf("put vote failed")
	}

	// Test transfer votes from future to cur, latest block header is #293
	for i := 0; i < 20; i++ {
		bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
		if _, err := chain.InsertChain(bs); err != nil {
			panic(err)
		}
	}

	// Pruner will keep the size of votePool as latestBlockHeader-255~latestBlockHeader, plus one futureVote transfer, then final result should be 257!
	if !votePool.verifyStructureSizeOfVotePool(257, 257, 0, 257, 0) {
		t.Fatalf("put vote failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(256, 293) {
		t.Fatalf("journal failed")
	}
}

func setUpKeyManager(t *testing.T) (string, string) {
	walletDir := filepath.Join(t.TempDir(), "wallet")
	walletConfig := &accounts.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      walletDir,
			KeymanagerKind: keymanager.Imported,
			WalletPassword: password,
		},
		SkipMnemonicConfirm: true,
	}
	walletPasswordDir := filepath.Join(t.TempDir(), "password")
	if err := os.MkdirAll(filepath.Dir(walletPasswordDir), 0700); err != nil {
		t.Fatalf("failed to create walletPassword dir: %v", err)
	}
	if err := ioutil.WriteFile(walletPasswordDir, []byte(password), 0600); err != nil {
		t.Fatalf("failed to write wallet password dir: %v", err)
	}

	w, err := accounts.CreateWalletWithKeymanager(context.Background(), walletConfig)
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}
	km, err := w.InitializeKeymanager(context.Background(), iface.InitKeymanagerConfig{ListenForChanges: false})
	k, _ := km.(keymanager.Importer)
	secretKey, err := bls.RandKey()
	encryptor := keystorev4.New()
	pubKeyBytes := secretKey.PublicKey().Marshal()
	cryptoFields, err := encryptor.Encrypt(secretKey.Marshal(), password)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	id, _ := uuid.NewRandom()
	keystore := &keymanager.Keystore{
		Crypto:  cryptoFields,
		ID:      id.String(),
		Pubkey:  fmt.Sprintf("%x", pubKeyBytes),
		Version: encryptor.Version(),
		Name:    encryptor.Name(),
	}

	encodedFile, _ := json.MarshalIndent(keystore, "", "\t")
	keyStoreDir := filepath.Join(t.TempDir(), "keystore")
	keystoreFile, _ := os.Create(fmt.Sprintf("%s/keystore-%s.json", keyStoreDir, "publichh"))
	keystoreFile.Write(encodedFile)
	_, err = accounts.ImportAccounts(context.Background(), &accounts.ImportAccountsConfig{
		Importer:        k,
		Keystores:       []*keymanager.Keystore{keystore},
		AccountPassword: password,
	})
	return walletPasswordDir, walletDir
}
