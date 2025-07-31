package main

import (
    "context"
    "flag"
    "fmt"
    "log"
    "os"
    "strings"
    "time"

    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/crypto"
    "github.com/ethereum/go-ethereum/ethclient"
)

func keccak256(data []byte) common.Hash {
    return common.BytesToHash(crypto.Keccak256(data))
}

func main() {
    var (
        rpcURL    string
        txHashStr string
        wantHash  string
    )

    flag.StringVar(&rpcURL, "rpc", "http://localhost:8545", "RPC endpoint of local geth node")
    flag.StringVar(&txHashStr, "tx", "", "Transaction hash to verify")
    flag.StringVar(&wantHash, "codehash", "", "Expected contract code hash (0x...) to compare")
    flag.Parse()

    if txHashStr == "" || wantHash == "" {
        fmt.Println("Usage: verify_codehash --tx <txHash> --codehash <0x...> [--rpc <url>]")
        os.Exit(1)
    }

    txHash := common.HexToHash(txHashStr)
    wantHash = strings.ToLower(wantHash)

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    client, err := ethclient.DialContext(ctx, rpcURL)
    if err != nil {
        log.Fatalf("Failed to connect to RPC: %v", err)
    }

    tx, isPending, err := client.TransactionByHash(ctx, txHash)
    if err != nil {
        log.Fatalf("Cannot find transaction: %v", err)
    }
    if isPending {
        log.Fatalf("Transaction is still pending; need mined tx")
    }

    var contractAddr common.Address
    if tx.To() == nil {
        receipt, err := client.TransactionReceipt(ctx, txHash)
        if err != nil {
            log.Fatalf("Failed to get receipt: %v", err)
        }
        contractAddr = receipt.ContractAddress
    } else {
        contractAddr = *tx.To()
    }

    code, err := client.CodeAt(ctx, contractAddr, nil)
    if err != nil {
        log.Fatalf("Failed to get code: %v", err)
    }
    hash := keccak256(code).Hex()

    fmt.Printf("Transaction   : %s\n", txHash.Hex())
    fmt.Printf("Contract addr : %s\n", contractAddr.Hex())
    fmt.Printf("Computed hash : %s\n", hash)
    fmt.Printf("Expected hash : %s\n", wantHash)

    if strings.EqualFold(hash, wantHash) {
        fmt.Println("\n✅ MATCH: The transaction executed the contract with the expected code hash.")
    } else {
        fmt.Println("\n❌ MISMATCH: Code hash differs. The error log may originate from another contract execution.")
    }
}
