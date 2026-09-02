// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

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
