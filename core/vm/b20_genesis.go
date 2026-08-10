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

// B20 activation state, installed once at the fork that ships the precompiles
// (BEP-702 sections 3.15 and 3.16).
//
// Shipping the code and permitting its use are separate decisions: the fork
// makes every B20 precompile present, and this seeding only installs the switch
// that lets a network open features later. Every feature stays deactivated, so
// the fork opens nothing on its own.
//
// Two things have to exist from the fork onward, and neither can be created
// afterwards by any call:
//
// The activation admin. It is the only account that can ever activate anything,
// and the registry treats the zero address as "no admin, nothing activatable".
// A network that ships without it has a permanently inert B20 (BEP-702 3.15
// requires the initial value to come from chain configuration).
//
// The sentinel on both registries. Their state is storage-only, which leaves
// their accounts EIP-161-empty; a state-clearing pass would reap them and take
// the activation flags and every policy with them. The sentinel keeps the
// accounts non-empty. It is written before the storage for the same reason the
// factory writes it before a token's: the EVM may prune writes made under an
// empty account.

// SeedB20Activation installs the B20 activation state at a fork boundary. It is
// idempotent: an already-seeded registry is left untouched, so replaying the
// fork block cannot overwrite an admin that governance has since rotated.
//
// A zero admin seeds nothing but the sentinels. That is a valid, deliberate
// configuration — it ships the code with the switch welded shut — so it is not
// treated as an error here.
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
