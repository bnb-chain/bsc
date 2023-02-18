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

// Package state provides a caching layer atop the Ethereum state trie.
package state

import (
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

const defaultNumOfSlots = 100

type revision struct {
	id           int
	journalIndex int
}

var (
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	emptyAddr = crypto.Keccak256Hash(common.Address{}.Bytes())
)

type proofList [][]byte

func (n *proofList) Put(key []byte, value []byte) error {
	*n = append(*n, value)
	return nil
}

func (n *proofList) Delete(key []byte) error {
	panic("not supported")
}

// StateDB structs within the ethereum protocol are used to store anything
// within the merkle trie. StateDBs take care of caching and storing
// nested states. It's the general query interface to retrieve:
// * Contracts
// * Accounts
type StateDB struct {
	db             Database
	prefetcherLock sync.Mutex
	prefetcher     *triePrefetcher
	originalRoot   common.Hash // The pre-state root, before any changes were made
	expectedRoot   common.Hash // The state root in the block header
	stateRoot      common.Hash // The calculation result of IntermediateRoot

	trie           Trie
	noTrie         bool
	hasher         crypto.KeccakState
	diffLayer      *types.DiffLayer
	diffTries      map[common.Address]Trie
	diffCode       map[common.Hash][]byte
	lightProcessed bool
	fullProcessed  bool
	pipeCommit     bool

	snaps          *snapshot.Tree
	snap           snapshot.Snapshot
	snapAccountMux sync.Mutex // Mutex for snap account access
	snapStorageMux sync.Mutex // Mutex for snap storage access
	snapDestructs  map[common.Address]struct{}
	snapAccounts   map[common.Address][]byte
	snapStorage    map[common.Address]map[string][]byte

	// This map holds 'live' objects, which will get modified while processing a state transition.
	stateObjects        map[common.Address]*StateObject
	stateObjectsPending map[common.Address]struct{} // State objects finalized but not yet written to the trie
	stateObjectsDirty   map[common.Address]struct{} // State objects modified in the current execution

	storagePool          *StoragePool // sharedPool to store L1 originStorage of stateObjects
	writeOnSharedStorage bool         // Write to the shared origin storage of a stateObject while reading from the underlying storage layer.
	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by StateDB.Commit.
	dbErr error

	// The refund counter, also used by state transitioning.
	refund uint64

	thash   common.Hash
	txIndex int
	logs    map[common.Hash][]*types.Log
	logSize uint

	preimages map[common.Hash][]byte

	// Per-transaction access list
	accessList *accessList

	// Journal of state modifications. This is the backbone of
	// Snapshot and RevertToSnapshot.
	journal        *journal
	validRevisions []revision
	nextRevisionId int

	// Measurements gathered during execution for debugging purposes
	MetricsMux           sync.Mutex
	AccountReads         time.Duration
	AccountHashes        time.Duration
	AccountUpdates       time.Duration
	AccountCommits       time.Duration
	StorageReads         time.Duration
	StorageHashes        time.Duration
	StorageUpdates       time.Duration
	StorageCommits       time.Duration
	SnapshotAccountReads time.Duration
	SnapshotStorageReads time.Duration
	SnapshotCommits      time.Duration

	AccountUpdated int
	StorageUpdated int
	AccountDeleted int
	StorageDeleted int
}

// New creates a new state from a given trie.
func New(root common.Hash, db Database, snaps *snapshot.Tree) (*StateDB, error) {
	return newStateDB(root, db, snaps)
}

// NewWithSharedPool creates a new state with sharedStorge on layer 1.5
func NewWithSharedPool(root common.Hash, db Database, snaps *snapshot.Tree) (*StateDB, error) {
	statedb, err := newStateDB(root, db, snaps)
	if err != nil {
		return nil, err
	}
	statedb.storagePool = NewStoragePool()
	return statedb, nil
}

func newStateDB(root common.Hash, db Database, snaps *snapshot.Tree) (*StateDB, error) {
	sdb := &StateDB{
		db:                  db,
		originalRoot:        root,
		snaps:               snaps,
		stateObjects:        make(map[common.Address]*StateObject, defaultNumOfSlots),
		stateObjectsPending: make(map[common.Address]struct{}, defaultNumOfSlots),
		stateObjectsDirty:   make(map[common.Address]struct{}, defaultNumOfSlots),
		logs:                make(map[common.Hash][]*types.Log, defaultNumOfSlots),
		preimages:           make(map[common.Hash][]byte),
		journal:             newJournal(),
		hasher:              crypto.NewKeccakState(),
	}

	if sdb.snaps != nil {
		if sdb.snap = sdb.snaps.Snapshot(root); sdb.snap != nil {
			sdb.snapDestructs = make(map[common.Address]struct{})
			sdb.snapAccounts = make(map[common.Address][]byte)
			sdb.snapStorage = make(map[common.Address]map[string][]byte)
		}
	}

	snapVerified := sdb.snap != nil && sdb.snap.Verified()
	tr, err := db.OpenTrie(root)
	// return error when 1. failed to open trie and 2. the snap is nil or the snap is not nil and done verification
	if err != nil && (sdb.snap == nil || snapVerified) {
		return nil, err
	}
	_, sdb.noTrie = tr.(*trie.EmptyTrie)
	sdb.trie = tr
	return sdb, nil
}

func (s *StateDB) EnableWriteOnSharedStorage() {
	s.writeOnSharedStorage = true
}

// In mining mode, we will try multi-fillTransactions to get the most profitable one.
// StateDB will be created for each fillTransactions with same block height.
// Share a single triePrefetcher to avoid too much prefetch routines.
func (s *StateDB) TransferPrefetcher(prev *StateDB) {
	if prev == nil {
		return
	}
	var fetcher *triePrefetcher

	prev.prefetcherLock.Lock()
	fetcher = prev.prefetcher
	prev.prefetcher = nil
	prev.prefetcherLock.Unlock()

	s.prefetcherLock.Lock()
	s.prefetcher = fetcher
	s.prefetcherLock.Unlock()
}

// StartPrefetcher initializes a new trie prefetcher to pull in nodes from the
// state trie concurrently while the state is mutated so that when we reach the
// commit phase, most of the needed data is already hot.
func (s *StateDB) StartPrefetcher(namespace string) {
	if s.noTrie {
		return
	}
	s.prefetcherLock.Lock()
	defer s.prefetcherLock.Unlock()
	if s.prefetcher != nil {
		s.prefetcher.close()
		s.prefetcher = nil
	}
	if s.snap != nil {
		parent := s.snap.Parent()
		if parent != nil {
			s.prefetcher = newTriePrefetcher(s.db, s.originalRoot, parent.Root(), namespace)
		} else {
			s.prefetcher = newTriePrefetcher(s.db, s.originalRoot, common.Hash{}, namespace)
		}
	}
}

// StopPrefetcher terminates a running prefetcher and reports any leftover stats
// from the gathered metrics.
func (s *StateDB) StopPrefetcher() {
	if s.noTrie {
		return
	}
	s.prefetcherLock.Lock()
	if s.prefetcher != nil {
		s.prefetcher.close()
	}
	s.prefetcherLock.Unlock()
}

func (s *StateDB) TriePrefetchInAdvance(block *types.Block, signer types.Signer) {
	// s is a temporary throw away StateDB, s.prefetcher won't be resetted to nil
	// so no need to add lock for s.prefetcher
	prefetcher := s.prefetcher
	if prefetcher == nil {
		return
	}
	accounts := make(map[common.Address]struct{}, block.Transactions().Len()<<1)
	for _, tx := range block.Transactions() {
		from, err := types.Sender(signer, tx)
		if err != nil {
			// invalid block, skip prefetch
			return
		}
		accounts[from] = struct{}{}
		if tx.To() != nil {
			accounts[*tx.To()] = struct{}{}
		}
	}
	addressesToPrefetch := make([][]byte, 0, len(accounts))
	for addr := range accounts {
		addressesToPrefetch = append(addressesToPrefetch, common.CopyBytes(addr[:])) // Copy needed for closure
	}

	if len(addressesToPrefetch) > 0 {
		prefetcher.prefetch(s.originalRoot, addressesToPrefetch, emptyAddr)
	}
}

// Mark that the block is processed by diff layer
func (s *StateDB) SetExpectedStateRoot(root common.Hash) {
	s.expectedRoot = root
}

// Mark that the block is processed by diff layer
func (s *StateDB) MarkLightProcessed() {
	s.lightProcessed = true
}

// Enable the pipeline commit function of statedb
func (s *StateDB) EnablePipeCommit() {
	if s.snap != nil && s.snaps.Layers() > 1 {
		s.pipeCommit = true
	}
}

// IsPipeCommit checks whether pipecommit is enabled on the statedb or not
func (s *StateDB) IsPipeCommit() bool {
	return s.pipeCommit
}

// Mark that the block is full processed
func (s *StateDB) MarkFullProcessed() {
	s.fullProcessed = true
}

func (s *StateDB) IsLightProcessed() bool {
	return s.lightProcessed
}

// setError remembers the first non-nil error it is called with.
func (s *StateDB) setError(err error) {
	if s.dbErr == nil {
		s.dbErr = err
	}
}

func (s *StateDB) NoTrie() bool {
	return s.noTrie
}

func (s *StateDB) Error() error {
	return s.dbErr
}

// Not thread safe
func (s *StateDB) Trie() (Trie, error) {
	if s.trie == nil {
		err := s.WaitPipeVerification()
		if err != nil {
			return nil, err
		}
		tr, err := s.db.OpenTrie(s.originalRoot)
		if err != nil {
			return nil, err
		}
		s.trie = tr
	}
	return s.trie, nil
}

func (s *StateDB) SetDiff(diffLayer *types.DiffLayer, diffTries map[common.Address]Trie, diffCode map[common.Hash][]byte) {
	s.diffLayer, s.diffTries, s.diffCode = diffLayer, diffTries, diffCode
}

func (s *StateDB) SetSnapData(snapDestructs map[common.Address]struct{}, snapAccounts map[common.Address][]byte,
	snapStorage map[common.Address]map[string][]byte) {
	s.snapDestructs, s.snapAccounts, s.snapStorage = snapDestructs, snapAccounts, snapStorage
}

func (s *StateDB) AddLog(log *types.Log) {
	s.journal.append(addLogChange{txhash: s.thash})

	log.TxHash = s.thash
	log.TxIndex = uint(s.txIndex)
	log.Index = s.logSize
	s.logs[s.thash] = append(s.logs[s.thash], log)
	s.logSize++
}

func (s *StateDB) GetLogs(hash common.Hash, blockHash common.Hash) []*types.Log {
	logs := s.logs[hash]
	for _, l := range logs {
		l.BlockHash = blockHash
	}
	return logs
}

func (s *StateDB) Logs() []*types.Log {
	var logs []*types.Log
	for _, lgs := range s.logs {
		logs = append(logs, lgs...)
	}
	return logs
}

// AddPreimage records a SHA3 preimage seen by the VM.
func (s *StateDB) AddPreimage(hash common.Hash, preimage []byte) {
	if _, ok := s.preimages[hash]; !ok {
		s.journal.append(addPreimageChange{hash: hash})
		pi := make([]byte, len(preimage))
		copy(pi, preimage)
		s.preimages[hash] = pi
	}
}

// Preimages returns a list of SHA3 preimages that have been submitted.
func (s *StateDB) Preimages() map[common.Hash][]byte {
	return s.preimages
}

// AddRefund adds gas to the refund counter
func (s *StateDB) AddRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	s.refund += gas
}

