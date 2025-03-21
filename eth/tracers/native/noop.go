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

    "github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
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
func newNoopTracer(ctx *tracers.Context, cfg json.RawMessage, chainConfig *params.ChainConfig) (*tracers.Tracer, error) {
	t := &noopTracer{}
	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnOpcode:                   t.OnOpcode,
			OnEnter:                    t.OnEnter,
		},
		GetResult: t.GetResult,
		Stop:      t.Stop,
	}, nil
}

// CaptureState implements the EVMLogger interface to trace a single step of VM execution.
func (t *noopTracer) OnOpcode(pc uint64, opcode byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
    op := vm.OpCode(opcode)
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

// CaptureEnter is called when EVM enters a new scope (via call, create or selfdestruct).
func (t *noopTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
    t.List[to] = to
}

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