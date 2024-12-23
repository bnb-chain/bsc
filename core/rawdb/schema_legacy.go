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
	"github.com/ethereum/go-ethereum/log"
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

// ReadFrozenOfAncientFreezer return freezer block number
func ReadFrozenOfAncientFreezer(db ethdb.KeyValueReader) uint64 {
	fozen, _ := db.Get(frozenOfAncientDBKey)
	if fozen == nil {
		return 0
	}
	return new(big.Int).SetBytes(fozen).Uint64()
}

// WriteFrozenOfAncientFreezer write freezer block number
func WriteFrozenOfAncientFreezer(db ethdb.KeyValueWriter, frozen uint64) {
	if err := db.Put(frozenOfAncientDBKey, new(big.Int).SetUint64(frozen).Bytes()); err != nil {
		log.Crit("Failed to store the ancient frozen number", "err", err)
	}
}

// ReadAncientType return freezer type
func ReadAncientType(db ethdb.KeyValueReader) uint64 {
	data, _ := db.Get(pruneAncientKey)
	if data == nil {
		return EntireFreezerType
	}
	return new(big.Int).SetBytes(data).Uint64()
}

// WriteAncientType write freezer type
func WriteAncientType(db ethdb.KeyValueWriter, flag uint64) {
	if err := db.Put(pruneAncientKey, new(big.Int).SetUint64(flag).Bytes()); err != nil {
		log.Crit("Failed to store prune ancient type", "err", err)
	}
}

// ReadOffSetOfCurrentAncientFreezer return prune block start
func ReadOffSetOfCurrentAncientFreezer(db ethdb.KeyValueReader) uint64 {
	offset, _ := db.Get(offSetOfCurrentAncientFreezer)
	if offset == nil {
		return 0
	}
	return new(big.Int).SetBytes(offset).Uint64()
}

// WriteOffSetOfCurrentAncientFreezer write prune block start
func WriteOffSetOfCurrentAncientFreezer(db ethdb.KeyValueWriter, offset uint64) {
	if err := db.Put(offSetOfCurrentAncientFreezer, new(big.Int).SetUint64(offset).Bytes()); err != nil {
		log.Crit("Failed to store the current offset of ancient", "err", err)
	}
}

// ReadOffSetOfLastAncientFreezer return last prune block start
func ReadOffSetOfLastAncientFreezer(db ethdb.KeyValueReader) uint64 {
	offset, _ := db.Get(offSetOfLastAncientFreezer)
	if offset == nil {
		return 0
	}
	return new(big.Int).SetBytes(offset).Uint64()
}

// WriteOffSetOfLastAncientFreezer wirte before prune block start
func WriteOffSetOfLastAncientFreezer(db ethdb.KeyValueWriter, offset uint64) {
	if err := db.Put(offSetOfLastAncientFreezer, new(big.Int).SetUint64(offset).Bytes()); err != nil {
		log.Crit("Failed to store the old offset of ancient", "err", err)
	}
}
