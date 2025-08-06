package vm

import (
	"fmt"
	"math/big"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
)

type MockStateDB struct {
	storage map[common.Address]*types.Account
	refund  uint64
	touched bool
}

func (m MockStateDB) NoTrie() bool {
	m.touched = true
	return true
}

func (m MockStateDB) IsAddressInMutations(addr common.Address) bool {
	m.touched = true
	return false
}

func (m MockStateDB) CreateAccount(address common.Address) {
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	m.storage[address] = &types.Account{
		Code:       nil,
		Storage:    make(map[common.Hash]common.Hash),
		Balance:    big.NewInt(0),
		Nonce:      0,
		PrivateKey: nil,
	}
	m.touched = true

}

func (m MockStateDB) CreateContract(address common.Address) {
	m.touched = true
}

func (m MockStateDB) SubBalance(address common.Address, u *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if _, ok := m.storage[address]; !ok {
		return uint256.Int{}
	}
	bal := m.storage[address].Balance
	bal = big.NewInt(0).Sub(bal, u.ToBig())
	return *uint256.MustFromBig(bal)
}

func (m MockStateDB) AddBalance(address common.Address, u *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if _, ok := m.storage[address]; !ok {
		return uint256.Int{}
	}
	bal := m.storage[address].Balance
	bal = big.NewInt(0).Add(bal, u.ToBig())
	return *uint256.MustFromBig(bal)
}

func (m MockStateDB) GetBalance(address common.Address) *uint256.Int {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		return uint256.MustFromBig(acc.Balance)
	}
	return uint256.NewInt(0)
}

func (m MockStateDB) SetBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[addr]; ok {
		acc.Balance = acc.Balance.Set(amount.ToBig())
	}
}

func (m MockStateDB) GetNonce(address common.Address) uint64 {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		return acc.Nonce
	}
	return 0
}

func (m MockStateDB) SetNonce(address common.Address, u uint64, reason tracing.NonceChangeReason) {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		acc.Nonce = u
	}
}

func (m MockStateDB) GetCodeHash(address common.Address) common.Hash {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		return crypto.Keccak256Hash(acc.Code)
	}
	return common.Hash{}
}

func (m MockStateDB) GetCode(address common.Address) []byte {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		return acc.Code
	}
	return nil
}

func (m MockStateDB) SetCode(address common.Address, bytes []byte) []byte {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		var ret []byte
		ret = append(ret, acc.Code...)
		acc.Code = bytes
		return ret
	}
	return nil
}

func (m MockStateDB) GetCodeSize(address common.Address) int {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		return len(acc.Code)
	}
	return 0
}

func (m MockStateDB) AddRefund(u uint64) {
	m.touched = true
	m.refund += u
}

func (m MockStateDB) SubRefund(u uint64) {
	m.touched = true
	if u > m.refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", u, m.refund))
	}
	m.refund -= u
}

func (m MockStateDB) GetRefund() uint64 {
	m.touched = true
	return m.refund
}

func (m MockStateDB) GetCommittedState(address common.Address, hash common.Hash) common.Hash {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		return acc.Storage[hash]
	}
	return common.Hash{}
}

func (m MockStateDB) GetState(address common.Address, hash common.Hash) common.Hash {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		return acc.Storage[hash]
	}
	return common.Hash{}
}

func (m MockStateDB) SetState(address common.Address, hash common.Hash, hash2 common.Hash) common.Hash {
	m.touched = true
	if m.storage == nil {
		m.storage = make(map[common.Address]*types.Account)
	}
	if acc, ok := m.storage[address]; ok {
		old := acc.Storage[hash]
		acc.Storage[hash] = hash2
		return old
	}
	return common.Hash{}
}

func (m MockStateDB) GetStorageRoot(addr common.Address) common.Hash {
	m.touched = true
	return common.Hash{}
}

func (m MockStateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	m.touched = true
	return common.Hash{}
}

func (m MockStateDB) SetTransientState(addr common.Address, key, value common.Hash) {
	m.touched = true
}

func (m MockStateDB) SelfDestruct(address common.Address) uint256.Int {
	m.touched = true
	return uint256.Int{}
}

func (m MockStateDB) HasSelfDestructed(address common.Address) bool {
	m.touched = true
	return false
}

func (m MockStateDB) SelfDestruct6780(address common.Address) (uint256.Int, bool) {
	m.touched = true
	return uint256.Int{}, false
}

func (m MockStateDB) Exist(address common.Address) bool {
	m.touched = true
	return false
}

func (m MockStateDB) Empty(address common.Address) bool {
	m.touched = true
	return true
}

func (m MockStateDB) AddressInAccessList(addr common.Address) bool {
	m.touched = true
	return false
}

func (m MockStateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	m.touched = true
	return false, false
}

func (m MockStateDB) AddAddressToAccessList(addr common.Address) {
	m.touched = true
}

func (m MockStateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	m.touched = true
}

func (m MockStateDB) ClearAccessList() {
	m.touched = true
}

func (m MockStateDB) PointCache() *utils.PointCache {
	m.touched = true
	return nil
}

func (m MockStateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
	m.touched = true
}

func (m MockStateDB) SetTxContext(thash common.Hash, ti int) {
	m.touched = true
}

func (m MockStateDB) TxIndex() int {
	m.touched = true
	return 0
}

func (m MockStateDB) RevertToSnapshot(i int) {
	m.touched = true
}

func (m MockStateDB) Snapshot() int {
	m.touched = true
	return 0
}

func (m MockStateDB) AddLog(log *types.Log) {
	m.touched = true
}

func (m MockStateDB) GetLogs(hash common.Hash, blockNumber uint64, blockHash common.Hash) []*types.Log {
	m.touched = true
	return nil
}

func (m MockStateDB) AddPreimage(hash common.Hash, bytes []byte) {
	m.touched = true
}

func (m MockStateDB) Witness() *stateless.Witness {
	m.touched = true
	return nil
}

func (m MockStateDB) AccessEvents() *state.AccessEvents {
	m.touched = true
	return nil
}

func (m MockStateDB) Finalise(b bool) {
	m.touched = true
}

func (m MockStateDB) IntermediateRoot(deleteEmptyObjects bool) common.Hash {
	m.touched = true
	return common.Hash{}
}
