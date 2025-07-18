package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
)

func main() {
	// Example EVM bytecode sequence from the benchmark
	code := []byte{
		byte(compiler.PUSH1), 0x01, byte(compiler.PUSH1), 0x02, byte(compiler.ADD), byte(compiler.PUSH1), 0x03, byte(compiler.MUL), byte(compiler.STOP),
	}

	fmt.Println("Original code:")
	for i, b := range code {
		opcodeName := getOpcodeName(compiler.ByteCode(b))
		fmt.Printf("0x%02x: 0x%02x (%s)\n", i, b, opcodeName)
	}

	// Apply fusion to get optimized code
	fusedCode, err := compiler.DoCodeFusion(append([]byte{}, code...))
	if err != nil {
		fmt.Printf("doCodeFusion failed: %v\n", err)
		return
	}

	fmt.Println("\nFused code:")
	for i, b := range fusedCode {
		opcodeName := getOpcodeName(compiler.ByteCode(b))
		fmt.Printf("0x%02x: 0x%02x (%s)\n", i, b, opcodeName)
	}

	fmt.Printf("\nOriginal length: %d, Fused length: %d\n", len(code), len(fusedCode))

	// Check if any fusion actually happened
	changed := 0
	for i := 0; i < len(code) && i < len(fusedCode); i++ {
		if code[i] != fusedCode[i] {
			changed++
			fmt.Printf("Changed at position %d: 0x%02x -> 0x%02x\n", i, code[i], fusedCode[i])
		}
	}
	fmt.Printf("Changed bytes: %d\n", changed)

	// Analyze the fusion pattern
	fmt.Println("\nFusion analysis:")
	fmt.Println("Original pattern: PUSH1(0x01) PUSH1(0x02) ADD PUSH1(0x03) MUL STOP")
	fmt.Println("The fusion logic looks for: PUSH1 at pos 0, ADD at pos 2")
	fmt.Println("But pos 2 is actually PUSH1, not ADD!")
	fmt.Println("This is why the fusion is incorrect.")
}

func getOpcodeName(op compiler.ByteCode) string {
	names := map[compiler.ByteCode]string{
		compiler.PUSH1:    "PUSH1",
		compiler.ADD:      "ADD",
		compiler.MUL:      "MUL",
		compiler.STOP:     "STOP",
		compiler.Nop:      "NOP",
		compiler.Push1Add: "Push1Add",
	}
	if name, exists := names[op]; exists {
		return name
	}
	return fmt.Sprintf("UNKNOWN_%d", op)
}
