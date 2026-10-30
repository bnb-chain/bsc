// Copyright 2026 The go-ethereum Authors
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

package trie

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestSyncDeletesMismatchedRootWithoutDownload(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	parts := make([][]byte, 17)
	for i := 0; i < 16; i++ {
		parts[i] = common.Hash{byte(i + 1)}.Bytes()
	}
	rootBlob, err := rlp.EncodeToBytes(parts)
	if err != nil {
		t.Fatal(err)
	}
	rawdb.WriteAccountTrieNode(db, nil, rootBlob)
	rawdb.WriteAccountTrieNode(db, []byte{0}, []byte("retained-child"))
	same := NewSync(crypto.Keccak256Hash(rootBlob), db, nil, rawdb.PathScheme)
	batch := db.NewBatch()
	if err = same.Commit(batch); err != nil {
		t.Fatal(err)
	}
	if err = batch.Write(); err != nil {
		t.Fatal(err)
	}
	if !rawdb.HasAccountTrieNode(db, nil) {
		t.Fatal("same-root sync deleted root")
	}
	different := NewSync(common.Hash{99}, db, nil, rawdb.PathScheme)
	batch = db.NewBatch()
	if err = different.Commit(batch); err != nil {
		t.Fatal(err)
	}
	if err = batch.Write(); err != nil {
		t.Fatal(err)
	}
	if rawdb.HasAccountTrieNode(db, nil) {
		t.Fatal("expected mismatched root deletion")
	}
	if !rawdb.HasAccountTrieNode(db, []byte{0}) {
		t.Fatal("child unexpectedly removed")
	}
}
