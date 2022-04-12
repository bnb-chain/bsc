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

package state

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"
)

var emptyCodeHash = crypto.Keccak256(nil)

type Code []byte

func (c Code) String() string {
	return string(c) //strings.Join(Disassemble(c), " ")
}

type Storage interface {
	String() string
	GetValue(hash common.Hash) (common.Hash, bool)
	StoreValue(hash common.Hash, value common.Hash)
	Length() (length int)
	Copy() Storage
	Range(func(key, value interface{}) bool)
}

type StorageMap map[common.Hash]common.Hash

func (s StorageMap) String() (str string) {
	for key, value := range s {
		str += fmt.Sprintf("%X : %X\n", key, value)
	}

	return
}

func (s StorageMap) Copy() Storage {
	cpy := make(StorageMap)
	for key, value := range s {
		cpy[key] = value
	}

	return cpy
}

func (s StorageMap) GetValue(hash common.Hash) (common.Hash, bool) {
	value, ok := s[hash]
	return value, ok
}

func (s StorageMap) StoreValue(hash common.Hash, value common.Hash) {
	s[hash] = value
}

func (s StorageMap) Length() int {
	return len(s)
}

func (s StorageMap) Range(f func(hash, value interface{}) bool) {
	for k, v := range s {
		result := f(k, v)
		if !result {
			return
		}
	}
}

type StorageSyncMap struct {
	sync.Map
}

func (s *StorageSyncMap) String() (str string) {
	s.Range(func(key, value interface{}) bool {
		str += fmt.Sprintf("%X : %X\n", key, value)
		return true
	})

	return
}

func (s *StorageSyncMap) GetValue(hash common.Hash) (common.Hash, bool) {
	value, ok := s.Load(hash)
	if !ok {
		return common.Hash{}, ok
	}

	return value.(common.Hash), ok
}

func (s *StorageSyncMap) StoreValue(hash common.Hash, value common.Hash) {
	s.Store(hash, value)
}

func (s *StorageSyncMap) Length() (length int) {
	s.Range(func(key, value interface{}) bool {
		length++
		return true
	})
	return length
}

func (s *StorageSyncMap) Copy() Storage {
	cpy := StorageSyncMap{}
	s.Range(func(key, value interface{}) bool {
		cpy.Store(key, value)
		return true
	})

	return &cpy
}

func newStorage(isParallel bool) Storage {
	if isParallel {
		return &StorageSyncMap{}
	}
	return make(StorageMap)
}

// StateObject represents an Ethereum account which is being modified.
//
// The usage pattern is as follows:
// First you need to obtain a state object.
// Account values can be accessed and modified through the object.
// Finally, call CommitTrie to write the modified storage trie into a database.
type StateObject struct {
	address       common.Address
	addrHash      common.Hash // hash of ethereum address of the account
	data          types.StateAccount
	db            *StateDB
	dbItf         StateDBer
	rootCorrected bool // To indicate whether the root has been corrected in pipecommit mode

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by StateDB.Commit.
	dbErr error

	// Write caches.
	trie Trie // storage trie, which becomes non-nil on first access
	code Code // contract bytecode, which gets set when code is loaded

	isParallel          bool      // isParallel indicates this state object is used in parallel mode
	sharedOriginStorage *sync.Map // Storage cache of original entries to dedup rewrites, reset for every transaction
	originStorage       Storage   // Storage cache of original entries to dedup rewrites, reset for every transaction
	pendingStorage      Storage   // Storage entries that need to be flushed to disk, at the end of an entire block
	dirtyStorage        Storage   // Storage entries that have been modified in the current transaction execution
	fakeStorage         Storage   // Fake storage which constructed by caller for debugging purpose.

	// Cache flags.
	// When an object is marked suicided it will be delete from the trie
	// during the "update" phase of the state transition.
	dirtyCode bool // true if the code was updated
	suicided  bool
	deleted   bool

	//encode
	encodeData []byte
}

