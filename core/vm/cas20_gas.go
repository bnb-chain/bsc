package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

// CAS20 gas metering (BEP-702 3.14): mirror existing EVM cost functions so nothing
// a CAS20 call does is cheaper than the same work through bytecode. That covers
// state access and equally the calldata copy, the keccaks, the logs and permit's
// ecrecover — every charge goes through chargeGas, which is why the counter it
// feeds is not GasCosts.StateGas (see meteredGasUsed). Opcode, memory and call
// overhead have no counterpart here and are not synthesized.

// The EIP-2929 warm/cold policy is normative and lives in BEP-702 3.14: no CAS20
// address starts warm, a transaction access list applies, warmth is keyed on
// (address, slot), and a foreign slot read warms the foreign address too.
//
// None of those four clauses has a test. The addresses are absent from the
// static precompile map today, which is what keeps the first of them true by
// accident; a refactor that added them would start every call warm and reprice
// it, with nothing here to notice.

// cas20CalldataWordGas is the per-word part of bringing calldata into memory:
// CALLDATACOPY's copy cost plus the linear part of the memory expansion that
// receives it (3 + 3).
const cas20CalldataWordGas = params.CopyGas + params.MemoryGas

// chargeCalldata charges G_copy + G_memory per 32-byte word, which is what
// BEP-702 3.14 specifies and all of it: the table there is exhaustive and forbids
// synthesizing opcode or memory-expansion overhead. It used to add CALLDATACOPY's
// base step and a quadratic memory term, making the implementation and the
// specification two different consensus sources. Internal Go redispatches read no
// calldata and are not charged again.
func (ctx *PrecompileContext) chargeCalldata(input []byte) bool {
	words := (uint64(len(input)) + 31) / 32
	if words == 0 {
		return true
	}
	return ctx.chargeGas(words * cas20CalldataWordGas)
}

// chargeKeccak meters a keccak256 over size bytes: the same base plus per-word
// cost the KECCAK256 opcode pays. Slot derivation for a mapping hashes 64
// bytes; leaving it unmetered would donate computation.
func (ctx *PrecompileContext) chargeKeccak(size int) bool {
	words := (uint64(size) + 31) / 32
	return ctx.chargeGas(params.Keccak256Gas + params.Keccak256WordGas*words)
}

// chargeLog meters a log emission at LOG* prices: base, per topic, per data
// byte.
func (ctx *PrecompileContext) chargeLog(topics int, dataLen int) bool {
	return ctx.chargeGas(params.LogGas +
		params.LogTopicGas*uint64(topics) +
		params.LogDataGas*uint64(dataLen))
}

// chargeAccountAccess meters reading an account's balance, nonce or code hash
// at EIP-2929 prices: warm always, plus the cold surcharge on first touch of
// the address in this transaction.
func (ctx *PrecompileContext) chargeAccountAccess(addr common.Address) bool {
	if ctx.StateDB.AddressInAccessList(addr) {
		return ctx.chargeGas(params.WarmStorageReadCostEIP2929)
	}
	// Warming before the charge mirrors the interpreter's own order; the addition
	// is journalled, so a refused charge leaves nothing behind once the frame
	// reverts.
	ctx.StateDB.AddAddressToAccessList(addr)
	return ctx.chargeGas(params.ColdAccountAccessCostEIP2929)
}

// chargeCodeWrite meters writing account code: the per-byte deposit cost, the
// keccak of the code, and — when the target had no code — the account-creation
// cost. The creation cost is owed even at a prefunded address, since balance
// alone does not make an account a contract.
func (ctx *PrecompileContext) chargeCodeWrite(addr common.Address, code []byte) bool {
	cost := params.CreateDataGas * uint64(len(code))
	if hadNoCode(ctx.StateDB, addr) {
		cost += params.CreateGas
	}
	return ctx.chargeGas(cost) && ctx.chargeKeccak(len(code))
}

// sstoreSentry enforces EIP-2200's reentrancy guard: an SSTORE is refused
// whenever remaining gas is at or below the 2300-gas call stipend, however
// cheap the write itself would be. That check, not the write's price, is what
// makes Solidity's transfer()/send() safe — net metering prices a warm dirty
// rewrite at roughly a hundred gas. A CAS20 token writes state without executing
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
func (s cas20Storage) chargeStorageWrite(slot, value common.Hash) bool {
	if s.ctx == nil {
		return true
	}
	// First, as in gasSStoreEIP2200: a read-only frame may not write, whatever it
	// could afford and whatever the net-metered price would have been.
	if s.ctx.ReadOnly {
		s.ctx.markWriteProtected()
		return false
	}
	if !s.ctx.sstoreSentry() {
		// Propagate, do not assign: the sentry refuses the write without
		// draining the budget, so a spawner that only saw its own flag would
		// still have gas in hand and report success over a write that never
		// happened.
		s.ctx.markOutOfGas()
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
		return s.ctx.chargeGas(cost + params.WarmStorageReadCostEIP2929)

	case original == current:
		if original == (common.Hash{}) { // create slot (2.1.1)
			return s.ctx.chargeGas(cost + params.SstoreSetGasEIP2200)
		}
		if value == (common.Hash{}) { // delete slot (2.1.2b)
			s.state.AddRefund(clearingRefund)
		}
		// write existing slot (2.1.2)
		return s.ctx.chargeGas(cost + (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929))
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
	return s.ctx.chargeGas(cost + params.WarmStorageReadCostEIP2929)
}