// SubRefund removes gas from the refund counter.
// This method will panic if the refund counter goes below zero
func (s *StateDB) SubRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	if gas > s.refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, s.refund))
	}
	s.refund -= gas
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for suicided accounts.
func (s *StateDB) Exist(addr common.Address) bool {
	return s.getStateObject(addr) != nil
}

// Empty returns whether the state object is either non-existent
// or empty according to the EIP161 specification (balance = nonce = code = 0)
func (s *StateDB) Empty(addr common.Address) bool {
	so := s.getStateObject(addr)
	return so == nil || so.empty()
}

// GetBalance retrieves the balance from the given address or 0 if object not found
func (s *StateDB) GetBalance(addr common.Address) *big.Int {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Balance()
	}
	return common.Big0
}

func (s *StateDB) GetNonce(addr common.Address) uint64 {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Nonce()
	}

	return 0
}

// TxIndex returns the current transaction index set by Prepare.
func (s *StateDB) TxIndex() int {
	return s.txIndex
}

func (s *StateDB) GetCode(addr common.Address) []byte {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Code(s.db)
	}
	return nil
}

func (s *StateDB) GetCodeSize(addr common.Address) int {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.CodeSize(s.db)
	}
	return 0
}

func (s *StateDB) GetCodeHash(addr common.Address) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return common.Hash{}
	}
	return common.BytesToHash(stateObject.CodeHash())
}

// GetState retrieves a value from the given account's storage trie.
func (s *StateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.GetState(s.db, hash)
	}
	return common.Hash{}
}

// GetProof returns the Merkle proof for a given account.
func (s *StateDB) GetProof(addr common.Address) ([][]byte, error) {
	return s.GetProofByHash(crypto.Keccak256Hash(addr.Bytes()))
}

// GetProofByHash returns the Merkle proof for a given account.
func (s *StateDB) GetProofByHash(addrHash common.Hash) ([][]byte, error) {
	var proof proofList
	if _, err := s.Trie(); err != nil {
		return nil, err
	}
	err := s.trie.Prove(addrHash[:], 0, &proof)
	return proof, err
}