// empty returns whether the account is considered empty.
func (s *StateObject) empty() bool {
	// return s.data.Nonce == 0 && s.data.Balance.Sign() == 0 && bytes.Equal(s.data.CodeHash, emptyCodeHash)

	// 0426, leave some notation, empty() works so far
	// empty() has 3 use cases:
	// 1.StateDB.Empty(), to empty check
	//   A: It is ok, we have handled it in Empty(), to make sure nonce, balance, codeHash are solid
	// 2:AddBalance 0, empty check for touch event
	//   empty() will add a touch event.
	//   if we misjudge it, the touch event could be lost, which make address not deleted.  // fixme
	// 3.Finalise(), to do empty delete
	//   the address should be dirtied or touched
	//   if it nonce dirtied, it is ok, since nonce is monotonically increasing, won't be zero
	//   if balance is dirtied, balance could be zero, we should refer solid nonce & codeHash  // fixme
	//   if codeHash is dirtied, it is ok, since code will not be updated.
	//   if suicide, it is ok
	//   if object is new created, it is ok
	//   if CreateAccout, recreate the address, it is ok.

	// Slot 0 tx 0: AddBalance(100) to addr_1, => addr_1: balance = 100, nonce = 0, code is empty
	// Slot 1 tx 1: addr_1 Transfer 99.9979 with GasFee 0.0021, => addr_1: balance = 0, nonce = 1, code is empty
	//              notice: balance transfer cost 21,000 gas, with gasPrice = 100Gwei, GasFee will be 0.0021
	// Slot 0 tx 2: add balance 0 to addr_1(empty check for touch event),
	//              the object was lightCopied from tx 0,

	// in parallel mode, we should not check empty by raw nonce, balance, codeHash any more,
	// since it could be invalid.
	// e.g., AddBalance() to an address, we will do lightCopy to get a new StateObject, we did balance fixup to
	// make sure object's Balance is reliable. But we did not fixup nonce or code, we only do nonce or codehash
	// fixup on need, that's when we wanna to update the nonce or codehash.
	// So nonce, blance
	// Before the block is processed, addr_1 account: nonce = 0, emptyCodeHash, balance = 100
	//   Slot 0 tx 0: no access to addr_1
	//   Slot 1 tx 1: sub balance 100, it is empty and deleted
	//   Slot 0 tx 2: GetNonce, lightCopy based on main DB(balance = 100) , not empty
	// return s.db.GetNonce(s.address) == 0 && s.db.GetBalance(s.address).Sign() == 0 && bytes.Equal(s.db.GetCodeHash(s.address).Bytes(), emptyCodeHash)

	if s.dbItf.GetBalance(s.address).Sign() != 0 { // check balance first, since it is most likely not zero
		return false
	}
	if s.dbItf.GetNonce(s.address) != 0 {
		return false
	}
	codeHash := s.dbItf.GetCodeHash(s.address)
	return bytes.Equal(codeHash.Bytes(), emptyCodeHash) // code is empty, the object is empty

}

// newObject creates a state object.
func newObject(dbItf StateDBer, isParallel bool, address common.Address, data types.StateAccount) *StateObject {
	db := dbItf.getBaseStateDB()
	if data.Balance == nil {
		data.Balance = new(big.Int) // todo: why not common.Big0?
	}
	if data.CodeHash == nil {
		data.CodeHash = emptyCodeHash
	}
	if data.Root == (common.Hash{}) {
		data.Root = emptyRoot
	}
	var storageMap *sync.Map
	// Check whether the storage exist in pool, new originStorage if not exist
	if db != nil && db.storagePool != nil {
		storageMap = db.GetStorage(address)
	}

	return &StateObject{
		db:                  db,
		dbItf:               dbItf,
		address:             address,
		addrHash:            crypto.Keccak256Hash(address[:]),
		data:                data,
		isParallel:          isParallel,
		sharedOriginStorage: storageMap,
		originStorage:       newStorage(isParallel),
		dirtyStorage:        newStorage(isParallel),
		pendingStorage:      newStorage(isParallel),
	}
}

// EncodeRLP implements rlp.Encoder.
func (s *StateObject) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, &s.data)
}

// setError remembers the first non-nil error it is called with.
func (s *StateObject) setError(err error) {
	if s.dbErr == nil {
		s.dbErr = err
	}
}

func (s *StateObject) markSuicided() {
	s.suicided = true
}

func (s *StateObject) touch() {
	s.db.journal.append(touchChange{
		account: &s.address,
	})
	if s.address == ripemd {
		// Explicitly put it in the dirty-cache, which is otherwise generated from
		// flattened journals.
		s.db.journal.dirty(s.address)
	}
}

