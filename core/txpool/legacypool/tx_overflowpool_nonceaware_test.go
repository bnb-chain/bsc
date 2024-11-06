package legacypool

import (
	"crypto/ecdsa"
	//"crypto"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func createTestTransaction(nonce uint64, key *ecdsa.PrivateKey, chainID *big.Int) *types.Transaction {
	tx := types.NewTransaction(
		nonce,
		common.HexToAddress("0x1"),
		big.NewInt(1),
		21000,
		big.NewInt(1),
		nil,
	)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	return signedTx
}

func TestNonceAwareTxOverflowPool_Add(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	chainID := big.NewInt(1)
	signer := types.NewEIP155Signer(chainID)
	pool := NewNonceAwareTxOverflowPool(1000, signer)

	tx := createTestTransaction(0, privateKey, chainID)
	pool.Add(tx)

	if pool.Len() != 1 {
		t.Errorf("Expected pool length 1, got %d", pool.Len())
	}
}

func TestNonceAwareTxOverflowPool_Get(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	chainID := big.NewInt(1)
	signer := types.NewEIP155Signer(chainID)
	pool := NewNonceAwareTxOverflowPool(1000, signer)

	tx := createTestTransaction(0, privateKey, chainID)
	pool.Add(tx)

	retrievedTx, found := pool.Get(tx.Hash())
	if !found {
		t.Errorf("Transaction not found in pool")
	}
	if retrievedTx.Hash() != tx.Hash() {
		t.Errorf("Retrieved transaction hash does not match")
	}
}

func TestNonceAwareTxOverflowPool_Remove(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	chainID := big.NewInt(1)
	signer := types.NewEIP155Signer(chainID)
	pool := NewNonceAwareTxOverflowPool(1000, signer)

	tx := createTestTransaction(0, privateKey, chainID)
	pool.Add(tx)
	pool.Remove(tx.Hash())

	if pool.Len() != 0 {
		t.Errorf("Expected pool length 0 after removal, got %d", pool.Len())
	}
}

func TestNonceAwareTxOverflowPool_GetExecutableTransactions(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	chainID := big.NewInt(1)
	signer := types.NewEIP155Signer(chainID)
	pool := NewNonceAwareTxOverflowPool(1000, signer)

	// Initialize a new in-memory database for testing
	db := rawdb.NewMemoryDatabase()

	// Create a new state database
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	// Simulate pending nonces
	pendingNonces := newNoncer(stateDB)
	pendingNonces.set(fromAddress, 0)

	// Add transactions with nonces 0 to 4
	for i := uint64(0); i < 5; i++ {
		tx := createTestTransaction(i, privateKey, chainID)
		pool.Add(tx)
	}

	executableTxs := pool.GetExecutableTransactions(pendingNonces, 10)
	if len(executableTxs) != 5 {
		t.Errorf("Expected 5 executable transactions, got %d", len(executableTxs))
	}
	// Verify that transactions are returned in order
	for i, tx := range executableTxs {
		if tx.Nonce() != uint64(i) {
			t.Errorf("Expected nonce %d, got %d", i, tx.Nonce())
		}
	}

	// Ensure pool is empty after retrieving executable transactions
	if pool.Len() != 0 {
		t.Errorf("Expected pool length 0 after retrieving executables, got %d", pool.Len())
	}
}

func TestNonceAwareTxOverflowPool_NonceGaps(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	chainID := big.NewInt(1)
	signer := types.NewEIP155Signer(chainID)
	pool := NewNonceAwareTxOverflowPool(1000, signer)

	// Initialize a new in-memory database for testing
	db := rawdb.NewMemoryDatabase()

	// Create a new state database
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	// Simulate pending nonces
	pendingNonces := newNoncer(stateDB)
	pendingNonces.set(fromAddress, 0)

	// Add transactions with nonces 1, 3, 5 (gapped)
	nonces := []uint64{1, 3, 5}
	for _, nonce := range nonces {
		tx := createTestTransaction(nonce, privateKey, chainID)
		pool.Add(tx)
	}

	executableTxs := pool.GetExecutableTransactions(pendingNonces, 10)
	if len(executableTxs) != 0 {
		t.Errorf("Expected 0 executable transactions due to nonce gaps, got %d", len(executableTxs))
	}

	// Add missing transaction with nonce 0
	tx0 := createTestTransaction(0, privateKey, chainID)
	pool.Add(tx0)

	executableTxs = pool.GetExecutableTransactions(pendingNonces, 10)
	if len(executableTxs) != 1 {
		t.Errorf("Expected 1 executable transaction after adding nonce 0, got %d", len(executableTxs))
	}
	if executableTxs[0].Nonce() != 0 {
		t.Errorf("Expected nonce 0, got %d", executableTxs[0].Nonce())
	}

	// Update pending nonce
	pendingNonces.set(fromAddress, 1)

	// Get executable transactions again
	executableTxs = pool.GetExecutableTransactions(pendingNonces, 10)
	if len(executableTxs) != 1 {
		t.Errorf("Expected 1 executable transaction after updating pending nonce, got %d", len(executableTxs))
	}
	if executableTxs[0].Nonce() != 1 {
		t.Errorf("Expected nonce 1, got %d", executableTxs[0].Nonce())
	}
}

func TestNonceAwareTxOverflowPool_ConcurrentAccess(t *testing.T) {
	privateKey, _ := crypto.GenerateKey()
	chainID := big.NewInt(1)
	signer := types.NewEIP155Signer(chainID)
	pool := NewNonceAwareTxOverflowPool(1000, signer)

	var wg sync.WaitGroup
	numTransactions := 100

	// Simulate concurrent adding of transactions
	for i := 0; i < numTransactions; i++ {
		wg.Add(1)
		go func(nonce uint64) {
			defer wg.Done()
			tx := createTestTransaction(nonce, privateKey, chainID)
			pool.Add(tx)
		}(uint64(i))
	}

	wg.Wait()

	if pool.Len() != numTransactions {
		t.Errorf("Expected pool length %d, got %d", numTransactions, pool.Len())
	}
}
