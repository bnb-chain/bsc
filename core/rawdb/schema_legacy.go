// Copyright 2018 The go-ethereum Authors
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

// Package rawdb contains a collection of low level database accessors.
package rawdb

import (
	"math/big"

	"github.com/ethereum/go-ethereum/ethdb"
)

// The fields below define the low level database schema prefixing.
var (
	// frozenOfAncientDBKey tracks the block number for ancientDB to save.
	frozenOfAncientDBKey = []byte("FrozenOfAncientDB")

	//PruneAncientFlag flag whether prune ancient
	pruneAncientKey = []byte("PruneAncientFlag")

	//offSet of new updated ancientDB.
	offSetOfCurrentAncientFreezer = []byte("offSetOfCurrentAncientFreezer")

	//offSet of the ancientDB before updated version.
	offSetOfLastAncientFreezer = []byte("offSetOfLastAncientFreezer")
)

// ReadLegacyOffset read legacy offset metadata from prune-block/pruneancient feature
func ReadLegacyOffset(db ethdb.KeyValueStore) uint64 {
	pruneAncientOffset := ReadFrozenOfAncientFreezer(db)
	pruneBlockCurrentOffset := ReadOffSetOfCurrentAncientFreezer(db)
	offset := max(pruneAncientOffset, pruneBlockCurrentOffset)
	return offset
}
func CleanLegacyOffset(db ethdb.KeyValueStore) {
	db.Delete(pruneAncientKey)
	db.Delete(frozenOfAncientDBKey)
	db.Delete(offSetOfCurrentAncientFreezer)
	db.Delete(offSetOfLastAncientFreezer)
}

// ReadFrozenOfAncientFreezer return freezer block number
func ReadFrozenOfAncientFreezer(db ethdb.KeyValueReader) uint64 {
	fozen, _ := db.Get(frozenOfAncientDBKey)
	if fozen == nil {
		return 0
	}
	return new(big.Int).SetBytes(fozen).Uint64()
}

// ReadOffSetOfCurrentAncientFreezer return prune block start
func ReadOffSetOfCurrentAncientFreezer(db ethdb.KeyValueReader) uint64 {
	offset, _ := db.Get(offSetOfCurrentAncientFreezer)
	if offset == nil {
		return 0
	}
	return new(big.Int).SetBytes(offset).Uint64()
}