func (s *StateObject) getTrie(db Database) Trie {
	if s.trie == nil {
		// Try fetching from prefetcher first
		// We don't prefetch empty tries
		prefetcher := s.db.prefetcher
		if s.data.Root != emptyRoot && prefetcher != nil {
			// When the miner is creating the pending state, there is no
			// prefetcher
			s.trie = prefetcher.trie(s.data.Root)
		}
		if s.trie == nil {
			var err error
			s.trie, err = db.OpenStorageTrie(s.addrHash, s.data.Root)
			if err != nil {
				s.trie, _ = db.OpenStorageTrie(s.addrHash, common.Hash{})
				s.setError(fmt.Errorf("can't create storage trie: %v", err))
			}
		}
	}
	return s.trie
}

// GetState retrieves a value from the account storage trie.
func (s *StateObject) GetState(db Database, key common.Hash) common.Hash {
	// If the fake storage is set, only lookup the state here(in the debugging mode)
	if s.fakeStorage != nil {
		fakeValue, _ := s.fakeStorage.GetValue(key)
		return fakeValue
	}
	// If we have a dirty value for this state entry, return it
	value, dirty := s.dirtyStorage.GetValue(key)
	if dirty {
		return value
	}
	// Otherwise return the entry's original value
	return s.GetCommittedState(db, key)
}

func (s *StateObject) getOriginStorage(key common.Hash) (common.Hash, bool) {
	if value, cached := s.originStorage.GetValue(key); cached {
		return value, true
	}
	// if L1 cache miss, try to get it from shared pool
	if s.sharedOriginStorage != nil {
		val, ok := s.sharedOriginStorage.Load(key)
		if !ok {
			return common.Hash{}, false
		}
		s.originStorage.StoreValue(key, val.(common.Hash))
		return val.(common.Hash), true
	}
	return common.Hash{}, false
}

func (s *StateObject) setOriginStorage(key common.Hash, value common.Hash) {
	if s.db.writeOnSharedStorage && s.sharedOriginStorage != nil {
		s.sharedOriginStorage.Store(key, value)
	}
	s.originStorage.StoreValue(key, value)
}

// GetCommittedState retrieves a value from the committed account storage trie.
func (s *StateObject) GetCommittedState(db Database, key common.Hash) common.Hash {
	// If the fake storage is set, only lookup the state here(in the debugging mode)
	if s.fakeStorage != nil {
		fakeValue, _ := s.fakeStorage.GetValue(key)
		return fakeValue
	}
	// If we have a pending write or clean cached, return that
	if value, pending := s.pendingStorage.GetValue(key); pending {
		return value
	}

	if value, cached := s.getOriginStorage(key); cached {
		return value
	}
	// If no live objects are available, attempt to use snapshots
	var (
		enc []byte
		err error
	)
	if s.db.snap != nil {
		// If the object was destructed in *this* block (and potentially resurrected),
		// the storage has been cleared out, and we should *not* consult the previous
		// snapshot about any storage values. The only possible alternatives are:
		//   1) resurrect happened, and new slot values were set -- those should
		//      have been handles via pendingStorage above.
		//   2) we don't have new values, and can deliver empty response back
		s.db.snapParallelLock.RLock()
		if _, destructed := s.db.snapDestructs[s.address]; destructed { // fixme: use sync.Map, instead of RWMutex?
			s.db.snapParallelLock.RUnlock()
			return common.Hash{}
		}
		start := time.Now()
		s.db.snapParallelLock.RUnlock()
		enc, err = s.db.snap.Storage(s.addrHash, crypto.Keccak256Hash(key.Bytes()))
		if metrics.EnabledExpensive {
			s.db.SnapshotStorageReads += time.Since(start)
		}
	}

	// If snapshot unavailable or reading from it failed, load from the database
	if s.db.snap == nil || err != nil {
		start := time.Now()
		//		if metrics.EnabledExpensive {
		//			meter = &s.db.StorageReads
		//		}
		enc, err = s.getTrie(db).TryGet(key.Bytes())
		if metrics.EnabledExpensive {
			s.db.StorageReads += time.Since(start)
		}
		if err != nil {
			s.setError(err)
			return common.Hash{}
		}
	}
	var value common.Hash
	if len(enc) > 0 {
		_, content, _, err := rlp.Split(enc)
		if err != nil {
			s.setError(err)
		}
		value.SetBytes(content)
	}
	s.setOriginStorage(key, value)
	return value
}

