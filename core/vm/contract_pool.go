package vm

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

var contractPool = sync.Pool{
	New: func() any {
		return &Contract{}
	},
}

// GetContract returns a contract from the pool or creates a new one
func GetContract(caller common.Address, address common.Address, value *uint256.Int, gas uint64, jumpDests JumpDestCache) *Contract {
	contract := contractPool.Get().(*Contract)

	// Reset the contract with new values
	contract.caller = caller
	contract.address = address
	contract.value = value
	contract.Gas = gas
	contract.Code = nil
	contract.CodeHash = common.Hash{}
	contract.CodeAddr = nil
	contract.Input = nil
	contract.IsDeployment = false
	contract.IsSystemCall = false

	// Initialize the jump analysis map if it's nil, mostly for tests
	if jumpDests == nil {
		jumpDests = newMapJumpDests()
	}
	contract.codeBitmapFunc = codeBitmap
	contract.jumpDests = jumpDests
	contract.analysis = nil

	return contract
}

// ReturnContract returns a contract to the pool
func ReturnContract(contract *Contract) {
	if contract == nil {
		return
	}
	contractPool.Put(contract)
}
