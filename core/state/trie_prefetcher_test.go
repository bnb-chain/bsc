// Copyright 2021 The go-ethereum Authors
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

package state

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/internal/testrand"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func filledStateDB() *StateDB {
	state, _ := New(types.EmptyRootHash, NewDatabaseForTesting())

	// Create an account and check if the retrieved balance is correct
	addr := common.HexToAddress("0xaffeaffeaffeaffeaffeaffeaffeaffeaffeaffe")
	skey := common.HexToHash("aaa")
	sval := common.HexToHash("bbb")

	state.SetBalance(addr, uint256.NewInt(42), tracing.BalanceChangeUnspecified) // Change the account trie
	state.SetCode(addr, []byte("hello"))                                         // Change an external metadata
	state.SetState(addr, skey, sval)                                             // Change the storage trie
	for i := 0; i < 100; i++ {
		sk := common.BigToHash(big.NewInt(int64(i)))
		state.SetState(addr, sk, sk) // Change the storage trie
	}
	return state
}

func prefetchGuaranteed(prefetcher *triePrefetcher, owner common.Hash, root common.Hash, addr common.Address, addrs []common.Address, slots []common.Hash) {
	prefetcher.prefetch(owner, root, addr, addrs, slots, false)
	for {
		if len(prefetcher.prefetchChan) == 0 {
			return
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func TestCopyAndClose(t *testing.T) {
	db := filledStateDB()
	prefetcher := newTriePrefetcher(db.db, db.originalRoot, "", false)
	skey := common.HexToHash("aaa")
	prefetchGuaranteed(prefetcher, common.Hash{}, db.originalRoot, common.Address{}, nil, []common.Hash{skey})
	prefetchGuaranteed(prefetcher, common.Hash{}, db.originalRoot, common.Address{}, nil, []common.Hash{skey})
	time.Sleep(1 * time.Second)
	a := prefetcher.trie(common.Hash{}, db.originalRoot)
	prefetchGuaranteed(prefetcher, common.Hash{}, db.originalRoot, common.Address{}, nil, []common.Hash{skey})
	b := prefetcher.trie(common.Hash{}, db.originalRoot)
	cpy := prefetcher.copy()
	prefetchGuaranteed(cpy, common.Hash{}, db.originalRoot, common.Address{}, nil, []common.Hash{skey})
	prefetchGuaranteed(cpy, common.Hash{}, db.originalRoot, common.Address{}, nil, []common.Hash{skey})
	c := cpy.trie(common.Hash{}, db.originalRoot)
	prefetcher.close()
	cpy2 := cpy.copy()
	prefetchGuaranteed(cpy2, common.Hash{}, db.originalRoot, common.Address{}, nil, []common.Hash{skey})
	d := cpy2.trie(common.Hash{}, db.originalRoot)
	cpy.close()
	cpy2.close()
	if a.Hash() != b.Hash() || a.Hash() != c.Hash() || a.Hash() != d.Hash() {
		t.Fatalf("Invalid trie, hashes should be equal: %v %v %v %v", a.Hash(), b.Hash(), c.Hash(), d.Hash())
	}
}

func TestUseAfterClose(t *testing.T) {
	db := filledStateDB()
	prefetcher := newTriePrefetcher(db.db, db.originalRoot, "", false)
	skey := common.HexToHash("aaa")
	prefetchGuaranteed(prefetcher, common.Hash{}, db.originalRoot, common.Address{}, nil, []common.Hash{skey})
	a := prefetcher.trie(common.Hash{}, db.originalRoot)
	prefetcher.close()
	b := prefetcher.trie(common.Hash{}, db.originalRoot)
	if a == nil {
		t.Fatal("Prefetching before close should not return nil")
	}
	if b != nil {
		t.Fatal("Trie after close should return nil")
	}
}

func TestCopyClose(t *testing.T) {
	db := filledStateDB()
	prefetcher := newTriePrefetcher(db.db, db.originalRoot, "", false)
	skey := common.HexToHash("aaa")
	prefetchGuaranteed(prefetcher, common.Hash{}, db.originalRoot, common.Address{}, nil, []common.Hash{skey})
	cpy := prefetcher.copy()
	a := prefetcher.trie(common.Hash{}, db.originalRoot)
	b := cpy.trie(common.Hash{}, db.originalRoot)
	prefetcher.close()
	c := prefetcher.trie(common.Hash{}, db.originalRoot)
	d := cpy.trie(common.Hash{}, db.originalRoot)
	if a == nil {
		t.Fatal("Prefetching before close should not return nil")
	}
	if b == nil {
		t.Fatal("Copy trie should return nil")
	}
	if c != nil {
		t.Fatal("Trie after close should return nil")
	}
	if d == nil {
		t.Fatal("Copy trie should not return nil")
	}
}

// TODO(Nathan): fix before verkle enabled
//
//nolint:unused
func testVerklePrefetcher(t *testing.T) {
	disk := rawdb.NewMemoryDatabase()
	db := triedb.NewDatabase(disk, triedb.VerkleDefaults)
	sdb := NewDatabase(db, nil)

	state, err := New(types.EmptyRootHash, sdb)
	if err != nil {
		t.Fatalf("failed to initialize state: %v", err)
	}
	// Create an account and check if the retrieved balance is correct
	addr := testrand.Address()
	skey := testrand.Hash()
	sval := testrand.Hash()

	state.SetBalance(addr, uint256.NewInt(42), tracing.BalanceChangeUnspecified) // Change the account trie
	state.SetCode(addr, []byte("hello"))                                         // Change an external metadata
	state.SetState(addr, skey, sval)                                             // Change the storage trie
	root, _, _ := state.Commit(0, true, false)

	state, _ = New(root, sdb)
	sRoot := state.GetStorageRoot(addr)
	fetcher := newTriePrefetcher(sdb, root, "", false)

	// Read account
	fetcher.prefetch(common.Hash{}, root, common.Address{}, []common.Address{addr}, nil, false)

	// Read storage slot
	fetcher.prefetch(crypto.Keccak256Hash(addr.Bytes()), sRoot, addr, nil, []common.Hash{skey}, false)

	fetcher.close()
	accountTrie := fetcher.trie(common.Hash{}, root)
	storageTrie := fetcher.trie(crypto.Keccak256Hash(addr.Bytes()), sRoot)

	rootA := accountTrie.Hash()
	rootB := storageTrie.Hash()
	if rootA != rootB {
		t.Fatal("Two different tries are retrieved")
	}
}