// SetState updates a value in account storage.
func (s *StateObject) SetState(db Database, key, value common.Hash) {
	// If the fake storage is set, put the temporary state update here.
	if s.fakeStorage != nil {
		s.fakeStorage.StoreValue(key, value)
		return
	}
	// If the new value is the same as old, don't set
	// In parallel mode, it has to get from StateDB, in case:
	//  a.the Slot did not set the key before and try to set it to `val_1`
	//  b.Unconfirmed DB has set the key to `val_2`
	//  c.if we use StateObject.GetState, and the key load from the main DB is `val_1`
	//    this `SetState could be skipped`
	//  d.Finally, the key's value will be `val_2`, while it should be `val_1`
	// such as: https://bscscan.com/txs?block=2491181
	prev := s.dbItf.GetState(s.address, key) // fixme: if it is for journal, may not necessary, we can remove this change record
	if prev == value {
		return
	}
	// New value is different, update and journal the change
	s.db.journal.append(storageChange{
		account:  &s.address,
		key:      key,
		prevalue: prev,
	})
	if s.db.parallel.isSlotDB {
		s.db.parallel.kvChangesInSlot[s.address][key] = struct{}{} // should be moved to here, after `s.db.GetState()`
	}

	s.setState(key, value)
}

// SetStorage replaces the entire state storage with the given one.
//
// After this function is called, all original state will be ignored and state
// lookup only happens in the fake state storage.
//
// Note this function should only be used for debugging purpose.
func (s *StateObject) SetStorage(storage map[common.Hash]common.Hash) {
	// Allocate fake storage if it's nil.
	if s.fakeStorage == nil {
		s.fakeStorage = newStorage(s.isParallel)
	}
	for key, value := range storage {
		s.fakeStorage.StoreValue(key, value)
	}
	// Don't bother journal since this function should only be used for
	// debugging and the `fake` storage won't be committed to database.
}

func (s *StateObject) setState(key, value common.Hash) {
	s.dirtyStorage.StoreValue(key, value)
}

// finalise moves all dirty storage slots into the pending area to be hashed or
// committed later. It is invoked at the end of every transaction.
func (s *StateObject) finalise(prefetch bool) {
	slotsToPrefetch := make([][]byte, 0, s.dirtyStorage.Length())
	s.dirtyStorage.Range(func(key, value interface{}) bool {
		s.pendingStorage.StoreValue(key.(common.Hash), value.(common.Hash))

		originalValue, _ := s.originStorage.GetValue(key.(common.Hash))
		if value.(common.Hash) != originalValue {
			originalKey := key.(common.Hash)
			slotsToPrefetch = append(slotsToPrefetch, common.CopyBytes(originalKey[:])) // Copy needed for closure
		}
		return true
	})

	prefetcher := s.db.prefetcher
	if prefetcher != nil && prefetch && len(slotsToPrefetch) > 0 && s.data.Root != emptyRoot {
		prefetcher.prefetch(s.data.Root, slotsToPrefetch, s.addrHash)
	}
	if s.dirtyStorage.Length() > 0 {
		s.dirtyStorage = newStorage(s.isParallel)
	}
}

