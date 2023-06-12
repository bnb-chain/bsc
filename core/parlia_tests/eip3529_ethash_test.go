package parlia_tests

import (
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

func postLondonConfig() *params.ChainConfig {
	config := *params.TestChainConfig
	config.LondonBlock = big.NewInt(0)
	return &config
}

func preLondonConfig() *params.ChainConfig {
	config := *params.TestChainConfig
	config.LondonBlock = nil
	return &config
}

func TestSelfDestructGasPreLondon(t *testing.T) {
	bytecode := []byte{
		byte(vm.PC),
		byte(vm.SELFDESTRUCT),
	}

	// Expected gas is (intrinsic +  pc + cold load (due to legacy tx)  + selfdestruct cost ) / 2
	// The refund of 24000 gas (i.e. params.SelfdestructRefundGas) is not applied since refunds pre-EIP3529 are
	// capped to half of the transaction's gas.
	expectedGasUsed := (params.TxGas + vm.GasQuickStep + params.ColdAccountAccessCostEIP2929 + params.SelfdestructGasEIP150) / 2
	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.LegacyTx{
		Nonce:    0,
		To:       &aa,
		Gas:      60_000,
		GasPrice: newGwei(5),
	}
	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: nil,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}
	TestGasUsage(t, preLondonConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsed)
}

func TestSstoreModifyGasPreLondon(t *testing.T) {
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
	// i.e. no refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.ColdSloadCostEIP2929 + (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929)

	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.LegacyTx{
		Nonce:    0,
		To:       &aa,
		Gas:      60_000,
		GasPrice: newGwei(5),
	}

	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: initialStorage,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}
	TestGasUsage(t, preLondonConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsed)
}

func TestSstoreClearGasPreLondon(t *testing.T) {
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

	// Expected gas is (intrinsic +  2*pushGas + cold load (due to legacy tx) + SstoreReset (a->b such that a!=0) ) / 2
	// The refund of params.SstoreClearsScheduleRefundEIP2200 is not applied because of the refund cap to half the gas cost.
	expectedGasUsage := (params.TxGas + 2*vm.GasFastestStep + params.ColdSloadCostEIP2929 + (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929)) / 2
	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.LegacyTx{
		Nonce:    0,
		To:       &aa,
		Gas:      60_000,
		GasPrice: newGwei(5),
	}
	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: initialStorage,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}
	TestGasUsage(t, preLondonConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsage)
}

func TestSstoreGasPreLondon(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x3, // value
		byte(vm.PUSH1), 0x3, // location
		byte(vm.SSTORE), // Set slot[3] = 3
	}
	// Expected gas is intrinsic +  2*pushGas + cold load (due to legacy tx) +  SstoreGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.ColdSloadCostEIP2929 + params.SstoreSetGasEIP2200

	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.LegacyTx{
		Nonce:    0,
		To:       &aa,
		Gas:      60_000,
		GasPrice: newGwei(5),
	}
	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: nil,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}
	TestGasUsage(t, preLondonConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsed)
}

func TestSelfDestructGasPostLondon(t *testing.T) {
	bytecode := []byte{
		byte(vm.PC),
		byte(vm.SELFDESTRUCT),
	}
	// Expected gas is intrinsic +  pc + cold load (due to legacy tx) + SelfDestructGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + vm.GasQuickStep + params.ColdAccountAccessCostEIP2929 + params.SelfdestructGasEIP150

	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.LegacyTx{
		Nonce:    0,
		To:       &aa,
		Gas:      60_000,
		GasPrice: newGwei(5),
	}
	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: nil,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}
	TestGasUsage(t, postLondonConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsed)
}

func TestSstoreGasPostLondon(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x3, // value
		byte(vm.PUSH1), 0x3, // location
		byte(vm.SSTORE), // Set slot[3] = 3
	}
	// Expected gas is intrinsic +  2*pushGas + cold load (due to legacy tx) +  SstoreGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.ColdSloadCostEIP2929 + params.SstoreSetGasEIP2200
	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.LegacyTx{
		Nonce:    0,
		To:       &aa,
		Gas:      60_000,
		GasPrice: newGwei(5),
	}
	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: nil,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}
	TestGasUsage(t, postLondonConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsed)
}

func TestSstoreModifyGasPostLondon(t *testing.T) {
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
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.ColdSloadCostEIP2929 + (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929)

	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.LegacyTx{
		Nonce:    0,
		To:       &aa,
		Gas:      60_000,
		GasPrice: newGwei(5),
	}
	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: initialStorage,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}

	TestGasUsage(t, postLondonConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsed)
}

func TestSstoreClearGasPostLondon(t *testing.T) {
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

	// Expected gas is intrinsic +  2*pushGas + cold load (due to legacy tx) + SstoreReset (a->b such that a!=0) - sstoreClearGasRefund
	expectedGasUsage := params.TxGas + 2*vm.GasFastestStep + params.ColdSloadCostEIP2929 + (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) - params.SstoreClearsScheduleRefundEIP3529
	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.LegacyTx{
		Nonce:    0,
		To:       &aa,
		Gas:      60_000,
		GasPrice: newGwei(5),
	}
	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: initialStorage,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}
	TestGasUsage(t, postLondonConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsage)
}
