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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	"github.com/prysmaticlabs/prysm/v5/validator/accounts"
	"github.com/prysmaticlabs/prysm/v5/validator/accounts/iface"
	"github.com/prysmaticlabs/prysm/v5/validator/keymanager"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
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

func (p *mockPOSA) GetJustifiedNumberAndHash(chain consensus.ChainHeaderReader, headers []*types.Header) (uint64, common.Hash, error) {
	parentHeader := chain.GetHeaderByHash(headers[len(headers)-1].ParentHash)
	if parentHeader == nil {
		return 0, common.Hash{}, errors.New("unexpected error")
	}
	return parentHeader.Number.Uint64(), parentHeader.Hash(), nil
}

func (p *mockInvalidPOSA) GetJustifiedNumberAndHash(chain consensus.ChainHeaderReader, headers []*types.Header) (uint64, common.Hash, error) {
	return 0, common.Hash{}, errors.New("not supported")
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
