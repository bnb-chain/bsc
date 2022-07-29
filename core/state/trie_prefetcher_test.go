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
)

func filledStateDB() *StateDB {
	state, _ := New(common.Hash{}, NewDatabase(rawdb.NewMemoryDatabase()), nil)

	// Create an account and check if the retrieved balance is correct
	addr := common.HexToAddress("0xaffeaffeaffeaffeaffeaffeaffeaffeaffeaffe")
	skey := common.HexToHash("aaa")
	sval := common.HexToHash("bbb")

	state.SetBalance(addr, big.NewInt(42)) // Change the account trie
	state.SetCode(addr, []byte("hello"))   // Change an external metadata
	state.SetState(addr, skey, sval)       // Change the storage trie
	for i := 0; i < 100; i++ {
		sk := common.BigToHash(big.NewInt(int64(i)))
		state.SetState(addr, sk, sk) // Change the storage trie
	}
	return state
}

func prefetchGuaranteed(prefetcher *triePrefetcher, root common.Hash, keys [][]byte, accountHash common.Hash) {
	prefetcher.prefetch(root, keys, accountHash)
	for {
		if len(prefetcher.prefetchChan) == 0 {
			return
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func TestCopyAndClose(t *testing.T) {
	db := filledStateDB()
	prefetcher := newTriePrefetcher(db.db, db.originalRoot, common.Hash{}, "")
	skey := common.HexToHash("aaa")
	prefetchGuaranteed(prefetcher, db.originalRoot, [][]byte{skey.Bytes()}, common.Hash{})
	prefetchGuaranteed(prefetcher, db.originalRoot, [][]byte{skey.Bytes()}, common.Hash{})
	time.Sleep(1 * time.Second)
	a := prefetcher.trie(db.originalRoot)
	prefetchGuaranteed(prefetcher, db.originalRoot, [][]byte{skey.Bytes()}, common.Hash{})
	b := prefetcher.trie(db.originalRoot)
	cpy := prefetcher.copy()
	prefetchGuaranteed(cpy, db.originalRoot, [][]byte{skey.Bytes()}, common.Hash{})
	prefetchGuaranteed(cpy, db.originalRoot, [][]byte{skey.Bytes()}, common.Hash{})
	c := cpy.trie(db.originalRoot)
	prefetcher.close()
	cpy2 := cpy.copy()
	prefetchGuaranteed(cpy2, db.originalRoot, [][]byte{skey.Bytes()}, common.Hash{})
	d := cpy2.trie(db.originalRoot)
	cpy.close()
	cpy2.close()
	if a.Hash() != b.Hash() || a.Hash() != c.Hash() || a.Hash() != d.Hash() {
		t.Fatalf("Invalid trie, hashes should be equal: %v %v %v %v", a.Hash(), b.Hash(), c.Hash(), d.Hash())
	}
}

func TestUseAfterClose(t *testing.T) {
	db := filledStateDB()
	prefetcher := newTriePrefetcher(db.db, db.originalRoot, common.Hash{}, "")
	skey := common.HexToHash("aaa")
	prefetchGuaranteed(prefetcher, db.originalRoot, [][]byte{skey.Bytes()}, common.Hash{})
	a := prefetcher.trie(db.originalRoot)
	prefetcher.close()
	b := prefetcher.trie(db.originalRoot)
	if a == nil {
		t.Fatal("Prefetching before close should not return nil")
	}
	if b != nil {
		t.Fatal("Trie after close should return nil")
	}
}

func TestCopyClose(t *testing.T) {
	db := filledStateDB()
	prefetcher := newTriePrefetcher(db.db, db.originalRoot, common.Hash{}, "")
	skey := common.HexToHash("aaa")
	prefetchGuaranteed(prefetcher, db.originalRoot, [][]byte{skey.Bytes()}, common.Hash{})
	cpy := prefetcher.copy()
	a := prefetcher.trie(db.originalRoot)
	b := cpy.trie(db.originalRoot)
	prefetcher.close()
	c := prefetcher.trie(db.originalRoot)
	d := cpy.trie(db.originalRoot)
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
