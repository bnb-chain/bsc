package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	epochNum = 10000000
)

func main() {
	//query := hexutil.MustDecode("0x000000000000000000000000765388e911Ab4bcBB6da83793dF3F8EE4783a9Ef")
	query := hexutil.MustDecode("0x000000000000000000000000765388e911ab4bcbb6da83793df3f8ee4783a9ef")
	// 0x40a42e9f7cb0da6f665cee9a364aeda2965a9b595de818a7a3136549fd9417d8

	query = append(query,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1,
	)

	hasherBuf := common.Hash{}

	hasher := crypto.NewKeccakState()
	hasher.Write(query[:])
	hasher.Read(hasherBuf[:])
	fmt.Printf("slot %s\n", hasherBuf.String())

	buf2 := crypto.Keccak256(query)

	fmt.Printf("slot %s\n", hexutil.Encode(buf2))
}
