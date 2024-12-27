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