// updateTrie writes cached storage modifications into the object's storage trie.
// It will return nil if the trie has not been loaded and no changes have been made
func (s *StateObject) updateTrie(db Database) Trie {
	// Make sure all dirty slots are finalized into the pending storage area
	s.finalise(false) // Don't prefetch anymore, pull directly if need be
	if s.pendingStorage.Length() == 0 {
		return s.trie
	}
	// Track the amount of time wasted on updating the storage trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) {
			s.db.MetricsMux.Lock()
			s.db.StorageUpdates += time.Since(start)
			s.db.MetricsMux.Unlock()
		}(time.Now())
	}
	// Insert all the pending updates into the trie
	tr := s.getTrie(db)

	dirtyStorage := make(map[common.Hash][]byte)
	usedStorage := make([][]byte, 0, s.pendingStorage.Length())
	s.pendingStorage.Range(func(keyItf, valueItf interface{}) bool {
		key := keyItf.(common.Hash)
		value := valueItf.(common.Hash)
		// Skip noop changes, persist actual changes
		originalValue, _ := s.originStorage.GetValue(key)
		if value == originalValue {
			return true
		}

		s.setOriginStorage(key, value)

		var vs []byte
		if (value == common.Hash{}) {
			s.setError(tr.TryDelete(key[:]))
		} else {
			// Encoding []byte cannot fail, ok to ignore the error.
			vs, _ = rlp.EncodeToBytes(common.TrimLeftZeroes(value[:]))
			s.setError(tr.TryUpdate(key[:], vs))
		}
		dirtyStorage[key] = vs
		return true
	})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for key, value := range dirtyStorage {
			if len(value) == 0 {
				s.setError(tr.TryDelete(key[:]))
			} else {
				s.setError(tr.TryUpdate(key[:], value))
			}
			usedStorage = append(usedStorage, common.CopyBytes(key[:])) // Copy needed for closure
		}
	}()
	if s.db.snap != nil {
		// If state snapshotting is active, cache the data til commit
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.db.snapStorageMux.Lock()
			// The snapshot storage map for the object
			storage := s.db.snapStorage[s.address]
			if storage == nil {
				storage = make(map[string][]byte, len(dirtyStorage))
				s.db.snapStorage[s.address] = storage
			}
			s.db.snapStorageMux.Unlock()
			for key, value := range dirtyStorage {
				storage[string(key[:])] = value
			}
		}()
	}
	wg.Wait()

	prefetcher := s.db.prefetcher
	if prefetcher != nil {
		prefetcher.used(s.data.Root, usedStorage)
	}
	if s.pendingStorage.Length() > 0 {
		s.pendingStorage = newStorage(s.isParallel)
	}
	return tr
}

// UpdateRoot sets the trie root to the current root hash of
func (s *StateObject) updateRoot(db Database) {
	// If node runs in no trie mode, set root to empty.
	defer func() {
		if db.NoTries() {
			s.data.Root = common.Hash{}
		}
	}()

	// If nothing changed, don't bother with hashing anything
	if s.updateTrie(db) == nil {
		return
	}
	// Track the amount of time wasted on hashing the storage trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) {
			s.db.MetricsMux.Lock()
			s.db.StorageHashes += time.Since(start)
			s.db.MetricsMux.Unlock()
		}(time.Now())
	}
	s.data.Root = s.trie.Hash()
}

// CommitTrie the storage trie of the object to db.
// This updates the trie root.
func (s *StateObject) CommitTrie(db Database) (int, error) {
	// If nothing changed, don't bother with hashing anything
	if s.updateTrie(db) == nil {
		if s.trie != nil && s.data.Root != emptyRoot {
			db.CacheStorage(s.addrHash, s.data.Root, s.trie)
		}
		return 0, nil
	}
	if s.dbErr != nil {
		return 0, s.dbErr
	}
	// Track the amount of time wasted on committing the storage trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.db.StorageCommits += time.Since(start) }(time.Now())
	}
	root, committed, err := s.trie.Commit(nil)
	if err == nil {
		s.data.Root = root
	}
	if s.data.Root != emptyRoot {
		db.CacheStorage(s.addrHash, s.data.Root, s.trie)
	}
	return committed, err
}

// AddBalance adds amount to s's balance.
// It is used to add funds to the destination account of a transfer.
func (s *StateObject) AddBalance(amount *big.Int) {
	// EIP161: We must check emptiness for the objects such that the account
	// clearing (0,0,0 objects) can take effect.
	if amount.Sign() == 0 {
		if s.empty() {
			s.touch()
		}
		return
	}
	s.SetBalance(new(big.Int).Add(s.Balance(), amount))
}

// SubBalance removes amount from s's balance.
// It is used to remove funds from the origin account of a transfer.
func (s *StateObject) SubBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	s.SetBalance(new(big.Int).Sub(s.Balance(), amount))
}

func (s *StateObject) SetBalance(amount *big.Int) {
	s.db.journal.append(balanceChange{
		account: &s.address,
		prev:    new(big.Int).Set(s.data.Balance), // prevBalance,
		// prev:    prevBalance,
	})
	s.setBalance(amount)
}

func (s *StateObject) setBalance(amount *big.Int) {
	s.data.Balance = amount
}

