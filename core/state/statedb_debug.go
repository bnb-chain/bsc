package state

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	versa "github.com/bnb-chain/versioned-state-database"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

type VersaAccountInfo struct {
	Address common.Address
	Account *types.StateAccount
}

type VersaStorageInfo struct {
	Handler versa.TreeHandler
	Address common.Address
	Key     string
	Val     string
}

type DebugVersionState struct {
	disk      ethdb.KeyValueStore
	versionDB versa.Database
	lock      sync.Mutex

	Version     int64
	PreState    *versa.StateInfo
	PostState   *versa.StateInfo
	AccessTrees []*versa.TreeInfo
	CommitTrees []versa.TreeHandler

	GetAccounts    []*VersaAccountInfo
	UpdateAccounts []*VersaAccountInfo
	DeleteAccounts []common.Address

	GetStorage        []*VersaStorageInfo
	UpdateStorage     []*VersaStorageInfo
	DeleteStorage     []*VersaStorageInfo
	StorageAddr2Owner map[common.Address]common.Hash

	GetCode    []common.Hash
	UpdateCode []common.Hash

	Errs []string
}

func NewDebugVersionState(disk ethdb.KeyValueStore, versionDB versa.Database) *DebugVersionState {
	return &DebugVersionState{
		disk:              disk,
		versionDB:         versionDB,
		AccessTrees:       make([]*versa.TreeInfo, 0),
		CommitTrees:       make([]versa.TreeHandler, 0),
		GetAccounts:       make([]*VersaAccountInfo, 0),
		UpdateAccounts:    make([]*VersaAccountInfo, 0),
		DeleteAccounts:    make([]common.Address, 0),
		GetStorage:        make([]*VersaStorageInfo, 0),
		UpdateStorage:     make([]*VersaStorageInfo, 0),
		DeleteStorage:     make([]*VersaStorageInfo, 0),
		StorageAddr2Owner: make(map[common.Address]common.Hash),
		GetCode:           make([]common.Hash, 0),
		UpdateCode:        make([]common.Hash, 0),
	}
}
func (ds *DebugVersionState) SetVersion(version int64) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.Version = version
}

func (ds *DebugVersionState) OnOpenState(handler versa.StateHandler) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	stateInfo, err := ds.versionDB.GetStateInfo(handler)
	if err != nil {
		panic(fmt.Sprintf("failed to get state info on open state, err: %s", err.Error()))
	}
	ds.PreState = stateInfo
}

func (ds *DebugVersionState) OnOpenTree(handler versa.TreeHandler, owner common.Hash, address common.Address) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	treeInfo, err := ds.versionDB.GetTreeInfo(handler)
	if err != nil {
		panic(fmt.Sprintf("failed to get tree info on open tree, err: %s", err.Error()))
	}
	ds.AccessTrees = append(ds.AccessTrees, treeInfo)
	if owner != (common.Hash{}) && address != (common.Address{}) {
		ds.StorageAddr2Owner[address] = owner
	}
}

func (ds *DebugVersionState) OnGetAccount(addr common.Address, acc *types.StateAccount) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.GetAccounts = append(ds.GetAccounts, &VersaAccountInfo{
		Address: addr,
		Account: acc,
	})
}

func (ds *DebugVersionState) OnUpdateAccount(addr common.Address, acc *types.StateAccount) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.UpdateAccounts = append(ds.UpdateAccounts, &VersaAccountInfo{
		Address: addr,
		Account: acc,
	})
}

func (ds *DebugVersionState) OnDeleteAccount(address common.Address) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.DeleteAccounts = append(ds.DeleteAccounts, address)
}

func (ds *DebugVersionState) OnGetStorage(handler versa.TreeHandler, address common.Address, key []byte, val []byte) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ds.GetStorage = append(ds.GetStorage, &VersaStorageInfo{
		Handler: handler,
		Address: address,
		Key:     common.Bytes2Hex(key),
		Val:     common.Bytes2Hex(val),
	})
}

