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
	"github.com/prysmaticlabs/prysm/validator/keymanager/imported"
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
)

func setupVoteSigner(t *testing.T) *VoteSigner {
	// Create keymanager
	km := keymanager.IKeymanager(&imported.Keymanager{})
	voteSigner, err := NewVoteSigner(&km)
	if err != nil {
		t.Fatalf("failed to create vote signer: %v", err)
	}
	return voteSigner
}

func setupVoteJournal(t *testing.T) *VoteJournal {
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

	voteJournal, err := NewVoteJournal(journal)
	if err != nil {
		t.Fatalf("failed to create temporary votes journal: %v", err)
	}

	return voteJournal
}

func setupVoteManager(t *testing.T) *VoteManager {
	db := rawdb.NewMemoryDatabase()
	blockchain, _ := core.NewBlockChain(db, nil, params.TestChainConfig, ethash.NewFullFaker(), vm.Config{}, nil, nil)
	// Create event Mux
	mux := new(event.TypeMux)
	voteJournal := setupVoteJournal(t)
	voteSigner := setupVoteSigner(t)
	voteManager, err := NewVoteManager(mux, params.TestChainConfig, blockchain, voteJournal, voteSigner)
	if err != nil {
		t.Fatalf("failed to create vote manager: %v", err)
	}
	return voteManager

}

func TestVotePool(t *testing.T) {

	km := setUpKeyManager(t)

	// Create vote Signer
	voteSigner, _ := NewVoteSigner(km)

	// Create a database pre-initialize with a genesis block
	db := rawdb.NewMemoryDatabase()
	(&core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  core.GenesisAlloc{testAddr: {Balance: big.NewInt(1000000)}},
	}).MustCommit(db)
	chain, _ := core.NewBlockChain(db, nil, params.TestChainConfig, ethash.NewFullFaker(), vm.Config{}, nil, nil)

	mux := new(event.TypeMux)

	// Create vote journal
	voteJournal := setupVoteJournal(t)

	// Create vote manager
	voteManager, err := NewVoteManager(mux, params.TestChainConfig, chain, voteJournal, voteSigner)
	if err != nil {
		t.Fatalf("failed to create vote manager: %v", err)
	}

	// Create vote pool
	votePool := NewVotePool(params.TestChainConfig, chain, voteManager, ethash.NewFaker())
	voteManager.pool = votePool

	// Send the done event of downloader
	time.Sleep(10 * time.Millisecond)
	mux.Post(downloader.DoneEvent{})

	bs, _ := core.GenerateChain(params.TestChainConfig, chain.Genesis(), ethash.NewFaker(), db, 16, nil)
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}

	// The scenario of no voting happened before, current block header is 1
	receivedVotes, curVotes, futureVotes, curVotesPq, futureVotesPq := votePool.receivedVotes, votePool.curVotes, votePool.futureVotes, votePool.curVotesPq, votePool.futureVotesPq
	flag := false
	for i := 0; i < 60; i++ {
		time.Sleep(1 * time.Second)
		if receivedVotes.Cardinality() == 11 && len(curVotes) == 11 && len(futureVotes) == 0 && curVotesPq.Len() == 11 && futureVotesPq.Len() == 0 {
			flag = true
			break
		}
	}
	if !flag {
		t.Fatalf("put vote failed: %v", err)
	}

	bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}
	flag = false

	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		if receivedVotes.Cardinality() == 12 && len(curVotes) == 12 && len(futureVotes) == 0 && curVotesPq.Len() == 12 && futureVotesPq.Len() == 0 {
			flag = true
			break
		}
	}
	if !flag {
		t.Fatalf("put vote failed: %v", err)
	}

	for i := 0; i < 256; i++ {
		bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
		if _, err := chain.InsertChain(bs); err != nil {
			panic(err)
		}
	}

	// currently chain size is 273, and blockNumber before 17 in votePool should be pruned to 257
	flag = false
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		if len(votePool.curVotes) == 257 && votePool.curVotesPq.Len() == 257 {
			flag = true
			break
		}
	}
	if !flag {
		t.Fatalf("put vote failed: %v", err)
	}
	invalidVoteData := &types.VoteData{
		BlockNumber: 1000,
	}
	invalidVote := &types.VoteEnvelope{
		Data: invalidVoteData,
	}
	voteManager.pool.PutVote(invalidVote)

	flag = false
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		if len(votePool.curVotes) == 257 && votePool.curVotesPq.Len() == 257 && len(votePool.futureVotes) == 0 && votePool.futureVotesPq.Len() == 0 {
			flag = true
			break
		}
	}
	if !flag {
		t.Fatalf("put vote failed: %v", err)
	}
}

func setUpKeyManager(t *testing.T) *keymanager.IKeymanager {
	walletConfig := &accounts.CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      filepath.Join(t.TempDir(), "wallet"),
			KeymanagerKind: keymanager.Imported,
			WalletPassword: password,
		},
		SkipMnemonicConfirm: true,
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
	return &km
}
