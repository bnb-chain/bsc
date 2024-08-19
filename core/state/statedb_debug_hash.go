package state

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
)

type DebugHashState struct {
	disk ethdb.KeyValueStore
	lock sync.Mutex

	Version     int64
	AccessTrees map[common.Address][]common.Hash
	CommitTrees map[common.Address][]common.Hash
	CalcHash    map[common.Address]common.Hash

	GetAccounts    []*VersaAccountInfo
	UpdateAccounts []*VersaAccountInfo
	DeleteAccounts []common.Address

	GetStorage        []*VersaStorageInfo
	UpdateStorage     []*VersaStorageInfo
	DeleteStorage     []*VersaStorageInfo
	StorageAddr2Owner map[common.Address]common.Hash

	GetCode    map[common.Address][]common.Hash
	UpdateCode map[common.Address][]common.Hash

	Errs []string
}

func NewDebugHashState(disk ethdb.KeyValueStore) *DebugHashState {
	return &DebugHashState{
		disk:              disk,
		AccessTrees:       make(map[common.Address][]common.Hash),
		CommitTrees:       make(map[common.Address][]common.Hash),
		CalcHash:          make(map[common.Address]common.Hash),
		GetAccounts:       make([]*VersaAccountInfo, 0),
		UpdateAccounts:    make([]*VersaAccountInfo, 0),
		DeleteAccounts:    make([]common.Address, 0),
		GetStorage:        make([]*VersaStorageInfo, 0),
		UpdateStorage:     make([]*VersaStorageInfo, 0),
		DeleteStorage:     make([]*VersaStorageInfo, 0),
		StorageAddr2Owner: make(map[common.Address]common.Hash),
		GetCode:           make(map[common.Address][]common.Hash),
		UpdateCode:        make(map[common.Address][]common.Hash),
		Errs:              make([]string, 0),
	}
}

func (hs *DebugHashState) OnOpenTree(root common.Hash, owner common.Hash, address common.Address) {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	if _, ok := hs.AccessTrees[address]; !ok {
		hs.AccessTrees[address] = make([]common.Hash, 0)
	}
	hs.AccessTrees[address] = append(hs.AccessTrees[address], root)
	if owner != (common.Hash{}) && address != (common.Address{}) {
		hs.StorageAddr2Owner[address] = owner
	}
}

func (hs *DebugHashState) OnGetAccount(addr common.Address, acc *types.StateAccount) {
	hs.lock.Lock()
	defer hs.lock.Unlock()
	hs.GetAccounts = append(hs.GetAccounts, &VersaAccountInfo{
		Address: addr,
		Account: acc,
	})
}

func (hs *DebugHashState) OnUpdateAccount(addr common.Address, acc *types.StateAccount) {
	hs.lock.Lock()
	defer hs.lock.Unlock()
	hs.UpdateAccounts = append(hs.UpdateAccounts, &VersaAccountInfo{
		Address: addr,
		Account: acc,
	})
}

func (hs *DebugHashState) OnDeleteAccount(address common.Address) {
	hs.lock.Lock()
	defer hs.lock.Unlock()
	hs.DeleteAccounts = append(hs.DeleteAccounts, address)
}

func (hs *DebugHashState) OnGetStorage(address common.Address, key []byte, val []byte) {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	hs.GetStorage = append(hs.GetStorage, &VersaStorageInfo{
		Address: address,
		Key:     common.Bytes2Hex(key),
		Val:     common.Bytes2Hex(val),
	})
}

func (hs *DebugHashState) OnUpdateStorage(address common.Address, key []byte, val []byte) {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	hs.UpdateStorage = append(hs.UpdateStorage, &VersaStorageInfo{
		Address: address,
		Key:     common.Bytes2Hex(key),
		Val:     common.Bytes2Hex(val),
	})
}

func (hs *DebugHashState) OnDeleteStorage(address common.Address, key []byte) {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	hs.DeleteStorage = append(hs.DeleteStorage, &VersaStorageInfo{
		Address: address,
		Key:     common.Bytes2Hex(key),
	})
}

