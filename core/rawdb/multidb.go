package rawdb

import "github.com/ethereum/go-ethereum/ethdb"

type multiDatabase struct {
	ethdb.Database // chainDB is the default database
	indexdb        ethdb.Database
	snapdb         ethdb.Database
	triedb         ethdb.Database
}

func NewMultiDatabase(chaindb ethdb.Database, indexdb ethdb.Database, snapdb ethdb.Database, triedb ethdb.Database) ethdb.Database {
	return &multiDatabase{
		Database: chaindb,
		indexdb:  indexdb,
		snapdb:   snapdb,
		triedb:   triedb,
	}
}

func (db *multiDatabase) MultiDB() bool {
	return true
}

func (db *multiDatabase) ChainDB() ethdb.Database {
	return db
}

func (db *multiDatabase) IndexDB() ethdb.Database {
	return db.indexdb
}

func (db *multiDatabase) SnapDB() ethdb.Database {
	return db.snapdb
}

func (db *multiDatabase) TrieDB() ethdb.Database {
	return db.triedb
}
