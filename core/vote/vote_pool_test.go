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
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prysmaticlabs/prysm/v4/crypto/bls"
	"github.com/prysmaticlabs/prysm/v4/validator/accounts"
	"github.com/prysmaticlabs/prysm/v4/validator/accounts/iface"
	"github.com/prysmaticlabs/prysm/v4/validator/keymanager"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
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

type mockPOSA struct {
	consensus.PoSA
}

type mockInvalidPOSA struct {
	consensus.PoSA
}

// testBackend is a mock implementation of the live Ethereum message handler.
type testBackend struct {
	eventMux *event.TypeMux
}

func newTestBackend() *testBackend {
	return &testBackend{eventMux: new(event.TypeMux)}
}
func (b *testBackend) IsMining() bool           { return true }
func (b *testBackend) EventMux() *event.TypeMux { return b.eventMux }

func (p *mockPOSA) GetJustifiedNumberAndHash(chain consensus.ChainHeaderReader, header *types.Header) (uint64, common.Hash, error) {
	parentHeader := chain.GetHeaderByHash(header.ParentHash)
	if parentHeader == nil {
		return 0, common.Hash{}, fmt.Errorf("unexpected error")
	}
	return parentHeader.Number.Uint64(), parentHeader.Hash(), nil
}

func (p *mockInvalidPOSA) GetJustifiedNumberAndHash(chain consensus.ChainHeaderReader, header *types.Header) (uint64, common.Hash, error) {
	return 0, common.Hash{}, fmt.Errorf("not supported")
}

func (m *mockPOSA) VerifyVote(chain consensus.ChainHeaderReader, vote *types.VoteEnvelope) error {
	return nil
}

func (m *mockInvalidPOSA) VerifyVote(chain consensus.ChainHeaderReader, vote *types.VoteEnvelope) error {
	return nil
}

func (m *mockPOSA) IsActiveValidatorAt(chain consensus.ChainHeaderReader, header *types.Header, checkVoteKeyFn func(bLSPublicKey *types.BLSPublicKey) bool) bool {
	return true
}

func (m *mockInvalidPOSA) IsActiveValidatorAt(chain consensus.ChainHeaderReader, header *types.Header, checkVoteKeyFn func(bLSPublicKey *types.BLSPublicKey) bool) bool {
	return true
}

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
		if int(lastIndex)-int(firstIndex)+1 == size {
			return true
		}
		lastVote, _ := journal.ReadVote(lastIndex)
		if lastVote != nil && lastVote.Data.TargetNumber == uint64(lastLatestVoteNumber) {
			return true
		}
	}
	return false
}

func TestValidVotePool(t *testing.T) {
	testVotePool(t, true)
}

func TestInvalidVotePool(t *testing.T) {
	testVotePool(t, false)
}

