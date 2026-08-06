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
	"github.com/ethereum/go-ethereum/params"
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

	// Rules are the chain rules active for this block.
	Rules params.Rules

	// gas points at the caller's remaining budget so UseGas can meter
	// data-dependent cost in place.
	gas *GasBudget

	// outOfGas is set sticky once a charge cannot be covered; the dispatcher
	// checks it and returns ErrOutOfGas.
	outOfGas bool

	// stateGasUsed accumulates the cost attributed to state operations. Today
	// it is booked within RegularGas (the only enforced dimension); the tally
	// keeps the accounting ready for when the StateGas reservoir is activated.
	stateGasUsed uint64

	// inAnnounce guards against re-entrant announce() during the Asset
	// disclosure window.
	inAnnounce bool
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
// this frame's state, gas budget and rules. Used by the B20 factory to run a
// new token's initCalls inside the creating frame.
func (ctx *PrecompileContext) spawnBootstrap(self, caller common.Address) *PrecompileContext {
	return &PrecompileContext{
		evm:        ctx.evm,
		StateDB:    ctx.StateDB,
		Self:       self,
		Caller:     caller,
		DirectCall: true,
		Rules:      ctx.Rules,
		gas:        ctx.gas,
	}
}

// chargeStateGas charges a state-operation cost against the (enforced)
// RegularGas budget and accumulates it into the StateGas tally. On
// insufficient gas it exhausts the budget and marks the context out of gas;
// callers surface this via OutOfGas.
func (ctx *PrecompileContext) chargeStateGas(cost uint64) {
	if !ctx.UseGas(GasCosts{RegularGas: cost}) {
		ctx.outOfGas = true
		return
	}
	ctx.stateGasUsed += cost
}

// OutOfGas reports whether a charge has failed during this call. The
// dispatcher must check it after driving the token logic and return
// ErrOutOfGas so the frame reverts.
func (ctx *PrecompileContext) OutOfGas() bool { return ctx.outOfGas }

// StateGasUsed returns the gas attributed to state operations so far.
func (ctx *PrecompileContext) StateGasUsed() uint64 { return ctx.stateGasUsed }

// GasLeft reports the regular gas remaining in the budget.
func (ctx *PrecompileContext) GasLeft() uint64 { return ctx.gas.RegularGas }

// BlockTime returns the timestamp of the block being executed (used e.g. by
// EIP-2612 permit deadline checks).
func (ctx *PrecompileContext) BlockTime() uint64 { return ctx.evm.Context.Time }

// ChainID returns the active chain id (used e.g. by the EIP-712 domain
// separator).
func (ctx *PrecompileContext) ChainID() *uint256.Int {
	id, _ := uint256.FromBig(ctx.evm.chainConfig.ChainID)
	return id
}

// Snapshot / RevertToSnapshot expose nested journalling so a precompile can
// make a group of mutations atomic (e.g. the factory's initCalls bootstrap or
// the Asset variant's announce/batchMint).
func (ctx *PrecompileContext) Snapshot() int           { return ctx.StateDB.Snapshot() }
func (ctx *PrecompileContext) RevertToSnapshot(id int) { ctx.StateDB.RevertToSnapshot(id) }

// AddLog emits an EVM log at the precompile's own address (Self). The
// remaining fields (tx hash/index, block hash, log index) are filled by the
// StateDB at finalisation, exactly as for contract-emitted logs.
func (ctx *PrecompileContext) AddLog(topics []common.Hash, data []byte) {
	ctx.StateDB.AddLog(&types.Log{
		Address: ctx.Self,
		Topics:  topics,
		Data:    data,
	})
}

// runStatefulPrecompiledContract charges the flat RequiredGas, builds the
// PrecompileContext and dispatches into the stateful precompile. It is the
// stateful counterpart of RunPrecompiledContract.
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
		Rules:      evm.chainRules,
		gas:        &gas,
	}
	output, err := p.RunStateful(ctx, input)
	return output, gas, err
}
