package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// StatefulPrecompiledContract is a precompile that reads and writes consensus
// state through a PrecompileContext, unlike PrecompiledContract, whose Run is a
// pure function of its input.
type StatefulPrecompiledContract interface {
	// RequiredGas is the flat charge taken before RunStateful; data-dependent
	// cost is charged inside it.
	RequiredGas(input []byte) uint64

	// RunStateful's error reverts every state mutation of the call.
	RunStateful(ctx *PrecompileContext, input []byte) ([]byte, error)
}

// PrecompileContext is the execution environment handed to a stateful precompile.
type PrecompileContext struct {
	evm *EVM

	StateDB StateDB

	// Self is the callee address and the storage root of its state.
	Self common.Address

	// Caller is msg.sender for this frame.
	Caller common.Address

	// ReadOnly is true inside a STATICCALL or any read-only ancestor frame.
	ReadOnly bool

	// DirectCall is false for CALLCODE and DELEGATECALL, which would make Self
	// something other than the genuine callee.
	DirectCall bool

	// Value is the wei attached to the frame; nil means zero.
	Value *uint256.Int

	gas *GasBudget

	// adminRenounced is per-frame, not stored: outside the bootstrap window the zero
	// admin count freezes role mutation on its own (BEP-702 3.4).
	adminRenounced bool

	// frame is shared with every context spawned in this EVM frame, so a child's
	// exhaustion and charges are not lost. Lazily allocated.
	frame *frameAccounting
}

type frameAccounting struct {
	outOfGas bool

	// writeProtected outranks outOfGas at the exit: the frame had no business
	// writing at all, whatever it could afford.
	writeProtected bool

	meteredGasUsed uint64
}

// UseGas exhausts the budget when the charge cannot be covered, as the EVM does.
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

// ReadOnly is carried across: dropping it would let initCalls write during a STATICCALL.
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

func (ctx *PrecompileContext) frameGas() *frameAccounting {
	if ctx.frame == nil {
		ctx.frame = new(frameAccounting)
	}
	return ctx.frame
}

func (ctx *PrecompileContext) markOutOfGas() { ctx.frameGas().outOfGas = true }

func (ctx *PrecompileContext) traceStorageRead(addr common.Address, slot common.Hash) {
	if ctx.evm != nil && ctx.evm.Config.Tracer != nil && ctx.evm.Config.Tracer.OnStorageRead != nil {
		ctx.evm.Config.Tracer.OnStorageRead(addr, slot)
	}
}

func (ctx *PrecompileContext) traceAccountRead(addr common.Address) {
	if ctx.evm != nil && ctx.evm.Config.Tracer != nil && ctx.evm.Config.Tracer.OnAccountRead != nil {
		ctx.evm.Config.Tracer.OnAccountRead(addr)
	}
}

// chargeGas is where every CAS20 charge arrives. False means stop before the
// operation the charge pays for, as the interpreter checks gas before an opcode.
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

func (ctx *PrecompileContext) OutOfGas() bool { return ctx.frame != nil && ctx.frame.outOfGas }

// meteredGasUsed is read only by the metering tests. It is not GasCosts.StateGas:
// half of what it counts is computation.
func (ctx *PrecompileContext) meteredGasUsed() uint64 {
	if ctx.frame == nil {
		return 0
	}
	return ctx.frame.meteredGasUsed
}

func (ctx *PrecompileContext) gasLeft() uint64 { return ctx.gas.RegularGas }

func (ctx *PrecompileContext) BlockTime() uint64 { return ctx.evm.Context.Time }

func (ctx *PrecompileContext) ChainID() *uint256.Int {
	id, _ := uint256.FromBig(ctx.evm.chainConfig.ChainID)
	return id
}

// The handlers each check ReadOnly first; this is what makes a missing check fail
// closed instead of writing inside a STATICCALL.
func (ctx *PrecompileContext) markWriteProtected() { ctx.frameGas().writeProtected = true }

func (ctx *PrecompileContext) writeProtectionViolated() bool {
	return ctx.frame != nil && ctx.frame.writeProtected
}

// AddLog's result must be honoured: an Approval log fits inside the 2,300 gas the
// EIP-2200 sentry leaves behind, so a refused write could still emit its event.
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