// GetStorageProof returns the Merkle proof for given storage slot.
func (s *StateDB) GetStorageProof(a common.Address, key common.Hash) ([][]byte, error) {
	var proof proofList
	trie := s.StorageTrie(a)
	if trie == nil {
		return proof, errors.New("storage trie for requested address does not exist")
	}
	err := trie.Prove(crypto.Keccak256(key.Bytes()), 0, &proof)
	return proof, err
}

// GetCommittedState retrieves a value from the given account's committed storage trie.
func (s *StateDB) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.GetCommittedState(s.db, hash)
	}
	return common.Hash{}
}

// Database retrieves the low level database supporting the lower level trie ops.
func (s *StateDB) Database() Database {
	return s.db
}

// StorageTrie returns the storage trie of an account.
// The return value is a copy and is nil for non-existent accounts.
func (s *StateDB) StorageTrie(addr common.Address) Trie {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return nil
	}
	cpy := stateObject.deepCopy(s)
	cpy.updateTrie(s.db)
	return cpy.getTrie(s.db)
}

func (s *StateDB) HasSuicided(addr common.Address) bool {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.suicided
	}
	return false
}

/*
 * SETTERS
 */

// AddBalance adds amount to the account associated with addr.
func (s *StateDB) AddBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.AddBalance(amount)
	}
}

// SubBalance subtracts amount from the account associated with addr.
func (s *StateDB) SubBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SubBalance(amount)
	}
}

func (s *StateDB) SetBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetBalance(amount)
	}
}

func (s *StateDB) SetNonce(addr common.Address, nonce uint64) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetNonce(nonce)
	}
}

func (s *StateDB) SetCode(addr common.Address, code []byte) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetCode(crypto.Keccak256Hash(code), code)
	}
}

func (s *StateDB) SetState(addr common.Address, key, value common.Hash) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetState(s.db, key, value)
	}
}

// SetStorage replaces the entire storage for the specified account with given
// storage. This function should only be used for debugging.
func (s *StateDB) SetStorage(addr common.Address, storage map[common.Hash]common.Hash) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetStorage(storage)
	}
}

// Suicide marks the given account as suicided.
// This clears the account balance.
//
// The account's state object is still available until the state is committed,
// getStateObject will return a non-nil account after Suicide.
func (s *StateDB) Suicide(addr common.Address) bool {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return false
	}
	s.journal.append(suicideChange{
		account:     &addr,
		prev:        stateObject.suicided,
		prevbalance: new(big.Int).Set(stateObject.Balance()),
	})
	stateObject.markSuicided()
	stateObject.data.Balance = new(big.Int)

	return true
}

//
// Setting, updating & deleting state object methods.
//

// updateStateObject writes the given object to the trie.
func (s *StateDB) updateStateObject(obj *StateObject) {
	if s.noTrie {
		return
	}
	// Track the amount of time wasted on updating the account from the trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountUpdates += time.Since(start) }(time.Now())
	}
	// Encode the account and update the account trie
	addr := obj.Address()
	if err := s.trie.TryUpdateAccount(addr[:], &obj.data); err != nil {
		s.setError(fmt.Errorf("updateStateObject (%x) error: %v", addr[:], err))
	}
}

// deleteStateObject removes the given object from the state trie.
func (s *StateDB) deleteStateObject(obj *StateObject) {
	if s.noTrie {
		return
	}
	// Track the amount of time wasted on deleting the account from the trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountUpdates += time.Since(start) }(time.Now())
	}
	// Delete the account from the trie
	addr := obj.Address()
	if err := s.trie.TryDelete(addr[:]); err != nil {
		s.setError(fmt.Errorf("deleteStateObject (%x) error: %v", addr[:], err))
	}
}

// getStateObject retrieves a state object given by the address, returning nil if
// the object is not found or was deleted in this execution context. If you need
// to differentiate between non-existent/just-deleted, use getDeletedStateObject.
func (s *StateDB) getStateObject(addr common.Address) *StateObject {
	if obj := s.getDeletedStateObject(addr); obj != nil && !obj.deleted {
		return obj
	}
	return nil
}

// getDeletedStateObject is similar to getStateObject, but instead of returning
// nil for a deleted state object, it returns the actual object with the deleted
// flag set. This is needed by the state journal to revert to the correct s-
// destructed object instead of wiping all knowledge about the state object.
func (s *StateDB) getDeletedStateObject(addr common.Address) *StateObject {
	// Prefer live objects if any is available
	if obj := s.stateObjects[addr]; obj != nil {
		return obj
	}
	// If no live objects are available, attempt to use snapshots
	var data *types.StateAccount
	if s.snap != nil {
		start := time.Now()
		acc, err := s.snap.Account(crypto.HashData(s.hasher, addr.Bytes()))
		if metrics.EnabledExpensive {
			s.SnapshotAccountReads += time.Since(start)
		}
		if err == nil {
			if acc == nil {
				return nil
			}
			data = &types.StateAccount{
				Nonce:    acc.Nonce,
				Balance:  acc.Balance,
				CodeHash: acc.CodeHash,
				Root:     common.BytesToHash(acc.Root),
			}
			if len(data.CodeHash) == 0 {
				data.CodeHash = emptyCodeHash
			}
			if data.Root == (common.Hash{}) {
				data.Root = emptyRoot
			}
		}
	}

	// If snapshot unavailable or reading from it failed, load from the database
	if data == nil {
		if s.trie == nil {
			tr, err := s.db.OpenTrie(s.originalRoot)
			if err != nil {
				s.setError(fmt.Errorf("failed to open trie tree"))
				return nil
			}
			s.trie = tr
		}
		start := time.Now()
		enc, err := s.trie.TryGet(addr.Bytes())
		if metrics.EnabledExpensive {
			s.AccountReads += time.Since(start)
		}
		if err != nil {
			s.setError(fmt.Errorf("getDeleteStateObject (%x) error: %v", addr.Bytes(), err))
			return nil
		}
		if len(enc) == 0 {
			return nil
		}
		data = new(types.StateAccount)
		if err := rlp.DecodeBytes(enc, data); err != nil {
			log.Error("Failed to decode state object", "addr", addr, "err", err)
			return nil
		}
	}
	// Insert into the live set
	obj := newObject(s, addr, *data)
	s.SetStateObject(obj)
	return obj
}

func (s *StateDB) SetStateObject(object *StateObject) {
	s.stateObjects[object.Address()] = object
}

