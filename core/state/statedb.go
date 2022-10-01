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
	"bytes"
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

	WBNBAddress    = common.HexToAddress("0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c")
	parallelKvOnce sync.Once
)

type proofList [][]byte

func (n *proofList) Put(key []byte, value []byte) error {
	*n = append(*n, value)
	return nil
}

func (n *proofList) Delete(key []byte) error {
	panic("not supported")
}

type StateKeys map[common.Hash]struct{}

type StateObjectSyncMap struct {
	sync.Map
}

func (s *StateObjectSyncMap) LoadStateObject(addr common.Address) (*StateObject, bool) {
	stateObject, ok := s.Load(addr)
	if !ok {
		return nil, ok
	}
	return stateObject.(*StateObject), ok
}

func (s *StateObjectSyncMap) StoreStateObject(addr common.Address, stateObject *StateObject) {
	s.Store(addr, stateObject)
}

// loadStateObj is the entry for loading state object from stateObjects in StateDB or stateObjects in parallel
func (s *StateDB) loadStateObj(addr common.Address) (*StateObject, bool) {
	if s.isParallel {
		return s.parallel.stateObjects.LoadStateObject(addr)
	}
	obj, ok := s.stateObjects[addr]
	return obj, ok
}

// storeStateObj is the entry for storing state object to stateObjects in StateDB or stateObjects in parallel
func (s *StateDB) storeStateObj(addr common.Address, stateObject *StateObject) {
	if s.isParallel {
		// When a state object is stored into s.parallel.stateObjects,
		// it belongs to base StateDB, it is confirmed and valid.
		stateObject.db.storeParallelLock.Lock()
		s.parallel.stateObjects.Store(addr, stateObject)
		stateObject.db.storeParallelLock.Unlock()
	} else {
		s.stateObjects[addr] = stateObject
	}
}

// deleteStateObj is the entry for deleting state object to stateObjects in StateDB or stateObjects in parallel
func (s *StateDB) deleteStateObj(addr common.Address) {
	if s.isParallel {
		s.parallel.stateObjects.Delete(addr)
	} else {
		delete(s.stateObjects, addr)
	}
}

// ParallelState is for parallel mode only
type ParallelState struct {
	isSlotDB  bool // denotes StateDB is used in slot, we will try to remove it
	SlotIndex int  // for debug, to be removed
	// stateObjects holds the state objects in the base slot db
	// the reason for using stateObjects instead of stateObjects on the outside is
	// we need a thread safe map to hold state objects since there are many slots will read
	// state objects from it;
	// And we will merge all the changes made by the concurrent slot into it.
	stateObjects *StateObjectSyncMap

	baseStateDB               *StateDB // for parallel mode, there will be a base StateDB in dispatcher routine.
	baseTxIndex               int      // slotDB is created base on this tx index.
	dirtiedStateObjectsInSlot map[common.Address]*StateObject
	unconfirmedDBs            *sync.Map /*map[int]*ParallelStateDB*/ // do unconfirmed reference in same slot.

	// we will record the read detail for conflict check and
	// the changed addr or key for object merge, the changed detail can be achieved from the dirty object
	nonceChangesInSlot   map[common.Address]struct{}
	nonceReadsInSlot     map[common.Address]uint64
	balanceChangesInSlot map[common.Address]struct{} // the address's balance has been changed
	balanceReadsInSlot   map[common.Address]*big.Int // the address's balance has been read and used.
	// codeSize can be derived based on code, but codeHash can not be directly derived based on code
	// - codeSize is 0 for address not exist or empty code
	// - codeHash is `common.Hash{}` for address not exist, emptyCodeHash(`Keccak256Hash(nil)`) for empty code,
	// so we use codeReadsInSlot & codeHashReadsInSlot to keep code and codeHash, codeSize is derived from code
	codeReadsInSlot     map[common.Address][]byte // empty if address not exist or no code in this address
	codeHashReadsInSlot map[common.Address]common.Hash
	codeChangesInSlot   map[common.Address]struct{}
	kvReadsInSlot       map[common.Address]Storage
	kvChangesInSlot     map[common.Address]StateKeys // value will be kept in dirtiedStateObjectsInSlot
	// Actions such as SetCode, Suicide will change address's state.
	// Later call like Exist(), Empty(), HasSuicided() depend on the address's state.
	addrStateReadsInSlot   map[common.Address]bool // true: exist, false: not exist or deleted
	addrStateChangesInSlot map[common.Address]bool // true: created, false: deleted

	addrSnapDestructsReadsInSlot map[common.Address]bool

	// Transaction will pay gas fee to system address.
	// Parallel execution will clear system address's balance at first, in order to maintain transaction's
	// gas fee value. Normal transaction will access system address twice, otherwise it means the transaction
	// needs real system address's balance, the transaction will be marked redo with keepSystemAddressBalance = true
	systemAddress            common.Address
	systemAddressOpsCount    int
	keepSystemAddressBalance bool

	// we may need to redo for some specific reasons, like we read the wrong state and need to panic in sequential mode in SubRefund
	needsRedo bool
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

	snaps             *snapshot.Tree
	snap              snapshot.Snapshot
	snapAccountMux    sync.Mutex // Mutex for snap account access
	snapStorageMux    sync.Mutex // Mutex for snap storage access
	storeParallelLock sync.RWMutex
	snapParallelLock  sync.RWMutex // for parallel mode, for main StateDB, slot will read snapshot, while processor will write.
	snapDestructs     map[common.Address]struct{}
	snapAccounts      map[common.Address][]byte
	snapStorage       map[common.Address]map[string][]byte

	// This map holds 'live' objects, which will get modified while processing a state transition.
	stateObjects        map[common.Address]*StateObject
	stateObjectsPending map[common.Address]struct{} // State objects finalized but not yet written to the trie
	stateObjectsDirty   map[common.Address]struct{} // State objects modified in the current execution

	storagePool          *StoragePool // sharedPool to store L1 originStorage of stateObjects
	writeOnSharedStorage bool         // Write to the shared origin storage of a stateObject while reading from the underlying storage layer.
	isParallel           bool
	parallel             ParallelState // to keep all the parallel execution elements

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

// NewWithSharedPool creates a new state with sharedStorage on layer 1.5
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
		db:           db,
		originalRoot: root,
		snaps:        snaps,
		stateObjects: make(map[common.Address]*StateObject, defaultNumOfSlots),
		parallel: ParallelState{
			SlotIndex: -1,
		},
		stateObjectsPending: make(map[common.Address]struct{}, defaultNumOfSlots),
		stateObjectsDirty:   make(map[common.Address]struct{}, defaultNumOfSlots),
		txIndex:             -1,
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
	sdb.EnableWriteOnSharedStorage()
	return sdb, nil
}

func (s *StateDB) EnableWriteOnSharedStorage() {
	s.writeOnSharedStorage = true
}

func (s *StateDB) getBaseStateDB() *StateDB {
	return s
}

func (s *StateDB) getStateObjectFromStateObjects(addr common.Address) (*StateObject, bool) {
	return s.loadStateObj(addr)
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
	// s is a temporary throw away StateDB, s.prefetcher won't be reset to nil
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
	exist := s.getStateObject(addr) != nil
	log.Info("StateDB Exist", "exist", exist)
	return exist
}

// Empty returns whether the state object is either non-existent
// or empty according to the EIP161 specification (balance = nonce = code = 0)
func (s *StateDB) Empty(addr common.Address) bool {
	so := s.getStateObject(addr)
	return so == nil || so.empty()
}

// GetBalance retrieves the balance from the given address or 0 if object not found
func (s *StateDB) GetBalance(addr common.Address) *big.Int {
	balance := common.Big0
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		balance = stateObject.Balance()
	}
	return balance
}

func (s *StateDB) GetBalanceOpCode(addr common.Address) *big.Int {
	return s.GetBalance(addr)
}

func (s *StateDB) GetNonce(addr common.Address) uint64 {
	var nonce uint64 = 0
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		nonce = stateObject.Nonce()
	}

	return nonce
}

// TxIndex returns the current transaction index set by Prepare.
func (s *StateDB) TxIndex() int {
	return s.txIndex
}

// BaseTxIndex returns the tx index that slot db based.
func (s *StateDB) BaseTxIndex() int {
	return s.parallel.baseTxIndex
}

func (s *StateDB) GetCode(addr common.Address) []byte {
	stateObject := s.getStateObject(addr)
	var code []byte
	if stateObject != nil {
		code = stateObject.Code(s.db)
	}
	return code
}

func (s *StateDB) GetCodeSize(addr common.Address) int {
	var codeSize = 0
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		codeSize = stateObject.CodeSize(s.db)
	}
	return codeSize
}

// GetCodeHash return:
//  - common.Hash{}: the address does not exist
//  - emptyCodeHash: the address exist, but code is empty
//  - others:        the address exist, and code is not empty
func (s *StateDB) GetCodeHash(addr common.Address) common.Hash {
	stateObject := s.getStateObject(addr)
	codeHash := common.Hash{}
	if stateObject != nil {
		codeHash = common.BytesToHash(stateObject.CodeHash())
	}
	return codeHash
}