func (ds *DebugVersionState) OnUpdateStorage(handler versa.TreeHandler, address common.Address, key []byte, val []byte) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ds.UpdateStorage = append(ds.UpdateStorage, &VersaStorageInfo{
		Handler: handler,
		Address: address,
		Key:     common.Bytes2Hex(key),
		Val:     common.Bytes2Hex(val),
	})
}

func (ds *DebugVersionState) OnDeleteStorage(handler versa.TreeHandler, address common.Address, key []byte) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ds.DeleteStorage = append(ds.DeleteStorage, &VersaStorageInfo{
		Handler: handler,
		Address: address,
		Key:     common.Bytes2Hex(key),
	})
}

func (ds *DebugVersionState) OnGetCode(codeHash common.Hash) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.GetCode = append(ds.GetCode, codeHash)
}

func (ds *DebugVersionState) OnUpdateCode(codeHash common.Hash) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.UpdateCode = append(ds.UpdateCode, codeHash)
}

func (ds *DebugVersionState) OnCommitTree(handler versa.TreeHandler) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.CommitTrees = append(ds.CommitTrees, handler)
}

func (ds *DebugVersionState) OnError(err error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.Errs = append(ds.Errs, err.Error())
}

func (ds *DebugVersionState) OnCloseState(handler versa.StateHandler) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	stateInfo, err := ds.versionDB.GetStateInfo(handler)
	if err != nil {
		panic(fmt.Sprintf("failed to get state info on close state, err: %s", err.Error()))
	}
	ds.PostState = stateInfo

	data, err := json.Marshal(ds)
	if err != nil {
		panic(fmt.Sprintf("failed to json encode debug info, err: %s", err.Error()))
	}

	if len(ds.Errs) != 0 {
		log.Info("version state occurs error", "debug info", string(data))
		log.Crit("exit....")
	}

	err = ds.disk.Put(DebugStateKey(ds.Version), data)
	if err != nil {
		panic(fmt.Sprintf("failed to put debug version state into disk, err: %s", err.Error()))
	}
}

func (ds *DebugVersionState) sortItems() {
	sort.Slice(ds.GetAccounts, func(i, j int) bool {
		return ds.GetAccounts[i].Address.Cmp(ds.GetAccounts[j].Address) < 0
	})
	sort.Slice(ds.UpdateAccounts, func(i, j int) bool {
		return ds.UpdateAccounts[i].Address.Cmp(ds.UpdateAccounts[j].Address) < 0
	})
	sort.Slice(ds.DeleteAccounts, func(i, j int) bool {
		return ds.DeleteAccounts[i].Cmp(ds.DeleteAccounts[j]) < 0
	})

	sort.Slice(ds.GetStorage, func(i, j int) bool {
		if ds.GetStorage[i].Address.Cmp(ds.GetStorage[j].Address) == 0 {
			return ds.GetStorage[i].Key < ds.GetStorage[j].Key
		}
		return ds.GetStorage[i].Address.Cmp(ds.GetStorage[j].Address) < 0
	})

	sort.Slice(ds.UpdateStorage, func(i, j int) bool {
		if ds.UpdateStorage[i].Address.Cmp(ds.UpdateStorage[j].Address) == 0 {
			return ds.UpdateStorage[i].Key < ds.UpdateStorage[j].Key
		}
		return ds.UpdateStorage[i].Address.Cmp(ds.UpdateStorage[j].Address) < 0
	})

	sort.Slice(ds.DeleteStorage, func(i, j int) bool {
		if ds.DeleteStorage[i].Address.Cmp(ds.DeleteStorage[j].Address) == 0 {
			return ds.DeleteStorage[i].Key < ds.DeleteStorage[j].Key
		}
		return ds.DeleteStorage[i].Address.Cmp(ds.DeleteStorage[j].Address) < 0
	})

	sort.Slice(ds.GetCode, func(i, j int) bool {
		return ds.GetCode[i].Cmp(ds.GetCode[j]) < 0
	})
	sort.Slice(ds.UpdateCode, func(i, j int) bool {
		return ds.UpdateCode[i].Cmp(ds.UpdateCode[j]) < 0
	})
}

func DebugStateKey(version int64) []byte {
	key := "debug_version_prefix" + strconv.FormatInt(version, 10)
	return []byte(key)
}
