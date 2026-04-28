package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/pq/mldsa"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// pqChainConfig returns a chain config with PQ active from timestamp 0.
func pqChainConfig() *params.ChainConfig {
	cfg := *params.AllEthashProtocolChanges // shallow copy
	cfg.PQForkTime = new(uint64)
	return &cfg
}

// TestPQTransactionE2E tests the full lifecycle of a PQ (ML-DSA-44) transaction:
//
//  1. Generate a PQ keypair; derive the sender address.
//  2. Pre-fund sender in the genesis block.
//  3. Build a one-block chain; include a PQ value-transfer tx.
//  4. Insert the block into a BlockChain.
//  5. Assert recipient received the transferred value.
//  6. Assert sender nonce incremented to 1.
func TestPQTransactionE2E(t *testing.T) {
	// --- 1. Keys & addresses ---
	pubKey, privKey, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("mldsa.GenerateKey: %v", err)
	}
	sender := crypto.PQPubkeyToAddress(pubKey)
	recipient := common.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	chainID := big.NewInt(1337) // matches AllEthashProtocolChanges
	signer := types.NewPQSigner(chainID)
	restore := types.SetPQRegistryBackend(func(addr common.Address) []byte {
		if addr == sender {
			return pubKey
		}
		return nil
	})
	defer restore()

	// --- 2. Genesis with funded sender ---
	fundAmount := new(big.Int).Mul(big.NewInt(1e6), big.NewInt(params.GWei)) // 1 mGwei
	config := pqChainConfig()
	genesis := &Genesis{
		Config:  config,
		BaseFee: big.NewInt(params.InitialBaseFee),
		Alloc: GenesisAlloc{
			sender: {Balance: new(big.Int).Mul(fundAmount, big.NewInt(100))},
		},
	}

	// --- 3. Generate one block containing the PQ tx ---
	transferValue := big.NewInt(1000)
	gasPrice := big.NewInt(params.InitialBaseFee) // baseFee == gasPrice for simplicity

	genDB, blocks, _ := GenerateChainWithGenesis(genesis, ethash.NewFaker(), 1,
		func(_ int, b *BlockGen) {
			tx := types.NewTx(&types.PQTxData{
				ChainID:  new(big.Int).Set(chainID),
				Nonce:    0,
				GasPrice: gasPrice,
				Gas:      params.TxGas,
				From:     sender,
				To:       &recipient,
				Value:    transferValue,
			})
			signed, err := types.SignPQTx(tx, signer, privKey)
			if err != nil {
				t.Fatalf("SignPQTx: %v", err)
			}
			b.AddTx(signed)
		},
	)
	_ = genDB

	// --- 4. Insert chain ---
	db := rawdb.NewMemoryDatabase()
	blockchain, err := NewBlockChain(db, genesis, ethash.NewFaker(),
		DefaultConfig().WithStateScheme(rawdb.HashScheme))
	if err != nil {
		t.Fatalf("NewBlockChain: %v", err)
	}
	defer blockchain.Stop()

	if _, err := blockchain.InsertChain(blocks); err != nil {
		t.Fatalf("InsertChain: %v", err)
	}

	// --- 5. Verify state ---
	state, err := blockchain.State()
	if err != nil {
		t.Fatalf("blockchain.State: %v", err)
	}

	recipientBal := state.GetBalance(recipient)
	wantBal, _ := uint256.FromBig(transferValue)
	if recipientBal.Cmp(wantBal) != 0 {
		t.Errorf("recipient balance: got %s want %s", recipientBal, wantBal)
	}

	senderNonce := state.GetNonce(sender)
	if senderNonce != 1 {
		t.Errorf("sender nonce: got %d want 1", senderNonce)
	}

	t.Logf("sender:    %s", sender.Hex())
	t.Logf("recipient: %s balance=%s", recipient.Hex(), recipientBal)
	t.Logf("nonce:     %d", senderNonce)
}