func (hs *DebugHashState) OnGetCode(addr common.Address, codeHash common.Hash) {
	hs.lock.Lock()
	defer hs.lock.Unlock()
	if _, ok := hs.GetCode[addr]; !ok {
		hs.GetCode[addr] = make([]common.Hash, 0)
	}
	hs.GetCode[addr] = append(hs.GetCode[addr], codeHash)
}

func (hs *DebugHashState) OnUpdateCode(addr common.Address, codeHash common.Hash) {
	hs.lock.Lock()
	defer hs.lock.Unlock()
	if _, ok := hs.UpdateCode[addr]; !ok {
		hs.UpdateCode[addr] = make([]common.Hash, 0)
	}
	hs.UpdateCode[addr] = append(hs.UpdateCode[addr], codeHash)
}

func (hs *DebugHashState) OnCalcHash(addr common.Address, root common.Hash) {
	hs.lock.Lock()
	defer hs.lock.Unlock()
	hs.CalcHash[addr] = root
}

func (hs *DebugHashState) OnCommitTree(addr common.Address, root common.Hash) {
	hs.lock.Lock()
	defer hs.lock.Unlock()
	if _, ok := hs.CommitTrees[addr]; !ok {
		hs.CommitTrees[addr] = make([]common.Hash, 0)
	}
	hs.CommitTrees[addr] = append(hs.CommitTrees[addr], root)
}

func (hs *DebugHashState) OnError(err error) {
	hs.lock.Lock()
	defer hs.lock.Unlock()
	hs.Errs = append(hs.Errs, err.Error())
}

func (hs *DebugHashState) flush() {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	hs.sortItems()
	data, err := json.Marshal(hs)
	if err != nil {
		panic(fmt.Sprintf("failed to json encode debug info, err: %s", err.Error()))
	}

	err = hs.disk.Put(DebugHashStateKey(hs.Version), data)
	if err != nil {
		panic(fmt.Sprintf("failed to put debug version state into disk, err: %s", err.Error()))
	}
}

func (hs *DebugHashState) sortItems() {
	sort.Slice(hs.GetAccounts, func(i, j int) bool {
		return hs.GetAccounts[i].Address.Cmp(hs.GetAccounts[j].Address) < 0
	})
	sort.Slice(hs.UpdateAccounts, func(i, j int) bool {
		return hs.UpdateAccounts[i].Address.Cmp(hs.UpdateAccounts[j].Address) < 0
	})
	sort.Slice(hs.DeleteAccounts, func(i, j int) bool {
		return hs.DeleteAccounts[i].Cmp(hs.DeleteAccounts[j]) < 0
	})

	sort.Slice(hs.GetStorage, func(i, j int) bool {
		if hs.GetStorage[i].Address.Cmp(hs.GetStorage[j].Address) == 0 {
			return hs.GetStorage[i].Key < hs.GetStorage[j].Key
		}
		return hs.GetStorage[i].Address.Cmp(hs.GetStorage[j].Address) < 0
	})

	sort.Slice(hs.UpdateStorage, func(i, j int) bool {
		if hs.UpdateStorage[i].Address.Cmp(hs.UpdateStorage[j].Address) == 0 {
			return hs.UpdateStorage[i].Key < hs.UpdateStorage[j].Key
		}
		return hs.UpdateStorage[i].Address.Cmp(hs.UpdateStorage[j].Address) < 0
	})

	sort.Slice(hs.DeleteStorage, func(i, j int) bool {
		if hs.DeleteStorage[i].Address.Cmp(hs.DeleteStorage[j].Address) == 0 {
			return hs.DeleteStorage[i].Key < hs.DeleteStorage[j].Key
		}
		return hs.DeleteStorage[i].Address.Cmp(hs.DeleteStorage[j].Address) < 0
	})
}

func DebugHashStateKey(version int64) []byte {
	key := "debug_hash_prefix" + strconv.FormatInt(version, 10)
	return []byte(key)
}
