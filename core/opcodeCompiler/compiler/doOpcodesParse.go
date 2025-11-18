package compiler

import "github.com/ethereum/go-ethereum/common"

// doOpcodesParse is a thin helper used by tests to invoke the MIR CFG builder.
// It returns the generated CFG (if any) and an error when parsing/building fails.
func doOpcodesParse(hash common.Hash, code []byte) (*CFG, error) {
	return GenerateMIRCFG(hash, code)
}
