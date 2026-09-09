package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/log"
)

// SeedCAS20Activation plants the registries' account sentinels (BEP-702 3.16).
// Without code, EIP-161 would clear the accounts and their storage, and GovHub
// would refuse them as a target: it tests extcodesize before forwarding a
// parameter change, and swallows the refusal into an event.
func SeedCAS20Activation(state StateDB) {
	seedCAS20Sentinel(state, CAS20ActivationRegistryAddress)
	seedCAS20Sentinel(state, CAS20PolicyRegistryAddress)
	seedCAS20Sentinel(state, CAS20MemoFormatRegistryAddress)
}

func seedCAS20Sentinel(state StateDB, addr common.Address) {
	if !hadNoCode(state, addr) {
		log.Error("CAS20 registry already carries code", "addr", addr)
		return
	}
	state.SetCode(addr, CAS20MarkerCode, tracing.CodeChangeSystemContractUpgrade)
}
