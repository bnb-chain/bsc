package runtime_test

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

// Reuse USDT runtime code already embedded in mir_real_bench_test.go
const usdtHexForBlock = usdtHex

// buildUSDTBlockBundle constructs a single block containing many ERC20 calls
// against a contract address seeded with USDT code in genesis.
type tb interface{ Fatalf(string, ...any) }

func buildUSDTBlockBundle(t tb, signer types.Signer, key *ecdsa.PrivateKey, sender common.Address, contract common.Address, calls [][]byte, nRepeat int) (*types.Block, types.Receipts, *core.Genesis) {
	db := rawdb.NewMemoryDatabase()
	// Decode runtime code
	code, err := hex.DecodeString(usdtHexForBlock[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}
	// Genesis with sender funded and contract code installed
	genesis := &core.Genesis{
		Config:   params.MainnetChainConfig,
		GasLimit: 200_000_000,
		Alloc: core.GenesisAlloc{
			sender:   {Balance: new(big.Int).SetUint64(1_000_000_000_000_000_000), Nonce: 0},
			contract: {Balance: big.NewInt(0), Code: code},
		},
	}
	engine := ethash.NewFaker()
	triedb := triedb.NewDatabase(db, nil)
	defer triedb.Close()
	genesisBlock, err := genesis.Commit(db, triedb)
	if err != nil {
		t.Fatalf("genesis commit: %v", err)
	}

	// Generate 1 block with repeated calls
	var txns []*types.Transaction
	blocks, receipts := core.GenerateChain(genesis.Config, genesisBlock, engine, db, 1, func(i int, gen *core.BlockGen) {
		nonce := gen.TxNonce(sender)
		gasPrice := big.NewInt(0)
		for k := 0; k < nRepeat; k++ {
			for _, input := range calls {
				raw := &types.LegacyTx{
					Nonce:    nonce,
					To:       &contract,
					Value:    big.NewInt(0),
					Gas:      200000,
					GasPrice: gasPrice,
					Data:     input,
				}
				tx, err := types.SignTx(types.NewTx(raw), signer, key)
				if err != nil {
					t.Fatalf("sign tx: %v", err)
				}
				gen.AddTx(tx)
				txns = append(txns, tx)
				nonce++
			}
		}
	})
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	return blocks[0], receipts[0], genesis
}

// Shared helper to build a USDT-heavy single block for benchmarks
func buildUSDTBenchInputs(b tb) (key *ecdsa.PrivateKey, sender common.Address, contract common.Address, signer types.Signer, payloads [][]byte, block *types.Block, genesis *core.Genesis) {
	key, err := crypto.GenerateKey()
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}
	sender = crypto.PubkeyToAddress(key.PublicKey)
	contract = common.BytesToAddress([]byte("usdt_block_bench"))
	signer = types.HomesteadSigner{}
	// Prepare ERC20 call payloads (approve, transfer)
	zeroAddress := make([]byte, 32)
	oneUint := make([]byte, 32)
	oneUint[31] = 1
	payloads = [][]byte{
		append([]byte{0x09, 0x5e, 0xa7, 0xb3}, append(append([]byte{}, zeroAddress...), oneUint...)...), // approve
		append([]byte{0xa9, 0x05, 0x9c, 0xbb}, append([]byte{}, zeroAddress...)...),                     // transfer
	}
	// Build a 1-block bundle with many calls
	block, _, genesis = buildUSDTBlockBundle(b, signer, key, sender, contract, payloads, 300) // ~300 tx
	return
}

// BenchmarkUSDT_Block_BaseThroughput executes the USDT block using the base EVM.
// Reports Mgas/s and total gas used clearly.
func BenchmarkUSDT_Block_BaseThroughput(b *testing.B) {
	key, sender, contract, signer, payloads, block, genesis := buildUSDTBenchInputs(b)
	_ = key
	_ = sender
	_ = contract
	_ = signer
	_ = payloads
	run := func(cfg vm.Config) (time.Duration, uint64) {
		db := rawdb.NewMemoryDatabase()
		engine := ethash.NewFaker()
		chain, err := core.NewBlockChain(db, nil, genesis, nil, engine, cfg, nil, nil)
		if err != nil {
			b.Fatalf("new blockchain: %v", err)
		}
		defer chain.Stop()
		start := time.Now()
		if _, err := chain.InsertChain([]*types.Block{block}); err != nil {
			b.Fatalf("insert chain: %v", err)
		}
		elapsed := time.Since(start)
		receipts := chain.GetReceiptsByHash(block.Hash())
		var used uint64
		for _, r := range receipts {
			used += r.GasUsed
		}
		return elapsed, used
	}
	cfgBase := vm.Config{EnableOpcodeOptimizations: false}
	// Loop over b.N to make ns/op meaningful and stabilize metrics
	var totalElapsed time.Duration
	var totalGas uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		elapsed, gas := run(cfgBase)
		totalElapsed += elapsed
		totalGas += gas
	}
	// Aggregate throughput over total elapsed; report average gas per run
	if totalElapsed > 0 {
		mgasPerSec := (float64(totalGas) / 1e6) / totalElapsed.Seconds()
		b.ReportMetric(mgasPerSec, "Mgas/s")
	}
	if b.N > 0 {
		avgGas := float64(totalGas) / float64(b.N)
		b.ReportMetric(avgGas, "gas_total")
	}
}

// BenchmarkUSDT_Block_MIRThroughput executes the same USDT block using MIR.
// Reports Mgas/s and total gas used clearly.
func BenchmarkUSDT_Block_MIRThroughput(b *testing.B) {
	key, sender, contract, signer, payloads, block, genesis := buildUSDTBenchInputs(b)
	_ = key
	_ = sender
	_ = contract
	_ = signer
	_ = payloads
	run := func(cfg vm.Config) (time.Duration, uint64) {
		db := rawdb.NewMemoryDatabase()
		engine := ethash.NewFaker()
		chain, err := core.NewBlockChain(db, nil, genesis, nil, engine, cfg, nil, nil)
		if err != nil {
			b.Fatalf("new blockchain: %v", err)
		}
		defer chain.Stop()
		start := time.Now()
		if _, err := chain.InsertChain([]*types.Block{block}); err != nil {
			b.Fatalf("insert chain: %v", err)
		}
		elapsed := time.Since(start)
		receipts := chain.GetReceiptsByHash(block.Hash())
		var used uint64
		for _, r := range receipts {
			used += r.GasUsed
		}
		return elapsed, used
	}
	compiler.EnableOpcodeParse()
	cfgMIR := vm.Config{EnableOpcodeOptimizations: true, EnableMIR: true, EnableMIRInitcode: true, MIRStrictNoFallback: true}
	// Loop over b.N to make ns/op meaningful and stabilize metrics
	var totalElapsed time.Duration
	var totalGas uint64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		elapsed, gas := run(cfgMIR)
		totalElapsed += elapsed
		totalGas += gas
	}
	// Aggregate throughput over total elapsed; report average gas per run
	if totalElapsed > 0 {
		mgasPerSec := (float64(totalGas) / 1e6) / totalElapsed.Seconds()
		b.ReportMetric(mgasPerSec, "Mgas/s")
	}
	if b.N > 0 {
		avgGas := float64(totalGas) / float64(b.N)
		b.ReportMetric(avgGas, "gas_total")
	}
}
