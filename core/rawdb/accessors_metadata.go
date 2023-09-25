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

package rawdb

import (
	"encoding/json"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// FreezerType enumerator
const (
	EntireFreezerType uint64 = iota // classic ancient type
	PruneFreezerType                // prune ancient type
)

// ReadDatabaseVersion retrieves the version number of the database.
func ReadDatabaseVersion(db ethdb.KeyValueReader) *uint64 {
	var version uint64

	enc, _ := db.Get(databaseVersionKey)
	if len(enc) == 0 {
		return nil
	}
	if err := rlp.DecodeBytes(enc, &version); err != nil {
		return nil
	}

	return &version
}

// WriteDatabaseVersion stores the version number of the database
func WriteDatabaseVersion(db ethdb.KeyValueWriter, version uint64) {
	enc, err := rlp.EncodeToBytes(version)
	if err != nil {
		log.Crit("Failed to encode database version", "err", err)
	}
	if err = db.Put(databaseVersionKey, enc); err != nil {
		log.Crit("Failed to store the database version", "err", err)
	}
}

// ReadChainConfig retrieves the consensus settings based on the given genesis hash.
func ReadChainConfig(db ethdb.KeyValueReader, hash common.Hash) *params.ChainConfig {
	data, _ := db.Get(configKey(hash))
	if len(data) == 0 {
		return nil
	}
	var config params.ChainConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.Error("Invalid chain config JSON", "hash", hash, "err", err)
		return nil
	}
	return &config
}

// WriteChainConfig writes the chain config settings to the database.
func WriteChainConfig(db ethdb.KeyValueWriter, hash common.Hash, cfg *params.ChainConfig) {
	if cfg == nil {
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		log.Crit("Failed to JSON encode chain config", "err", err)
	}
	if err := db.Put(configKey(hash), data); err != nil {
		log.Crit("Failed to store chain config", "err", err)
	}
}

// ReadGenesisStateSpec retrieves the genesis state specification based on the
// given genesis (block-)hash.
func ReadGenesisStateSpec(db ethdb.KeyValueReader, blockhash common.Hash) []byte {
	data, _ := db.Get(genesisStateSpecKey(blockhash))
	return data
}

// WriteGenesisStateSpec writes the genesis state specification into the disk.
func WriteGenesisStateSpec(db ethdb.KeyValueWriter, blockhash common.Hash, data []byte) {
	if err := db.Put(genesisStateSpecKey(blockhash), data); err != nil {
		log.Crit("Failed to store genesis state", "err", err)
	}
}

// crashList is a list of unclean-shutdown-markers, for rlp-encoding to the
// database
type crashList struct {
	Discarded uint64   // how many ucs have we deleted
	Recent    []uint64 // unix timestamps of 10 latest unclean shutdowns
}

const crashesToKeep = 10

// PushUncleanShutdownMarker appends a new unclean shutdown marker and returns
// the previous data
// - a list of timestamps
// - a count of how many old unclean-shutdowns have been discarded
func PushUncleanShutdownMarker(db ethdb.KeyValueStore) ([]uint64, uint64, error) {
	var uncleanShutdowns crashList
	// Read old data
	if data, err := db.Get(uncleanShutdownKey); err != nil {
		log.Warn("Error reading unclean shutdown markers", "error", err)
	} else if err := rlp.DecodeBytes(data, &uncleanShutdowns); err != nil {
		return nil, 0, err
	}
	var discarded = uncleanShutdowns.Discarded
	var previous = make([]uint64, len(uncleanShutdowns.Recent))
	copy(previous, uncleanShutdowns.Recent)
	// Add a new (but cap it)
	uncleanShutdowns.Recent = append(uncleanShutdowns.Recent, uint64(time.Now().Unix()))
	if count := len(uncleanShutdowns.Recent); count > crashesToKeep+1 {
		numDel := count - (crashesToKeep + 1)
		uncleanShutdowns.Recent = uncleanShutdowns.Recent[numDel:]
		uncleanShutdowns.Discarded += uint64(numDel)
	}
	// And save it again
	data, _ := rlp.EncodeToBytes(uncleanShutdowns)
	if err := db.Put(uncleanShutdownKey, data); err != nil {
		log.Warn("Failed to write unclean-shutdown marker", "err", err)
		return nil, 0, err
	}
	return previous, discarded, nil
}

// PopUncleanShutdownMarker removes the last unclean shutdown marker
func PopUncleanShutdownMarker(db ethdb.KeyValueStore) {
	var uncleanShutdowns crashList
	// Read old data
	if data, err := db.Get(uncleanShutdownKey); err != nil {
		log.Warn("Error reading unclean shutdown markers", "error", err)
	} else if err := rlp.DecodeBytes(data, &uncleanShutdowns); err != nil {
		log.Error("Error decoding unclean shutdown markers", "error", err) // Should mos def _not_ happen
	}
	if l := len(uncleanShutdowns.Recent); l > 0 {
		uncleanShutdowns.Recent = uncleanShutdowns.Recent[:l-1]
	}
	data, _ := rlp.EncodeToBytes(uncleanShutdowns)
	if err := db.Put(uncleanShutdownKey, data); err != nil {
		log.Warn("Failed to clear unclean-shutdown marker", "err", err)
	}
}

// UpdateUncleanShutdownMarker updates the last marker's timestamp to now.
func UpdateUncleanShutdownMarker(db ethdb.KeyValueStore) {
	var uncleanShutdowns crashList
	// Read old data
	if data, err := db.Get(uncleanShutdownKey); err != nil {
		log.Warn("Error reading unclean shutdown markers", "error", err)
	} else if err := rlp.DecodeBytes(data, &uncleanShutdowns); err != nil {
		log.Warn("Error decoding unclean shutdown markers", "error", err)
	}
	// This shouldn't happen because we push a marker on Backend instantiation
	count := len(uncleanShutdowns.Recent)
	if count == 0 {
		log.Warn("No unclean shutdown marker to update")
		return
	}
	uncleanShutdowns.Recent[count-1] = uint64(time.Now().Unix())
	data, _ := rlp.EncodeToBytes(uncleanShutdowns)
	if err := db.Put(uncleanShutdownKey, data); err != nil {
		log.Warn("Failed to write unclean-shutdown marker", "err", err)
	}
}

// ReadTransitionStatus retrieves the eth2 transition status from the database
func ReadTransitionStatus(db ethdb.KeyValueReader) []byte {
	data, _ := db.Get(transitionStatusKey)
	return data
}

// WriteTransitionStatus stores the eth2 transition status to the database
func WriteTransitionStatus(db ethdb.KeyValueWriter, data []byte) {
	if err := db.Put(transitionStatusKey, data); err != nil {
		log.Crit("Failed to store the eth2 transition status", "err", err)
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

// ReadSafePointBlockNumber return the number of block that roothash save to disk
func ReadSafePointBlockNumber(db ethdb.KeyValueReader) uint64 {
	num, _ := db.Get(LastSafePointBlockKey)
	if num == nil {
		return 0
	}
	return new(big.Int).SetBytes(num).Uint64()
}

// WriteSafePointBlockNumber write the number of block that roothash save to disk
func WriteSafePointBlockNumber(db ethdb.KeyValueWriter, number uint64) {
	if err := db.Put(LastSafePointBlockKey, new(big.Int).SetUint64(number).Bytes()); err != nil {
		log.Crit("Failed to store safe point of block number", "err", err)
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