// GetOrNewStateObject retrieves a state object or create a new state object if nil.
func (s *StateDB) GetOrNewStateObject(addr common.Address) *StateObject {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		stateObject, _ = s.createObject(addr)
	}
	return stateObject
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.
func (s *StateDB) createObject(addr common.Address) (newobj, prev *StateObject) {
	prev = s.getDeletedStateObject(addr) // Note, prev might have been deleted, we need that!

	var prevdestruct bool
	if s.snap != nil && prev != nil {
		_, prevdestruct = s.snapDestructs[prev.address]
		if !prevdestruct {
			s.snapDestructs[prev.address] = struct{}{}
		}
	}
	newobj = newObject(s, addr, types.StateAccount{})
	if prev == nil {
		s.journal.append(createObjectChange{account: &addr})
	} else {
		s.journal.append(resetObjectChange{prev: prev, prevdestruct: prevdestruct})
	}
	s.SetStateObject(newobj)
	if prev != nil && !prev.deleted {
		return newobj, prev
	}
	return newobj, nil
}

// CreateAccount explicitly creates a state object. If a state object with the address
// already exists the balance is carried over to the new account.
//
// CreateAccount is called during the EVM CREATE operation. The situation might arise that
// a contract does the following:
//
//  1. sends funds to sha(account ++ (nonce + 1))
//  2. tx_create(sha(account ++ nonce)) (note that this gets the address of 1)
//
// Carrying over the balance ensures that Ether doesn't disappear.
func (s *StateDB) CreateAccount(addr common.Address) {
	newObj, prev := s.createObject(addr)
	if prev != nil {
		newObj.setBalance(prev.data.Balance)
	}
}

func (db *StateDB) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	so := db.getStateObject(addr)
	if so == nil {
		return nil
	}
	it := trie.NewIterator(so.getTrie(db.db).NodeIterator(nil))

	for it.Next() {
		key := common.BytesToHash(db.trie.GetKey(it.Key))
		if value, dirty := so.dirtyStorage[key]; dirty {
			if !cb(key, value) {
				return nil
			}
			continue
		}

		if len(it.Value) > 0 {
			_, content, _, err := rlp.Split(it.Value)
			if err != nil {
				return err
			}
			if !cb(key, common.BytesToHash(content)) {
				return nil
			}
		}
	}
	return nil
}

// Copy creates a deep, independent copy of the state.
// Snapshots of the copied state cannot be applied to the copy.
func (s *StateDB) Copy() *StateDB {
	return s.copyInternal(false)
}

// It is mainly for state prefetcher to do trie prefetch right now.
func (s *StateDB) CopyDoPrefetch() *StateDB {
	return s.copyInternal(true)
}

// If doPrefetch is true, it tries to reuse the prefetcher, the copied StateDB will do active trie prefetch.
// otherwise, just do inactive copy trie prefetcher.
func (s *StateDB) copyInternal(doPrefetch bool) *StateDB {
	// Copy all the basic fields, initialize the memory ones
	state := &StateDB{
		db:                  s.db,
		trie:                s.db.CopyTrie(s.trie),
		stateObjects:        make(map[common.Address]*StateObject, len(s.journal.dirties)),
		stateObjectsPending: make(map[common.Address]struct{}, len(s.stateObjectsPending)),
		stateObjectsDirty:   make(map[common.Address]struct{}, len(s.journal.dirties)),
		storagePool:         s.storagePool,
		refund:              s.refund,
		logs:                make(map[common.Hash][]*types.Log, len(s.logs)),
		logSize:             s.logSize,
		preimages:           make(map[common.Hash][]byte, len(s.preimages)),
		journal:             newJournal(),
		hasher:              crypto.NewKeccakState(),
	}
	// Copy the dirty states, logs, and preimages
	for addr := range s.journal.dirties {
		// As documented [here](https://github.com/ethereum/go-ethereum/pull/16485#issuecomment-380438527),
		// and in the Finalise-method, there is a case where an object is in the journal but not
		// in the stateObjects: OOG after touch on ripeMD prior to Byzantium. Thus, we need to check for
		// nil
		if object, exist := s.stateObjects[addr]; exist {
			// Even though the original object is dirty, we are not copying the journal,
			// so we need to make sure that anyside effect the journal would have caused
			// during a commit (or similar op) is already applied to the copy.
			state.stateObjects[addr] = object.deepCopy(state)

			state.stateObjectsDirty[addr] = struct{}{}   // Mark the copy dirty to force internal (code/state) commits
			state.stateObjectsPending[addr] = struct{}{} // Mark the copy pending to force external (account) commits
		}
	}
	// Above, we don't copy the actual journal. This means that if the copy is copied, the
	// loop above will be a no-op, since the copy's journal is empty.
	// Thus, here we iterate over stateObjects, to enable copies of copies
	for addr := range s.stateObjectsPending {
		if _, exist := state.stateObjects[addr]; !exist {
			state.stateObjects[addr] = s.stateObjects[addr].deepCopy(state)
		}
		state.stateObjectsPending[addr] = struct{}{}
	}
	for addr := range s.stateObjectsDirty {
		if _, exist := state.stateObjects[addr]; !exist {
			state.stateObjects[addr] = s.stateObjects[addr].deepCopy(state)
		}
		state.stateObjectsDirty[addr] = struct{}{}
	}
	for hash, logs := range s.logs {
		cpy := make([]*types.Log, len(logs))
		for i, l := range logs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		state.logs[hash] = cpy
	}
	for hash, preimage := range s.preimages {
		state.preimages[hash] = preimage
	}
	// Do we need to copy the access list? In practice: No. At the start of a
	// transaction, the access list is empty. In practice, we only ever copy state
	// _between_ transactions/blocks, never in the middle of a transaction.
	// However, it doesn't cost us much to copy an empty list, so we do it anyway
	// to not blow up if we ever decide copy it in the middle of a transaction
	if s.accessList != nil {
		state.accessList = s.accessList.Copy()
	}

	state.prefetcher = s.prefetcher
	if s.prefetcher != nil && !doPrefetch {
		// If there's a prefetcher running, make an inactive copy of it that can
		// only access data but does not actively preload (since the user will not
		// know that they need to explicitly terminate an active copy).
		state.prefetcher = state.prefetcher.copy()
	}
	if s.snaps != nil {
		// In order for the miner to be able to use and make additions
		// to the snapshot tree, we need to copy that aswell.
		// Otherwise, any block mined by ourselves will cause gaps in the tree,
		// and force the miner to operate trie-backed only
		state.snaps = s.snaps
		state.snap = s.snap
		// deep copy needed
		state.snapDestructs = make(map[common.Address]struct{})
		for k, v := range s.snapDestructs {
			state.snapDestructs[k] = v
		}
		state.snapAccounts = make(map[common.Address][]byte)
		for k, v := range s.snapAccounts {
			state.snapAccounts[k] = v
		}
		state.snapStorage = make(map[common.Address]map[string][]byte)
		for k, v := range s.snapStorage {
			temp := make(map[string][]byte)
			for kk, vv := range v {
				temp[kk] = vv
			}
			state.snapStorage[k] = temp
		}
	}
	return state
}

