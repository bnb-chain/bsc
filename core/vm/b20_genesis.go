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

// At the B20 fork, seed the activation admin and the registry sentinels; the
// registries are storage-only and so otherwise EIP-161-empty. Every feature stays
// disabled (BEP-702 3.15, 3.16).

// SeedB20Activation installs the B20 activation state at a fork boundary. An
// existing admin is preserved, so replaying the fork block cannot undo a rotation.
//
// A zero configured admin seeds only the sentinels, and that is permanent:
// requireAdmin refuses a zero admin, so setAdmin can never be reached, and this
// runs only on the boundary block. Only a further hard fork can install one.
func SeedB20Activation(state StateDB, admin common.Address) {
	seedB20Sentinel(state, B20ActivationRegistryAddress)
	seedB20Sentinel(state, B20PolicyRegistryAddress)

	if admin == (common.Address{}) {
		return
	}
	reg := b20Storage{state: state, token: B20ActivationRegistryAddress}
	// Only ever set on a registry that has none: rotation is governance's to do
	// through setAdmin, and a fork must not silently undo it.
	if reg.getWord(actSlot(actSlotAdmin)) == (common.Hash{}) {
		reg.setWord(actSlot(actSlotAdmin), addrKey(admin))
	}
}

// seedB20Sentinel gives a registry account the marker code that keeps it out of
// reach of EIP-161 state clearing, unless it already carries code.
func seedB20Sentinel(state StateDB, addr common.Address) {
	if !hadNoCode(state, addr) {
		return
	}
	state.SetCode(addr, b20MarkerCode, tracing.CodeChangeSystemContractUpgrade)
}

// B20ActivationAdmin reports the account seeded as the activation admin. It
// reads the registry rather than configuration, so it follows any rotation
// governance has performed since the fork.
func B20ActivationAdmin(state StateDB) common.Address {
	reg := b20Storage{state: state, token: B20ActivationRegistryAddress}
	return common.BytesToAddress(reg.getWord(actSlot(actSlotAdmin)).Bytes())
}
