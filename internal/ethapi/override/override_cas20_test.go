package override

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// A code override on a CAS20 address has to disable the prefix routing, or the
// call would still reach native code whatever code the override installed.
func TestCAS20CodeOverrideRunsTheOverride(t *testing.T) {
	returns42 := hexutil.Bytes(common.FromHex("602a60005260206000f3"))
	for _, tc := range []struct {
		name string
		addr common.Address
		code []byte // what the account holds before the override
	}{
		{"a static precompile, for contrast", common.HexToAddress("0x1"), nil},
		{"an initialized token", common.HexToAddress("0xca52000000000000000000000000000000000001"), vm.CAS20MarkerCode},
		{"a reserved address holding no token", common.HexToAddress("0xca52000000000000000000000000000000000002"), nil},
		{"the factory", vm.CAS20FactoryAddress, vm.CAS20MarkerCode},
		{"the ActivationRegistry", vm.CAS20ActivationRegistryAddress, vm.CAS20MarkerCode},
		{"the PolicyRegistry", vm.CAS20PolicyRegistryAddress, vm.CAS20MarkerCode},
	} {
		statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		if tc.code != nil {
			statedb.SetCode(tc.addr, tc.code, tracing.CodeChangeContractCreation)
		}
		cfg := *params.TestChainConfig
		cfg.JennerTime = new(uint64)
		cfg.Parlia = &params.ParliaConfig{}
		precompiles := vm.ActivePrecompiledContracts(cfg.Rules(big.NewInt(1), false, 1))
		over := StateOverride{tc.addr: OverrideAccount{Code: &returns42}}
		if err := over.Apply(statedb, precompiles); err != nil {
			t.Fatalf("%s: Apply: %v", tc.name, err)
		}
		blockCtx := vm.BlockContext{BlockNumber: big.NewInt(1), Time: 1,
			CanTransfer: func(vm.StateDB, common.Address, *uint256.Int) bool { return true },
			Transfer:    func(vm.StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {}}
		evm := vm.NewEVM(blockCtx, statedb, &cfg, vm.Config{})
		evm.SetPrecompiles(precompiles)
		ret, _, err := evm.Call(common.Address{19: 9}, tc.addr, nil, vm.NewGasBudget(100_000), new(uint256.Int))
		evm.Release()
		if err != nil || new(uint256.Int).SetBytes(ret).Uint64() != 42 {
			t.Errorf("%s: override not executed: ret %x err %v", tc.name, ret, err)
		}
	}
}

// Without an override the routing is untouched: the same call reaches native code.
func TestCAS20RoutingWithoutOverride(t *testing.T) {
	token := common.HexToAddress("0xca52000000000000000000000000000000000001")
	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	statedb.SetCode(token, vm.CAS20MarkerCode, tracing.CodeChangeContractCreation)
	cfg := *params.TestChainConfig
	cfg.JennerTime = new(uint64)
	cfg.Parlia = &params.ParliaConfig{}
	precompiles := vm.ActivePrecompiledContracts(cfg.Rules(big.NewInt(1), false, 1))
	if err := (&StateOverride{}).Apply(statedb, precompiles); err != nil {
		t.Fatal(err)
	}
	blockCtx := vm.BlockContext{BlockNumber: big.NewInt(1), Time: 1,
		CanTransfer: func(vm.StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(vm.StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {}}
	evm := vm.NewEVM(blockCtx, statedb, &cfg, vm.Config{})
	evm.SetPrecompiles(precompiles)
	// Empty calldata is an unknown selector to the token: an empty revert, not the
	// 0xEF sentinel executing as bytecode.
	ret, _, err := evm.Call(common.Address{19: 9}, token, nil, vm.NewGasBudget(100_000), new(uint256.Int))
	evm.Release()
	if err != vm.ErrExecutionReverted || len(ret) != 0 {
		t.Errorf("unoverridden token: ret %x err %v, want an empty revert from native code", ret, err)
	}
}
