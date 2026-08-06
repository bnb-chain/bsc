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
	"github.com/ethereum/go-ethereum/params"
)

// B20 gas metering (BEP-702 section 3.14).
//
// This standard publishes no gas schedule of its own. Every charge below is
// produced by an existing EVM cost function applied to the work actually
// performed, and is metered as the work happens — RequiredGas returns zero
// because a stateful precompile's cost depends on state it has not read when
// the call begins.
//
// The rule the numbers must satisfy: a B20 operation is never cheaper than the
// same state accesses performed through bytecode. Each helper therefore mirrors
// the interpreter's own gas function rather than approximating it; where the
// interpreter's logic is non-trivial (net-metered SSTORE), the mirror is
// line-for-line against makeGasSStoreFunc in operations_acl.go.
//
// Not charged, deliberately: per-opcode execution, memory expansion and call
// machinery have no counterpart here, and their absence is the whole of this
// standard's efficiency claim. No synthetic overhead is added to approximate
// them.

// b20CalldataWordGas prices one 32-byte word of input, charged once per
// dispatch. It stands for what bytecode pays to bring its own calldata into
// memory before decoding: CALLDATACOPY's per-word copy cost plus the memory
// expansion that receives it (3 + 3).
const b20CalldataWordGas = params.CopyGas + params.MemoryGas

// chargeCalldata meters the input of one dispatch. Charged once at the entry
// point, never per selector, so nested internal calls (announce bundles,
// factory initCalls) pay for their own payloads exactly once.
func (ctx *PrecompileContext) chargeCalldata(input []byte) {
	words := (uint64(len(input)) + 31) / 32
	ctx.chargeStateGas(words * b20CalldataWordGas)
}

// chargeKeccak meters a keccak256 over size bytes: the same base plus per-word
// cost the KECCAK256 opcode pays. Slot derivation for a mapping hashes 64
// bytes; leaving it unmetered would donate computation.
func (ctx *PrecompileContext) chargeKeccak(size int) {
	words := (uint64(size) + 31) / 32
	ctx.chargeStateGas(params.Keccak256Gas + params.Keccak256WordGas*words)
}

// chargeLog meters a log emission at LOG* prices: base, per topic, per data
// byte.
func (ctx *PrecompileContext) chargeLog(topics int, dataLen int) {
	ctx.chargeStateGas(params.LogGas +
		params.LogTopicGas*uint64(topics) +
		params.LogDataGas*uint64(dataLen))
}

// chargeAccountAccess meters reading an account's balance, nonce or code hash
// at EIP-2929 prices: warm always, plus the cold surcharge on first touch of
// the address in this transaction.
func (ctx *PrecompileContext) chargeAccountAccess(addr common.Address) {
	if ctx.StateDB.AddressInAccessList(addr) {
		ctx.chargeStateGas(params.WarmStorageReadCostEIP2929)
		return
	}
	ctx.StateDB.AddAddressToAccessList(addr)
	ctx.chargeStateGas(params.ColdAccountAccessCostEIP2929)
}

// chargeCodeWrite meters writing account code: the per-byte deposit cost, the
// keccak of the code, and — when the target had no code — the account-creation
// cost. The creation cost is owed even at a prefunded address, since balance
// alone does not make an account a contract.
func (ctx *PrecompileContext) chargeCodeWrite(addr common.Address, code []byte) {
	cost := params.CreateDataGas * uint64(len(code))
	if hadNoCode(ctx.StateDB, addr) {
		cost += params.CreateGas
	}
	ctx.chargeStateGas(cost)
	ctx.chargeKeccak(len(code))
}

// sstoreSentry enforces EIP-2200's reentrancy guard: an SSTORE is refused
// whenever remaining gas is at or below the 2300-gas call stipend, however
// cheap the write itself would be. That check, not the write's price, is what
// makes Solidity's transfer()/send() safe — net metering prices a warm dirty
// rewrite at roughly a hundred gas. A B20 token writes state without executing
// SSTORE, so it has to apply the same check itself; omitting it would
// retroactively invalidate the reentrancy assumption already-deployed,
// unchangeable contracts were audited against.
func (ctx *PrecompileContext) sstoreSentry() bool {
	return ctx.gas.RegularGas > params.SstoreSentryGasEIP2200
}

// chargeStorageWrite meters an SSTORE against the same net-metering rules the
// interpreter applies, including EIP-3529 refunds. It mirrors
// makeGasSStoreFunc; the arms are kept in the same order and with the same
// clause numbers so the two can be diffed.
//
// Reports false when the reentrancy sentry refuses the write, in which case the
// caller must fail the call with out-of-gas and perform no write.
func (s b20Storage) chargeStorageWrite(slot, value common.Hash) bool {
	if s.ctx == nil {
		return true
	}
	if !s.ctx.sstoreSentry() {
		s.ctx.outOfGas = true
		return false
	}
	var (
		current, original = s.state.GetStateAndCommittedState(s.token, slot)
		clearingRefund    = params.SstoreClearsScheduleRefundEIP3529
		cost              = uint64(0)
	)
	if _, slotPresent := s.state.SlotInAccessList(s.token, slot); !slotPresent {
		cost = params.ColdSloadCostEIP2929
		s.state.AddSlotToAccessList(s.token, slot)
	}
	switch {
	case current == value: // noop (1)
		s.ctx.chargeStateGas(cost + params.WarmStorageReadCostEIP2929)
		return true

	case original == current:
		if original == (common.Hash{}) { // create slot (2.1.1)
			s.ctx.chargeStateGas(cost + params.SstoreSetGasEIP2200)
			return true
		}
		if value == (common.Hash{}) { // delete slot (2.1.2b)
			s.state.AddRefund(clearingRefund)
		}
		// write existing slot (2.1.2)
		s.ctx.chargeStateGas(cost + (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929))
		return true
	}

	// dirty slot (2.2)
	if original != (common.Hash{}) {
		if current == (common.Hash{}) { // recreate slot (2.2.1.1)
			s.state.SubRefund(clearingRefund)
		} else if value == (common.Hash{}) { // delete slot (2.2.1.2)
			s.state.AddRefund(clearingRefund)
		}
	}
	if original == value {
		if original == (common.Hash{}) { // reset to original inexistent slot (2.2.2.1)
			s.state.AddRefund(params.SstoreSetGasEIP2200 - params.WarmStorageReadCostEIP2929)
		} else { // reset to original existing slot (2.2.2.2)
			s.state.AddRefund((params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) - params.WarmStorageReadCostEIP2929)
		}
	}
	// dirty update (2.2)
	s.ctx.chargeStateGas(cost + params.WarmStorageReadCostEIP2929)
	return true
}
