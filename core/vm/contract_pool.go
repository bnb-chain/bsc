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
func GetContract(caller ContractRef, object ContractRef, value *uint256.Int, gas uint64) *Contract {
	contract := contractPool.Get().(*Contract)

	// Reset the contract with new values
	contract.CallerAddress = caller.Address()
	contract.caller = caller
	contract.self = object
	contract.value = value
	contract.Gas = gas
	contract.Code = nil
	contract.CodeHash = common.Hash{}
	contract.CodeAddr = nil
	contract.Input = nil
	contract.IsDeployment = false
	contract.IsSystemCall = false
	if parent, ok := caller.(*Contract); ok {
		// Reuse JUMPDEST analysis from parent context if available.
		contract.jumpdests = parent.jumpdests
	} else {
		contract.jumpdests = make(map[common.Hash]bitvec)
	}
	contract.analysis = nil
	contract.optimized = false

	return contract
}

// ReturnContract returns a contract to the pool
func ReturnContract(contract *Contract) {
	if contract == nil {
		return
	}
	contractPool.Put(contract)
}
