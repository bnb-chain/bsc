// Copyright 2017 The go-ethereum Authors
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
	"errors"
	"fmt"

	versa "github.com/bnb-chain/versioned-state-database"
	"github.com/crate-crypto/go-ipa/banderwagon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/ethereum/go-ethereum/triedb"
)

const (
	// Number of codehash->size associations to keep.
	codeSizeCacheSize = 100000

	// Cache size granted for caching clean code.
	codeCacheSize = 64 * 1024 * 1024

	// commitmentSize is the size of commitment stored in cache.
	commitmentSize = banderwagon.UncompressedSize

	// Cache item granted for caching commitment results.
	commitmentCacheItems = 64 * 1024 * 1024 / (commitmentSize + common.AddressLength)
)

// Database wraps access to tries and contract code.
type Database interface {
	// OpenTrie opens the main account trie.
	OpenTrie(root common.Hash) (Trie, error)

	// OpenStorageTrie opens the storage trie of an account.
	OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, trie Trie) (Trie, error)

	// CopyTrie returns an independent copy of the given trie.
	CopyTrie(Trie) Trie

	// ContractCode retrieves a particular contract's code.
	ContractCode(addr common.Address, codeHash common.Hash) ([]byte, error)

	// ContractCodeSize retrieves a particular contracts code's size.
	ContractCodeSize(addr common.Address, codeHash common.Hash) (int, error)

	// DiskDB returns the underlying key-value disk database.
	DiskDB() ethdb.KeyValueStore

	// TrieDB returns the underlying trie database for managing trie nodes.
	TrieDB() *triedb.Database

	// Scheme returns triedb scheme, used to distinguish version triedb.
	Scheme() string

	// Flush used for version caching versa db to commit block state data.
	Flush() error

	// Release used for caching versa db to release resource.
	Release() error

	// Reset used for caching versa db to clean up meta data.
	Reset()

	// Copy used for caching versa db to copy db, main to transfer triedb with rw mode.
	Copy() Database

	// HasState returns the state data whether in the triedb.
	HasState(root common.Hash) bool

	// HasTreeExpired used for caching versa db, whether the state where the opened tree resides has been closed
	HasTreeExpired(tr Trie) bool

	// NoTries returns whether the database has tries storage.
	NoTries() bool

	SetVersion(version int64)

	GetVersion() int64
}

// Trie is a Ethereum Merkle Patricia trie.
type Trie interface {
	// GetKey returns the sha3 preimage of a hashed key that was previously used
	// to store a value.
	//
	// TODO(fjl): remove this when StateTrie is removed
	GetKey([]byte) []byte

	// GetAccount abstracts an account read from the trie. It retrieves the
	// account blob from the trie with provided account address and decodes it
	// with associated decoding algorithm. If the specified account is not in
	// the trie, nil will be returned. If the trie is corrupted(e.g. some nodes
	// are missing or the account blob is incorrect for decoding), an error will
	// be returned.
	GetAccount(address common.Address) (*types.StateAccount, error)

	// GetStorage returns the value for key stored in the trie. The value bytes
	// must not be modified by the caller. If a node was not found in the database,
	// a trie.MissingNodeError is returned.
	GetStorage(addr common.Address, key []byte) ([]byte, error)

	// UpdateAccount abstracts an account write to the trie. It encodes the
	// provided account object with associated algorithm and then updates it
	// in the trie with provided address.
	UpdateAccount(address common.Address, account *types.StateAccount) error

	// UpdateStorage associates key with value in the trie. If value has length zero,
	// any existing value is deleted from the trie. The value bytes must not be modified
	// by the caller while they are stored in the trie. If a node was not found in the
	// database, a trie.MissingNodeError is returned.
	UpdateStorage(addr common.Address, key, value []byte) error

	// DeleteAccount abstracts an account deletion from the trie.
	DeleteAccount(address common.Address) error

	// DeleteStorage removes any existing value for key from the trie. If a node
	// was not found in the database, a trie.MissingNodeError is returned.
	DeleteStorage(addr common.Address, key []byte) error

	// UpdateContractCode abstracts code write to the trie. It is expected
	// to be moved to the stateWriter interface when the latter is ready.
	UpdateContractCode(address common.Address, codeHash common.Hash, code []byte) error

	// Hash returns the root hash of the trie. It does not write to the database and
	// can be used even if the trie doesn't have one.
	Hash() common.Hash

	// Commit collects all dirty nodes in the trie and replace them with the
	// corresponding node hash. All collected nodes(including dirty leaves if
	// collectLeaf is true) will be encapsulated into a nodeset for return.
	// The returned nodeset can be nil if the trie is clean(nothing to commit).
	// Once the trie is committed, it's not usable anymore. A new trie must
	// be created with new root and updated trie database for following usage
	Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error)

	// NodeIterator returns an iterator that returns nodes of the trie. Iteration
	// starts at the key after the given start key. And error will be returned
	// if fails to create node iterator.
	NodeIterator(startKey []byte) (trie.NodeIterator, error)

	// Prove constructs a Merkle proof for key. The result contains all encoded nodes
	// on the path to the value at key. The value itself is also included in the last
	// node and can be retrieved by verifying the proof.
	//
	// If the trie does not contain a value for key, the returned proof contains all
	// nodes of the longest existing prefix of the key (at least the root), ending
	// with the node that proves the absence of the key.
	Prove(key []byte, proofDb ethdb.KeyValueWriter) error
}