// Return the gas back to the origin. Used by the Virtual machine or Closures
func (s *StateObject) ReturnGas(gas *big.Int) {}

func (s *StateObject) lightCopy(db *ParallelStateDB) *StateObject {
	stateObject := newObject(db, s.isParallel, s.address, s.data)
	if s.trie != nil {
		// fixme: no need to copy trie for light copy, since light copied object won't access trie DB
		stateObject.trie = db.db.CopyTrie(s.trie)
	}
	stateObject.code = s.code
	stateObject.suicided = false        // should be false
	stateObject.dirtyCode = s.dirtyCode // it is not used in slot, but keep it is ok
	stateObject.deleted = false         // should be false
	return stateObject
}

func (s *StateObject) deepCopy(db *StateDB) *StateObject {
	stateObject := newObject(db, s.isParallel, s.address, s.data)
	if s.trie != nil {
		stateObject.trie = db.db.CopyTrie(s.trie)
	}
	stateObject.code = s.code
	stateObject.dirtyStorage = s.dirtyStorage.Copy()
	stateObject.originStorage = s.originStorage.Copy()
	stateObject.pendingStorage = s.pendingStorage.Copy()
	stateObject.suicided = s.suicided
	stateObject.dirtyCode = s.dirtyCode
	stateObject.deleted = s.deleted
	return stateObject
}

func (s *StateObject) MergeSlotObject(db Database, dirtyObjs *StateObject, keys StateKeys) {
	for key := range keys {
		// In parallel mode, always GetState by StateDB, not by StateObject directly,
		// since it the KV could exist in unconfirmed DB.
		// But here, it should be ok, since the KV should be changed and valid in the SlotDB,
		// s.SetState(db, key, dirtyObjs.GetState(db, key))
		s.setState(key, dirtyObjs.GetState(db, key))
	}
}

//
// Attribute accessors
//

// Returns the address of the contract/account
func (s *StateObject) Address() common.Address {
	return s.address
}

// Code returns the contract code associated with this object, if any.
func (s *StateObject) Code(db Database) []byte {
	if s.code != nil {
		return s.code
	}
	if bytes.Equal(s.CodeHash(), emptyCodeHash) {
		return nil
	}
	code, err := db.ContractCode(s.addrHash, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.setError(fmt.Errorf("can't load code hash %x: %v", s.CodeHash(), err))
	}
	s.code = code
	return code
}

// CodeSize returns the size of the contract code associated with this object,
// or zero if none. This method is an almost mirror of Code, but uses a cache
// inside the database to avoid loading codes seen recently.
func (s *StateObject) CodeSize(db Database) int {
	if s.code != nil {
		return len(s.code)
	}
	if bytes.Equal(s.CodeHash(), emptyCodeHash) {
		return 0
	}
	size, err := db.ContractCodeSize(s.addrHash, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.setError(fmt.Errorf("can't load code size %x: %v", s.CodeHash(), err))
	}
	return size
}

func (s *StateObject) SetCode(codeHash common.Hash, code []byte) {
	prevcode := s.dbItf.GetCode(s.address)
	s.db.journal.append(codeChange{
		account:  &s.address,
		prevhash: s.CodeHash(),
		prevcode: prevcode,
	})
	s.setCode(codeHash, code)
}

func (s *StateObject) setCode(codeHash common.Hash, code []byte) {
	s.code = code
	s.data.CodeHash = codeHash[:]
	s.dirtyCode = true
}

func (s *StateObject) SetNonce(nonce uint64) {
	prevNonce := s.dbItf.GetNonce(s.address)
	s.db.journal.append(nonceChange{
		account: &s.address,
		prev:    prevNonce,
	})
	s.setNonce(nonce)
}

func (s *StateObject) setNonce(nonce uint64) {
	s.data.Nonce = nonce
}

func (s *StateObject) CodeHash() []byte {
	return s.data.CodeHash
}

func (s *StateObject) Balance() *big.Int {
	return s.data.Balance
}

func (s *StateObject) Nonce() uint64 {
	return s.data.Nonce
}

// Never called, but must be present to allow StateObject to be used
// as a vm.Account interface that also satisfies the vm.ContractRef
// interface. Interfaces are awesome.
func (s *StateObject) Value() *big.Int {
	panic("Value on StateObject should never be called")
}
