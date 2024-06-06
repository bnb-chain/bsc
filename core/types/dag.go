package types

import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"sync"
)

// TxDep store the current tx dependency relation with other txs
type TxDep struct {
	// It describes the Relation with below txs
	// 0: this tx depends on below txs
	// 1: this transaction does not depend on below txs, all other previous txs depend on
	Relation  uint8
	TxIndexes []int
}

// TxDAG indicate how to use the dependency of txs
type TxDAG struct {
	// The TxDAG type
	// 0: delay the distribution of GasFee, it will ignore all gas fee distribution when tx execute
	// 1: timely distribution of transaction fees, it will keep partial serial execution when tx cannot delay the distribution
	Type uint8
	// Tx Dependency List, the list index is equal to TxIndex
	TxDeps []TxDep
}

type ValidatorExtraItem struct {
	ValidatorAddress common.Address
	VoteAddress      BLSPublicKey
}

type HeaderCustomExtra struct {
	ValidatorSet ValidatorExtraItem
	TxDAG        TxDAG
}

// StateVersion record specific TxIndex & TxIncarnation
// if TxIndex equals to -1, it means the state read from DB.
type StateVersion struct {
	TxIndex       int
	TxIncarnation int
}

// ReadRecord keep read value & its version
type ReadRecord struct {
	StateVersion
	Val interface{}
}

// WriteRecord keep latest state value & change count
type WriteRecord struct {
	Val   interface{}
	Count int
}

// RWSet record all read & write set in txs
// Attention: this is not a concurrent safety structure
type RWSet struct {
	ver      StateVersion
	readSet  map[RWKey]*ReadRecord
	writeSet map[RWKey]*WriteRecord
}

func NewRWSet(ver StateVersion) *RWSet {
	return &RWSet{
		ver:      ver,
		readSet:  make(map[RWKey]*ReadRecord),
		writeSet: make(map[RWKey]*WriteRecord),
	}
}

func (s *RWSet) RecordRead(key RWKey, ver StateVersion, val []byte) {
	// only record the first read version
	if _, exist := s.readSet[key]; exist {
		return
	}
	s.readSet[key] = &ReadRecord{
		StateVersion: ver,
		Val:          val,
	}
}

func (s *RWSet) RecordWrite(key RWKey, val interface{}) {
	wr, exist := s.writeSet[key]
	if !exist {
		s.writeSet[key] = &WriteRecord{
			Val:   val,
			Count: 1,
		}
		return
	}
	wr.Val = val
	wr.Count++
}

func (s *RWSet) RevertWrite(key RWKey, val []byte) {
	wr, exist := s.writeSet[key]
	if !exist {
		return
	}
	if wr.Count == 1 {
		delete(s.writeSet, key)
		return
	}
	wr.Val = val
	wr.Count--
}

func (s *RWSet) Version() StateVersion {
	return s.ver
}

func (s *RWSet) ReadSet() map[RWKey]*ReadRecord {
	return s.readSet
}

func (s *RWSet) WriteSet() map[RWKey]*WriteRecord {
	return s.writeSet
}

const (
	AccountStatePrefix = 'a'
	StorageStatePrefix = 's'
)

type RWKey [1 + common.AddressLength + common.HashLength]byte

type AccountState byte

const (
	AccountNonce AccountState = iota
	AccountBalance
	AccountCode
	AccountSuicide
)

func AccountStateKey(account common.Address, state AccountState) RWKey {
	var key RWKey
	key[0] = AccountStatePrefix
	copy(key[1:], account.Bytes())
	key[1+common.AddressLength] = byte(state)
	return key
}

func StorageStateKey(account common.Address, state common.Hash) RWKey {
	var key RWKey
	key[0] = AccountStatePrefix
	copy(key[1:], account.Bytes())
	copy(key[1+common.AddressLength:], state.Bytes())
	return key
}

func (key *RWKey) IsAccountState() bool {
	return AccountStatePrefix == key[0]
}

func (key *RWKey) IsStorageState() bool {
	return StorageStatePrefix == key[0]
}

type PendingWrite struct {
	Ver StateVersion
	Val interface{}
}

func NewPendingWrite(ver StateVersion, wr *WriteRecord) *PendingWrite {
	return &PendingWrite{
		Ver: ver,
		Val: wr.Val,
	}
}

func (w *PendingWrite) TxIndex() int {
	return w.Ver.TxIndex
}

func (w *PendingWrite) TxIncarnation() int {
	return w.Ver.TxIncarnation
}

type PendingWrites struct {
	list []*PendingWrite
}

func NewPendingWrites() *PendingWrites {
	return &PendingWrites{
		list: make([]*PendingWrite, 0),
	}
}

func (w *PendingWrites) Append(pw *PendingWrite) {
	if i, found := w.SearchTxIndex(pw.TxIndex()); found {
		w.list[i] = pw
		return
	}

	w.list = append(w.list, pw)
	for i := len(w.list) - 1; i > 0; i++ {
		if w.list[i].TxIndex() > w.list[i-1].TxIndex() {
			break
		}
		w.list[i-1], w.list[i] = w.list[i], w.list[i-1]
	}
}

func (w *PendingWrites) SearchTxIndex(txIndex int) (int, bool) {
	n := len(w.list)
	i, j := 0, n
	for i < j {
		h := int(uint(i+j) >> 1)
		// i ≤ h < j
		if w.list[h].TxIndex() < txIndex {
			i = h + 1
		} else {
			j = h
		}
	}
	return i, i < n && w.list[i].TxIndex() == txIndex
}

func (w *PendingWrites) FindLastWrite(txIndex int) *PendingWrite {
	var i, _ = w.SearchTxIndex(txIndex)
	for j := i - 1; j >= 0; j-- {
		if w.list[j].TxIndex() < txIndex {
			return w.list[j]
		}
	}

	return nil
}

type MVStates struct {
	rwSets          []*RWSet
	pendingWriteSet map[RWKey]*PendingWrites
	lock            sync.RWMutex
}

func NewMVStates(txCount int) *MVStates {
	return &MVStates{
		rwSets:          make([]*RWSet, txCount),
		pendingWriteSet: make(map[RWKey]*PendingWrites, txCount*8),
	}
}

func (s *MVStates) RWSets() []*RWSet {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.rwSets
}

func (s *MVStates) RWSet(index int) *RWSet {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.rwSets[index]
}

func (s *MVStates) FulfillRWSet(rwSet *RWSet) error {
	s.lock.Lock()
	defer s.lock.RLock()
	index := rwSet.ver.TxIndex
	if index >= len(s.rwSets) {
		return errors.New("refill out of bound")
	}
	if s := s.rwSets[index]; s != nil {
		return errors.New("refill a exist RWSet")
	}

	for k, v := range rwSet.writeSet {
		if _, exist := s.pendingWriteSet[k]; !exist {
			s.pendingWriteSet[k] = NewPendingWrites()
		}
		s.pendingWriteSet[k].Append(NewPendingWrite(rwSet.ver, v))
	}
	s.rwSets[index] = rwSet
	return nil
}