// NewDatabase creates a backing store for state. The returned database is safe for
// concurrent use, but does not retain any recent trie nodes in memory. To keep some
// historical state in memory, use the NewDatabaseWithConfig constructor.
func NewDatabase(db ethdb.Database) Database {
	return NewDatabaseWithConfig(db, nil, false)
}

// NewDatabaseWithConfig creates a backing store for state. The returned database
// is safe for concurrent use and retains a lot of collapsed RLP trie nodes in a
// large memory cache.
func NewDatabaseWithConfig(db ethdb.Database, config *triedb.Config, needCommit bool) Database {
	noTries := config != nil && config.NoTries

	triedb := triedb.NewDatabase(db, config)
	if triedb.Scheme() == rawdb.VersionScheme {
		if needCommit {
			return NewVersaDatabase(db, triedb, versa.S_COMMIT)
		}
		return NewVersaDatabase(db, triedb, versa.S_RW)
	}

	return &cachingDB{
		disk:          db,
		codeSizeCache: lru.NewCache[common.Hash, int](codeSizeCacheSize),
		codeCache:     lru.NewSizeConstrainedCache[common.Hash, []byte](codeCacheSize),
		triedb:        triedb,
		noTries:       noTries,
	}
}

// NewDatabaseWithNodeDB creates a state database with an already initialized node database.
func NewDatabaseWithNodeDB(db ethdb.Database, triedb *triedb.Database, needCommit bool) Database {
	noTries := triedb != nil && triedb.Config() != nil && triedb.Config().NoTries

	if triedb.Scheme() == rawdb.VersionScheme {
		if needCommit {
			return NewVersaDatabase(db, triedb, versa.S_COMMIT)
		}
		return NewVersaDatabase(db, triedb, versa.S_RW)
	}

	return &cachingDB{
		disk:          db,
		codeSizeCache: lru.NewCache[common.Hash, int](codeSizeCacheSize),
		codeCache:     lru.NewSizeConstrainedCache[common.Hash, []byte](codeCacheSize),
		triedb:        triedb,
		noTries:       noTries,
	}
}

type cachingDB struct {
	disk          ethdb.KeyValueStore
	codeSizeCache *lru.Cache[common.Hash, int]
	codeCache     *lru.SizeConstrainedCache[common.Hash, []byte]
	triedb        *triedb.Database
	noTries       bool

	//debug *DebugHashState
}

// OpenTrie opens the main account trie at a specific root hash.
func (db *cachingDB) OpenTrie(root common.Hash) (Trie, error) {
	if db.noTries {
		return trie.NewEmptyTrie(), nil
	}
	if db.triedb.IsVerkle() {
		return trie.NewVerkleTrie(root, db.triedb, utils.NewPointCache(commitmentCacheItems))
	}
	tr, err := trie.NewStateTrie(trie.StateTrieID(root), db.triedb)
	if err != nil {
		//if db.debug != nil {
		//	db.debug.OnError(fmt.Errorf("failed to open tree, root: %s, error: %s", root.String(), err.Error()))
		//}
		return nil, err
	}
	//ht := &HashTrie{
	//	trie:    tr,
	//	root:    root,
	//	address: common.Address{},
	//	owner:   common.Hash{},
	//	debug:   db.debug,
	//}
	//if db.debug != nil {
	//	db.debug.OnOpenTree(root, common.Hash{}, common.Address{})
	//}
	//return ht, nil
	return tr, nil
}

