package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
)

// SeedCAS20Activation gives the two singleton registries their account sentinels.
// They hold storage and no code, so EIP-161 would otherwise clear them — and the
// storage with them (BEP-702 3.16). Every feature stays disabled.
//
// The sentinel is also what makes the registry reachable by governance at all:
// GovHub refuses a target with no code (isContract) and swallows the refusal into
// an event, so a registry without one could never be sent its first parameter
// change, and the failed proposal would look like a successful one.
func SeedCAS20Activation(state StateDB) {
	seedCAS20Sentinel(state, CAS20ActivationRegistryAddress)
	seedCAS20Sentinel(state, CAS20PolicyRegistryAddress)
}

// seedCAS20Sentinel gives a registry account the marker code that keeps it out of
// reach of EIP-161 state clearing, unless it already carries code.
func seedCAS20Sentinel(state StateDB, addr common.Address) {
	if !hadNoCode(state, addr) {
		return
	}
	state.SetCode(addr, CAS20MarkerCode, tracing.CodeChangeSystemContractUpgrade)
}