func TestPQRegistryPrecompileE2E(t *testing.T) {
	pubKey, _, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("mldsa.GenerateKey: %v", err)
	}
	caller := crypto.PQPubkeyToAddress(pubKey)
	precompileAddr := common.BytesToAddress([]byte{0x70})

	config := pqChainConfig()
	genesis := &Genesis{Config: config, BaseFee: big.NewInt(params.InitialBaseFee)}
	db := rawdb.NewMemoryDatabase()
	blockchain, err := NewBlockChain(db, genesis, ethash.NewFaker(),
		DefaultConfig().WithStateScheme(rawdb.HashScheme))
	if err != nil {
		t.Fatalf("NewBlockChain: %v", err)
	}
	defer blockchain.Stop()

	state, _ := blockchain.State()
	blockCtx := vm.BlockContext{
		BlockNumber: big.NewInt(0),
		Time:        0,
		Difficulty:  big.NewInt(1),
		GasLimit:    1e8,
		BaseFee:     big.NewInt(params.InitialBaseFee),
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
	}
	evm := vm.NewEVM(blockCtx, state, config, vm.Config{})

	precompiles := vm.ActivePrecompiledContracts(config.Rules(big.NewInt(0), false, 0))
	precompiles[precompileAddr] = vm.NewPQKeyRegistryPrecompile()
	evm.SetPrecompiles(precompiles)

	zero := uint256.NewInt(0)

	ret, _, err := evm.Call(caller, precompileAddr, pubKey, 1e7, zero)
	if err != nil {
		t.Fatalf("EVM.Call (register): %v", err)
	}
	if len(ret) != 1 || ret[0] != 1 {
		t.Fatalf("unexpected register result: %x", ret)
	}

	if _, _, err := evm.Call(caller, precompileAddr, pubKey, 1e7, zero); err == nil {
		t.Fatal("expected second register to fail")
	}

	lookupRet, _, err := evm.Call(caller, precompileAddr, caller.Bytes(), 1e7, zero)
	if err != nil {
		t.Fatalf("EVM.Call (lookup): %v", err)
	}
	if len(lookupRet) != len(pubKey) {
		t.Fatalf("unexpected lookup size: have %d want %d", len(lookupRet), len(pubKey))
	}
	if string(lookupRet) != string(pubKey) {
		t.Fatal("lookup returned unexpected pubkey")
	}
}

// TestPQRecoverPrecompileE2E tests the pqRecover precompile (0x68) via a direct
// EVM call, verifying it returns the correct sender address for a valid ML-DSA-44
// signature and 32 zero bytes for an invalid one.
func TestPQRecoverPrecompileE2E(t *testing.T) {
	pubKey, privKey, err := mldsa.GenerateKey()
	if err != nil {
		t.Fatalf("mldsa.GenerateKey: %v", err)
	}

	msg := crypto.Keccak256([]byte("pq-precompile-test"))
	sig, err := crypto.SignPQ(msg, privKey)
	if err != nil {
		t.Fatalf("SignPQ: %v", err)
	}

	// Build the 3764-byte precompile input: hash(32) || sig(2420) || pubKey(1312)
	input := make([]byte, 0, 32+len(sig)+len(pubKey))
	input = append(input, msg...)
	input = append(input, sig...)
	input = append(input, pubKey...)

	precompileAddr := common.BytesToAddress([]byte{0x68})

	config := pqChainConfig()
	genesis := &Genesis{Config: config, BaseFee: big.NewInt(params.InitialBaseFee)}
	db := rawdb.NewMemoryDatabase()
	blockchain, err := NewBlockChain(db, genesis, ethash.NewFaker(),
		DefaultConfig().WithStateScheme(rawdb.HashScheme))
	if err != nil {
		t.Fatalf("NewBlockChain: %v", err)
	}
	defer blockchain.Stop()

	state, _ := blockchain.State()
	blockCtx := vm.BlockContext{
		BlockNumber: big.NewInt(0),
		Time:        0,
		Difficulty:  big.NewInt(1),
		GasLimit:    1e8,
		BaseFee:     big.NewInt(params.InitialBaseFee),
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
	}
	evm := vm.NewEVM(blockCtx, state, config, vm.Config{})

	// Explicitly inject the pqRecover precompile (0x68) because AllEthashProtocolChanges
	// does not enable BSC-specific Hertz rules where 0x68 is normally activated.
	precompiles := vm.ActivePrecompiledContracts(config.Rules(big.NewInt(0), false, 0))
	precompiles[precompileAddr] = vm.NewPQRecoverPrecompile()
	evm.SetPrecompiles(precompiles)

	zero := uint256.NewInt(0)
	caller := common.Address{}

	// --- valid signature: expect 32-byte left-padded address ---
	ret, _, err := evm.Call(caller, precompileAddr, input, 1e7, zero)
	if err != nil {
		t.Fatalf("EVM.Call (valid): %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32-byte return, got %d", len(ret))
	}
	// first 12 bytes must be zero (left-pad), last 20 bytes are the address
	for i := 0; i < 12; i++ {
		if ret[i] != 0 {
			t.Errorf("byte %d of return should be 0, got %x", i, ret[i])
		}
	}
	wantAddr := crypto.PQPubkeyToAddress(pubKey)
	gotAddr := common.BytesToAddress(ret[12:])
	if gotAddr != wantAddr {
		t.Errorf("recovered address: got %s want %s", gotAddr.Hex(), wantAddr.Hex())
	}
	t.Logf("pqRecover precompile returned address: %s", gotAddr.Hex())

	// --- tampered signature: expect 32 zero bytes ---
	tampered := make([]byte, len(input))
	copy(tampered, input)
	tampered[32] ^= 0xff // flip first byte of sig

	ret2, _, err2 := evm.Call(caller, precompileAddr, tampered, 1e7, zero)
	if err2 != nil {
		t.Fatalf("EVM.Call (tampered): %v", err2)
	}
	for i, b := range ret2 {
		if b != 0 {
			t.Errorf("tampered: byte %d should be 0, got %x", i, b)
		}
	}
	t.Log("pqRecover precompile correctly returned zero address for tampered signature")
}