// OpenStorageTrie opens the storage trie of an account.
func (db *cachingDB) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, self Trie) (Trie, error) {
	if db.noTries {
		return trie.NewEmptyTrie(), nil
	}

	// In the verkle case, there is only one tree. But the two-tree structure
	// is hardcoded in the codebase. So we need to return the same trie in this
	// case.
	if db.triedb.IsVerkle() {
		return self, nil
	}
	owner := crypto.Keccak256Hash(address.Bytes())
	tr, err := trie.NewStateTrie(trie.StorageTrieID(stateRoot, owner, root), db.triedb)
	if err != nil {
		//if db.debug != nil {
		//	db.debug.OnError(fmt.Errorf("failed to storage open tree, stateRoot: %s, address: %s, root: %s, error: %s",
		//		stateRoot.String(), address.String(), root.String(), err.Error()))
		//}
		return nil, err
	}
	//ht := &HashTrie{
	//	trie:     tr,
	//	root:     stateRoot,
	//	statRoot: root,
	//	address:  address,
	//	owner:    owner,
	//	debug:    db.debug,
	//}
	//if db.debug != nil {
	//	db.debug.OnOpenTree(root, owner, address)
	//}
	//return ht, nil
	return tr, nil
}

func (db *cachingDB) NoTries() bool {
	return db.noTries
}

// CopyTrie returns an independent copy of the given trie.
func (db *cachingDB) CopyTrie(t Trie) Trie {
	if t == nil {
		return nil
	}
	switch t := t.(type) {
	case *trie.StateTrie:
		return t.Copy()
	case *trie.EmptyTrie:
		return t.Copy()
	//case *HashTrie:
	//	return db.CopyTrie(t.trie)
	default:
		panic(fmt.Errorf("unknown trie type %T", t))
	}
}

// ContractCode retrieves a particular contract's code.
func (db *cachingDB) ContractCode(address common.Address, codeHash common.Hash) ([]byte, error) {
	//if db.debug != nil {
	//	db.debug.OnGetCode(address, codeHash)
	//}
	code, _ := db.codeCache.Get(codeHash)
	if len(code) > 0 {
		return code, nil
	}
	code = rawdb.ReadCode(db.disk, codeHash)
	if len(code) > 0 {
		db.codeCache.Add(codeHash, code)
		db.codeSizeCache.Add(codeHash, len(code))
		return code, nil
	}
	return nil, errors.New("not found")
}

// ContractCodeWithPrefix retrieves a particular contract's code. If the
// code can't be found in the cache, then check the existence with **new**
// db scheme.
func (db *cachingDB) ContractCodeWithPrefix(address common.Address, codeHash common.Hash) ([]byte, error) {
	code, _ := db.codeCache.Get(codeHash)
	if len(code) > 0 {
		return code, nil
	}
	code = rawdb.ReadCodeWithPrefix(db.disk, codeHash)
	if len(code) > 0 {
		db.codeCache.Add(codeHash, code)
		db.codeSizeCache.Add(codeHash, len(code))
		return code, nil
	}
	return nil, errors.New("not found")
}

// ContractCodeSize retrieves a particular contracts code's size.
func (db *cachingDB) ContractCodeSize(addr common.Address, codeHash common.Hash) (int, error) {
	if cached, ok := db.codeSizeCache.Get(codeHash); ok {
		return cached, nil
	}
	code, err := db.ContractCode(addr, codeHash)
	return len(code), err
}

// DiskDB returns the underlying key-value disk database.
func (db *cachingDB) DiskDB() ethdb.KeyValueStore {
	return db.disk
}

// TrieDB retrieves any intermediate trie-node caching layer.
func (db *cachingDB) TrieDB() *triedb.Database {
	return db.triedb
}

func (db *cachingDB) Reset() {
	return
}

func (db *cachingDB) Scheme() string {
	return db.triedb.Scheme()
}

func (db *cachingDB) Flush() error {
	return nil
}

func (db *cachingDB) Release() error {
	//db.debug.flush()
	//db.debug = nil
	return nil
}

func (db *cachingDB) SetVersion(version int64) {
	//db.debug = NewDebugHashState(db.disk)
	//db.debug.Version = version
}

func (db *cachingDB) GetVersion() int64 {
	//return db.debug.Version
	return 0
}

func (db *cachingDB) Copy() Database {
	return db
}

func (db *cachingDB) HasState(root common.Hash) bool {
	_, err := db.OpenTrie(root)
	return err == nil
}

func (db *cachingDB) HasTreeExpired(_ Trie) bool {
	return false
}

