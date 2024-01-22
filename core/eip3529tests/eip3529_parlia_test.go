package eip3529tests

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

func postHertzConfig() *params.ChainConfig {
	config := *params.ParliaTestChainConfig
	return &config
}

func preHertzConfig() *params.ChainConfig {
	config := *params.ParliaTestChainConfig
	config.LondonBlock = nil
	config.BerlinBlock = nil
	config.HertzBlock = nil
	config.HertzfixBlock = nil
	return &config
}

func TestSelfDestructGasPreHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PC),
		byte(vm.SELFDESTRUCT),
	}

	// Expected gas is (intrinsic + selfdestruct cost ) / 2
	// The refund of 24000 gas (i.e. params.SelfdestructRefundGas) is not applied since refunds pre-EIP3529 are
	// capped to half of the transaction's gas.
	expectedGasUsed := (params.TxGas + vm.GasQuickStep + params.SelfdestructGasEIP150) / 2
	TestGasUsage(t, preHertzConfig(), ethash.NewFaker(), bytecode, nil, 60_000, expectedGasUsed)
}

func TestSstoreClearGasPreHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x0, // value
		byte(vm.PUSH1), 0x1, // location
		byte(vm.SSTORE), // Set slot[1] = 0
	}
	// initialize contract storage
	initialStorage := make(map[common.Hash]common.Hash)
	// Populate two slots
	initialStorage[common.HexToHash("01")] = common.HexToHash("01")
	initialStorage[common.HexToHash("02")] = common.HexToHash("02")

	// Expected gas is (intrinsic +  2*pushGas  + SstoreReset (a->b such that a!=0) ) / 2
	// The refund of params.SstoreClearsScheduleRefundEIP2200 is not applied because of the refund cap to half the gas cost.
	expectedGasUsage := (params.TxGas + 2*vm.GasFastestStep + params.SstoreResetGasEIP2200) / 2
	TestGasUsage(t, preHertzConfig(), ethash.NewFaker(), bytecode, initialStorage, 60_000, expectedGasUsage)
}

func TestSstoreModifyGasPreHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x3, // value
		byte(vm.PUSH1), 0x1, // location
		byte(vm.SSTORE), // Set slot[1] = 3
	}
	// initialize contract storage
	initialStorage := make(map[common.Hash]common.Hash)
	// Populate two slots
	initialStorage[common.HexToHash("01")] = common.HexToHash("01")
	initialStorage[common.HexToHash("02")] = common.HexToHash("02")
	// Expected gas is intrinsic +  2*pushGas + SstoreReset (a->b such that a!=0)
	// i.e. no refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.SstoreResetGasEIP2200
	TestGasUsage(t, preHertzConfig(), ethash.NewFaker(), bytecode, initialStorage, 60_000, expectedGasUsed)
}

func TestSstoreGasPreHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x3, // value
		byte(vm.PUSH1), 0x3, // location
		byte(vm.SSTORE), // Set slot[3] = 3
	}
	// Expected gas is intrinsic +  2*pushGas  +  SstoreGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.SstoreSetGasEIP2200
	TestGasUsage(t, preHertzConfig(), ethash.NewFaker(), bytecode, nil, 60_000, expectedGasUsed)
}

func TestSelfDestructGasPostHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PC),
		byte(vm.SELFDESTRUCT),
	}
	// Expected gas is intrinsic +  pc + cold load (due to legacy tx) + SelfDestructGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + vm.GasQuickStep + params.ColdAccountAccessCostEIP2929 + params.SelfdestructGasEIP150
	TestGasUsage(t, postHertzConfig(), ethash.NewFaker(), bytecode, nil, 60_000, expectedGasUsed)
}

func TestSstoreGasPostHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x3, // value
		byte(vm.PUSH1), 0x3, // location
		byte(vm.SSTORE), // Set slot[3] = 3
	}
	// Expected gas is intrinsic +  2*pushGas + cold load (due to legacy tx) +  SstoreGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.ColdSloadCostEIP2929 + params.SstoreSetGasEIP2200
	TestGasUsage(t, postHertzConfig(), ethash.NewFaker(), bytecode, nil, 60_000, expectedGasUsed)
}

func TestSstoreModifyGasPostHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x3, // value
		byte(vm.PUSH1), 0x1, // location
		byte(vm.SSTORE), // Set slot[1] = 3
	}
	// initialize contract storage
	initialStorage := make(map[common.Hash]common.Hash)
	// Populate two slots
	initialStorage[common.HexToHash("01")] = common.HexToHash("01")
	initialStorage[common.HexToHash("02")] = common.HexToHash("02")
	// Expected gas is intrinsic +  2*pushGas + cold load (due to legacy tx) + SstoreReset (a->b such that a!=0)
	// i.e. No refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.SstoreResetGasEIP2200
	TestGasUsage(t, postHertzConfig(), ethash.NewFaker(), bytecode, initialStorage, 60_000, expectedGasUsed)
}

func TestSstoreClearGasPostHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x0, // value
		byte(vm.PUSH1), 0x1, // location
		byte(vm.SSTORE), // Set slot[1] = 0
	}
	// initialize contract storage
	initialStorage := make(map[common.Hash]common.Hash)
	// Populate two slots
	initialStorage[common.HexToHash("01")] = common.HexToHash("01")
	initialStorage[common.HexToHash("02")] = common.HexToHash("02")

	// Expected gas is intrinsic +  2*pushGas + SstoreReset (a->b such that a!=0) - sstoreClearGasRefund
	expectedGasUsage := params.TxGas + 2*vm.GasFastestStep + params.SstoreResetGasEIP2200 - params.SstoreClearsScheduleRefundEIP3529
	TestGasUsage(t, postHertzConfig(), ethash.NewFaker(), bytecode, initialStorage, 60_000, expectedGasUsage)
}