// Snapshot returns an identifier for the current revision of the state.
func (s *StateDB) Snapshot() int {
	id := s.nextRevisionId
	s.nextRevisionId++
	s.validRevisions = append(s.validRevisions, revision{id, s.journal.length()})
	return id
}

// RevertToSnapshot reverts all state changes made since the given revision.
func (s *StateDB) RevertToSnapshot(revid int) {
	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(s.validRevisions), func(i int) bool {
		return s.validRevisions[i].id >= revid
	})
	if idx == len(s.validRevisions) || s.validRevisions[idx].id != revid {
		panic(fmt.Errorf("revision id %v cannot be reverted", revid))
	}
	snapshot := s.validRevisions[idx].journalIndex

	// Replay the journal to undo changes and remove invalidated snapshots
	s.journal.revert(s, snapshot)
	s.validRevisions = s.validRevisions[:idx]
}

// GetRefund returns the current value of the refund counter.
func (s *StateDB) GetRefund() uint64 {
	return s.refund
}

// WaitPipeVerification waits until the snapshot been verified
func (s *StateDB) WaitPipeVerification() error {
	// Need to wait for the parent trie to commit
	if s.snap != nil {
		if valid := s.snap.WaitAndGetVerifyRes(); !valid {
			return fmt.Errorf("verification on parent snap failed")
		}
	}
	return nil
}

// Finalise finalises the state by removing the s destructed objects and clears
// the journal as well as the refunds. Finalise, however, will not push any updates
// into the tries just yet. Only IntermediateRoot or Commit will do that.
func (s *StateDB) Finalise(deleteEmptyObjects bool) {
	addressesToPrefetch := make([][]byte, 0, len(s.journal.dirties))
	for addr := range s.journal.dirties {
		obj, exist := s.stateObjects[addr]
		if !exist {
			// ripeMD is 'touched' at block 1714175, in tx 0x1237f737031e40bcde4a8b7e717b2d15e3ecadfe49bb1bbc71ee9deb09c6fcf2
			// That tx goes out of gas, and although the notion of 'touched' does not exist there, the
			// touch-event will still be recorded in the journal. Since ripeMD is a special snowflake,
			// it will persist in the journal even though the journal is reverted. In this special circumstance,
			// it may exist in `s.journal.dirties` but not in `s.stateObjects`.
			// Thus, we can safely ignore it here
			continue
		}
		if obj.suicided || (deleteEmptyObjects && obj.empty()) {
			obj.deleted = true

			// If state snapshotting is active, also mark the destruction there.
			// Note, we can't do this only at the end of a block because multiple
			// transactions within the same block might self destruct and then
			// ressurrect an account; but the snapshotter needs both events.
			if s.snap != nil {
				s.snapDestructs[obj.address] = struct{}{} // We need to maintain account deletions explicitly (will remain set indefinitely)
				delete(s.snapAccounts, obj.address)       // Clear out any previously updated account data (may be recreated via a ressurrect)
				delete(s.snapStorage, obj.address)        // Clear out any previously updated storage data (may be recreated via a ressurrect)
			}
		} else {
			obj.finalise(true) // Prefetch slots in the background
		}
		if _, exist := s.stateObjectsPending[addr]; !exist {
			s.stateObjectsPending[addr] = struct{}{}
		}
		if _, exist := s.stateObjectsDirty[addr]; !exist {
			s.stateObjectsDirty[addr] = struct{}{}
			// At this point, also ship the address off to the precacher. The precacher
			// will start loading tries, and when the change is eventually committed,
			// the commit-phase will be a lot faster
			addressesToPrefetch = append(addressesToPrefetch, common.CopyBytes(addr[:])) // Copy needed for closure
		}
	}
	prefetcher := s.prefetcher
	if prefetcher != nil && len(addressesToPrefetch) > 0 {
		if s.snap.Verified() {
			prefetcher.prefetch(s.originalRoot, addressesToPrefetch, emptyAddr)
		} else if prefetcher.rootParent != (common.Hash{}) {
			prefetcher.prefetch(prefetcher.rootParent, addressesToPrefetch, emptyAddr)
		}
	}
	// Invalidate journal because reverting across transactions is not allowed.
	s.clearJournalAndRefund()
}

// IntermediateRoot computes the current root hash of the state trie.
// It is called in between transactions to get the root hash that
// goes into transaction receipts.
func (s *StateDB) IntermediateRoot(deleteEmptyObjects bool) common.Hash {
	// light process is not allowed when there is no trie
	if s.lightProcessed {
		s.StopPrefetcher()
		return s.trie.Hash()
	}
	// Finalise all the dirty storage states and write them into the tries
	s.Finalise(deleteEmptyObjects)
	s.AccountsIntermediateRoot()
	return s.StateIntermediateRoot()
}

// CorrectAccountsRoot will fix account roots in pipecommit mode
func (s *StateDB) CorrectAccountsRoot(blockRoot common.Hash) {
	var snapshot snapshot.Snapshot
	if blockRoot == (common.Hash{}) {
		snapshot = s.snap
	} else if s.snaps != nil {
		snapshot = s.snaps.Snapshot(blockRoot)
	}

	if snapshot == nil {
		return
	}
	if accounts, err := snapshot.Accounts(); err == nil && accounts != nil {
		for _, obj := range s.stateObjects {
			if !obj.deleted {
				if account, exist := accounts[crypto.Keccak256Hash(obj.address[:])]; exist {
					if len(account.Root) == 0 {
						obj.data.Root = emptyRoot
					} else {
						obj.data.Root = common.BytesToHash(account.Root)
					}
					obj.rootCorrected = true
				}
			}
		}
	}
}

// PopulateSnapAccountAndStorage tries to populate required accounts and storages for pipecommit
func (s *StateDB) PopulateSnapAccountAndStorage() {
	for addr := range s.stateObjectsPending {
		if obj := s.stateObjects[addr]; !obj.deleted {
			if s.snap != nil {
				s.populateSnapStorage(obj)
				s.snapAccounts[obj.address] = snapshot.SlimAccountRLP(obj.data.Nonce, obj.data.Balance, obj.data.Root, obj.data.CodeHash)
			}
		}
	}
}

// populateSnapStorage tries to populate required storages for pipecommit, and returns a flag to indicate whether the storage root changed or not
func (s *StateDB) populateSnapStorage(obj *StateObject) bool {
	for key, value := range obj.dirtyStorage {
		obj.pendingStorage[key] = value
	}
	if len(obj.pendingStorage) == 0 {
		return false
	}
	var storage map[string][]byte
	for key, value := range obj.pendingStorage {
		var v []byte
		if (value != common.Hash{}) {
			// Encoding []byte cannot fail, ok to ignore the error.
			v, _ = rlp.EncodeToBytes(common.TrimLeftZeroes(value[:]))
		}
		// If state snapshotting is active, cache the data til commit
		if obj.db.snap != nil {
			if storage == nil {
				// Retrieve the old storage map, if available, create a new one otherwise
				if storage = obj.db.snapStorage[obj.address]; storage == nil {
					storage = make(map[string][]byte)
					obj.db.snapStorage[obj.address] = storage
				}
			}
			storage[string(key[:])] = v // v will be nil if value is 0x00
		}
	}
	return true
}