//type HashTrie struct {
//	trie     Trie
//	root     common.Hash
//	statRoot common.Hash
//	address  common.Address
//	owner    common.Hash
//
//	debug *DebugHashState
//}
//
//func (ht *HashTrie) GetKey(key []byte) []byte {
//	return ht.trie.GetKey(key)
//}
//
//func (ht *HashTrie) GetAccount(address common.Address) (*types.StateAccount, error) {
//	acc, err := ht.trie.GetAccount(address)
//	if err != nil {
//		if ht.debug != nil {
//			ht.debug.OnError(fmt.Errorf("failed to get account, address: %s, error: %s", address.String(), err.Error()))
//		}
//		return nil, err
//	}
//	if ht.debug != nil {
//		ht.debug.OnGetAccount(address, acc)
//	}
//	return acc, nil
//}
//
//func (ht *HashTrie) GetStorage(addr common.Address, key []byte) ([]byte, error) {
//	val, err := ht.trie.GetStorage(addr, key)
//	if err != nil {
//		if ht.debug != nil {
//			ht.debug.OnError(fmt.Errorf("failed to get storage, address: %s, error: %s", addr.String(), err.Error()))
//		}
//		return val, err
//	}
//	if ht.debug != nil {
//		ht.debug.OnGetStorage(addr, key, val)
//	}
//	return val, err
//}
//
//func (ht *HashTrie) UpdateAccount(address common.Address, account *types.StateAccount) error {
//	err := ht.trie.UpdateAccount(address, account)
//	if err != nil {
//		if ht.debug != nil {
//			ht.debug.OnError(fmt.Errorf("failed to update account, address: %s, account: %s, error: %s",
//				address.String(), account.String(), err.Error()))
//		}
//		return err
//	}
//	if ht.debug != nil {
//		ht.debug.OnUpdateAccount(address, account)
//	}
//	return nil
//}
//
//func (ht *HashTrie) UpdateStorage(addr common.Address, key, value []byte) error {
//	err := ht.trie.UpdateStorage(addr, key, value)
//	if err != nil {
//		if ht.debug != nil {
//			ht.debug.OnError(fmt.Errorf("failed to update storage, address: %s, key: %s, val: %s, error: %s",
//				addr.String(), common.Bytes2Hex(key), common.Bytes2Hex(value), err.Error()))
//		}
//		return err
//	}
//	if ht.debug != nil {
//		ht.debug.OnUpdateStorage(addr, key, value)
//	}
//	return nil
//}
//
//func (ht *HashTrie) DeleteAccount(address common.Address) error {
//	err := ht.trie.DeleteAccount(address)
//	if err != nil {
//		if ht.debug != nil {
//			ht.debug.OnError(fmt.Errorf("failed to delete account, address: %s, error: %s", address.String(), err.Error()))
//		}
//		return err
//	}
//	if ht.debug != nil {
//		ht.debug.OnDeleteAccount(address)
//	}
//	return nil
//}
//
//func (ht *HashTrie) DeleteStorage(addr common.Address, key []byte) error {
//	err := ht.trie.DeleteStorage(addr, key)
//	if err != nil {
//		if ht.debug != nil {
//			ht.debug.OnError(fmt.Errorf("failed to update storage, address: %s, key: %s, error: %s",
//				addr.String(), common.Bytes2Hex(key), err.Error()))
//		}
//		return err
//	}
//	if ht.debug != nil {
//		ht.debug.OnDeleteStorage(addr, key)
//	}
//	return nil
//}
//
//func (ht *HashTrie) UpdateContractCode(address common.Address, codeHash common.Hash, code []byte) error {
//	return ht.trie.UpdateContractCode(address, codeHash, code)
//}
//
//func (ht *HashTrie) Hash() common.Hash {
//	root := ht.trie.Hash()
//	if ht.debug != nil {
//		ht.debug.OnCalcHash(ht.address, root)
//	}
//	return root
//}
//
//func (ht *HashTrie) Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet, error) {
//	hash, set, err := ht.trie.Commit(collectLeaf)
//	if err != nil {
//		ht.debug.OnError(fmt.Errorf("failed to commit tree, address: %s, error: %s",
//			ht.address.String(), err.Error()))
//		return hash, set, err
//	}
//	if ht.debug != nil {
//		ht.debug.OnCalcHash(ht.address, hash)
//		ht.debug.OnCommitTree(ht.address, hash)
//	}
//	return hash, set, nil
//}
//
//func (ht *HashTrie) NodeIterator(startKey []byte) (trie.NodeIterator, error) {
//	return ht.trie.NodeIterator(startKey)
//}
//
//func (ht *HashTrie) Prove(key []byte, proofDb ethdb.KeyValueWriter) error {
//	return ht.trie.Prove(key, proofDb)
//}