func testVotePool(t *testing.T, isValidRules bool) {
	walletPasswordDir, walletDir := setUpKeyManager(t)

	genesis := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  core.GenesisAlloc{testAddr: {Balance: big.NewInt(1000000)}},
	}

	mux := new(event.TypeMux)
	db := rawdb.NewMemoryDatabase()
	chain, _ := core.NewBlockChain(db, nil, genesis, nil, ethash.NewFullFaker(), vm.Config{}, nil, nil)

	var mockEngine consensus.PoSA
	if isValidRules {
		mockEngine = &mockPOSA{}
	} else {
		mockEngine = &mockInvalidPOSA{}
	}

	// Create vote pool
	votePool := NewVotePool(chain, mockEngine)

	// Create vote manager
	// Create a temporary file for the votes journal
	file, err := os.CreateTemp("", "")
	if err != nil {
		t.Fatalf("failed to create temporary file path: %v", err)
	}
	journal := file.Name()
	defer os.Remove(journal)

	// Clean up the temporary file, we only need the path for now
	file.Close()
	os.Remove(journal)

	voteManager, err := NewVoteManager(newTestBackend(), chain, votePool, journal, walletPasswordDir, walletDir, mockEngine)
	if err != nil {
		t.Fatalf("failed to create vote managers")
	}

	voteJournal := voteManager.journal

	// Send the done event of downloader
	time.Sleep(10 * time.Millisecond)
	mux.Post(downloader.DoneEvent{})

	bs, _ := core.GenerateChain(params.TestChainConfig, chain.Genesis(), ethash.NewFaker(), db, 1, nil)
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}
	for i := 0; i < 10+blocksNumberSinceMining; i++ {
		bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
		if _, err := chain.InsertChain(bs); err != nil {
			panic(err)
		}
	}

	if !isValidRules {
		if votePool.verifyStructureSizeOfVotePool(11, 11, 0, 11, 0) {
			t.Fatalf("put vote failed")
		}
		return
	}

	if !votePool.verifyStructureSizeOfVotePool(11, 11, 0, 11, 0) {
		t.Fatalf("put vote failed")
	}

	// Verify if votesPq is min heap
	votesPq := votePool.curVotesPq
	pqBuffer := make([]*types.VoteData, 0)
	lastVotedBlockNumber := uint64(0)
	for votesPq.Len() > 0 {
		voteData := heap.Pop(votesPq).(*types.VoteData)
		if voteData.TargetNumber < lastVotedBlockNumber {
			t.Fatalf("votesPq verification failed")
		}
		lastVotedBlockNumber = voteData.TargetNumber
		pqBuffer = append(pqBuffer, voteData)
	}
	for _, voteData := range pqBuffer {
		heap.Push(votesPq, voteData)
	}

	// Verify journal
	if !voteJournal.verifyJournal(11, 11) {
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
	if !voteJournal.verifyJournal(12, 12) {
		t.Fatalf("journal failed")
	}

	for i := 0; i < 256; i++ {
		bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
		if _, err := chain.InsertChain(bs); err != nil {
			panic(err)
		}
	}

	// Verify journal
	if !voteJournal.verifyJournal(268, 268) {
		t.Fatalf("journal failed")
	}

	// currently chain size is 268, and votePool should be pruned, so vote pool size should be 256!
	if !votePool.verifyStructureSizeOfVotePool(256, 256, 0, 256, 0) {
		t.Fatalf("put vote failed")
	}

	// Test invalid vote whose number larger than latestHeader + 13
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
	if !voteJournal.verifyJournal(268, 268) {
		t.Fatalf("journal failed")
	}

	// Test future votes scenario: votes number within latestBlockHeader ~ latestBlockHeader + 13
	futureVote := &types.VoteEnvelope{
		Data: &types.VoteData{
			TargetNumber: 279,
		},
	}
	if err := voteManager.signer.SignVote(futureVote); err != nil {
		t.Fatalf("sign vote failed")
	}
	voteManager.pool.PutVote(futureVote)

	if !votePool.verifyStructureSizeOfVotePool(257, 256, 1, 256, 1) {
		t.Fatalf("put vote failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(268, 268) {
		t.Fatalf("journal failed")
	}

	// Test duplicate vote case, shouldn'd be put into vote pool
	duplicateVote := &types.VoteEnvelope{
		Data: &types.VoteData{
			TargetNumber: 279,
		},
	}
	if err := voteManager.signer.SignVote(duplicateVote); err != nil {
		t.Fatalf("sign vote failed")
	}
	voteManager.pool.PutVote(duplicateVote)

	if !votePool.verifyStructureSizeOfVotePool(257, 256, 1, 256, 1) {
		t.Fatalf("put vote failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(268, 268) {
		t.Fatalf("journal failed")
	}

	// Test future votes larger than latestBlockNumber + 13 should be rejected
	futureVote = &types.VoteEnvelope{
		Data: &types.VoteData{
			TargetNumber: 282,
			TargetHash:   common.Hash{},
		},
	}
	voteManager.pool.PutVote(futureVote)
	if !votePool.verifyStructureSizeOfVotePool(257, 256, 1, 256, 1) {
		t.Fatalf("put vote failed")
	}

	// Test transfer votes from future to cur, latest block header is #288 after the following generation
	// For the above BlockNumber 279, it did not have blockHash, should be assigned as well below.
	curNumber := 268
	var futureBlockHash common.Hash
	for i := 0; i < 20; i++ {
		bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
		curNumber += 1
		if curNumber == 279 {
			futureBlockHash = bs[0].Hash()
			futureVotesMap := votePool.futureVotes
			voteBox := futureVotesMap[common.Hash{}]
			futureVotesMap[futureBlockHash] = voteBox
			delete(futureVotesMap, common.Hash{})
			futureVotesPq := votePool.futureVotesPq
			futureVotesPq.Peek().TargetHash = futureBlockHash
		}
		if _, err := chain.InsertChain(bs); err != nil {
			panic(err)
		}
	}

	for i := 0; i < timeThreshold; i++ {
		time.Sleep(1 * time.Second)
		_, ok := votePool.curVotes[futureBlockHash]
		if ok && len(votePool.curVotes[futureBlockHash].voteMessages) == 2 {
			break
		}
	}
	if votePool.curVotes[futureBlockHash] == nil || len(votePool.curVotes[futureBlockHash].voteMessages) != 2 {
		t.Fatalf("transfer vote failed")
	}

	// Pruner will keep the size of votePool as latestBlockHeader-255~latestBlockHeader, then final result should be 256!
	if !votePool.verifyStructureSizeOfVotePool(257, 256, 0, 256, 0) {
		t.Fatalf("put vote failed")
	}

	// Verify journal
	if !voteJournal.verifyJournal(288, 288) {
		t.Fatalf("journal failed")
	}

	for i := 0; i < 224; i++ {
		bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
		if _, err := chain.InsertChain(bs); err != nil {
			panic(err)
		}
	}

	// Verify journal
	if !voteJournal.verifyJournal(512, 512) {
		t.Fatalf("journal failed")
	}

	bs, _ = core.GenerateChain(params.TestChainConfig, bs[len(bs)-1], ethash.NewFaker(), db, 1, nil)
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}

	// Verify if journal no longer than 512
	if !voteJournal.verifyJournal(512, 513) {
		t.Fatalf("journal failed")
	}
}

func setUpKeyManager(t *testing.T) (string, string) {
	walletDir := filepath.Join(t.TempDir(), "wallet")
	opts := []accounts.Option{}
	opts = append(opts, accounts.WithWalletDir(walletDir))
	opts = append(opts, accounts.WithWalletPassword(password))
	opts = append(opts, accounts.WithKeymanagerType(keymanager.Local))
	opts = append(opts, accounts.WithSkipMnemonicConfirm(true))
	acc, err := accounts.NewCLIManager(opts...)
	if err != nil {
		t.Fatalf("New Accounts CLI Manager failed: %v.", err)
	}
	walletPasswordDir := filepath.Join(t.TempDir(), "password")
	if err := os.MkdirAll(filepath.Dir(walletPasswordDir), 0700); err != nil {
		t.Fatalf("failed to create walletPassword dir: %v", err)
	}
	if err := os.WriteFile(walletPasswordDir, []byte(password), 0600); err != nil {
		t.Fatalf("failed to write wallet password dir: %v", err)
	}

	w, err := acc.WalletCreate(context.Background())
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}
	km, _ := w.InitializeKeymanager(context.Background(), iface.InitKeymanagerConfig{ListenForChanges: false})
	k, _ := km.(keymanager.Importer)
	secretKey, _ := bls.RandKey()
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
	accounts.ImportAccounts(context.Background(), &accounts.ImportAccountsConfig{
		Importer:        k,
		Keystores:       []*keymanager.Keystore{keystore},
		AccountPassword: password,
	})
	return walletPasswordDir, walletDir
}
