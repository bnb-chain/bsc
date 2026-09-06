package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
)

// SeedCAS20Activation plants the registries' account sentinels (BEP-702 3.16).
// Without code, EIP-161 would clear the accounts and their storage, and GovHub
// would refuse them as a target: it tests extcodesize before forwarding a
// parameter change, and swallows the refusal into an event.
func SeedCAS20Activation(state StateDB) {
	seedCAS20Sentinel(state, CAS20ActivationRegistryAddress)
	seedCAS20Sentinel(state, CAS20PolicyRegistryAddress)
}

func seedCAS20Sentinel(state StateDB, addr common.Address) {
	if !hadNoCode(state, addr) {
		return
	}
	state.SetCode(addr, CAS20MarkerCode, tracing.CodeChangeSystemContractUpgrade)
}