// GetState retrieves a value from the given account's storage trie.
func (s *StateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	val := common.Hash{}
	if stateObject != nil {
		val = stateObject.GetState(s.db, hash)
	}
	return val
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
	val := common.Hash{}
	if stateObject != nil {
		val = stateObject.GetCommittedState(s.db, hash)
	}
	return val
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
		codeHash := crypto.Keccak256Hash(code)
		stateObject.SetCode(codeHash, code)
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
		prev:        stateObject.suicided, // todo: must be false?
		prevbalance: new(big.Int).Set(s.GetBalance(addr)),
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

func (s *StateDB) getStateObjectFromSnapshotOrTrie(addr common.Address) (data *types.StateAccount, ok bool) {
	// If no live objects are available, attempt to use snapshots
	if s.snap != nil {
		start := time.Now()
		acc, err := s.snap.Account(crypto.HashData(s.hasher, addr.Bytes()))
		if metrics.EnabledExpensive {
			s.SnapshotAccountReads += time.Since(start)
		}
		if err == nil {
			if acc == nil {
				return nil, false
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
				return nil, false
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
			return nil, false
		}
		if len(enc) == 0 {
			return nil, false
		}
		data = new(types.StateAccount)
		if err := rlp.DecodeBytes(enc, data); err != nil {
			log.Error("Failed to decode state object", "addr", addr, "err", err)
			return nil, false
		}
	}
	return data, true
}

// getDeletedStateObject is similar to getStateObject, but instead of returning
// nil for a deleted state object, it returns the actual object with the deleted
// flag set. This is needed by the state journal to revert to the correct s-
// destructed object instead of wiping all knowledge about the state object.
func (s *StateDB) getDeletedStateObject(addr common.Address) *StateObject {
	// Prefer live objects if any is available
	if obj, _ := s.getStateObjectFromStateObjects(addr); obj != nil {
		return obj
	}
	data, ok := s.getStateObjectFromSnapshotOrTrie(addr)
	if !ok {
		return nil
	}
	// Insert into the live set
	obj := newObject(s, s.isParallel, addr, *data)
	s.storeStateObj(addr, obj)
	return obj
}

// GetOrNewStateObject retrieves a state object or create a new state object if nil.
func (s *StateDB) GetOrNewStateObject(addr common.Address) *StateObject {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		stateObject = s.createObject(addr)
	}
	return stateObject
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.

// prev is used for CreateAccount to get its balance
// Parallel mode:
// if prev in dirty:  revert is ok
// if prev in unconfirmed DB:  addr state read record, revert should not put it back
// if prev in main DB:  addr state read record, revert should not put it back
// if pre no exist:  addr state read record,

// `prev` is used to handle revert, to recover with the `prev` object
// In Parallel mode, we only need to recover to `prev` in SlotDB,
//  a.if it is not in SlotDB, `revert` will remove it from the SlotDB
//  b.if it is existed in SlotDB, `revert` will recover to the `prev` in SlotDB
//  c.as `snapDestructs` it is the same
func (s *StateDB) createObject(addr common.Address) (newobj *StateObject) {
	prev := s.getDeletedStateObject(addr) // Note, prev might have been deleted, we need that!
	var prevdestruct bool

	if s.snap != nil && prev != nil {
		s.snapParallelLock.Lock() // fixme: with new dispatch policy, the ending Tx could running, while the block have processed.
		_, prevdestruct = s.snapDestructs[prev.address]
		if !prevdestruct {
			// To destroy the previous trie node first and update the trie tree
			// with the new object on block commit.
			s.snapDestructs[prev.address] = struct{}{}
		}
		s.snapParallelLock.Unlock()
	}
	newobj = newObject(s, s.isParallel, addr, types.StateAccount{})
	newobj.setNonce(0) // sets the object to dirty
	if prev == nil {
		s.journal.append(createObjectChange{account: &addr})
	} else {
		s.journal.append(resetObjectChange{prev: prev, prevdestruct: prevdestruct})
	}

	s.storeStateObj(addr, newobj)
	return newobj
}

// CreateAccount explicitly creates a state object. If a state object with the address
// already exists the balance is carried over to the new account.
//
// CreateAccount is called during the EVM CREATE operation. The situation might arise that
// a contract does the following:
//
//   1. sends funds to sha(account ++ (nonce + 1))
//   2. tx_create(sha(account ++ nonce)) (note that this gets the address of 1)
//
// Carrying over the balance ensures that Ether doesn't disappear.
func (s *StateDB) CreateAccount(addr common.Address) {
	// no matter it is got from dirty, unconfirmed or main DB
	// if addr not exist, preBalance will be common.Big0, it is same as new(big.Int) which
	// is the value newObject(),
	preBalance := s.GetBalance(addr)
	newObj := s.createObject(addr)
	newObj.setBalance(new(big.Int).Set(preBalance)) // new big.Int for newObj
}

func (s *StateDB) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	so := s.getStateObject(addr)
	if so == nil {
		return nil
	}
	it := trie.NewIterator(so.getTrie(s.db).NodeIterator(nil))

	for it.Next() {
		key := common.BytesToHash(s.trie.GetKey(it.Key))
		if value, dirty := so.dirtyStorage.GetValue(key); dirty {
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
		parallel:            ParallelState{},
	}
	// Copy the dirty states, logs, and preimages
	for addr := range s.journal.dirties {
		// As documented [here](https://github.com/ethereum/go-ethereum/pull/16485#issuecomment-380438527),
		// and in the Finalise-method, there is a case where an object is in the journal but not
		// in the stateObjects: OOG after touch on ripeMD prior to Byzantium. Thus, we need to check for
		// nil
		if object, exist := s.getStateObjectFromStateObjects(addr); exist {
			// Even though the original object is dirty, we are not copying the journal,
			// so we need to make sure that anyside effect the journal would have caused
			// during a commit (or similar op) is already applied to the copy.
			state.storeStateObj(addr, object.deepCopy(state))

			state.stateObjectsDirty[addr] = struct{}{}   // Mark the copy dirty to force internal (code/state) commits
			state.stateObjectsPending[addr] = struct{}{} // Mark the copy pending to force external (account) commits
		}
	}
	// Above, we don't copy the actual journal. This means that if the copy is copied, the
	// loop above will be a no-op, since the copy's journal is empty.
	// Thus, here we iterate over stateObjects, to enable copies of copies
	for addr := range s.stateObjectsPending {
		if _, exist := state.getStateObjectFromStateObjects(addr); !exist {
			object, _ := s.getStateObjectFromStateObjects(addr)
			state.storeStateObj(addr, object.deepCopy(state))
		}
		state.stateObjectsPending[addr] = struct{}{}
	}
	for addr := range s.stateObjectsDirty {
		if _, exist := state.getStateObjectFromStateObjects(addr); !exist {
			object, _ := s.getStateObjectFromStateObjects(addr)
			state.storeStateObj(addr, object.deepCopy(state))
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
		// to the snapshot tree, we need to copy that as well.
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

var journalPool = sync.Pool{
	New: func() interface{} {
		return &journal{
			dirties: make(map[common.Address]int, defaultNumOfSlots),
			entries: make([]journalEntry, 0, defaultNumOfSlots),
		}
	},
}

var addressToStructPool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]struct{}, defaultNumOfSlots) },
}

var addressToStateKeysPool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]StateKeys, defaultNumOfSlots) },
}

var addressToStoragePool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]Storage, defaultNumOfSlots) },
}

var addressToStateObjectsPool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]*StateObject, defaultNumOfSlots) },
}

var balancePool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]*big.Int, defaultNumOfSlots) },
}

var addressToHashPool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]common.Hash, defaultNumOfSlots) },
}

var addressToBytesPool = sync.Pool{
	New: func() interface{} { return make(map[common.Address][]byte, defaultNumOfSlots) },
}

var addressToBoolPool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]bool, defaultNumOfSlots) },
}

var addressToUintPool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]uint64, defaultNumOfSlots) },
}

var snapStoragePool = sync.Pool{
	New: func() interface{} { return make(map[common.Address]map[string][]byte, defaultNumOfSlots) },
}

var snapStorageValuePool = sync.Pool{
	New: func() interface{} { return make(map[string][]byte, defaultNumOfSlots) },
}

var logsPool = sync.Pool{
	New: func() interface{} { return make(map[common.Hash][]*types.Log, defaultNumOfSlots) },
}

func (s *StateDB) PutSyncPool() {
	for key := range s.parallel.codeReadsInSlot {
		delete(s.parallel.codeReadsInSlot, key)
	}
	addressToStructPool.Put(s.parallel.codeReadsInSlot)

	for key := range s.parallel.codeHashReadsInSlot {
		delete(s.parallel.codeHashReadsInSlot, key)
	}
	addressToHashPool.Put(s.parallel.codeHashReadsInSlot)

	for key := range s.parallel.codeChangesInSlot {
		delete(s.parallel.codeChangesInSlot, key)
	}
	addressToStructPool.Put(s.parallel.codeChangesInSlot)

	for key := range s.parallel.kvChangesInSlot {
		delete(s.parallel.kvChangesInSlot, key)
	}
	addressToStateKeysPool.Put(s.parallel.kvChangesInSlot)

	for key := range s.parallel.kvReadsInSlot {
		delete(s.parallel.kvReadsInSlot, key)
	}
	addressToStoragePool.Put(s.parallel.kvReadsInSlot)

	for key := range s.parallel.balanceChangesInSlot {
		delete(s.parallel.balanceChangesInSlot, key)
	}
	addressToStructPool.Put(s.parallel.balanceChangesInSlot)

	for key := range s.parallel.balanceReadsInSlot {
		delete(s.parallel.balanceReadsInSlot, key)
	}
	balancePool.Put(s.parallel.balanceReadsInSlot)

	for key := range s.parallel.addrStateReadsInSlot {
		delete(s.parallel.addrStateReadsInSlot, key)
	}
	addressToBoolPool.Put(s.parallel.addrStateReadsInSlot)

	for key := range s.parallel.addrStateChangesInSlot {
		delete(s.parallel.addrStateChangesInSlot, key)
	}
	addressToBoolPool.Put(s.parallel.addrStateChangesInSlot)

	for key := range s.parallel.nonceChangesInSlot {
		delete(s.parallel.nonceChangesInSlot, key)
	}
	addressToStructPool.Put(s.parallel.nonceChangesInSlot)

	for key := range s.parallel.nonceReadsInSlot {
		delete(s.parallel.nonceReadsInSlot, key)
	}
	addressToUintPool.Put(s.parallel.nonceReadsInSlot)

	for key := range s.parallel.addrSnapDestructsReadsInSlot {
		delete(s.parallel.addrSnapDestructsReadsInSlot, key)
	}
	addressToBoolPool.Put(s.parallel.addrSnapDestructsReadsInSlot)

	for key := range s.parallel.dirtiedStateObjectsInSlot {
		delete(s.parallel.dirtiedStateObjectsInSlot, key)
	}
	addressToStateObjectsPool.Put(s.parallel.dirtiedStateObjectsInSlot)

	for key := range s.stateObjectsPending {
		delete(s.stateObjectsPending, key)
	}
	addressToStructPool.Put(s.stateObjectsPending)

	for key := range s.stateObjectsDirty {
		delete(s.stateObjectsDirty, key)
	}
	addressToStructPool.Put(s.stateObjectsDirty)

	for key := range s.logs {
		delete(s.logs, key)
	}
	logsPool.Put(s.logs)

	for key := range s.journal.dirties {
		delete(s.journal.dirties, key)
	}
	s.journal.entries = s.journal.entries[:0]
	journalPool.Put(s.journal)

	for key := range s.snapDestructs {
		delete(s.snapDestructs, key)
	}
	addressToStructPool.Put(s.snapDestructs)

	for key := range s.snapAccounts {
		delete(s.snapAccounts, key)
	}
	addressToBytesPool.Put(s.snapAccounts)

	for key, storage := range s.snapStorage {
		for key := range storage {
			delete(storage, key)
		}
		snapStorageValuePool.Put(storage)
		delete(s.snapStorage, key)
	}
	snapStoragePool.Put(s.snapStorage)
}

