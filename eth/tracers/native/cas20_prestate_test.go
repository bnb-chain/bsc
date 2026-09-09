package native

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
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

var (
	cas20Alice = common.HexToAddress("0x111111")
	cas20Bob   = common.HexToAddress("0x222222")
	cas20Token = common.HexToAddress("0xca52000000000000000000000000000000000001")
)

// ERC-7201 root of a namespace, as BEP-702 §3.17 derives it.
func cas20Root(namespace string) *uint256.Int {
	x := new(uint256.Int).SetBytes(crypto.Keccak256([]byte(namespace)))
	x.SubUint64(x, 1)
	root := crypto.Keccak256Hash(x.Bytes())
	root[31] = 0
	return new(uint256.Int).SetBytes(root[:])
}

// balances is slot 4 of bsc.cas20.
func cas20BalanceSlot(a common.Address) common.Hash {
	base := cas20Root("bsc.cas20")
	base.AddUint64(base, 4)
	return crypto.Keccak256Hash(common.LeftPadBytes(a.Bytes(), 32), base.Bytes())
}

func cas20JennerConfig() *params.ChainConfig {
	cfg := *params.TestChainConfig
	cfg.JennerTime = new(uint64)
	cfg.Parlia = &params.ParliaConfig{}
	return &cfg
}

// cas20Run applies one transaction from alice to `to` under the given tracer.
func cas20Run(t *testing.T, statedb *state.StateDB, cfg *params.ChainConfig, hooks *tracing.Hooks, to common.Address, input []byte) *types.Receipt {
	t.Helper()
	blockCtx := vm.BlockContext{CanTransfer: core.CanTransfer, Transfer: core.Transfer, BlockNumber: big.NewInt(1), Time: 1, Difficulty: big.NewInt(1), BaseFee: big.NewInt(0), GasLimit: 30_000_000}
	evm := vm.NewEVM(blockCtx, state.NewHookedState(statedb, hooks), cfg, vm.Config{Tracer: hooks})
	defer evm.Release()
	tx := types.NewTx(&types.LegacyTx{To: &to, Gas: 2_000_000, GasPrice: big.NewInt(1), Data: input})
	msg := &core.Message{From: cas20Alice, To: &to, GasLimit: tx.Gas(), GasPrice: uint256.NewInt(1), GasFeeCap: uint256.NewInt(1), GasTipCap: uint256.NewInt(1), Value: new(uint256.Int), Data: input}
	statedb.SetTxContext(tx.Hash(), 0)
	receipt, err := core.ApplyTransactionWithEVM(msg, core.NewGasPool(tx.Gas()), statedb, blockCtx.BlockNumber, common.Hash{}, blockCtx.Time, tx, evm)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

// A token holding 100 for alice, and the transfer(bob, 7) calldata.
func cas20TransferFixture(t *testing.T) (*state.StateDB, []byte) {
	t.Helper()
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.SetCode(cas20Token, vm.CAS20MarkerCode, tracing.CodeChangeContractCreation)
	statedb.SetState(cas20Token, cas20BalanceSlot(cas20Alice), common.BigToHash(big.NewInt(100)))
	statedb.SetBalance(cas20Alice, uint256.NewInt(1_000_000_000), tracing.BalanceChangeUnspecified)
	statedb.Finalise(false)
	input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(cas20Bob.Bytes(), 32)...)
	return statedb, append(input, common.LeftPadBytes([]byte{7}, 32)...)
}

type prestateDiff struct {
	Pre  map[common.Address]account `json:"pre"`
	Post map[common.Address]account `json:"post"`
}

