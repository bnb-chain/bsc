package runtime_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// Simple arithmetic bytecode: PUSH1 1; PUSH1 2; ADD; PUSH1 3; MUL; STOP
var simpleAddMul = []byte{byte(compiler.PUSH1), 0x01, byte(compiler.PUSH1), 0x02, byte(compiler.ADD), byte(compiler.PUSH1), 0x03, byte(compiler.MUL), byte(compiler.STOP)}

// Compute 1+2, then *3, then return the 32-byte value (9)
var addMulReturn = []byte{
	byte(compiler.PUSH1), 0x01,
	byte(compiler.PUSH1), 0x02,
	byte(compiler.ADD),
	byte(compiler.PUSH1), 0x03,
	byte(compiler.MUL),
	// store at memory[0x00..0x20]
	byte(compiler.PUSH1), 0x00,
	byte(compiler.MSTORE),
	// return 32 bytes from 0x00
	byte(compiler.PUSH1), 0x20,
	byte(compiler.PUSH1), 0x00,
	byte(compiler.RETURN),
}

func BenchmarkMIRVsEVM_AddMul(b *testing.B) {
	// Dump MIR ops for visibility (not part of timing)
	if cfg, err := compiler.GenerateMIRCFG(common.Hash{}, simpleAddMul); err == nil && cfg != nil {
		ops := make([]byte, 0)
		for _, bb := range cfg.GetBasicBlocks() {
			for _, mir := range bb.Instructions() {
				if mir != nil {
					ops = append(ops, byte(mir.Op()))
				}
			}
		}
		if len(ops) > 0 {
			b.Logf("MIR ops for simpleAddMul: % x", ops)
		}
	}
	// Base EVM interpreter
	cfgBase := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: false,
		},
	}

	// MIR path: enable opcode optimizations and MIR opcode parsing
	cfgMIR := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig: vm.Config{
			EnableOpcodeOptimizations: true,
		},
	}

	b.Run("EVM_Base", func(b *testing.B) {
		// Fresh StateDB
		if cfgBase.State == nil {
			cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		// Build a proper env with State
		evm := runtime.NewEnv(cfgBase)
		address := common.BytesToAddress([]byte("contract"))
		sender := vm.AccountRef(cfgBase.Origin)
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, simpleAddMul)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Execute the contract
			_, _, err := evm.Call(sender, address, nil, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
			if err != nil {
				// ignore reverts; simple code should not revert
				continue
			}
		}
	})

	b.Run("MIR_Interpreter", func(b *testing.B) {
		// Ensure MIR opcode parsing is enabled so MIR CFG is generated
		compiler.EnableOpcodeParse()
		// Ensure a fresh StateDB exists
		if cfgMIR.State == nil {
			cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		// Fresh env with optimizations enabled
		evm := runtime.NewEnv(cfgMIR)
		address := common.BytesToAddress([]byte("contract"))
		sender := vm.AccountRef(cfgMIR.Origin)
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, simpleAddMul)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
			if err != nil {
				continue
			}
		}
	})
}

func BenchmarkMIRVsEVM_AddMulReturn(b *testing.B) {
	// Base EVM interpreter
	cfgBase := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	// MIR path
	cfgMIR := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: true},
	}

	b.Run("EVM_Base_Return", func(b *testing.B) {
		if cfgBase.State == nil {
			cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgBase)
		address := common.BytesToAddress([]byte("contract"))
		sender := vm.AccountRef(cfgBase.Origin)
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, addMulReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
			if err != nil {
				b.Fatalf("base call err: %v", err)
			}
		}
	})

	b.Run("MIR_Interpreter_Return", func(b *testing.B) {
		compiler.EnableOpcodeParse()
		if cfgMIR.State == nil {
			cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfgMIR)
		address := common.BytesToAddress([]byte("contract"))
		sender := vm.AccountRef(cfgMIR.Origin)
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, addMulReturn)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _, err := evm.Call(sender, address, nil, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
			if err != nil {
				b.Fatalf("mir call err: %v", err)
			}
		}
	})
}

func TestAddMulReturn_BaseAndMIR(t *testing.T) {
	if cfg, err := compiler.GenerateMIRCFG(common.Hash{}, addMulReturn); err == nil && cfg != nil {
		ops := make([]byte, 0)
		for _, bb := range cfg.GetBasicBlocks() {
			for _, mir := range bb.Instructions() {
				if mir != nil {
					ops = append(ops, byte(mir.Op()))
				}
			}
		}
		t.Logf("MIR ops addMulReturn: % x", ops)
	}
	// Base
	cfgBase := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: false},
	}
	if cfgBase.State == nil {
		cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	evm := runtime.NewEnv(cfgBase)
	addr := common.BytesToAddress([]byte("contract"))
	sender := vm.AccountRef(cfgBase.Origin)
	evm.StateDB.CreateAccount(addr)
	evm.StateDB.SetCode(addr, addMulReturn)
	ret, _, err := evm.Call(sender, addr, nil, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
	if err != nil {
		t.Fatalf("base call err: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("unexpected ret len %d", len(ret))
	}
	got := uint256.NewInt(0).SetBytes(ret)
	if !got.Eq(uint256.NewInt(9)) {
		t.Fatalf("base expected 9, got %s", got.String())
	}

	// MIR
	compiler.EnableOpcodeParse()
	cfgMIR := &runtime.Config{
		ChainConfig: params.MainnetChainConfig,
		GasLimit:    10_000_000,
		Origin:      common.Address{},
		BlockNumber: big.NewInt(1),
		Value:       big.NewInt(0),
		EVMConfig:   vm.Config{EnableOpcodeOptimizations: true},
	}
	if cfgMIR.State == nil {
		cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	}
	evm2 := runtime.NewEnv(cfgMIR)
	evm2.StateDB.CreateAccount(addr)
	evm2.StateDB.SetCode(addr, addMulReturn)
	ret2, _, err2 := evm2.Call(sender, addr, nil, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
	if err2 != nil {
		t.Fatalf("mir call err: %v", err2)
	}
	if len(ret2) != 32 {
		t.Fatalf("unexpected ret len (mir) %d", len(ret2))
	}
	got2 := uint256.NewInt(0).SetBytes(ret2)
	if !got2.Eq(uint256.NewInt(9)) {
		t.Fatalf("mir expected 9, got %s", got2.String())
	}
}
