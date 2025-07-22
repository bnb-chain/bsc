package vm

import (
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
)

type MockStateDB struct {
}

func (m MockStateDB) NoTrie() bool {
	return true
}

func (m MockStateDB) IsAddressInMutations(addr common.Address) bool {
	return false
}

func (m MockStateDB) CreateAccount(address common.Address) {

}

func (m MockStateDB) CreateContract(address common.Address) {

}

func (m MockStateDB) SubBalance(address common.Address, u *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	return uint256.Int{}
}

func (m MockStateDB) AddBalance(address common.Address, u *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	return uint256.Int{}
}

func (m MockStateDB) GetBalance(address common.Address) *uint256.Int {
	return &uint256.Int{}
}

func (m MockStateDB) SetBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) {

}

func (m MockStateDB) GetNonce(address common.Address) uint64 {
	return 0
}

func (m MockStateDB) SetNonce(address common.Address, u uint64, reason tracing.NonceChangeReason) {

}

func (m MockStateDB) GetCodeHash(address common.Address) common.Hash {
	return common.Hash{}
}

func (m MockStateDB) GetCode(address common.Address) []byte {
	return nil
}

func (m MockStateDB) SetCode(address common.Address, bytes []byte) []byte {
	return nil
}

func (m MockStateDB) GetCodeSize(address common.Address) int {
	return 0
}

func (m MockStateDB) AddRefund(u uint64) {

}

func (m MockStateDB) SubRefund(u uint64) {

}

func (m MockStateDB) GetRefund() uint64 {
	return 0
}

func (m MockStateDB) GetCommittedState(address common.Address, hash common.Hash) common.Hash {
	return common.Hash{}
}

func (m MockStateDB) GetState(address common.Address, hash common.Hash) common.Hash {
	return common.Hash{}
}

func (m MockStateDB) SetState(address common.Address, hash common.Hash, hash2 common.Hash) common.Hash {
	return common.Hash{}
}

func (m MockStateDB) GetStorageRoot(addr common.Address) common.Hash {
	return common.Hash{}
}

func (m MockStateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	return common.Hash{}
}

func (m MockStateDB) SetTransientState(addr common.Address, key, value common.Hash) {

}

func (m MockStateDB) SelfDestruct(address common.Address) uint256.Int {
	return uint256.Int{}
}

func (m MockStateDB) HasSelfDestructed(address common.Address) bool {
	return false
}

func (m MockStateDB) SelfDestruct6780(address common.Address) (uint256.Int, bool) {
	return uint256.Int{}, false
}

func (m MockStateDB) Exist(address common.Address) bool {
	return false
}

func (m MockStateDB) Empty(address common.Address) bool {
	return true
}

func (m MockStateDB) AddressInAccessList(addr common.Address) bool {
	return false
}

func (m MockStateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	return false, false
}

func (m MockStateDB) AddAddressToAccessList(addr common.Address) {

}

func (m MockStateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {

}

func (m MockStateDB) ClearAccessList() {

}

func (m MockStateDB) PointCache() *utils.PointCache {
	return nil
}

func (m MockStateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {

}

func (m MockStateDB) SetTxContext(thash common.Hash, ti int) {

}

func (m MockStateDB) TxIndex() int {
	return 0
}

func (m MockStateDB) RevertToSnapshot(i int) {

}

func (m MockStateDB) Snapshot() int {
	return 0
}

func (m MockStateDB) AddLog(log *types.Log) {

}

func (m MockStateDB) GetLogs(hash common.Hash, blockNumber uint64, blockHash common.Hash) []*types.Log {
	return nil
}

func (m MockStateDB) AddPreimage(hash common.Hash, bytes []byte) {

}

func (m MockStateDB) Witness() *stateless.Witness {
	return nil
}

func (m MockStateDB) AccessEvents() *state.AccessEvents {
	return nil
}

func (m MockStateDB) Finalise(b bool) {

}

func (m MockStateDB) IntermediateRoot(deleteEmptyObjects bool) common.Hash {
	return common.Hash{}
}
