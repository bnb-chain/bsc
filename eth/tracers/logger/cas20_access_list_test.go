package logger

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// A CAS20 transfer touches storage through native code, not SLOAD/SSTORE, so the
// slots reach the access list through OnStorageRead.
func TestAccessListTracerSeesCAS20Storage(t *testing.T) {
	alice, bob := common.HexToAddress("0x111111"), common.HexToAddress("0x222222")
	token := common.HexToAddress("0xca52000000000000000000000000000000000001")
	slot := func(a common.Address) common.Hash {
		x := new(uint256.Int).SetBytes(crypto.Keccak256([]byte("bsc.cas20")))
		x.SubUint64(x, 1)
		root := crypto.Keccak256Hash(x.Bytes())
		root[31] = 0
		base := new(uint256.Int).SetBytes(root[:])
		base.AddUint64(base, 4)
		return crypto.Keccak256Hash(common.LeftPadBytes(a.Bytes(), 32), base.Bytes())
	}
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.SetCode(token, vm.CAS20MarkerCode, tracing.CodeChangeContractCreation)
	statedb.SetState(token, slot(alice), common.BigToHash(big.NewInt(100)))
	statedb.SetBalance(alice, uint256.NewInt(1_000_000_000), tracing.BalanceChangeUnspecified)
	statedb.Finalise(false)

	cfg := *params.TestChainConfig
	cfg.JennerTime = new(uint64)
	cfg.Parlia = &params.ParliaConfig{}
	tr := NewAccessListTracer(nil, nil)
	input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(bob.Bytes(), 32)...)
	input = append(input, common.LeftPadBytes([]byte{7}, 32)...)
	blockCtx := vm.BlockContext{CanTransfer: core.CanTransfer, Transfer: core.Transfer, BlockNumber: big.NewInt(1), Time: 1, Difficulty: big.NewInt(1), BaseFee: big.NewInt(0), GasLimit: 30_000_000}
	evm := vm.NewEVM(blockCtx, state.NewHookedState(statedb, tr.Hooks()), &cfg, vm.Config{Tracer: tr.Hooks()})
	tx := types.NewTx(&types.LegacyTx{To: &token, Gas: 100_000, GasPrice: big.NewInt(1), Data: input})
	msg := &core.Message{From: alice, To: &token, GasLimit: tx.Gas(), GasPrice: uint256.NewInt(1), GasFeeCap: uint256.NewInt(1), GasTipCap: uint256.NewInt(1), Value: new(uint256.Int), Data: input}
	statedb.SetTxContext(tx.Hash(), 0)
	receipt, err := core.ApplyTransactionWithEVM(msg, core.NewGasPool(tx.Gas()), statedb, blockCtx.BlockNumber, common.Hash{}, blockCtx.Time, tx, evm)
	evm.Release()
	if err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("transfer: err %v status %d", err, receipt.Status)
	}

	var tokenSlots []common.Hash
	for _, e := range tr.AccessList() {
		if e.Address == token {
			tokenSlots = e.StorageKeys
		}
	}
	want := map[common.Hash]bool{slot(alice): true, slot(bob): true}
	for _, s := range tokenSlots {
		delete(want, s)
	}
	if len(want) != 0 {
		t.Errorf("access list for the token lacks %v; got %v", want, tokenSlots)
	}
}
