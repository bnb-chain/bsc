package parlia_tests

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

func newGwei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(params.GWei))
}

func postHertzConfig() *params.ChainConfig {
	config := *params.ParliaTestChainConfig
	return &config
}

func preHertzConfig() *params.ChainConfig {
	config := *params.ParliaTestChainConfig
	config.LondonBlock = nil
	config.BerlinBlock = nil
	config.HertzBlock = nil
	return &config
}

func TestSelfDestructGasPreEIP3529(t *testing.T) {
	bytecode := []byte{
		byte(vm.PC),
		byte(vm.SELFDESTRUCT),
	}

	// Expected gas is (intrinsic + selfdestruct cost ) / 2
	// The refund of 24000 gas (i.e. params.SelfdestructRefundGas) is not applied since refunds pre-EIP3529 are
	// capped to half of the transaction's gas.
	expectedGasUsed := (params.TxGas + vm.GasQuickStep + params.SelfdestructGasEIP150) / 2
	testGasUsage(t, preHertzConfig(), ethash.NewFaker(), bytecode, nil, 60_000, expectedGasUsed)
}

func TestSstoreClearGasPreEIP3529(t *testing.T) {
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
	testGasUsage(t, preHertzConfig(), ethash.NewFaker(), bytecode, initialStorage, 60_000, expectedGasUsage)
}

func TestSstoreModifyGasPreEIP3529(t *testing.T) {
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
	testGasUsage(t, preHertzConfig(), ethash.NewFaker(), bytecode, initialStorage, 60_000, expectedGasUsed)
}

func TestSstoreGasPreEIP3529(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x3, // value
		byte(vm.PUSH1), 0x3, // location
		byte(vm.SSTORE), // Set slot[3] = 3
	}
	// Expected gas is intrinsic +  2*pushGas  +  SstoreGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.SstoreSetGasEIP2200
	testGasUsage(t, preHertzConfig(), ethash.NewFaker(), bytecode, nil, 60_000, expectedGasUsed)
}

func TestSelfDestructGasPostEIP3529(t *testing.T) {
	bytecode := []byte{
		byte(vm.PC),
		byte(vm.SELFDESTRUCT),
	}
	// Expected gas is intrinsic +  pc + cold load (due to legacy tx) + SelfDestructGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + vm.GasQuickStep + params.ColdAccountAccessCostEIP2929 + params.SelfdestructGasEIP150
	testGasUsage(t, postHertzConfig(), ethash.NewFaker(), bytecode, nil, 60_000, expectedGasUsed)
}

func TestSstoreGasPostEIP3529(t *testing.T) {
	bytecode := []byte{
		byte(vm.PUSH1), 0x3, // value
		byte(vm.PUSH1), 0x3, // location
		byte(vm.SSTORE), // Set slot[3] = 3
	}
	// Expected gas is intrinsic +  2*pushGas + cold load (due to legacy tx) +  SstoreGas
	// i.e. No refund
	expectedGasUsed := params.TxGas + 2*vm.GasFastestStep + params.ColdSloadCostEIP2929 + params.SstoreSetGasEIP2200
	testGasUsage(t, postHertzConfig(), ethash.NewFaker(), bytecode, nil, 60_000, expectedGasUsed)
}

func TestSstoreModifyGasPostEIP3529(t *testing.T) {
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
	testGasUsage(t, postHertzConfig(), ethash.NewFaker(), bytecode, initialStorage, 60_000, expectedGasUsed)
}

func TestSstoreClearGasPostEIP3529(t *testing.T) {
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
	testGasUsage(t, postHertzConfig(), ethash.NewFaker(), bytecode, initialStorage, 60_000, expectedGasUsage)
}

// Test the gas used by running a transaction sent to a smart contract with given bytecode and storage.
func testGasUsage(t *testing.T, config *params.ChainConfig, engine consensus.Engine, bytecode []byte, initialStorage map[common.Hash]common.Hash, initialGas, expectedGasUsed uint64) {
	var (
		aa = common.HexToAddress("0x000000000000000000000000000000000000aaaa")

		// Generate a canonical chain to act as the main dataset
		db = rawdb.NewMemoryDatabase()

		// A sender who makes transactions, has some funds
		key, _        = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		address       = crypto.PubkeyToAddress(key.PublicKey)
		balanceBefore = big.NewInt(1000000000000000)
		gspec         = &core.Genesis{
			Config: config,
			Alloc: core.GenesisAlloc{
				address: {Balance: balanceBefore},
				aa: {
					Code:    bytecode,
					Storage: initialStorage,
					Nonce:   0,
					Balance: big.NewInt(0),
				},
			},
		}
		genesis = gspec.MustCommit(db)
	)

	blocks, _ := core.GenerateChain(gspec.Config, genesis, engine, db, 1, func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{1})

		// One transaction to 0xAAAA
		signer := types.LatestSigner(gspec.Config)
		tx, _ := types.SignNewTx(key, signer, &types.LegacyTx{
			Nonce:    0,
			To:       &aa,
			Gas:      initialGas,
			GasPrice: newGwei(5),
		})
		b.AddTx(tx)
	})

	// Import the canonical chain
	diskdb := rawdb.NewMemoryDatabase()
	gspec.MustCommit(diskdb)

	chain, err := core.NewBlockChain(diskdb, nil, gspec.Config, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create tester chain: %v", err)
	}
	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("block %d: failed to insert into chain: %v", n, err)
	}

	block := chain.GetBlockByNumber(1)

	if block.GasUsed() != expectedGasUsed {
		t.Fatalf("incorrect amount of gas spent: expected %d, got %d", expectedGasUsed, block.GasUsed())
	}
}
