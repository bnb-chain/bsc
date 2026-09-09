package native

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// A CAS20 transfer touches storage through native code, not SLOAD/SSTORE, so the
// tracer has to learn of the slots through OnStorageRead.
func TestPrestateSeesCAS20Storage(t *testing.T) {
	alice, bob := common.HexToAddress("0x111111"), common.HexToAddress("0x222222")
	token := common.HexToAddress("0xca52000000000000000000000000000000000001")
	aliceSlot, bobSlot := cas20BalanceSlot(alice), cas20BalanceSlot(bob)

	for _, diffMode := range []bool{false, true} {
		statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		statedb.SetCode(token, vm.CAS20MarkerCode, tracing.CodeChangeContractCreation)
		statedb.SetState(token, aliceSlot, common.BigToHash(big.NewInt(100)))
		statedb.SetBalance(alice, uint256.NewInt(1_000_000_000), tracing.BalanceChangeUnspecified)
		statedb.Finalise(false)

		cfg := *params.TestChainConfig
		cfg.JennerTime = new(uint64)
		cfg.Parlia = &params.ParliaConfig{}
		tr, err := newPrestateTracer(&tracers.Context{}, json.RawMessage(`{"diffMode":`+map[bool]string{false: "false", true: "true"}[diffMode]+`}`), &cfg)
		if err != nil {
			t.Fatal(err)
		}
		input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(bob.Bytes(), 32)...)
		input = append(input, common.LeftPadBytes([]byte{7}, 32)...)
		blockCtx := vm.BlockContext{CanTransfer: core.CanTransfer, Transfer: core.Transfer, BlockNumber: big.NewInt(1), Time: 1, Difficulty: big.NewInt(1), BaseFee: big.NewInt(0), GasLimit: 30_000_000}
		evm := vm.NewEVM(blockCtx, state.NewHookedState(statedb, tr.Hooks), &cfg, vm.Config{Tracer: tr.Hooks})
		tx := types.NewTx(&types.LegacyTx{To: &token, Gas: 100_000, GasPrice: big.NewInt(1), Data: input})
		msg := &core.Message{From: alice, To: &token, GasLimit: tx.Gas(), GasPrice: uint256.NewInt(1), GasFeeCap: uint256.NewInt(1), GasTipCap: uint256.NewInt(1), Value: new(uint256.Int), Data: input}
		statedb.SetTxContext(tx.Hash(), 0)
		receipt, err := core.ApplyTransactionWithEVM(msg, core.NewGasPool(tx.Gas()), statedb, blockCtx.BlockNumber, common.Hash{}, blockCtx.Time, tx, evm)
		evm.Release()
		if err != nil || receipt.Status != types.ReceiptStatusSuccessful {
			t.Fatalf("transfer: err %v status %d", err, receipt.Status)
		}
		res, err := tr.GetResult()
		if err != nil {
			t.Fatal(err)
		}

		if !diffMode {
			var pre map[common.Address]account
			if err := json.Unmarshal(res, &pre); err != nil {
				t.Fatal(err)
			}
			if got := pre[token].Storage[aliceSlot]; got != common.BigToHash(big.NewInt(100)) {
				t.Errorf("prestate: alice's slot = %s, want 100; storage %v", got.Hex(), pre[token].Storage)
			}
			if _, ok := pre[token].Storage[bobSlot]; !ok {
				t.Errorf("prestate lacks bob's slot, which the transfer read before writing")
			}
			continue
		}
		var diff struct {
			Pre  map[common.Address]account `json:"pre"`
			Post map[common.Address]account `json:"post"`
		}
		if err := json.Unmarshal(res, &diff); err != nil {
			t.Fatal(err)
		}
		if got := diff.Pre[token].Storage[aliceSlot]; got != common.BigToHash(big.NewInt(100)) {
			t.Errorf("diff pre: alice's slot = %s, want 100", got.Hex())
		}
		if got := diff.Post[token].Storage[bobSlot]; got != common.BigToHash(big.NewInt(7)) {
			t.Errorf("diff post: bob's slot = %s, want 7; post %v", got.Hex(), diff.Post[token].Storage)
		}
	}
}

// balances is slot 4 of the bsc.cas20 ERC-7201 namespace (BEP-702 §3.17).
func cas20BalanceSlot(a common.Address) common.Hash {
	x := new(uint256.Int).SetBytes(crypto.Keccak256([]byte("bsc.cas20")))
	x.SubUint64(x, 1)
	root := crypto.Keccak256Hash(x.Bytes())
	root[31] = 0
	base := new(uint256.Int).SetBytes(root[:])
	base.AddUint64(base, 4)
	return crypto.Keccak256Hash(common.LeftPadBytes(a.Bytes(), 32), base.Bytes())
}