func (s *StateDB) AccountsIntermediateRoot() {
	tasks := make(chan func())
	finishCh := make(chan struct{})
	defer close(finishCh)
	wg := sync.WaitGroup{}
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for {
				select {
				case task := <-tasks:
					task()
				case <-finishCh:
					return
				}
			}
		}()
	}

	// Although naively it makes sense to retrieve the account trie and then do
	// the contract storage and account updates sequentially, that short circuits
	// the account prefetcher. Instead, let's process all the storage updates
	// first, giving the account prefeches just a few more milliseconds of time
	// to pull useful data from disk.
	for addr := range s.stateObjectsPending {
		if obj := s.stateObjects[addr]; !obj.deleted {
			wg.Add(1)
			tasks <- func() {
				obj.updateRoot(s.db)

				// If state snapshotting is active, cache the data til commit. Note, this
				// update mechanism is not symmetric to the deletion, because whereas it is
				// enough to track account updates at commit time, deletions need tracking
				// at transaction boundary level to ensure we capture state clearing.
				if s.snap != nil {
					s.snapAccountMux.Lock()
					// It is possible to add unnecessary change, but it is fine.
					s.snapAccounts[obj.address] = snapshot.SlimAccountRLP(obj.data.Nonce, obj.data.Balance, obj.data.Root, obj.data.CodeHash)
					s.snapAccountMux.Unlock()
				}
				data, err := rlp.EncodeToBytes(obj)
				if err != nil {
					panic(fmt.Errorf("can't encode object at %x: %v", addr[:], err))
				}
				obj.encodeData = data
				wg.Done()
			}
		}
	}
	wg.Wait()
}

func (s *StateDB) StateIntermediateRoot() common.Hash {
	// If there was a trie prefetcher operating, it gets aborted and irrevocably
	// modified after we start retrieving tries. Remove it from the statedb after
	// this round of use.
	//
	// This is weird pre-byzantium since the first tx runs with a prefetcher and
	// the remainder without, but pre-byzantium even the initial prefetcher is
	// useless, so no sleep lost.
	prefetcher := s.prefetcher
	defer s.StopPrefetcher()

	// Now we're about to start to write changes to the trie. The trie is so far
	// _untouched_. We can check with the prefetcher, if it can give us a trie
	// which has the same root, but also has some content loaded into it.
	if prefetcher != nil {
		if trie := prefetcher.trie(s.originalRoot); trie != nil {
			s.trie = trie
		}
	}
	if s.trie == nil {
		tr, err := s.db.OpenTrie(s.originalRoot)
		if err != nil {
			panic(fmt.Sprintf("Failed to open trie tree %s", s.originalRoot))
		}
		s.trie = tr
	}

	usedAddrs := make([][]byte, 0, len(s.stateObjectsPending))
	if !s.noTrie {
		for addr := range s.stateObjectsPending {
			if obj := s.stateObjects[addr]; obj.deleted {
				s.deleteStateObject(obj)
			} else {
				s.updateStateObject(obj)
			}
			usedAddrs = append(usedAddrs, common.CopyBytes(addr[:])) // Copy needed for closure
		}
		if prefetcher != nil {
			prefetcher.used(s.originalRoot, usedAddrs)
		}
	}

	if len(s.stateObjectsPending) > 0 {
		s.stateObjectsPending = make(map[common.Address]struct{})
	}
	// Track the amount of time wasted on hashing the account trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountHashes += time.Since(start) }(time.Now())
	}
	if s.noTrie {
		return s.expectedRoot
	} else {
		return s.trie.Hash()
	}
}

// Prepare sets the current transaction hash and index which are
// used when the EVM emits new state logs.
func (s *StateDB) Prepare(thash common.Hash, ti int) {
	s.thash = thash
	s.txIndex = ti
	s.accessList = nil
}

func (s *StateDB) clearJournalAndRefund() {
	if len(s.journal.entries) > 0 {
		s.journal = newJournal()
		s.refund = 0
	}
	s.validRevisions = s.validRevisions[:0] // Snapshots can be created without journal entires
}

