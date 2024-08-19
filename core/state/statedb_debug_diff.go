package state

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

var (
	VersionState = "version"
	HashState    = "hash"
)

type DebugStateDiff struct {
	DiffUpdateAccount map[string][]*VersaAccountInfo
	DiffDeleteAccount map[string][]common.Address

	DiffUpdateStorage map[string][]*VersaStorageInfo
	DiffDeleteStorage map[string][]*VersaStorageInfo

	DiffCalcHash map[string]map[common.Address]common.Hash
	OwnerMap     map[common.Address]common.Hash
	DiffErrs     map[string][]string
}

func (df *DebugStateDiff) diffUpdateAccount(vs []*VersaAccountInfo, hs []*VersaAccountInfo) {
	count := len(vs)
	if count > len(hs) {
		count = len(hs)
	}
	idx := 0
	for ; idx < count; idx++ {
		if vs[idx].Address.Cmp(hs[idx].Address) != 0 {
			break
		}
		if vs[idx].Account.Nonce != hs[idx].Account.Nonce {
			break
		}
		if vs[idx].Account.Balance.Cmp(hs[idx].Account.Balance) != 0 {
			break
		}
		if vs[idx].Account.Root.Cmp(hs[idx].Account.Root) != 0 {
			break
		}
		if common.BytesToHash(vs[idx].Account.CodeHash).Cmp(common.BytesToHash(hs[idx].Account.CodeHash)) != 0 {
			break
		}
	}
	if idx < len(vs) {
		df.DiffUpdateAccount[VersionState] = vs[idx:]
	}
	if idx < len(hs) {
		df.DiffUpdateAccount[HashState] = hs[idx:]
	}
	return
}

func (df *DebugStateDiff) diffDeleteAccount(vs []common.Address, hs []common.Address) {
	count := len(vs)
	if count > len(hs) {
		count = len(hs)
	}
	idx := 0
	for ; idx < count; idx++ {
		if vs[idx].Cmp(hs[idx]) != 0 {
			break
		}
	}
	if idx < len(vs) {
		df.DiffDeleteAccount[VersionState] = vs[idx:]
	}
	if idx < len(hs) {
		df.DiffDeleteAccount[HashState] = hs[idx:]
	}
	return
}

func (df *DebugStateDiff) diffUpdateStorage(vs []*VersaStorageInfo, hs []*VersaStorageInfo) {
	count := len(vs)
	if count > len(hs) {
		count = len(hs)
	}
	idx := 0
	for ; idx < count; idx++ {
		if vs[idx].Address.Cmp(hs[idx].Address) != 0 {
			break
		}
		if vs[idx].Key != hs[idx].Key {
			break
		}
		if vs[idx].Val != hs[idx].Val {
			break
		}
	}
	if idx < len(vs) {
		df.DiffUpdateStorage[VersionState] = vs[idx:]
	}
	if idx < len(hs) {
		df.DiffUpdateStorage[HashState] = hs[idx:]
	}
	return
}

func (df *DebugStateDiff) diffDeleteStorage(vs []*VersaStorageInfo, hs []*VersaStorageInfo) {
	count := len(vs)
	if count > len(hs) {
		count = len(hs)
	}
	idx := 0
	for ; idx < count; idx++ {
		if vs[idx].Address.Cmp(hs[idx].Address) != 0 {
			break
		}
		if vs[idx].Key != hs[idx].Key {
			break
		}
	}
	if idx < len(vs) {
		df.DiffDeleteStorage[VersionState] = vs[idx:]
	}
	if idx < len(hs) {
		df.DiffDeleteStorage[HashState] = hs[idx:]
	}
	return
}

func (df *DebugStateDiff) diffCalcHash(vs map[common.Address]common.Hash, hs map[common.Address]common.Hash) {
	record := make(map[common.Address]struct{})
	for address, vch := range vs {
		record[address] = struct{}{}
		hch, ok := hs[address]
		if !ok {
			df.DiffCalcHash[VersionState][address] = vch
		}
		if vch.Cmp(hch) != 0 {
			df.DiffCalcHash[VersionState][address] = vch
			df.DiffCalcHash[HashState][address] = hch
		}
	}

	for address := range record {
		delete(vs, address)
		delete(hs, address)
	}

	for address, hash := range vs {
		df.DiffCalcHash[VersionState][address] = hash
	}

	for address, hash := range hs {
		df.DiffCalcHash[HashState][address] = hash
	}
}

func GenerateDebugStateDiff(vs *DebugVersionState, hs *DebugHashState) string {
	diff := &DebugStateDiff{
		DiffUpdateAccount: make(map[string][]*VersaAccountInfo),
		DiffDeleteAccount: make(map[string][]common.Address),

		DiffUpdateStorage: make(map[string][]*VersaStorageInfo),
		DiffDeleteStorage: make(map[string][]*VersaStorageInfo),

		DiffCalcHash: make(map[string]map[common.Address]common.Hash),
		OwnerMap:     make(map[common.Address]common.Hash),
		DiffErrs:     make(map[string][]string),
	}
	diff.DiffUpdateAccount[VersionState] = make([]*VersaAccountInfo, 0)
	diff.DiffUpdateAccount[HashState] = make([]*VersaAccountInfo, 0)
	diff.DiffDeleteAccount[VersionState] = make([]common.Address, 0)
	diff.DiffDeleteAccount[HashState] = make([]common.Address, 0)

	diff.DiffUpdateStorage[VersionState] = make([]*VersaStorageInfo, 0)
	diff.DiffUpdateStorage[HashState] = make([]*VersaStorageInfo, 0)
	diff.DiffDeleteStorage[VersionState] = make([]*VersaStorageInfo, 0)
	diff.DiffDeleteStorage[HashState] = make([]*VersaStorageInfo, 0)

	diff.DiffCalcHash[VersionState] = make(map[common.Address]common.Hash)
	diff.DiffCalcHash[HashState] = make(map[common.Address]common.Hash)

	diff.DiffErrs[VersionState] = make([]string, 0)
	diff.DiffErrs[HashState] = make([]string, 0)
	
	diff.diffUpdateAccount(vs.UpdateAccounts, hs.UpdateAccounts)
	diff.diffDeleteAccount(vs.DeleteAccounts, hs.DeleteAccounts)
	diff.diffUpdateStorage(vs.UpdateStorage, hs.UpdateStorage)
	diff.diffDeleteStorage(vs.DeleteStorage, hs.DeleteStorage)
	diff.diffCalcHash(vs.CalcHash, hs.CalcHash)

	for address, _ := range diff.DiffCalcHash[VersionState] {
		diff.OwnerMap[address] = vs.StorageAddr2Owner[address]
	}
	for address, _ := range diff.DiffCalcHash[HashState] {
		diff.OwnerMap[address] = hs.StorageAddr2Owner[address]
	}

	if len(vs.Errs) != 0 || len(hs.Errs) != 0 {
		diff.DiffErrs[VersionState] = vs.Errs
		diff.DiffErrs[HashState] = hs.Errs
	}

	data, err := json.Marshal(diff)
	if err != nil {
		panic(fmt.Sprintf("failed to json encode debug info, err: %s", err.Error()))
	}
	return string(data)
}
