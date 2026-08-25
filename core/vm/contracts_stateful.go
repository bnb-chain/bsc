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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// StatefulPrecompiledContract is a precompile that reads and writes consensus
// state. Unlike the plain PrecompiledContract, whose Run is a pure function of
// its input, a stateful precompile is handed a PrecompileContext carrying the
// StateDB, the caller, the call mode, and dynamic gas metering. It is the host
// primitive the B20 token family is built on.
type StatefulPrecompiledContract interface {
	// RequiredGas returns the flat gas charged up-front, before RunStateful.
	// Additional, data-dependent cost is charged inside RunStateful via
	// PrecompileContext.UseGas.
	RequiredGas(input []byte) uint64

	// RunStateful executes the precompile against ctx. Returning a non-nil error
	// reverts every state mutation performed during the call (the enclosing
	// Call/StaticCall frame is already wrapped in a StateDB snapshot).
	RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error)
}

// PrecompileContext is the execution environment handed to a stateful
// precompile. It exposes exactly the host capabilities a native token needs —
// state access, caller identity, call-mode guards, log emission and dynamic
// gas — without leaking the whole *EVM.
type PrecompileContext struct {
	evm *EVM

	// StateDB is the live state; mutations are journalled and revert with the
	// enclosing call frame's snapshot when RunStateful returns an error.
	StateDB StateDB

	// Self is the address the precompile is bound to. For the B20 dynamic
	// family this is the token address, and is the storage root for its state.
	Self common.Address

	// Caller is msg.sender for this frame.
	Caller common.Address

	// ReadOnly is true inside a STATICCALL (or any read-only ancestor frame);
	// a precompile must reject state mutation when set.
	ReadOnly bool

	// DirectCall is true for CALL and STATICCALL, false for CALLCODE and
	// DELEGATECALL. Stateful token precompiles reject non-direct calls so that
	// Self is always the genuine callee and can be trusted as the storage root.
	DirectCall bool

	// Value is the wei attached to the frame (nil means zero). Every B20 entry
	// point is nonpayable and rejects a non-zero value with NonPayable
	// (BEP-702 section 3.2).
	Value *uint256.Int

	// gas points at the caller's remaining budget so UseGas can meter
	// data-dependent cost in place.
	gas *GasBudget

	// adminRenounced records that a token gave up its last admin during this
	// frame, so a bootstrap bundle cannot renounce and then grant again (BEP-702
	// 3.4). Per-frame rather than stored: outside the bootstrap window the zero
	// admin count freezes role mutation on its own.
	adminRenounced bool

	// frame holds accounting shared by every context in this EVM frame, so a
	// spawned child's exhaustion and charges are not lost. Lazily allocated.
	frame *frameAccounting
}

// frameAccounting is shared by every context in one EVM frame, so a child's gas
// exhaustion and state-gas charges cannot be lost on the way back.
type frameAccounting struct {
	// outOfGas is set sticky once a charge cannot be covered; the dispatcher
	// checks it and returns ErrOutOfGas.
	outOfGas bool

	// writeProtected is set sticky when a metered write is attempted in a
	// read-only frame. It outranks outOfGas at the exit: the frame had no business
	// writing at all, whatever it could afford.
	writeProtected bool

	meteredGasUsed uint64
}

// UseGas charges cost against the remaining budget. It returns false and
// exhausts the budget when the charge cannot be covered, mirroring the EVM's
// out-of-gas semantics; callers should propagate ErrOutOfGas on false.
func (ctx *PrecompileContext) UseGas(cost GasCosts) bool {
	prior, ok := ctx.gas.Charge(cost)
	if !ok {
		ctx.gas.Exhaust()
		return false
	}
	if ctx.evm != nil && ctx.evm.Config.Tracer != nil && ctx.evm.Config.Tracer.OnGasChange != nil {
		ctx.evm.Config.Tracer.OnGasChange(prior, ctx.gas.RegularGas, tracing.GasChangeCallPrecompiledContract)
	}
	return true
}

// spawnBootstrap derives a context bound to a different self address, sharing
// this frame's state, gas budget and accounting. ReadOnly is carried
// across: dropping it would let initCalls write during a STATICCALL.
func (ctx *PrecompileContext) spawnBootstrap(self, caller common.Address) *PrecompileContext {
	return &PrecompileContext{
		evm:        ctx.evm,
		StateDB:    ctx.StateDB,
		Self:       self,
		Caller:     caller,
		DirectCall: true,
		ReadOnly:   ctx.ReadOnly,
		gas:        ctx.gas,
		frame:      ctx.frameGas(),
	}
}

// frameGas returns the shared frame accounting, allocating it on first use so a
// context built as a plain struct literal needs no initialisation.
func (ctx *PrecompileContext) frameGas() *frameAccounting {
	if ctx.frame == nil {
		ctx.frame = new(frameAccounting)
	}
	return ctx.frame
}

