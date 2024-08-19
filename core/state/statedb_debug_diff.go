package state

import (
	"encoding/json"
	"fmt"

	versa "github.com/bnb-chain/versioned-state-database"
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

	DiffCommitRoot map[string]map[common.Address][]common.Hash
	OwnerMap       map[common.Address]common.Hash
	DiffErrs       map[string][]string
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

func (df *DebugStateDiff) diffCommit(vs map[common.Address][]*versa.TreeInfo, hs map[common.Address][]common.Hash) {
	record := make(map[common.Address]struct{})
	df.DiffCommitRoot[VersionState] = make(map[common.Address][]common.Hash)
	df.DiffCommitRoot[HashState] = make(map[common.Address][]common.Hash)
	for address, trees := range vs {
		record[address] = struct{}{}
		if _, ok := hs[address]; !ok {
			df.DiffCommitRoot[VersionState][address] = make([]common.Hash, 0)
			for _, tree := range trees {
				df.DiffCommitRoot[VersionState][address] = append(df.DiffCommitRoot[VersionState][address], tree.Tree.Root)
			}
		}
		if len(vs[address]) != 1 || len(vs[address]) != 1 {
			df.DiffCommitRoot[VersionState][address] = make([]common.Hash, 0)
			for _, tree := range trees {
				df.DiffCommitRoot[VersionState][address] = append(df.DiffCommitRoot[VersionState][address], tree.Tree.Root)
			}
			df.DiffCommitRoot[HashState][address] = hs[address]
		}
		if vs[address][0].Tree.Root.Cmp(hs[address][0]) != 0 {
			df.DiffCommitRoot[VersionState][address] = make([]common.Hash, 0)
			df.DiffCommitRoot[VersionState][address] = append(df.DiffCommitRoot[VersionState][address], vs[address][0].Tree.Root)
			df.DiffCommitRoot[HashState][address] = hs[address]
		}
	}
	for address, _ := range record {
		delete(vs, address)
		delete(hs, address)
	}
	for address, trees := range vs {
		df.DiffCommitRoot[VersionState][address] = make([]common.Hash, 0)
		for _, tree := range trees {
			df.DiffCommitRoot[VersionState][address] = append(df.DiffCommitRoot[VersionState][address], tree.Tree.Root)
		}
	}
	for address, _ := range hs {
		df.DiffCommitRoot[HashState][address] = hs[address]
	}
}

func GenerateDebugStateDiff(vs *DebugVersionState, hs *DebugHashState) string {
	diff := &DebugStateDiff{
		DiffUpdateAccount: make(map[string][]*VersaAccountInfo),
		DiffDeleteAccount: make(map[string][]common.Address),

		DiffUpdateStorage: make(map[string][]*VersaStorageInfo),
		DiffDeleteStorage: make(map[string][]*VersaStorageInfo),

		DiffCommitRoot: make(map[string]map[common.Address][]common.Hash),
		OwnerMap:       make(map[common.Address]common.Hash),
		DiffErrs:       make(map[string][]string),
	}
	diff.diffUpdateAccount(vs.UpdateAccounts, hs.UpdateAccounts)
	diff.diffDeleteAccount(vs.DeleteAccounts, hs.DeleteAccounts)
	diff.diffUpdateStorage(vs.UpdateStorage, hs.UpdateStorage)
	diff.diffDeleteStorage(vs.DeleteStorage, hs.DeleteStorage)
	diff.diffCommit(vs.CommitTrees, hs.CommitTrees)

	for address, _ := range diff.DiffCommitRoot[VersionState] {
		diff.OwnerMap[address] = vs.StorageAddr2Owner[address]
	}
	for address, _ := range diff.DiffCommitRoot[HashState] {
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
