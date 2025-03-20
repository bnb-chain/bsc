// Copyright 2021 The go-ethereum Authors
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

package native

import (
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
)

func init() {
	tracers.DefaultDirectory.Register("noopTracer", newNoopTracer, false)
}

// noopTracer is a go implementation of the Tracer interface which
// performs no action. It's mostly useful for testing purposes.
type list = map[common.Address]common.Address
type noopTracer struct{
    Op          string              `json:"op"`
	Interrupt   bool                `json:"interrupt"`
	Reason      error               `json:"reason"`
    List        list                `json:"list"`
}

// newNoopTracer returns a new noop tracer.
func newNoopTracer(ctx *tracers.Context, _ json.RawMessage) (tracers.Tracer, error) {
	return &noopTracer{}, nil
}

// CaptureStart implements the EVMLogger interface to initialize the tracing operation.
func (t *noopTracer) CaptureStart(env *vm.EVM, from common.Address, to common.Address, create bool, input []byte, gas uint64, value *big.Int) {
    t.List = make(map[common.Address]common.Address)
    t.Op = "START"
    t.List[to] = to
}

// CaptureEnd is called after the call finishes to finalize the tracing.
func (t *noopTracer) CaptureEnd(output []byte, gasUsed uint64, err error) {
}

// CaptureState implements the EVMLogger interface to trace a single step of VM execution.
func (t *noopTracer) CaptureState(pc uint64, op vm.OpCode, gas, cost uint64, scope *vm.ScopeContext, rData []byte, depth int, err error) {
    switch {
    case op == vm.RETURN || op == vm.STOP :
        t.Op = "RETURN"
    case op == vm.INVALID :
        t.Op = "INVALID"
    case op == vm.SELFDESTRUCT :
        t.Op = "SELFDESTRUCT"
    case op == vm.REVERT:
        t.Op = "REVERT"
    default:
        t.Op = "other"
    }
}

// CaptureFault implements the EVMLogger interface to trace an execution fault.
func (t *noopTracer) CaptureFault(pc uint64, op vm.OpCode, gas, cost uint64, _ *vm.ScopeContext, depth int, err error) {
}

// CaptureEnter is called when EVM enters a new scope (via call, create or selfdestruct).
func (t *noopTracer) CaptureEnter(typ vm.OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
    t.List[to] = to
}

// CaptureExit is called when EVM exits a scope, even if the scope didn't
// execute any code.
func (t *noopTracer) CaptureExit(output []byte, gasUsed uint64, err error) {
}

func (*noopTracer) CaptureTxStart(gasLimit uint64) {}

func (*noopTracer) CaptureTxEnd(restGas uint64) {}

func (t *noopTracer) CaptureSystemTxEnd(intrinsicGas uint64) {}

// GetResult returns an empty json object.
func (t *noopTracer) GetResult() (json.RawMessage, error) {
    
	res, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(res), nil
}

// Stop terminates execution of the tracer at the first opportune moment.
func (t *noopTracer) Stop(err error) {
	t.Reason = err
	t.Interrupt = true
}