// CopyForSlot copy all the basic fields, initialize the memory ones
func (s *StateDB) CopyForSlot() *ParallelStateDB {
	parallel := ParallelState{
		// use base(dispatcher) slot db's stateObjects.
		// It is a SyncMap, only readable to slot, not writable
		stateObjects:                 s.parallel.stateObjects,
		codeReadsInSlot:              addressToBytesPool.Get().(map[common.Address][]byte),
		codeHashReadsInSlot:          addressToHashPool.Get().(map[common.Address]common.Hash),
		codeChangesInSlot:            addressToStructPool.Get().(map[common.Address]struct{}),
		kvChangesInSlot:              addressToStateKeysPool.Get().(map[common.Address]StateKeys),
		kvReadsInSlot:                addressToStoragePool.Get().(map[common.Address]Storage),
		balanceChangesInSlot:         addressToStructPool.Get().(map[common.Address]struct{}),
		balanceReadsInSlot:           balancePool.Get().(map[common.Address]*big.Int),
		addrStateReadsInSlot:         addressToBoolPool.Get().(map[common.Address]bool),
		addrStateChangesInSlot:       addressToBoolPool.Get().(map[common.Address]bool),
		nonceChangesInSlot:           addressToStructPool.Get().(map[common.Address]struct{}),
		nonceReadsInSlot:             addressToUintPool.Get().(map[common.Address]uint64),
		addrSnapDestructsReadsInSlot: addressToBoolPool.Get().(map[common.Address]bool),
		isSlotDB:                     true,
		dirtiedStateObjectsInSlot:    addressToStateObjectsPool.Get().(map[common.Address]*StateObject),
	}
	state := &ParallelStateDB{
		StateDB: StateDB{
			db:                  s.db,
			trie:                nil,                                   // Parallel StateDB can not access trie, since it is concurrent safe.
			stateObjects:        make(map[common.Address]*StateObject), // replaced by parallel.stateObjects in parallel mode
			stateObjectsPending: addressToStructPool.Get().(map[common.Address]struct{}),
			stateObjectsDirty:   addressToStructPool.Get().(map[common.Address]struct{}),
			refund:              0, // should be 0
			logs:                logsPool.Get().(map[common.Hash][]*types.Log),
			logSize:             0,
			preimages:           make(map[common.Hash][]byte, len(s.preimages)),
			journal:             journalPool.Get().(*journal),
			hasher:              crypto.NewKeccakState(),
			isParallel:          true,
			parallel:            parallel,
		},
		wbnbMakeUp: true,
	}
	// no need to copy preimages, comment out and remove later
	// for hash, preimage := range s.preimages {
	//	state.preimages[hash] = preimage
	// }

	if s.snaps != nil {
		// In order for the miner to be able to use and make additions
		// to the snapshot tree, we need to copy that as well.
		// Otherwise, any block mined by ourselves will cause gaps in the tree,
		// and force the miner to operate trie-backed only
		state.snaps = s.snaps
		state.snap = s.snap
		// deep copy needed
		state.snapDestructs = addressToStructPool.Get().(map[common.Address]struct{})
		s.snapParallelLock.RLock()
		for k, v := range s.snapDestructs {
			state.snapDestructs[k] = v
		}
		s.snapParallelLock.RUnlock()
		// snapAccounts is useless in SlotDB, comment out and remove later
		// state.snapAccounts = make(map[common.Address][]byte) // snapAccountPool.Get().(map[common.Address][]byte)
		// for k, v := range s.snapAccounts {
		//	state.snapAccounts[k] = v
		// }

		// snapStorage is useless in SlotDB either, it is updated on updateTrie, which is validation phase to update the snapshot of a finalized block.
		// state.snapStorage = snapStoragePool.Get().(map[common.Address]map[string][]byte)
		// for k, v := range s.snapStorage {
		//	temp := snapStorageValuePool.Get().(map[string][]byte)
		//	for kk, vv := range v {
		//		temp[kk] = vv
		//	}
		//	state.snapStorage[k] = temp
		// }

		// trie prefetch should be done by dispatcher on StateObject Merge,
		// disable it in parallel slot
		// state.prefetcher = s.prefetcher
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
func (s *StateDB) Finalise(deleteEmptyObjects bool) { // fixme: concurrent safe...
	addressesToPrefetch := make([][]byte, 0, len(s.journal.dirties))
	for addr := range s.journal.dirties {
		var obj *StateObject
		var exist bool
		if s.parallel.isSlotDB {
			obj = s.parallel.dirtiedStateObjectsInSlot[addr]
			if obj != nil {
				exist = true
			} else {
				log.Error("StateDB Finalise dirty addr not in dirtiedStateObjectsInSlot",
					"addr", addr)
			}
		} else {
			obj, exist = s.getStateObjectFromStateObjects(addr)
		}
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
			if s.parallel.isSlotDB {
				s.parallel.addrStateChangesInSlot[addr] = false // false: deleted
			}
			obj.deleted = true

			// If state snapshotting is active, also mark the destruction there.
			// Note, we can't do this only at the end of a block because multiple
			// transactions within the same block might self-destruct and then
			// ressurrect an account; but the snapshotter needs both events.
			if s.snap != nil {
				s.snapParallelLock.Lock()
				s.snapDestructs[obj.address] = struct{}{} // We need to maintain account deletions explicitly (will remain set indefinitely)
				s.snapParallelLock.Unlock()
				delete(s.snapAccounts, obj.address) // Clear out any previously updated account data (maybe recreated via a ressurrect)
				delete(s.snapStorage, obj.address)  // Clear out any previously updated storage data (maybe recreated via a ressurrect)
			}
		} else {
			// 1.none parallel mode, we do obj.finalise(true) as normal
			// 2.with parallel mode, we do obj.finalise(true) on dispatcher, not on slot routine
			//   obj.finalise(true) will clear its dirtyStorage, will make prefetch broken.
			if !s.isParallel || !s.parallel.isSlotDB {
				obj.finalise(true) // Prefetch slots in the background
			}
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

//CorrectAccountsRoot will fix account roots in pipecommit mode
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

//PopulateSnapAccountAndStorage tries to populate required accounts and storages for pipecommit
func (s *StateDB) PopulateSnapAccountAndStorage() {
	for addr := range s.stateObjectsPending {
		if obj, _ := s.getStateObjectFromStateObjects(addr); !obj.deleted {
			if s.snap != nil {
				s.populateSnapStorage(obj)
				s.snapAccounts[obj.address] = snapshot.SlimAccountRLP(obj.data.Nonce, obj.data.Balance, obj.data.Root, obj.data.CodeHash)
			}
		}
	}
}

//populateSnapStorage tries to populate required storages for pipecommit, and returns a flag to indicate whether the storage root changed or not
func (s *StateDB) populateSnapStorage(obj *StateObject) bool {
	obj.dirtyStorage.Range(func(key, value interface{}) bool {
		obj.pendingStorage.StoreValue(key.(common.Hash), value.(common.Hash))
		return true
	})
	if obj.pendingStorage.Length() == 0 {
		return false
	}
	var storage map[string][]byte
	obj.pendingStorage.Range(func(keyItf, valueItf interface{}) bool {
		key := keyItf.(common.Hash)
		value := valueItf.(common.Hash)
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
		return true
	})
	return true
}

func (s *StateDB) AccountsIntermediateRoot() {
	tasks := make(chan func()) // use buffer chan?
	finishCh := make(chan struct{})
	defer close(finishCh)
	wg := sync.WaitGroup{}
	for i := 0; i < runtime.NumCPU(); i++ { // more the cpu num since there are async IO operation
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
	// first, giving the account prefetches just a few more milliseconds of time
	// to pull useful data from disk.
	for addr := range s.stateObjectsPending {
		if obj, _ := s.getStateObjectFromStateObjects(addr); !obj.deleted {
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
			if obj, _ := s.getStateObjectFromStateObjects(addr); obj.deleted {
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
	s.validRevisions = s.validRevisions[:0] // Snapshots can be created without journal entries
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
				if obj, _ := s.getStateObjectFromStateObjects(addr); !obj.deleted {
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
				if obj, _ := s.getStateObjectFromStateObjects(addr); !obj.deleted {
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

// PrepareForParallel prepares for state db to be used in parallel execution mode.
func (s *StateDB) PrepareForParallel() {
	s.isParallel = true
	s.parallel.stateObjects = &StateObjectSyncMap{}
}

func (s *StateDB) AddrPrefetch(slotDb *ParallelStateDB) {
	addressesToPrefetch := make([][]byte, 0, len(slotDb.parallel.dirtiedStateObjectsInSlot))
	for addr, obj := range slotDb.parallel.dirtiedStateObjectsInSlot {
		addressesToPrefetch = append(addressesToPrefetch, common.CopyBytes(addr[:])) // Copy needed for closure
		if obj.deleted {
			continue
		}
		// copied from obj.finalise(true)
		slotsToPrefetch := make([][]byte, 0, obj.dirtyStorage.Length())
		obj.dirtyStorage.Range(func(key, value interface{}) bool {
			originalValue, _ := obj.originStorage.GetValue(key.(common.Hash))
			if value.(common.Hash) != originalValue {
				originalKey := key.(common.Hash)
				slotsToPrefetch = append(slotsToPrefetch, common.CopyBytes(originalKey[:])) // Copy needed for closure
			}
			return true
		})
		if s.prefetcher != nil && len(slotsToPrefetch) > 0 && obj.data.Root != emptyRoot {
			s.prefetcher.prefetch(obj.data.Root, slotsToPrefetch, obj.addrHash)
		}
	}

	if s.prefetcher != nil && len(addressesToPrefetch) > 0 {
		// log.Info("AddrPrefetch", "slotDb.TxIndex", slotDb.TxIndex(),
		//	"len(addressesToPrefetch)", len(slotDb.parallel.addressesToPrefetch))
		s.prefetcher.prefetch(s.originalRoot, addressesToPrefetch, emptyAddr)
	}
}

// MergeSlotDB is for Parallel execution mode, when the transaction has been
// finalized(dirty -> pending) on execution slot, the execution results should be
// merged back to the main StateDB.
func (s *StateDB) MergeSlotDB(slotDb *ParallelStateDB, slotReceipt *types.Receipt, txIndex int) {
	// receipt.Logs use unified log index within a block
	// align slotDB's log index to the block stateDB's logSize
	for _, l := range slotReceipt.Logs {
		l.Index += s.logSize
	}
	s.logSize += slotDb.logSize

	// before merge, pay the gas fee first: AddBalance to consensus.SystemAddress
	systemAddress := slotDb.parallel.systemAddress
	if slotDb.parallel.keepSystemAddressBalance {
		s.SetBalance(systemAddress, slotDb.GetBalance(systemAddress))
	} else {
		s.AddBalance(systemAddress, slotDb.GetBalance(systemAddress))
	}
	// system address is EOA account, it should have no storage change
	delete(slotDb.stateObjectsDirty, systemAddress)
	// only merge dirty objects
	addressesToPrefetch := make([][]byte, 0, len(slotDb.stateObjectsDirty))
	for addr := range slotDb.stateObjectsDirty {
		if _, exist := s.stateObjectsDirty[addr]; !exist {
			s.stateObjectsDirty[addr] = struct{}{}
		}

		// stateObjects: KV, balance, nonce...
		dirtyObj, ok := slotDb.parallel.dirtiedStateObjectsInSlot[addr]
		if !ok {
			log.Error("parallel merge, but dirty object not exist!", "SlotIndex", slotDb.parallel.SlotIndex, "txIndex:", slotDb.txIndex, "addr", addr)
			continue
		}
		mainObj, exist := s.loadStateObj(addr)
		if !exist { // fixme: it is also state change
			// addr not exist on main DB, do ownership transfer
			// dirtyObj.db = s
			// dirtyObj.finalise(true) // true: prefetch on dispatcher
			mainObj = dirtyObj.deepCopy(s)
			if addr == WBNBAddress && slotDb.wbnbMakeUpBalance != nil {
				mainObj.setBalance(slotDb.wbnbMakeUpBalance)
			}
			mainObj.finalise(true)
			s.storeStateObj(addr, mainObj)
			// fixme: should not delete, would cause unconfirmed DB incorrect?
			// delete(slotDb.parallel.dirtiedStateObjectsInSlot, addr) // transfer ownership, fixme: shared read?
			if dirtyObj.deleted {
				// remove the addr from snapAccounts&snapStorage only when object is deleted.
				// "deleted" is not equal to "snapDestructs", since createObject() will add an addr for
				//  snapDestructs to destroy previous object, while it will keep the addr in snapAccounts & snapAccounts
				delete(s.snapAccounts, addr)
				delete(s.snapStorage, addr)
			}
		} else {
			// addr already in main DB, do merge: balance, KV, code, State(create, suicide)
			// can not do copy or ownership transfer directly, since dirtyObj could have outdated
			// data(maybe updated within the conflict window)

			var newMainObj = mainObj // we don't need to copy the object since the storages are thread safe
			if _, ok := slotDb.parallel.addrStateChangesInSlot[addr]; ok {
				// there are 3 kinds of state change:
				// 1.Suicide
				// 2.Empty Delete
				// 3.createObject
				//   a: AddBalance,SetState to a non-exist or deleted(suicide, empty delete) address.
				//   b: CreateAccount: like DAO the fork, regenerate an account carry its balance without KV
				// For these state change, do ownership transfer for efficiency:
				// dirtyObj.db = s
				// newMainObj = dirtyObj
				newMainObj = dirtyObj.deepCopy(s)
				// should not delete, would cause unconfirmed DB incorrect.
				// delete(slotDb.parallel.dirtiedStateObjectsInSlot, addr) // transfer ownership, fixme: shared read?
				if dirtyObj.deleted {
					// remove the addr from snapAccounts&snapStorage only when object is deleted.
					// "deleted" is not equal to "snapDestructs", since createObject() will add an addr for
					//  snapDestructs to destroy previous object, while it will keep the addr in snapAccounts & snapAccounts
					delete(s.snapAccounts, addr)
					delete(s.snapStorage, addr)
				}
			} else {
				// deepCopy a temporary *StateObject for safety, since slot could read the address,
				// dispatch should avoid overwrite the StateObject directly otherwise, it could
				// crash for: concurrent map iteration and map write

				if _, balanced := slotDb.parallel.balanceChangesInSlot[addr]; balanced {
					newMainObj.setBalance(dirtyObj.Balance())
				}
				if _, coded := slotDb.parallel.codeChangesInSlot[addr]; coded {
					newMainObj.code = dirtyObj.code
					newMainObj.data.CodeHash = dirtyObj.data.CodeHash
					newMainObj.dirtyCode = true
				}
				if keys, stated := slotDb.parallel.kvChangesInSlot[addr]; stated {
					newMainObj.MergeSlotObject(s.db, dirtyObj, keys)
				}
				if _, nonced := slotDb.parallel.nonceChangesInSlot[addr]; nonced {
					// dirtyObj.Nonce() should not be less than newMainObj
					newMainObj.setNonce(dirtyObj.Nonce())
				}
			}
			if addr == WBNBAddress && slotDb.wbnbMakeUpBalance != nil {
				newMainObj.setBalance(slotDb.wbnbMakeUpBalance)
			}
			newMainObj.finalise(true) // true: prefetch on dispatcher
			// update the object
			s.storeStateObj(addr, newMainObj)
		}
		addressesToPrefetch = append(addressesToPrefetch, common.CopyBytes(addr[:])) // Copy needed for closure
	}

	if s.prefetcher != nil && len(addressesToPrefetch) > 0 {
		s.prefetcher.prefetch(s.originalRoot, addressesToPrefetch, emptyAddr) // prefetch for trie node of account
	}

	for addr := range slotDb.stateObjectsPending {
		if _, exist := s.stateObjectsPending[addr]; !exist {
			s.stateObjectsPending[addr] = struct{}{}
		}
	}

	// slotDb.logs: logs will be kept in receipts, no need to do merge

	for hash, preimage := range slotDb.preimages {
		s.preimages[hash] = preimage
	}
	if s.accessList != nil {
		// fixme: accessList is not enabled yet, but it should use merge rather than overwrite Copy
		s.accessList = slotDb.accessList.Copy()
	}

	if slotDb.snaps != nil {
		for k := range slotDb.snapDestructs {
			// There could be a race condition for parallel transaction execution
			// One transaction add balance 0 to an empty address, will delete it(delete empty is enabled).
			// While another concurrent transaction could add a none-zero balance to it, make it not empty
			// We fixed it by add an addr state read record for add balance 0
			s.snapParallelLock.Lock()
			s.snapDestructs[k] = struct{}{}
			s.snapParallelLock.Unlock()
		}

		// slotDb.snapAccounts should be empty, comment out and to be deleted later
		// for k, v := range slotDb.snapAccounts {
		//	s.snapAccounts[k] = v
		// }
		// slotDb.snapStorage should be empty, comment out and to be deleted later
		// for k, v := range slotDb.snapStorage {
		// 	temp := make(map[string][]byte)
		//	for kk, vv := range v {
		//		temp[kk] = vv
		//	}
		//	s.snapStorage[k] = temp
		// }
	}
	s.txIndex = txIndex
}

func (s *StateDB) ParallelMakeUp(common.Address, []byte) {
	// do nothing, this API is for parallel mode
}

type ParallelKvCheckUnit struct {
	addr common.Address
	key  common.Hash
	val  common.Hash
}
type ParallelKvCheckMessage struct {
	slotDB   *ParallelStateDB
	isStage2 bool
	kvUnit   ParallelKvCheckUnit
}

var parallelKvCheckReqCh chan ParallelKvCheckMessage
var parallelKvCheckResCh chan bool

type ParallelStateDB struct {
	StateDB
	wbnbMakeUp        bool // default true, we can not do WBNB make up if its absolute balance is used.
	wbnbMakeUpBalance *big.Int
}

func hasKvConflict(slotDB *ParallelStateDB, addr common.Address, key common.Hash, val common.Hash, isStage2 bool) bool {
	mainDB := slotDB.parallel.baseStateDB

	if isStage2 { // update slotDB's unconfirmed DB list and try
		if valUnconfirm, ok := slotDB.getKVFromUnconfirmedDB(addr, key); ok {
			if !bytes.Equal(val.Bytes(), valUnconfirm.Bytes()) {
				log.Debug("IsSlotDBReadsValid KV read is invalid in unconfirmed", "addr", addr,
					"valSlot", val, "valUnconfirm", valUnconfirm,
					"SlotIndex", slotDB.parallel.SlotIndex,
					"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
				return true
			}
		}
	}
	valMain := mainDB.GetState(addr, key)
	if !bytes.Equal(val.Bytes(), valMain.Bytes()) {
		log.Debug("hasKvConflict is invalid", "addr", addr,
			"key", key, "valSlot", val,
			"valMain", valMain, "SlotIndex", slotDB.parallel.SlotIndex,
			"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
		return true // return false, Range will be terminated.
	}
	return false
}

// StartKvCheckLoop start several routines to do conflict check
func StartKvCheckLoop() {
	parallelKvCheckReqCh = make(chan ParallelKvCheckMessage, 200)
	parallelKvCheckResCh = make(chan bool, 10)
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for {
				kvEle1 := <-parallelKvCheckReqCh
				parallelKvCheckResCh <- hasKvConflict(kvEle1.slotDB, kvEle1.kvUnit.addr,
					kvEle1.kvUnit.key, kvEle1.kvUnit.val, kvEle1.isStage2)
			}
		}()
	}
}

// NewSlotDB creates a new State DB based on the provided StateDB.
// With parallel, each execution slot would have its own StateDB.
func NewSlotDB(db *StateDB, systemAddr common.Address, txIndex int, baseTxIndex int, keepSystem bool,
	unconfirmedDBs *sync.Map /*map[int]*ParallelStateDB*/) *ParallelStateDB {
	slotDB := db.CopyForSlot()
	slotDB.txIndex = txIndex
	slotDB.originalRoot = db.originalRoot
	slotDB.parallel.baseStateDB = db
	slotDB.parallel.baseTxIndex = baseTxIndex
	slotDB.parallel.systemAddress = systemAddr
	slotDB.parallel.systemAddressOpsCount = 0
	slotDB.parallel.keepSystemAddressBalance = keepSystem
	slotDB.storagePool = NewStoragePool()
	slotDB.EnableWriteOnSharedStorage()
	slotDB.parallel.unconfirmedDBs = unconfirmedDBs

	// All transactions will pay gas fee to the systemAddr at the end, this address is
	// deemed to conflict, we handle it specially, clear it now and set it back to the main
	// StateDB later;
	// But there are transactions that will try to read systemAddr's balance, such as:
	// https://bscscan.com/tx/0xcd69755be1d2f55af259441ff5ee2f312830b8539899e82488a21e85bc121a2a.
	// It will trigger transaction redo and keepSystem will be marked as true.
	if !keepSystem {
		slotDB.SetBalance(systemAddr, big.NewInt(0))
	}

	return slotDB
}

// RevertSlotDB keep the Read list for conflict detect,
// discard all state changes except:
//   - nonce and balance of from address
//   - balance of system address: will be used on merge to update SystemAddress's balance
func (s *ParallelStateDB) RevertSlotDB(from common.Address) {
	s.parallel.kvChangesInSlot = make(map[common.Address]StateKeys)

	s.parallel.nonceChangesInSlot = make(map[common.Address]struct{})
	s.parallel.balanceChangesInSlot = make(map[common.Address]struct{}, 1)
	s.parallel.addrStateChangesInSlot = make(map[common.Address]bool) // 0: created, 1: deleted

	selfStateObject := s.parallel.dirtiedStateObjectsInSlot[from]
	systemAddress := s.parallel.systemAddress
	systemStateObject := s.parallel.dirtiedStateObjectsInSlot[systemAddress]
	s.parallel.dirtiedStateObjectsInSlot = make(map[common.Address]*StateObject, 2)
	// keep these elements
	s.parallel.dirtiedStateObjectsInSlot[from] = selfStateObject
	s.parallel.dirtiedStateObjectsInSlot[systemAddress] = systemStateObject
	s.parallel.balanceChangesInSlot[from] = struct{}{}
	s.parallel.balanceChangesInSlot[systemAddress] = struct{}{}
	s.parallel.nonceChangesInSlot[from] = struct{}{}
}

func (s *ParallelStateDB) getBaseStateDB() *StateDB {
	return &s.StateDB
}

func (s *ParallelStateDB) SetSlotIndex(index int) {
	s.parallel.SlotIndex = index
}

// for parallel execution mode, try to get dirty StateObject in slot first.
// it is mainly used by journal revert right now.
func (s *ParallelStateDB) getStateObject(addr common.Address) *StateObject {
	if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
		return obj
	}
	// can not call s.StateDB.getStateObject(), since `newObject` need ParallelStateDB as the interface
	return s.getStateObjectNoSlot(addr)
}

func (s *ParallelStateDB) storeStateObj(addr common.Address, stateObject *StateObject) {
	// When a state object is stored into s.parallel.stateObjects,
	// it belongs to base StateDB, it is confirmed and valid.
	stateObject.db = s.parallel.baseStateDB
	stateObject.dbItf = s.parallel.baseStateDB
	// the object could be created in SlotDB, if it got the object from DB and
	// update it to the shared `s.parallel.stateObjects``
	stateObject.db.storeParallelLock.Lock()
	if _, ok := s.parallel.stateObjects.Load(addr); !ok {
		s.parallel.stateObjects.Store(addr, stateObject)
	}
	stateObject.db.storeParallelLock.Unlock()
}

func (s *ParallelStateDB) getStateObjectNoSlot(addr common.Address) *StateObject {
	if obj := s.getDeletedStateObject(addr); obj != nil && !obj.deleted {
		return obj
	}
	return nil
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.

// prev is used for CreateAccount to get its balance
// Parallel mode:
// if prev in dirty:  revert is ok
// if prev in unconfirmed DB:  addr state read record, revert should not put it back
// if prev in main DB:  addr state read record, revert should not put it back
// if pre no exist:  addr state read record,

// `prev` is used to handle revert, to recover with the `prev` object
// In Parallel mode, we only need to recover to `prev` in SlotDB,
//  a.if it is not in SlotDB, `revert` will remove it from the SlotDB
//  b.if it is existed in SlotDB, `revert` will recover to the `prev` in SlotDB
//  c.as `snapDestructs` it is the same
func (s *ParallelStateDB) createObject(addr common.Address) (newobj *StateObject) {
	// do not get from unconfirmed DB, since it will have problem on revert
	prev := s.parallel.dirtiedStateObjectsInSlot[addr]

	var prevdestruct bool

	if s.snap != nil && prev != nil {
		s.snapParallelLock.Lock()
		_, prevdestruct = s.snapDestructs[prev.address]
		s.parallel.addrSnapDestructsReadsInSlot[addr] = prevdestruct
		if !prevdestruct {
			// To destroy the previous trie node first and update the trie tree
			// with the new object on block commit.
			s.snapDestructs[prev.address] = struct{}{}
		}
		s.snapParallelLock.Lock()

	}
	newobj = newObject(s, s.isParallel, addr, types.StateAccount{})
	newobj.setNonce(0) // sets the object to dirty
	if prev == nil {
		s.journal.append(createObjectChange{account: &addr})
	} else {
		s.journal.append(resetObjectChange{prev: prev, prevdestruct: prevdestruct})
	}

	s.parallel.addrStateChangesInSlot[addr] = true // the object sis created
	s.parallel.nonceChangesInSlot[addr] = struct{}{}
	s.parallel.balanceChangesInSlot[addr] = struct{}{}
	s.parallel.codeChangesInSlot[addr] = struct{}{}
	// notice: all the KVs are cleared if any
	s.parallel.kvChangesInSlot[addr] = make(StateKeys)
	return newobj
}

// getDeletedStateObject is similar to getStateObject, but instead of returning
// nil for a deleted state object, it returns the actual object with the deleted
// flag set. This is needed by the state journal to revert to the correct s-
// destructed object instead of wiping all knowledge about the state object.
func (s *ParallelStateDB) getDeletedStateObject(addr common.Address) *StateObject {
	// Prefer live objects if any is available
	if obj, _ := s.getStateObjectFromStateObjects(addr); obj != nil {
		return obj
	}
	data, ok := s.getStateObjectFromSnapshotOrTrie(addr)
	if !ok {
		return nil
	}
	// this is why we have to use a separate getDeletedStateObject for ParallelStateDB
	// `s` has to be the ParallelStateDB
	obj := newObject(s, s.isParallel, addr, *data)
	s.storeStateObj(addr, obj)
	return obj
}

// GetOrNewStateObject retrieves a state object or create a new state object if nil.
// dirtyInSlot -> Unconfirmed DB -> main DB -> snapshot, no? create one
func (s *ParallelStateDB) GetOrNewStateObject(addr common.Address) *StateObject {
	var stateObject *StateObject = nil
	if stateObject, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
		return stateObject
	}

	stateObject, _ = s.getStateObjectFromUnconfirmedDB(addr)
	if stateObject == nil {
		stateObject = s.getStateObjectNoSlot(addr) // try to get from base db
	}

	exist := true
	if stateObject == nil || stateObject.deleted || stateObject.suicided {
		stateObject = s.createObject(addr)
		exist = false
	}

	s.parallel.addrStateReadsInSlot[addr] = exist // true: exist, false: not exist
	return stateObject
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for suicided accounts.
func (s *ParallelStateDB) Exist(addr common.Address) bool {
	// 1.Try to get from dirty
	if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
		// dirty object should not be deleted, since deleted is only flagged on finalise
		// and if it is suicided in contract call, suicide is taken as exist until it is finalised
		// todo: add a check here, to be removed later
		if obj.deleted || obj.suicided {
			log.Error("Exist in dirty, but marked as deleted or suicided",
				"txIndex", s.txIndex, "baseTxIndex:", s.parallel.baseTxIndex)
		}
		return true
	}
	// 2.Try to get from unconfirmed & main DB
	// 2.1 Already read before
	if exist, ok := s.parallel.addrStateReadsInSlot[addr]; ok {
		return exist
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if exist, ok := s.getAddrStateFromUnconfirmedDB(addr); ok {
		s.parallel.addrStateReadsInSlot[addr] = exist // update and cache
		return exist
	}

	// 3.Try to get from main StateDB
	exist := s.getStateObjectNoSlot(addr) != nil
	s.parallel.addrStateReadsInSlot[addr] = exist // update and cache
	return exist
}

// Empty returns whether the state object is either non-existent
// or empty according to the EIP161 specification (balance = nonce = code = 0)
func (s *ParallelStateDB) Empty(addr common.Address) bool {
	// 1.Try to get from dirty
	if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
		// dirty object is light copied and fixup on need,
		// empty could be wrong, except it is created with this TX
		if _, ok := s.parallel.addrStateChangesInSlot[addr]; ok {
			return obj.empty()
		}
		// so we have to check it manually
		// empty means: Nonce == 0 && Balance == 0 && CodeHash == emptyCodeHash
		if s.GetBalance(addr).Sign() != 0 { // check balance first, since it is most likely not zero
			return false
		}
		if s.GetNonce(addr) != 0 {
			return false
		}
		codeHash := s.GetCodeHash(addr)
		return bytes.Equal(codeHash.Bytes(), emptyCodeHash) // code is empty, the object is empty
	}
	// 2.Try to get from unconfirmed & main DB
	// 2.1 Already read before
	if exist, ok := s.parallel.addrStateReadsInSlot[addr]; ok {
		// exist means not empty
		return !exist
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if exist, ok := s.getAddrStateFromUnconfirmedDB(addr); ok {
		s.parallel.addrStateReadsInSlot[addr] = exist // update and cache
		return !exist
	}

	so := s.getStateObjectNoSlot(addr)
	empty := so == nil || so.empty()
	s.parallel.addrStateReadsInSlot[addr] = !empty // update and cache
	return empty
}

// GetBalance retrieves the balance from the given address or 0 if object not found
// GetFrom the dirty list => from unconfirmed DB => get from main stateDB
func (s *ParallelStateDB) GetBalance(addr common.Address) *big.Int {
	if addr == s.parallel.systemAddress {
		s.parallel.systemAddressOpsCount++
	}
	// 1.Try to get from dirty
	if _, ok := s.parallel.balanceChangesInSlot[addr]; ok {
		if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
			// on balance fixup, addr may not exist in dirtiedStateObjectsInSlot
			// we intend to fixup balance based on unconfirmed DB or main DB
			return obj.Balance()
		}
	}
	// 2.Try to get from unconfirmed DB or main DB
	// 2.1 Already read before
	if balance, ok := s.parallel.balanceReadsInSlot[addr]; ok {
		return balance
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if balance := s.getBalanceFromUnconfirmedDB(addr); balance != nil {
		s.parallel.balanceReadsInSlot[addr] = balance
		return balance
	}

	// 3. Try to get from main StateObject
	balance := common.Big0
	stateObject := s.getStateObjectNoSlot(addr)
	if stateObject != nil {
		balance = stateObject.Balance()
	}
	s.parallel.balanceReadsInSlot[addr] = balance
	return balance
}

// GetBalanceOpCode different from GetBalance(), it is opcode triggered
func (s *ParallelStateDB) GetBalanceOpCode(addr common.Address) *big.Int {
	if addr == WBNBAddress {
		s.wbnbMakeUp = false
	}
	return s.GetBalance(addr)
}

func (s *ParallelStateDB) GetNonce(addr common.Address) uint64 {
	// 1.Try to get from dirty
	if _, ok := s.parallel.nonceChangesInSlot[addr]; ok {
		if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
			// on nonce fixup, addr may not exist in dirtiedStateObjectsInSlot
			// we intend to fixup nonce based on unconfirmed DB or main DB
			return obj.Nonce()
		}
	}
	// 2.Try to get from unconfirmed DB or main DB
	// 2.1 Already read before
	if nonce, ok := s.parallel.nonceReadsInSlot[addr]; ok {
		return nonce
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if nonce, ok := s.getNonceFromUnconfirmedDB(addr); ok {
		s.parallel.nonceReadsInSlot[addr] = nonce
		return nonce
	}

	// 3.Try to get from main StateDB
	var nonce uint64 = 0
	stateObject := s.getStateObjectNoSlot(addr)
	if stateObject != nil {
		nonce = stateObject.Nonce()
	}
	s.parallel.nonceReadsInSlot[addr] = nonce
	return nonce
}

func (s *ParallelStateDB) GetCode(addr common.Address) []byte {
	// 1.Try to get from dirty
	if _, ok := s.parallel.codeChangesInSlot[addr]; ok {
		if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
			// on code fixup, addr may not exist in dirtiedStateObjectsInSlot
			// we intend to fixup code based on unconfirmed DB or main DB
			code := obj.Code(s.db)
			return code
		}
	}
	// 2.Try to get from unconfirmed DB or main DB
	// 2.1 Already read before
	if code, ok := s.parallel.codeReadsInSlot[addr]; ok {
		return code
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if code, ok := s.getCodeFromUnconfirmedDB(addr); ok {
		s.parallel.codeReadsInSlot[addr] = code
		return code
	}

	// 3. Try to get from main StateObject
	stateObject := s.getStateObjectNoSlot(addr)
	var code []byte
	if stateObject != nil {
		code = stateObject.Code(s.db)
	}
	s.parallel.codeReadsInSlot[addr] = code
	return code
}

func (s *ParallelStateDB) GetCodeSize(addr common.Address) int {
	// 1.Try to get from dirty
	if _, ok := s.parallel.codeChangesInSlot[addr]; ok {
		if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
			// on code fixup, addr may not exist in dirtiedStateObjectsInSlot
			// we intend to fixup code based on unconfirmed DB or main DB
			return obj.CodeSize(s.db)
		}
	}
	// 2.Try to get from unconfirmed DB or main DB
	// 2.1 Already read before
	if code, ok := s.parallel.codeReadsInSlot[addr]; ok {
		return len(code) // len(nil) is 0 too
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if code, ok := s.getCodeFromUnconfirmedDB(addr); ok {
		s.parallel.codeReadsInSlot[addr] = code
		return len(code) // len(nil) is 0 too
	}

	// 3. Try to get from main StateObject
	var codeSize = 0
	var code []byte
	stateObject := s.getStateObjectNoSlot(addr)

	if stateObject != nil {
		code = stateObject.Code(s.db)
		codeSize = stateObject.CodeSize(s.db)
	}
	s.parallel.codeReadsInSlot[addr] = code
	return codeSize
}

// GetCodeHash return:
//  - common.Hash{}: the address does not exist
//  - emptyCodeHash: the address exist, but code is empty
//  - others:        the address exist, and code is not empty
func (s *ParallelStateDB) GetCodeHash(addr common.Address) common.Hash {
	// 1.Try to get from dirty
	if _, ok := s.parallel.codeChangesInSlot[addr]; ok {
		if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
			// on code fixup, addr may not exist in dirtiedStateObjectsInSlot
			// we intend to fixup balance based on unconfirmed DB or main DB
			return common.BytesToHash(obj.CodeHash())
		}
	}
	// 2.Try to get from unconfirmed DB or main DB
	// 2.1 Already read before
	if codeHash, ok := s.parallel.codeHashReadsInSlot[addr]; ok {
		return codeHash
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if codeHash, ok := s.getCodeHashFromUnconfirmedDB(addr); ok {
		s.parallel.codeHashReadsInSlot[addr] = codeHash
		return codeHash
	}
	// 3. Try to get from main StateObject
	stateObject := s.getStateObjectNoSlot(addr)
	codeHash := common.Hash{}
	if stateObject != nil {
		codeHash = common.BytesToHash(stateObject.CodeHash())
	}
	s.parallel.codeHashReadsInSlot[addr] = codeHash
	return codeHash
}

// GetState retrieves a value from the given account's storage trie.
// For parallel mode wih, get from the state in order:
//   -> self dirty, both Slot & MainProcessor
//   -> pending of self: Slot on merge
//   -> pending of unconfirmed DB
//   -> pending of main StateDB
//   -> origin
func (s *ParallelStateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	// 1.Try to get from dirty
	if exist, ok := s.parallel.addrStateChangesInSlot[addr]; ok {
		if !exist {
			// it could be suicided within this SlotDB?
			// it should be able to get state from suicided address within a Tx:
			// e.g. within a transaction: call addr:suicide -> get state: should be ok
			// return common.Hash{}
			log.Info("ParallelStateDB GetState suicided", "addr", addr, "hash", hash)
		} else {
			obj := s.parallel.dirtiedStateObjectsInSlot[addr] // addr must exist in dirtiedStateObjectsInSlot
			return obj.GetState(s.db, hash)
		}
	}
	if keys, ok := s.parallel.kvChangesInSlot[addr]; ok {
		if _, ok := keys[hash]; ok {
			obj := s.parallel.dirtiedStateObjectsInSlot[addr] // addr must exist in dirtiedStateObjectsInSlot
			return obj.GetState(s.db, hash)
		}
	}
	// 2.Try to get from unconfirmed DB or main DB
	// 2.1 Already read before
	if storage, ok := s.parallel.kvReadsInSlot[addr]; ok {
		if val, ok := storage.GetValue(hash); ok {
			return val
		}
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if val, ok := s.getKVFromUnconfirmedDB(addr, hash); ok {
		if s.parallel.kvReadsInSlot[addr] == nil {
			s.parallel.kvReadsInSlot[addr] = newStorage(false)
		}
		s.parallel.kvReadsInSlot[addr].StoreValue(hash, val) // update cache
		return val
	}

	// 3.Get from main StateDB
	stateObject := s.getStateObjectNoSlot(addr)
	val := common.Hash{}
	if stateObject != nil {
		val = stateObject.GetState(s.db, hash)
	}
	if s.parallel.kvReadsInSlot[addr] == nil {
		s.parallel.kvReadsInSlot[addr] = newStorage(false)
	}
	s.parallel.kvReadsInSlot[addr].StoreValue(hash, val) // update cache
	return val
}

// GetCommittedState retrieves a value from the given account's committed storage trie.
func (s *ParallelStateDB) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	// 1.No need to get from pending of itself even on merge, since stateobject in SlotDB won't do finalise
	// 2.Try to get from unconfirmed DB or main DB
	//   KVs in unconfirmed DB can be seen as pending storage
	//   KVs in main DB are merged from SlotDB and has done finalise() on merge, can be seen as pending storage too.
	// 2.1 Already read before
	if storage, ok := s.parallel.kvReadsInSlot[addr]; ok {
		if val, ok := storage.GetValue(hash); ok {
			return val
		}
	}
	// 2.2 Try to get from unconfirmed DB if exist
	if val, ok := s.getKVFromUnconfirmedDB(addr, hash); ok {
		if s.parallel.kvReadsInSlot[addr] == nil {
			s.parallel.kvReadsInSlot[addr] = newStorage(false)
		}
		s.parallel.kvReadsInSlot[addr].StoreValue(hash, val) // update cache
		return val
	}

	// 3. Try to get from main DB
	stateObject := s.getStateObjectNoSlot(addr)
	val := common.Hash{}
	if stateObject != nil {
		val = stateObject.GetCommittedState(s.db, hash)
	}
	if s.parallel.kvReadsInSlot[addr] == nil {
		s.parallel.kvReadsInSlot[addr] = newStorage(false)
	}
	s.parallel.kvReadsInSlot[addr].StoreValue(hash, val) // update cache
	return val
}

func (s *ParallelStateDB) HasSuicided(addr common.Address) bool {
	// 1.Try to get from dirty
	if obj, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; ok {
		return obj.suicided
	}
	// 2.Try to get from unconfirmed
	if exist, ok := s.getAddrStateFromUnconfirmedDB(addr); ok {
		return !exist
	}

	stateObject := s.getStateObjectNoSlot(addr)
	if stateObject != nil {
		return stateObject.suicided
	}
	return false
}

// AddBalance adds amount to the account associated with addr.
func (s *ParallelStateDB) AddBalance(addr common.Address, amount *big.Int) {
	// add balance will perform a read operation first
	// if amount == 0, no balance change, but there is still an empty check.
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		if addr == s.parallel.systemAddress {
			s.parallel.systemAddressOpsCount++
		}
		if _, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; !ok {
			newStateObject := stateObject.lightCopy(s) // light copy from main DB
			// do balance fixup from the confirmed DB, it could be more reliable than main DB
			balance := s.GetBalance(addr) // it will record the balance read operation
			newStateObject.setBalance(balance)
			newStateObject.AddBalance(amount)
			s.parallel.dirtiedStateObjectsInSlot[addr] = newStateObject
			s.parallel.balanceChangesInSlot[addr] = struct{}{}
			return
		}
		// already dirty, make sure the balance is fixed up since it could be previously dirtied by nonce or KV...
		if addr != s.parallel.systemAddress {
			balance := s.GetBalance(addr)
			if stateObject.Balance().Cmp(balance) != 0 {
				log.Warn("AddBalance in dirty, but balance has not do fixup", "txIndex", s.txIndex, "addr", addr,
					"stateObject.Balance()", stateObject.Balance(), "s.GetBalance(addr)", balance)
				stateObject.setBalance(balance)
			}
		}

		stateObject.AddBalance(amount)
		s.parallel.balanceChangesInSlot[addr] = struct{}{}
	}
}

// SubBalance subtracts amount from the account associated with addr.
func (s *ParallelStateDB) SubBalance(addr common.Address, amount *big.Int) {
	// unlike add, sub 0 balance will not touch empty object
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		if addr == s.parallel.systemAddress {
			s.parallel.systemAddressOpsCount++
		}

		if _, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; !ok {
			newStateObject := stateObject.lightCopy(s) // light copy from main DB
			// do balance fixup from the confirmed DB, it could be more reliable than main DB
			balance := s.GetBalance(addr)
			newStateObject.setBalance(balance)
			newStateObject.SubBalance(amount)
			s.parallel.balanceChangesInSlot[addr] = struct{}{}
			s.parallel.dirtiedStateObjectsInSlot[addr] = newStateObject
			return
		}
		// already dirty, make sure the balance is fixed up since it could be previously dirtied by nonce or KV...		if addr != s.parallel.systemAddress {
		if addr != s.parallel.systemAddress {
			balance := s.GetBalance(addr)
			if stateObject.Balance().Cmp(balance) != 0 {
				log.Warn("SubBalance in dirty, but balance is incorrect", "txIndex", s.txIndex, "addr", addr,
					"stateObject.Balance()", stateObject.Balance(), "s.GetBalance(addr)", balance)
				stateObject.setBalance(balance)
			}
		}

		stateObject.SubBalance(amount)
		s.parallel.balanceChangesInSlot[addr] = struct{}{}
	}
}

func (s *ParallelStateDB) SetBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		if addr == s.parallel.systemAddress {
			s.parallel.systemAddressOpsCount++
		}
		if _, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; !ok {
			newStateObject := stateObject.lightCopy(s)
			// update balance for revert, in case child contract is reverted,
			// it should revert to the previous balance
			balance := s.GetBalance(addr)
			newStateObject.setBalance(balance)
			newStateObject.SetBalance(amount)
			s.parallel.balanceChangesInSlot[addr] = struct{}{}
			s.parallel.dirtiedStateObjectsInSlot[addr] = newStateObject
			return
		}
		// do balance fixup
		if addr != s.parallel.systemAddress {
			balance := s.GetBalance(addr)
			stateObject.setBalance(balance)
		}
		stateObject.SetBalance(amount)
		s.parallel.balanceChangesInSlot[addr] = struct{}{}
	}
}

func (s *ParallelStateDB) SetNonce(addr common.Address, nonce uint64) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		if _, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; !ok {
			newStateObject := stateObject.lightCopy(s)
			noncePre := s.GetNonce(addr)
			newStateObject.setNonce(noncePre) // nonce fixup
			newStateObject.SetNonce(nonce)
			s.parallel.nonceChangesInSlot[addr] = struct{}{}
			s.parallel.dirtiedStateObjectsInSlot[addr] = newStateObject
			return
		}
		noncePre := s.GetNonce(addr)
		stateObject.setNonce(noncePre) // nonce fixup

		stateObject.SetNonce(nonce)
		s.parallel.nonceChangesInSlot[addr] = struct{}{}
	}
}

func (s *ParallelStateDB) SetCode(addr common.Address, code []byte) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		codeHash := crypto.Keccak256Hash(code)
		if _, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; !ok {
			newStateObject := stateObject.lightCopy(s)
			codePre := s.GetCode(addr) // code fixup
			codeHashPre := crypto.Keccak256Hash(codePre)
			newStateObject.setCode(codeHashPre, codePre)

			newStateObject.SetCode(codeHash, code)
			s.parallel.dirtiedStateObjectsInSlot[addr] = newStateObject
			s.parallel.codeChangesInSlot[addr] = struct{}{}
			return
		}
		codePre := s.GetCode(addr) // code fixup
		codeHashPre := crypto.Keccak256Hash(codePre)
		stateObject.setCode(codeHashPre, codePre)

		stateObject.SetCode(codeHash, code)
		s.parallel.codeChangesInSlot[addr] = struct{}{}
	}
}

func (s *ParallelStateDB) SetState(addr common.Address, key, value common.Hash) {
	stateObject := s.GetOrNewStateObject(addr) // attention: if StateObject's lightCopy, its storage is only a part of the full storage,
	if stateObject != nil {
		if s.parallel.baseTxIndex+1 == s.txIndex {
			// we check if state is unchanged
			// only when current transaction is the next transaction to be committed
			// fixme: there is a bug, block: 14,962,284,
			//        stateObject is in dirty (light copy), but the key is in mainStateDB
			//        stateObject dirty -> committed, will skip mainStateDB dirty
			if s.GetState(addr, key) == value {
				log.Debug("Skip set same state", "baseTxIndex", s.parallel.baseTxIndex,
					"txIndex", s.txIndex, "addr", addr,
					"key", key, "value", value)
				return
			}
		}

		if s.parallel.kvChangesInSlot[addr] == nil {
			s.parallel.kvChangesInSlot[addr] = make(StateKeys) // make(Storage, defaultNumOfSlots)
		}

		if _, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; !ok {
			newStateObject := stateObject.lightCopy(s)
			newStateObject.SetState(s.db, key, value)
			s.parallel.dirtiedStateObjectsInSlot[addr] = newStateObject
			return
		}
		// do State Update
		stateObject.SetState(s.db, key, value)
	}
}

// Suicide marks the given account as suicided.
// This clears the account balance.
//
// The account's state object is still available until the state is committed,
// getStateObject will return a non-nil account after Suicide.
func (s *ParallelStateDB) Suicide(addr common.Address) bool {
	var stateObject *StateObject
	// 1.Try to get from dirty, it could be suicided inside of contract call
	stateObject = s.parallel.dirtiedStateObjectsInSlot[addr]
	if stateObject == nil {
		// 2.Try to get from unconfirmed, if deleted return false, since the address does not exist
		if obj, ok := s.getStateObjectFromUnconfirmedDB(addr); ok {
			stateObject = obj
			s.parallel.addrStateReadsInSlot[addr] = !stateObject.deleted // true: exist, false: deleted
			if stateObject.deleted {
				log.Error("Suicide addr already deleted in confirmed DB", "txIndex", s.txIndex, "addr", addr)
				return false
			}
		}
	}

	if stateObject == nil {
		// 3.Try to get from main StateDB
		stateObject = s.getStateObjectNoSlot(addr)
		if stateObject == nil {
			s.parallel.addrStateReadsInSlot[addr] = false // true: exist, false: deleted
			log.Error("Suicide addr not exist", "txIndex", s.txIndex, "addr", addr)
			return false
		}
		s.parallel.addrStateReadsInSlot[addr] = true // true: exist, false: deleted
	}

	s.journal.append(suicideChange{
		account:     &addr,
		prev:        stateObject.suicided, // todo: must be false?
		prevbalance: new(big.Int).Set(s.GetBalance(addr)),
	})

	if _, ok := s.parallel.dirtiedStateObjectsInSlot[addr]; !ok {
		// do copy-on-write for suicide "write"
		newStateObject := stateObject.lightCopy(s)
		newStateObject.markSuicided()
		newStateObject.data.Balance = new(big.Int)
		s.parallel.dirtiedStateObjectsInSlot[addr] = newStateObject
		s.parallel.addrStateChangesInSlot[addr] = false // false: the address does not exist any more,
		// s.parallel.nonceChangesInSlot[addr] = struct{}{}
		s.parallel.balanceChangesInSlot[addr] = struct{}{}
		s.parallel.codeChangesInSlot[addr] = struct{}{}
		// s.parallel.kvChangesInSlot[addr] = make(StateKeys) // all key changes are discarded
		return true
	}
	s.parallel.addrStateChangesInSlot[addr] = false // false: the address does not exist anymore
	s.parallel.balanceChangesInSlot[addr] = struct{}{}
	s.parallel.codeChangesInSlot[addr] = struct{}{}

	stateObject.markSuicided()
	stateObject.data.Balance = new(big.Int)
	return true
}

// CreateAccount explicitly creates a state object. If a state object with the address
// already exists the balance is carried over to the new account.
//
// CreateAccount is called during the EVM CREATE operation. The situation might arise that
// a contract does the following:
//
//   1. sends funds to sha(account ++ (nonce + 1))
//   2. tx_create(sha(account ++ nonce)) (note that this gets the address of 1)
//
// Carrying over the balance ensures that Ether doesn't disappear.
func (s *ParallelStateDB) CreateAccount(addr common.Address) {
	// no matter it is got from dirty, unconfirmed or main DB
	// if addr not exist, preBalance will be common.Big0, it is same as new(big.Int) which
	// is the value newObject(),
	preBalance := s.GetBalance(addr) // parallel balance read will be recorded inside GetBalance
	newObj := s.createObject(addr)
	newObj.setBalance(new(big.Int).Set(preBalance)) // new big.Int for newObj
}

// RevertToSnapshot reverts all state changes made since the given revision.
func (s *ParallelStateDB) RevertToSnapshot(revid int) {
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

// AddRefund adds gas to the refund counter
// journal.append will use ParallelState for revert
func (s *ParallelStateDB) AddRefund(gas uint64) { // todo: not needed, can be deleted
	s.journal.append(refundChange{prev: s.refund})
	s.refund += gas
}

// SubRefund removes gas from the refund counter.
// This method will panic if the refund counter goes below zero
func (s *ParallelStateDB) SubRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	if gas > s.refund {
		// we don't need to panic here if we read the wrong state in parallel mode
		// we just need to redo this transaction
		log.Info(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, s.refund), "tx", s.thash.String())
		s.parallel.needsRedo = true
		return
	}
	s.refund -= gas
}

// For Parallel Execution Mode, it can be seen as Penetrated Access:
//   -------------------------------------------------------
//   | BaseTxIndex | Unconfirmed Txs... | Current TxIndex |
//   -------------------------------------------------------
// Access from the unconfirmed DB with range&priority:  txIndex -1(previous tx) -> baseTxIndex + 1
func (s *ParallelStateDB) getBalanceFromUnconfirmedDB(addr common.Address) *big.Int {
	if addr == s.parallel.systemAddress {
		// never get systemaddress from unconfirmed DB
		return nil
	}

	for i := s.txIndex - 1; i > s.parallel.baseStateDB.txIndex; i-- {
		db_, ok := s.parallel.unconfirmedDBs.Load(i)
		if !ok {
			continue
		}
		db := db_.(*ParallelStateDB)
		// 1.Refer the state of address, exist or not in dirtiedStateObjectsInSlot
		balanceHit := false
		if _, exist := db.parallel.addrStateChangesInSlot[addr]; exist {
			balanceHit = true
		}
		if _, exist := db.parallel.balanceChangesInSlot[addr]; exist { // only changed balance is reliable
			balanceHit = true
		}
		if !balanceHit {
			continue
		}
		obj := db.parallel.dirtiedStateObjectsInSlot[addr]
		balance := obj.Balance()
		if obj.deleted {
			balance = common.Big0
		}
		return balance

	}
	return nil
}

// Similar to getBalanceFromUnconfirmedDB
func (s *ParallelStateDB) getNonceFromUnconfirmedDB(addr common.Address) (uint64, bool) {
	if addr == s.parallel.systemAddress {
		// never get systemaddress from unconfirmed DB
		return 0, false
	}

	for i := s.txIndex - 1; i > s.parallel.baseStateDB.txIndex; i-- {
		db_, ok := s.parallel.unconfirmedDBs.Load(i)
		if !ok {
			continue
		}
		db := db_.(*ParallelStateDB)

		nonceHit := false
		if _, ok := db.parallel.addrStateChangesInSlot[addr]; ok {
			nonceHit = true
		} else if _, ok := db.parallel.nonceChangesInSlot[addr]; ok {
			nonceHit = true
		}
		if !nonceHit {
			// nonce refer not hit, try next unconfirmedDb
			continue
		}
		// nonce hit, return the nonce
		obj := db.parallel.dirtiedStateObjectsInSlot[addr]
		if obj == nil {
			// could not exist, if it is changed but reverted
			// fixme: revert should remove the change record
			log.Debug("Get nonce from UnconfirmedDB, changed but object not exist, ",
				"txIndex", s.txIndex, "referred txIndex", i, "addr", addr)
			continue
		}
		nonce := obj.Nonce()
		// deleted object with nonce == 0
		if obj.deleted {
			nonce = 0
		}
		return nonce, true
	}
	return 0, false
}

// Similar to getBalanceFromUnconfirmedDB
// It is not only for code, but also codeHash and codeSize, we return the *StateObject for convenience.
func (s *ParallelStateDB) getCodeFromUnconfirmedDB(addr common.Address) ([]byte, bool) {
	if addr == s.parallel.systemAddress {
		// never get systemaddress from unconfirmed DB
		return nil, false
	}

	for i := s.txIndex - 1; i > s.parallel.baseStateDB.txIndex; i-- {
		db_, ok := s.parallel.unconfirmedDBs.Load(i)
		if !ok {
			continue
		}
		db := db_.(*ParallelStateDB)

		codeHit := false
		if _, exist := db.parallel.addrStateChangesInSlot[addr]; exist {
			codeHit = true
		}
		if _, exist := db.parallel.codeChangesInSlot[addr]; exist {
			codeHit = true
		}
		if !codeHit {
			// try next unconfirmedDb
			continue
		}
		obj := db.parallel.dirtiedStateObjectsInSlot[addr]
		if obj == nil {
			// could not exist, if it is changed but reverted
			// fixme: revert should remove the change record
			log.Debug("Get code from UnconfirmedDB, changed but object not exist, ",
				"txIndex", s.txIndex, "referred txIndex", i, "addr", addr)
			continue
		}
		code := obj.Code(s.db)
		if obj.deleted {
			code = nil
		}
		return code, true

	}
	return nil, false
}

// Similar to getCodeFromUnconfirmedDB
// but differ when address is deleted or not exist
func (s *ParallelStateDB) getCodeHashFromUnconfirmedDB(addr common.Address) (common.Hash, bool) {
	if addr == s.parallel.systemAddress {
		// never get systemaddress from unconfirmed DB
		return common.Hash{}, false
	}

	for i := s.txIndex - 1; i > s.parallel.baseStateDB.txIndex; i-- {
		db_, ok := s.parallel.unconfirmedDBs.Load(i)
		if !ok {
			continue
		}
		db := db_.(*ParallelStateDB)
		hashHit := false
		if _, exist := db.parallel.addrStateChangesInSlot[addr]; exist {
			hashHit = true
		}
		if _, exist := db.parallel.codeChangesInSlot[addr]; exist {
			hashHit = true
		}
		if !hashHit {
			// try next unconfirmedDb
			continue
		}
		obj := db.parallel.dirtiedStateObjectsInSlot[addr]
		if obj == nil {
			// could not exist, if it is changed but reverted
			// fixme: revert should remove the change record
			log.Debug("Get codeHash from UnconfirmedDB, changed but object not exist, ",
				"txIndex", s.txIndex, "referred txIndex", i, "addr", addr)
			continue
		}
		codeHash := common.Hash{}
		if !obj.deleted {
			codeHash = common.BytesToHash(obj.CodeHash())
		}
		return codeHash, true
	}
	return common.Hash{}, false
}

// Similar to getCodeFromUnconfirmedDB
// It is for address state check of: Exist(), Empty() and HasSuicided()
// Since the unconfirmed DB should have done Finalise() with `deleteEmptyObjects = true`
// If the dirty address is empty or suicided, it will be marked as deleted, so we only need to return `deleted` or not.
func (s *ParallelStateDB) getAddrStateFromUnconfirmedDB(addr common.Address) (bool, bool) {
	if addr == s.parallel.systemAddress {
		// never get systemaddress from unconfirmed DB
		return false, false
	}

	// check the unconfirmed DB with range:  baseTxIndex -> txIndex -1(previous tx)
	for i := s.txIndex - 1; i > s.parallel.baseStateDB.txIndex; i-- {
		db_, ok := s.parallel.unconfirmedDBs.Load(i)
		if !ok {
			continue
		}
		db := db_.(*ParallelStateDB)
		if exist, ok := db.parallel.addrStateChangesInSlot[addr]; ok {
			if _, ok := db.parallel.dirtiedStateObjectsInSlot[addr]; !ok {
				// could not exist, if it is changed but reverted
				// fixme: revert should remove the change record
				log.Debug("Get addr State from UnconfirmedDB, changed but object not exist, ",
					"txIndex", s.txIndex, "referred txIndex", i, "addr", addr)
				continue
			}

			return exist, true
		}
	}
	return false, false
}

func (s *ParallelStateDB) getKVFromUnconfirmedDB(addr common.Address, key common.Hash) (common.Hash, bool) {
	// check the unconfirmed DB with range:  baseTxIndex -> txIndex -1(previous tx)
	for i := s.txIndex - 1; i > s.parallel.baseStateDB.txIndex; i-- {
		db_, ok := s.parallel.unconfirmedDBs.Load(i)
		if !ok {
			continue
		}
		db := db_.(*ParallelStateDB)
		if _, ok := db.parallel.kvChangesInSlot[addr]; ok {
			obj := db.parallel.dirtiedStateObjectsInSlot[addr]
			if val, exist := obj.dirtyStorage.GetValue(key); exist {
				return val, true
			}
		}
	}
	return common.Hash{}, false
}

func (s *ParallelStateDB) getStateObjectFromUnconfirmedDB(addr common.Address) (*StateObject, bool) {
	// check the unconfirmed DB with range:  baseTxIndex -> txIndex -1(previous tx)
	for i := s.txIndex - 1; i > s.parallel.baseStateDB.txIndex; i-- {
		db_, ok := s.parallel.unconfirmedDBs.Load(i)
		if !ok {
			continue
		}
		db := db_.(*ParallelStateDB)
		if obj, ok := db.parallel.dirtiedStateObjectsInSlot[addr]; ok {
			return obj, true
		}
	}
	return nil, false
}

// IsParallelReadsValid If stage2 is true, it is a likely conflict check,
// to detect these potential conflict results in advance and schedule redo ASAP.
func (s *ParallelStateDB) IsParallelReadsValid(isStage2 bool) bool {
	parallelKvOnce.Do(func() {
		StartKvCheckLoop()
	})
	slotDB := s
	mainDB := slotDB.parallel.baseStateDB
	// for nonce
	for addr, nonceSlot := range slotDB.parallel.nonceReadsInSlot {
		if isStage2 { // update slotDB's unconfirmed DB list and try
			if nonceUnconfirm, ok := slotDB.getNonceFromUnconfirmedDB(addr); ok {
				if nonceSlot != nonceUnconfirm {
					log.Debug("IsSlotDBReadsValid nonce read is invalid in unconfirmed", "addr", addr,
						"nonceSlot", nonceSlot, "nonceUnconfirm", nonceUnconfirm, "SlotIndex", slotDB.parallel.SlotIndex,
						"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
					return false
				}
			}
		}
		nonceMain := mainDB.GetNonce(addr)
		if nonceSlot != nonceMain {
			log.Debug("IsSlotDBReadsValid nonce read is invalid", "addr", addr,
				"nonceSlot", nonceSlot, "nonceMain", nonceMain, "SlotIndex", slotDB.parallel.SlotIndex,
				"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
			return false
		}
	}
	// balance
	for addr, balanceSlot := range slotDB.parallel.balanceReadsInSlot {
		if isStage2 { // update slotDB's unconfirmed DB list and try
			if balanceUnconfirm := slotDB.getBalanceFromUnconfirmedDB(addr); balanceUnconfirm != nil {
				if balanceSlot.Cmp(balanceUnconfirm) == 0 {
					continue
				}
				if addr == WBNBAddress && slotDB.WBNBMakeUp() {
					log.Debug("IsSlotDBReadsValid skip makeup for WBNB in stage 2",
						"SlotIndex", slotDB.parallel.SlotIndex, "txIndex", slotDB.txIndex)
					continue // stage2 will skip WBNB check, no balance makeup
				}
				return false
			}
		}

		if addr != slotDB.parallel.systemAddress { // skip balance check for system address
			balanceMain := mainDB.GetBalance(addr)
			if balanceSlot.Cmp(balanceMain) != 0 {
				if addr == WBNBAddress && slotDB.WBNBMakeUp() { // WBNB balance make up
					if isStage2 {
						log.Debug("IsSlotDBReadsValid skip makeup for WBNB in stage 2",
							"SlotIndex", slotDB.parallel.SlotIndex, "txIndex", slotDB.txIndex)
						continue // stage2 will skip WBNB check, no balance makeup
					}
					if _, ok := s.parallel.balanceChangesInSlot[addr]; !ok {
						// balance unchanged, no need to make up
						log.Debug("IsSlotDBReadsValid WBNB balance no makeup since it is not changed ",
							"SlotIndex", slotDB.parallel.SlotIndex, "txIndex", slotDB.txIndex,
							"updated WBNB balance", slotDB.GetBalance(addr))
						continue
					}
					balanceDelta := new(big.Int).Sub(balanceMain, balanceSlot)
					slotDB.wbnbMakeUpBalance = new(big.Int).Add(slotDB.GetBalance(addr), balanceDelta)
					/*
						if _, exist := slotDB.stateObjectsPending[addr]; !exist {
							slotDB.stateObjectsPending[addr] = struct{}{}
						}
						if _, exist := slotDB.stateObjectsDirty[addr]; !exist {
							// only read, but never change WBNB's balance or state
							// log.Warn("IsSlotDBReadsValid balance makeup for WBNB, but it is not in dirty",
							//	"SlotIndex", slotDB.parallel.SlotIndex, "txIndex", slotDB.txIndex)
							slotDB.stateObjectsDirty[addr] = struct{}{}
						}
					*/
					log.Debug("IsSlotDBReadsValid balance makeup for WBNB",
						"SlotIndex", slotDB.parallel.SlotIndex, "txIndex", slotDB.txIndex,
						"updated WBNB balance", slotDB.GetBalance(addr))
					continue
				}

				log.Debug("IsSlotDBReadsValid balance read is invalid", "addr", addr,
					"balanceSlot", balanceSlot, "balanceMain", balanceMain, "SlotIndex", slotDB.parallel.SlotIndex,
					"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
				return false
			}
		}
	}
	// check KV
	var units []ParallelKvCheckUnit // todo: pre-allocate to make it faster
	for addr, read := range slotDB.parallel.kvReadsInSlot {
		read.Range(func(keySlot, valSlot interface{}) bool {
			units = append(units, ParallelKvCheckUnit{addr, keySlot.(common.Hash), valSlot.(common.Hash)})
			return true
		})
	}
	readLen := len(units)
	if readLen < 8 || isStage2 {
		for _, unit := range units {
			if hasKvConflict(slotDB, unit.addr, unit.key, unit.val, isStage2) {
				return false
			}
		}
	} else {
		msgHandledNum := 0
		msgSendNum := 0
		for _, unit := range units {
			for { // make sure the unit is consumed
				consumed := false
				select {
				case conflict := <-parallelKvCheckResCh:
					msgHandledNum++
					if conflict {
						// make sure all request are handled or discarded
						for {
							if msgHandledNum == msgSendNum {
								break
							}
							select {
							case <-parallelKvCheckReqCh:
								msgHandledNum++
							case <-parallelKvCheckResCh:
								msgHandledNum++
							}
						}
						return false
					}
				case parallelKvCheckReqCh <- ParallelKvCheckMessage{slotDB, isStage2, unit}:
					msgSendNum++
					consumed = true
				}
				if consumed {
					break
				}
			}
		}
		for {
			if msgHandledNum == readLen {
				break
			}
			conflict := <-parallelKvCheckResCh
			msgHandledNum++
			if conflict {
				// make sure all request are handled or discarded
				for {
					if msgHandledNum == msgSendNum {
						break
					}
					select {
					case <-parallelKvCheckReqCh:
						msgHandledNum++
					case <-parallelKvCheckResCh:
						msgHandledNum++
					}
				}
				return false
			}
		}
	}
	if isStage2 { // stage2 skip check code, or state, since they are likely unchanged.
		return true
	}

	// check code
	for addr, codeSlot := range slotDB.parallel.codeReadsInSlot {
		codeMain := mainDB.GetCode(addr)
		if !bytes.Equal(codeSlot, codeMain) {
			log.Debug("IsSlotDBReadsValid code read is invalid", "addr", addr,
				"len codeSlot", len(codeSlot), "len codeMain", len(codeMain), "SlotIndex", slotDB.parallel.SlotIndex,
				"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
			return false
		}
	}
	// check codeHash
	for addr, codeHashSlot := range slotDB.parallel.codeHashReadsInSlot {
		codeHashMain := mainDB.GetCodeHash(addr)
		if !bytes.Equal(codeHashSlot.Bytes(), codeHashMain.Bytes()) {
			log.Debug("IsSlotDBReadsValid codehash read is invalid", "addr", addr,
				"codeHashSlot", codeHashSlot, "codeHashMain", codeHashMain, "SlotIndex", slotDB.parallel.SlotIndex,
				"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
			return false
		}
	}
	// addr state check
	for addr, stateSlot := range slotDB.parallel.addrStateReadsInSlot {
		stateMain := false // addr not exist
		if mainDB.getStateObject(addr) != nil {
			stateMain = true // addr exist in main DB
		}
		if stateSlot != stateMain {
			// skip addr state check for system address
			if addr != slotDB.parallel.systemAddress {
				log.Debug("IsSlotDBReadsValid addrState read invalid(true: exist, false: not exist)",
					"addr", addr, "stateSlot", stateSlot, "stateMain", stateMain,
					"SlotIndex", slotDB.parallel.SlotIndex,
					"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
				return false
			}
		}
	}
	// snapshot destructs check
	for addr, destructRead := range slotDB.parallel.addrSnapDestructsReadsInSlot {
		mainObj := mainDB.getStateObject(addr)
		if mainObj == nil {
			log.Debug("IsSlotDBReadsValid snapshot destructs read invalid, address should exist",
				"addr", addr, "destruct", destructRead,
				"SlotIndex", slotDB.parallel.SlotIndex,
				"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
			return false
		}
		slotDB.snapParallelLock.RLock()               // fixme: this lock is not needed
		_, destructMain := mainDB.snapDestructs[addr] // addr not exist
		slotDB.snapParallelLock.RUnlock()
		if destructRead != destructMain {
			log.Debug("IsSlotDBReadsValid snapshot destructs read invalid",
				"addr", addr, "destructRead", destructRead, "destructMain", destructMain,
				"SlotIndex", slotDB.parallel.SlotIndex,
				"txIndex", slotDB.txIndex, "baseTxIndex", slotDB.parallel.baseTxIndex)
			return false
		}
	}

	return true
}

// SystemAddressRedo
// For most of the transactions, systemAddressOpsCount should be 3:
//  - one for SetBalance(0) on NewSlotDB()
//  - the second is for AddBalance(GasFee) at the end,
//  - the third is for GetBalance() which is triggered by AddBalance()
// (systemAddressOpsCount > 3) means the transaction tries to access systemAddress, in this case, the
// transaction needs the accurate systemAddress info, then it should redo and keep its balance on NewSlotDB()
// for example:
// https://bscscan.com/tx/0xe469f1f948de90e9508f96da59a96ed84b818e71432ca11c5176eb60eb66671b
func (s *ParallelStateDB) SystemAddressRedo() bool {
	if s.parallel.systemAddressOpsCount > 4 {
		log.Info("SystemAddressRedo", "SlotIndex", s.parallel.SlotIndex,
			"txIndex", s.txIndex,
			"systemAddressOpsCount", s.parallel.systemAddressOpsCount)
		return true
	}
	return false
}

// NeedsRedo returns true if there is any clear reason that we need to redo this transaction
func (s *ParallelStateDB) NeedsRedo() bool {
	return s.parallel.needsRedo
}

// WBNBMakeUp
// WBNB makeup is allowed only when its balance is accessed through contract Call.
// If it is accessed not through contract all, e.g., by `address.balance`, `address.transfer(amount)`,
// we can not do balance make up.
func (s *ParallelStateDB) WBNBMakeUp() bool {
	return s.wbnbMakeUp
}

func (s *ParallelStateDB) ParallelMakeUp(addr common.Address, input []byte) {
	if addr == WBNBAddress {
		if len(input) < 4 {
			// should never less than 4
			// log.Warn("ParallelMakeUp for WBNB input size invalid", "input size", len(input), "input", input)
			s.wbnbMakeUp = false
			return
		}
		// EVM use big-endian mode, so as the MethodID
		wbnbDeposit := []byte{0xd0, 0xe3, 0x0d, 0xb0}      // "0xd0e30db0": Keccak-256("deposit()")
		wbnbWithdraw := []byte{0x2e, 0x1a, 0x7d, 0x4d}     // "0x2e1a7d4d": Keccak-256("withdraw(uint256)")
		wbnbApprove := []byte{0x09, 0x5e, 0xa7, 0xb3}      // "0x095ea7b3": Keccak-256("approve(address,uint256)")
		wbnbTransfer := []byte{0xa9, 0x05, 0x9c, 0xbb}     // "0xa9059cbb": Keccak-256("transfer(address,uint256)")
		wbnbTransferFrom := []byte{0x23, 0xb8, 0x72, 0xdd} // "0x23b872dd": Keccak-256("transferFrom(address,address,uint256)")
		// wbnbTotalSupply := []byte{0x18, 0x16, 0x0d, 0xdd}  // "0x18160ddd": Keccak-256("totalSupply()")
		// unknown WBNB interface 1: {0xDD, 0x62,0xED, 0x3E} in block: 14,248,627
		// unknown WBNB interface 2: {0x70, 0xa0,0x82, 0x31} in block: 14,249,300

		methodId := input[:4]
		if bytes.Equal(methodId, wbnbDeposit) {
			return
		}
		if bytes.Equal(methodId, wbnbWithdraw) {
			return
		}
		if bytes.Equal(methodId, wbnbApprove) {
			return
		}
		if bytes.Equal(methodId, wbnbTransfer) {
			return
		}
		if bytes.Equal(methodId, wbnbTransferFrom) {
			return
		}
		// if bytes.Equal(methodId, wbnbTotalSupply) {
		// log.Debug("ParallelMakeUp for WBNB, not for totalSupply", "input size", len(input), "input", input)
		// s.wbnbMakeUp = false // can not makeup
		// return
		// }

		// log.Warn("ParallelMakeUp for WBNB unknown method id", "input size", len(input), "input", input)
		s.wbnbMakeUp = false
	}

}