// A CAS20 transfer touches storage through native code, not SLOAD/SSTORE, so the
// tracer has to learn of the slots through OnStorageRead.
func TestPrestateSeesCAS20Storage(t *testing.T) {
	aliceSlot, bobSlot := cas20BalanceSlot(cas20Alice), cas20BalanceSlot(cas20Bob)
	for _, diffMode := range []bool{false, true} {
		statedb, input := cas20TransferFixture(t)
		cfg := cas20JennerConfig()
		tr, err := newPrestateTracer(&tracers.Context{}, json.RawMessage(`{"diffMode":`+map[bool]string{false: "false", true: "true"}[diffMode]+`}`), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if r := cas20Run(t, statedb, cfg, tr.Hooks, cas20Token, input); r.Status != types.ReceiptStatusSuccessful {
			t.Fatal("transfer failed")
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
			if got := pre[cas20Token].Storage[aliceSlot]; got != common.BigToHash(big.NewInt(100)) {
				t.Errorf("prestate: alice's slot = %s, want 100; storage %v", got.Hex(), pre[cas20Token].Storage)
			}
			if _, ok := pre[cas20Token].Storage[bobSlot]; !ok {
				t.Errorf("prestate lacks bob's slot, which the transfer read before writing")
			}
			continue
		}
		var diff prestateDiff
		if err := json.Unmarshal(res, &diff); err != nil {
			t.Fatal(err)
		}
		if got := diff.Pre[cas20Token].Storage[aliceSlot]; got != common.BigToHash(big.NewInt(100)) {
			t.Errorf("diff pre: alice's slot = %s, want 100", got.Hex())
		}
		if got := diff.Post[cas20Token].Storage[bobSlot]; got != common.BigToHash(big.NewInt(7)) {
			t.Errorf("diff post: bob's slot = %s, want 7; post %v", got.Hex(), diff.Post[cas20Token].Storage)
		}
	}
}

// The mux tracer has to forward the native-access hooks, or a prestate tracer
// requested alongside a call tracer loses every CAS20 slot again.
func TestMuxForwardsCAS20StorageReads(t *testing.T) {
	statedb, input := cas20TransferFixture(t)
	cfg := cas20JennerConfig()
	pre, err := newPrestateTracer(&tracers.Context{}, json.RawMessage(`{"diffMode":true}`), cfg)
	if err != nil {
		t.Fatal(err)
	}
	call, err := newCallTracer(&tracers.Context{}, json.RawMessage(`{}`), cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := NewMuxTracer([]string{"prestateTracer", "callTracer"}, []*tracers.Tracer{pre, call})
	if err != nil {
		t.Fatal(err)
	}
	if r := cas20Run(t, statedb, cfg, mux.Hooks, cas20Token, input); r.Status != types.ReceiptStatusSuccessful {
		t.Fatal("transfer failed")
	}
	res, err := mux.GetResult()
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatalf("%v: %q", err, res)
	}
	var diff prestateDiff
	if err := json.Unmarshal(out["prestateTracer"], &diff); err != nil {
		t.Fatal(err)
	}
	if got := diff.Post[cas20Token].Storage[cas20BalanceSlot(cas20Bob)]; got != common.BigToHash(big.NewInt(7)) {
		t.Errorf("through the mux, diff post: bob's slot = %s, want 7", got.Hex())
	}
}

// createCAS20 reads the target account before it plants the sentinel, and the
// prestate has to capture it then: no code before, the sentinel after.
func TestPrestateSeesCAS20Creation(t *testing.T) {
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	// The ActivationRegistry with the Asset feature open, as the fork and governance would leave it.
	statedb.SetCode(vm.CAS20ActivationRegistryAddress, vm.CAS20MarkerCode, tracing.CodeChangeContractCreation)
	featuresSlot := cas20Root("bsc.activation_registry") // slot 0 of the namespace
	featureKey := crypto.Keccak256Hash(crypto.Keccak256([]byte("bsc.cas20_asset")), featuresSlot.Bytes())
	statedb.SetState(vm.CAS20ActivationRegistryAddress, featureKey, common.Hash{31: 1})
	statedb.SetBalance(cas20Alice, uint256.NewInt(1_000_000_000), tracing.BalanceChangeUnspecified)
	statedb.Finalise(false)

	// createCAS20(ASSET, salt, abi.encode(CAS20AssetCreateParams{1, "N", "S", alice, 18}), [])
	def, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"createCAS20","inputs":[{"name":"variant","type":"uint8"},{"name":"salt","type":"bytes32"},{"name":"params","type":"bytes"},{"name":"initCalls","type":"bytes[]"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	paramsT, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "version", Type: "uint8"}, {Name: "name", Type: "string"}, {Name: "symbol", Type: "string"},
		{Name: "initialAdmin", Type: "address"}, {Name: "decimals", Type: "uint8"},
	})
	params, err := abi.Arguments{{Type: paramsT}}.Pack(struct {
		Version      uint8
		Name         string
		Symbol       string
		InitialAdmin common.Address
		Decimals     uint8
	}{1, "N", "S", cas20Alice, 18})
	if err != nil {
		t.Fatal(err)
	}
	input, err := def.Pack("createCAS20", uint8(0), [32]byte{0xa1}, params, [][]byte{})
	if err != nil {
		t.Fatal(err)
	}

	cfg := cas20JennerConfig()
	tr, err := newPrestateTracer(&tracers.Context{}, json.RawMessage(`{"diffMode":true}`), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r := cas20Run(t, statedb, cfg, tr.Hooks, vm.CAS20FactoryAddress, input); r.Status != types.ReceiptStatusSuccessful {
		t.Fatal("createCAS20 failed")
	}
	res, err := tr.GetResult()
	if err != nil {
		t.Fatal(err)
	}
	var diff prestateDiff
	if err := json.Unmarshal(res, &diff); err != nil {
		t.Fatal(err)
	}
	var created common.Address
	for addr, acc := range diff.Post {
		if vm.IsCAS20Address(addr) && acc.Code != nil {
			created = addr
		}
	}
	if created == (common.Address{}) {
		t.Fatalf("no created token in the post-state: %v", diff.Post)
	}
	// The account did not exist before the transaction, so it is absent from the
	// pre-state and its whole post-state is reported; had it been captured only
	// after the sentinel was written, the code would have looked unchanged and the
	// token would not appear in the post-state at all.
	if pre, ok := diff.Pre[created]; ok {
		t.Errorf("pre-state lists the new token as %+v, want no entry", pre)
	}
	if got := *diff.Post[created].Code; string(got) != string(vm.CAS20MarkerCode) {
		t.Errorf("post-state code = %x, want the sentinel", got)
	}
}