func (s *StateDB) LightCommit() (common.Hash, *types.DiffLayer, error) {
	codeWriter := s.db.TrieDB().DiskDB().NewBatch()

	// light process already verified it, expectedRoot is trustworthy.
	root := s.expectedRoot

	commitFuncs := []func() error{
		func() error {
			for codeHash, code := range s.diffCode {
				rawdb.WriteCode(codeWriter, codeHash, code)
				if codeWriter.ValueSize() >= ethdb.IdealBatchSize {
					if err := codeWriter.Write(); err != nil {
						return err
					}
					codeWriter.Reset()
				}
			}
			if codeWriter.ValueSize() > 0 {
				if err := codeWriter.Write(); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			tasks := make(chan func())
			taskResults := make(chan error, len(s.diffTries))
			tasksNum := 0
			finishCh := make(chan struct{})
			defer close(finishCh)
			threads := gopool.Threads(len(s.diffTries))

			for i := 0; i < threads; i++ {
				go func() {
					for {
						select {
						case task := <-tasks:
							task()
						case <-finishCh:
							return
						}
					}
				}()
			}

			for account, diff := range s.diffTries {
				tmpAccount := account
				tmpDiff := diff
				tasks <- func() {
					root, _, err := tmpDiff.Commit(nil)
					if err != nil {
						taskResults <- err
						return
					}
					s.db.CacheStorage(crypto.Keccak256Hash(tmpAccount[:]), root, tmpDiff)
					taskResults <- nil
				}
				tasksNum++
			}

			for i := 0; i < tasksNum; i++ {
				err := <-taskResults
				if err != nil {
					return err
				}
			}

			// commit account trie
			var account types.StateAccount
			root, _, err := s.trie.Commit(func(_ [][]byte, _ []byte, leaf []byte, parent common.Hash) error {
				if err := rlp.DecodeBytes(leaf, &account); err != nil {
					return nil
				}
				if account.Root != emptyRoot {
					s.db.TrieDB().Reference(account.Root, parent)
				}
				return nil
			})
			if err != nil {
				return err
			}
			if root != emptyRoot {
				s.db.CacheAccount(root, s.trie)
			}
			return nil
		},
		func() error {
			if s.snap != nil {
				if metrics.EnabledExpensive {
					defer func(start time.Time) { s.SnapshotCommits += time.Since(start) }(time.Now())
				}
				// Only update if there's a state transition (skip empty Clique blocks)
				if parent := s.snap.Root(); parent != root {
					// for light commit, always do sync commit
					if err := s.snaps.Update(root, parent, s.snapDestructs, s.snapAccounts, s.snapStorage, nil); err != nil {
						log.Warn("Failed to update snapshot tree", "from", parent, "to", root, "err", err)
					}
					// Keep n diff layers in the memory
					// - head layer is paired with HEAD state
					// - head-1 layer is paired with HEAD-1 state
					// - head-(n-1) layer(bottom-most diff layer) is paired with HEAD-(n-1)state
					if err := s.snaps.Cap(root, s.snaps.CapLimit()); err != nil {
						log.Warn("Failed to cap snapshot tree", "root", root, "layers", s.snaps.CapLimit(), "err", err)
					}
				}
			}
			return nil
		},
	}
	commitRes := make(chan error, len(commitFuncs))
	for _, f := range commitFuncs {
		tmpFunc := f
		go func() {
			commitRes <- tmpFunc()
		}()
	}
	for i := 0; i < len(commitFuncs); i++ {
		r := <-commitRes
		if r != nil {
			return common.Hash{}, nil, r
		}
	}
	s.snap, s.snapDestructs, s.snapAccounts, s.snapStorage = nil, nil, nil, nil
	s.diffTries, s.diffCode = nil, nil
	return root, s.diffLayer, nil
}

// Commit writes the state to the underlying in-memory trie database.
func (s *StateDB) Commit(failPostCommitFunc func(), postCommitFuncs ...func() error) (common.Hash, *types.DiffLayer, error) {
	if s.dbErr != nil {
		s.StopPrefetcher()
		return common.Hash{}, nil, fmt.Errorf("commit aborted due to earlier error: %v", s.dbErr)
	}
	// Finalize any pending changes and merge everything into the tries
	if s.lightProcessed {
		defer s.StopPrefetcher()
		root, diff, err := s.LightCommit()
		if err != nil {
			return root, diff, err
		}
		for _, postFunc := range postCommitFuncs {
			err = postFunc()
			if err != nil {
				return root, diff, err
			}
		}
		return root, diff, nil
	}
	var diffLayer *types.DiffLayer
	var verified chan struct{}
	var snapUpdated chan struct{}

	if s.snap != nil {
		diffLayer = &types.DiffLayer{}
	}
	if s.pipeCommit {
		// async commit the MPT
		verified = make(chan struct{})
		snapUpdated = make(chan struct{})
	}

	commmitTrie := func() error {
		commitErr := func() error {
			if s.pipeCommit {
				<-snapUpdated
				// Due to state verification pipeline, the accounts roots are not updated, leading to the data in the difflayer is not correct, capture the correct data here
				s.AccountsIntermediateRoot()
				if parent := s.snap.Root(); parent != s.expectedRoot {
					accountData := make(map[common.Hash][]byte)
					for k, v := range s.snapAccounts {
						accountData[crypto.Keccak256Hash(k[:])] = v
					}
					s.snaps.Snapshot(s.expectedRoot).CorrectAccounts(accountData)
				}
			}

			if s.stateRoot = s.StateIntermediateRoot(); s.fullProcessed && s.expectedRoot != s.stateRoot {
				log.Error("Invalid merkle root", "remote", s.expectedRoot, "local", s.stateRoot)
				return fmt.Errorf("invalid merkle root (remote: %x local: %x)", s.expectedRoot, s.stateRoot)
			}

			tasks := make(chan func())
			taskResults := make(chan error, len(s.stateObjectsDirty))
			tasksNum := 0
			finishCh := make(chan struct{})

			threads := gopool.Threads(len(s.stateObjectsDirty))
			wg := sync.WaitGroup{}
			for i := 0; i < threads; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case task := <-tasks:
							task()
						case <-finishCh:
							return
						}
					}
				}()
			}

			for addr := range s.stateObjectsDirty {
				if obj := s.stateObjects[addr]; !obj.deleted {
					// Write any contract code associated with the state object
					tasks <- func() {
						// Write any storage changes in the state object to its storage trie
						if !s.noTrie {
							if _, err := obj.CommitTrie(s.db); err != nil {
								taskResults <- err
								return
							}
						}
						taskResults <- nil
					}
					tasksNum++
				}
			}

			for i := 0; i < tasksNum; i++ {
				err := <-taskResults
				if err != nil {
					close(finishCh)
					return err
				}
			}
			close(finishCh)

			// The onleaf func is called _serially_, so we can reuse the same account
			// for unmarshalling every time.
			if !s.noTrie {
				var account types.StateAccount
				root, _, err := s.trie.Commit(func(_ [][]byte, _ []byte, leaf []byte, parent common.Hash) error {
					if err := rlp.DecodeBytes(leaf, &account); err != nil {
						return nil
					}
					if account.Root != emptyRoot {
						s.db.TrieDB().Reference(account.Root, parent)
					}
					return nil
				})
				if err != nil {
					return err
				}
				if root != emptyRoot {
					s.db.CacheAccount(root, s.trie)
				}
			}

			for _, postFunc := range postCommitFuncs {
				err := postFunc()
				if err != nil {
					return err
				}
			}
			wg.Wait()
			return nil
		}()

		if s.pipeCommit {
			if commitErr == nil {
				s.snaps.Snapshot(s.stateRoot).MarkValid()
				close(verified)
			} else {
				// The blockchain will do the further rewind if write block not finish yet
				close(verified)
				if failPostCommitFunc != nil {
					failPostCommitFunc()
				}
				log.Error("state verification failed", "err", commitErr)
			}
		}
		return commitErr
	}

	commitFuncs := []func() error{
		func() error {
			codeWriter := s.db.TrieDB().DiskDB().NewBatch()
			for addr := range s.stateObjectsDirty {
				if obj := s.stateObjects[addr]; !obj.deleted {
					if obj.code != nil && obj.dirtyCode {
						rawdb.WriteCode(codeWriter, common.BytesToHash(obj.CodeHash()), obj.code)
						obj.dirtyCode = false
						if s.snap != nil {
							diffLayer.Codes = append(diffLayer.Codes, types.DiffCode{
								Hash: common.BytesToHash(obj.CodeHash()),
								Code: obj.code,
							})
						}
						if codeWriter.ValueSize() > ethdb.IdealBatchSize {
							if err := codeWriter.Write(); err != nil {
								return err
							}
							codeWriter.Reset()
						}
					}
				}
			}
			if codeWriter.ValueSize() > 0 {
				if err := codeWriter.Write(); err != nil {
					log.Crit("Failed to commit dirty codes", "error", err)
					return err
				}
			}
			return nil
		},
		func() error {
			// If snapshotting is enabled, update the snapshot tree with this new version
			if s.snap != nil {
				if metrics.EnabledExpensive {
					defer func(start time.Time) { s.SnapshotCommits += time.Since(start) }(time.Now())
				}
				if s.pipeCommit {
					defer close(snapUpdated)
					// State verification pipeline - accounts root are not calculated here, just populate needed fields for process
					s.PopulateSnapAccountAndStorage()
				}
				diffLayer.Destructs, diffLayer.Accounts, diffLayer.Storages = s.SnapToDiffLayer()
				// Only update if there's a state transition (skip empty Clique blocks)
				if parent := s.snap.Root(); parent != s.expectedRoot {
					err := s.snaps.Update(s.expectedRoot, parent, s.snapDestructs, s.snapAccounts, s.snapStorage, verified)

					if err != nil {
						log.Warn("Failed to update snapshot tree", "from", parent, "to", s.expectedRoot, "err", err)
					}

					// Keep n diff layers in the memory
					// - head layer is paired with HEAD state
					// - head-1 layer is paired with HEAD-1 state
					// - head-(n-1) layer(bottom-most diff layer) is paired with HEAD-(n-1)state
					go func() {
						if err := s.snaps.Cap(s.expectedRoot, s.snaps.CapLimit()); err != nil {
							log.Warn("Failed to cap snapshot tree", "root", s.expectedRoot, "layers", s.snaps.CapLimit(), "err", err)
						}
					}()
				}
			}
			return nil
		},
	}
	if s.pipeCommit {
		go commmitTrie()
	} else {
		defer s.StopPrefetcher()
		commitFuncs = append(commitFuncs, commmitTrie)
	}
	commitRes := make(chan error, len(commitFuncs))
	for _, f := range commitFuncs {
		tmpFunc := f
		go func() {
			commitRes <- tmpFunc()
		}()
	}
	for i := 0; i < len(commitFuncs); i++ {
		r := <-commitRes
		if r != nil {
			return common.Hash{}, nil, r
		}
	}
	root := s.stateRoot
	if s.pipeCommit {
		root = s.expectedRoot
	}

	return root, diffLayer, nil
}