// markOutOfGas records exhaustion. Every context spawned from this one — and the
// one it was spawned from — observes it, because they hold the same accounting.
func (ctx *PrecompileContext) markOutOfGas() { ctx.frameGas().outOfGas = true }

// chargeGas charges cost against the frame and reports whether it may continue.
// Every B20 charge arrives here, state access and computation alike.
//
// False means stop: the caller must return before the operation this charge pays
// for, the way the interpreter checks UseGas before executing an opcode rather
// than after. A charge that cannot be covered marks the frame, and an already
// marked frame refuses every later charge, so one failure ends the call rather
// than letting the rest of the handler run on an exhausted budget.
func (ctx *PrecompileContext) chargeGas(cost uint64) bool {
	if ctx.OutOfGas() {
		return false
	}
	if !ctx.UseGas(GasCosts{RegularGas: cost}) {
		ctx.markOutOfGas()
		return false
	}
	ctx.frameGas().meteredGasUsed += cost
	return true
}

// OutOfGas reports whether a charge has failed during this call. The
// dispatcher must check it after driving the token logic and return
// ErrOutOfGas so the frame reverts.
func (ctx *PrecompileContext) OutOfGas() bool { return ctx.frame != nil && ctx.frame.outOfGas }

// meteredGasUsed returns every charge this frame levied, a bootstrap child's
// included: state access and equally the calldata copy, the keccaks, the logs and
// permit's ecrecover. Read only by the metering tests.
//
// It is not GasCosts.StateGas, which is a separate dimension — half of what is
// counted here is computation. Wiring it there means dividing these charges by
// dimension first.
func (ctx *PrecompileContext) meteredGasUsed() uint64 {
	if ctx.frame == nil {
		return 0
	}
	return ctx.frame.meteredGasUsed
}

// gasLeft reports the regular gas remaining in the budget.
func (ctx *PrecompileContext) gasLeft() uint64 { return ctx.gas.RegularGas }

// BlockTime returns the timestamp of the block being executed (used e.g. by
// EIP-2612 permit deadline checks).
func (ctx *PrecompileContext) BlockTime() uint64 { return ctx.evm.Context.Time }

// ChainID returns the active chain id (used e.g. by the EIP-712 domain
// separator).
func (ctx *PrecompileContext) ChainID() *uint256.Int {
	id, _ := uint256.FromBig(ctx.evm.chainConfig.ChainID)
	return id
}

// markWriteProtected records an attempted write in a read-only frame, the way
// geth's gas functions return ErrWriteProtection before pricing anything
// (gasSStoreEIP2200, gasSelfdestruct, gasCallIntrinsic). The handlers each check
// ReadOnly first and none is known to be missing; this is what makes a missing
// one fail closed instead of writing state inside a STATICCALL.
func (ctx *PrecompileContext) markWriteProtected() { ctx.frameGas().writeProtected = true }

// writeProtectionViolated reports whether any write was attempted in a read-only
// frame during this call.
func (ctx *PrecompileContext) writeProtectionViolated() bool {
	return ctx.frame != nil && ctx.frame.writeProtected
}

// AddLog emits a log, or does nothing and reports false when the charge for it
// cannot be covered. Dropping that result let a handler whose write had already
// been refused still append its event: approve's Approval log costs 1,756 gas,
// which fits inside the 2,300 the EIP-2200 sentry leaves behind.
func (ctx *PrecompileContext) AddLog(topics []common.Hash, data []byte) bool {
	if ctx.ReadOnly {
		ctx.markWriteProtected()
		return false
	}
	if !ctx.chargeLog(len(topics), len(data)) {
		return false
	}
	ctx.StateDB.AddLog(&types.Log{
		Address: ctx.Self,
		Topics:  topics,
		Data:    data,
	})
	return true
}

func runStatefulPrecompiledContract(evm *EVM, p StatefulPrecompiledContract, caller, self common.Address, input []byte, gas GasBudget, readOnly, directCall bool, value *uint256.Int) (ret []byte, remaining GasBudget, err error) {
	gasCost := p.RequiredGas(input)
	prior, ok := gas.Charge(GasCosts{RegularGas: gasCost})
	if !ok {
		gas.Exhaust()
		return nil, gas, ErrOutOfGas
	}
	if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
		evm.Config.Tracer.OnGasChange(prior, gas.RegularGas, tracing.GasChangeCallPrecompiledContract)
	}
	// Mirror the access-list touch performed for plain precompiles.
	if evm.chainRules.IsAmsterdam {
		evm.StateDB.Touch(self)
	}
	ctx := &PrecompileContext{
		evm:        evm,
		StateDB:    evm.StateDB,
		Self:       self,
		Caller:     caller,
		ReadOnly:   readOnly,
		DirectCall: directCall,
		Value:      value,
		gas:        &gas,
	}
	output, err := p.RunStateful(ctx, input)
	return output, gas, err
}