func (s *StateDB) DiffLayerToSnap(diffLayer *types.DiffLayer) (map[common.Address]struct{}, map[common.Address][]byte, map[common.Address]map[string][]byte, error) {
	snapDestructs := make(map[common.Address]struct{})
	snapAccounts := make(map[common.Address][]byte)
	snapStorage := make(map[common.Address]map[string][]byte)

	for _, des := range diffLayer.Destructs {
		snapDestructs[des] = struct{}{}
	}
	for _, account := range diffLayer.Accounts {
		snapAccounts[account.Account] = account.Blob
	}
	for _, storage := range diffLayer.Storages {
		// should never happen
		if len(storage.Keys) != len(storage.Vals) {
			return nil, nil, nil, errors.New("invalid diffLayer: length of keys and values mismatch")
		}
		snapStorage[storage.Account] = make(map[string][]byte, len(storage.Keys))
		n := len(storage.Keys)
		for i := 0; i < n; i++ {
			snapStorage[storage.Account][storage.Keys[i]] = storage.Vals[i]
		}
	}
	return snapDestructs, snapAccounts, snapStorage, nil
}

func (s *StateDB) SnapToDiffLayer() ([]common.Address, []types.DiffAccount, []types.DiffStorage) {
	destructs := make([]common.Address, 0, len(s.snapDestructs))
	for account := range s.snapDestructs {
		destructs = append(destructs, account)
	}
	accounts := make([]types.DiffAccount, 0, len(s.snapAccounts))
	for accountHash, account := range s.snapAccounts {
		accounts = append(accounts, types.DiffAccount{
			Account: accountHash,
			Blob:    account,
		})
	}
	storages := make([]types.DiffStorage, 0, len(s.snapStorage))
	for accountHash, storage := range s.snapStorage {
		keys := make([]string, 0, len(storage))
		values := make([][]byte, 0, len(storage))
		for k, v := range storage {
			keys = append(keys, k)
			values = append(values, v)
		}
		storages = append(storages, types.DiffStorage{
			Account: accountHash,
			Keys:    keys,
			Vals:    values,
		})
	}
	return destructs, accounts, storages
}

// PrepareAccessList handles the preparatory steps for executing a state transition with
// regards to both EIP-2929 and EIP-2930:
//
// - Add sender to access list (2929)
// - Add destination to access list (2929)
// - Add precompiles to access list (2929)
// - Add the contents of the optional tx access list (2930)
//
// This method should only be called if Berlin/2929+2930 is applicable at the current number.
func (s *StateDB) PrepareAccessList(sender common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	s.AddAddressToAccessList(sender)
	if dst != nil {
		s.AddAddressToAccessList(*dst)
		// If it's a create-tx, the destination will be added inside evm.create
	}
	for _, addr := range precompiles {
		s.AddAddressToAccessList(addr)
	}
	for _, el := range list {
		s.AddAddressToAccessList(el.Address)
		for _, key := range el.StorageKeys {
			s.AddSlotToAccessList(el.Address, key)
		}
	}
}

// AddAddressToAccessList adds the given address to the access list
func (s *StateDB) AddAddressToAccessList(addr common.Address) {
	if s.accessList == nil {
		s.accessList = newAccessList()
	}
	if s.accessList.AddAddress(addr) {
		s.journal.append(accessListAddAccountChange{&addr})
	}
}

// AddSlotToAccessList adds the given (address, slot)-tuple to the access list
func (s *StateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	if s.accessList == nil {
		s.accessList = newAccessList()
	}
	addrMod, slotMod := s.accessList.AddSlot(addr, slot)
	if addrMod {
		// In practice, this should not happen, since there is no way to enter the
		// scope of 'address' without having the 'address' become already added
		// to the access list (via call-variant, create, etc).
		// Better safe than sorry, though
		s.journal.append(accessListAddAccountChange{&addr})
	}
	if slotMod {
		s.journal.append(accessListAddSlotChange{
			address: &addr,
			slot:    &slot,
		})
	}
}

// AddressInAccessList returns true if the given address is in the access list.
func (s *StateDB) AddressInAccessList(addr common.Address) bool {
	if s.accessList == nil {
		return false
	}
	return s.accessList.ContainsAddress(addr)
}

// SlotInAccessList returns true if the given (address, slot)-tuple is in the access list.
func (s *StateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressPresent bool, slotPresent bool) {
	if s.accessList == nil {
		return false, false
	}
	return s.accessList.Contains(addr, slot)
}

func (s *StateDB) GetDirtyAccounts() []common.Address {
	accounts := make([]common.Address, 0, len(s.stateObjectsDirty))
	for account := range s.stateObjectsDirty {
		accounts = append(accounts, account)
	}
	return accounts
}

func (s *StateDB) GetStorage(address common.Address) *sync.Map {
	return s.storagePool.getStorage(address)
}
