// Copyright 2022 The go-ethereum Authors
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

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/internal"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

func init() {
	tracers.DefaultDirectory.Register("prestateTracer", newPrestateTracer, false)
}

type prestate = map[common.Address]*account
type account struct {
	StateDiff state                      `json:"stateDiff"`
	Code    string                      `json:"code"`
}
type state = map[common.Hash]common.Hash

type prestate2 = map[common.Address]*account2
type account2 struct {
	StateDiff state                      `json:"stateDiff"`
	Initialized state2
}
type state2 = map[common.Hash]bool
type amountData struct{
    Op string      `json:"op"`
    Amount0     common.Hash `json:"amount0"`
    Amount1     common.Hash `json:"amount1"`
    Gas bool      `json:"gasprice"`
}

type prestateTracer struct {
    env     *tracing.VMContext
    PRE     prestate2
	SSTORE  prestate    `json:"sstore"`
    List        list                `json:"list"`
    callstack []callFrame
    gasLimit  uint64
    data    amountData
    
    Op      string      `json:"op"`
	Interrupt bool      `json:"interrupt"`
	Reason    error     `json:"reason"`
}

func newPrestateTracer(ctx *tracers.Context, cfg json.RawMessage, chainConfig *params.ChainConfig) (*tracers.Tracer, error) {
	// First callframe contains tx context info
	// and is populated on start and end.
	t := &prestateTracer{SSTORE : prestate{},PRE : prestate2{},callstack: make([]callFrame, 1),data:amountData{},Op : "start"}

	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnTxStart:       t.OnTxStart,
			OnTxEnd:         t.OnTxEnd,
			OnEnter:         t.OnEnter,
			OnExit:          t.OnExit,
			OnOpcode:        t.OnOpcode,


			//OnStorageChange: t.OnStorageChange,
		},
		GetResult: t.GetResult,
		Stop:      t.Stop,
	}, nil
}

// CaptureEnd is called after the call finishes to finalize the tracing.

func (t *prestateTracer) CaptureEnd(output []byte, gasUsed uint64, err error) {
    t.callstack[0].processOutput(output, err)
}

// CaptureState implements the EVMLogger interface to trace a single step of VM execution.
func (t *prestateTracer) OnOpcode(pc uint64, opcode byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {

	stackData := scope.StackData()
	stackLen := len(stackData)
	op := vm.OpCode(opcode)

	switch {
	case stackLen >= 1 && op == vm.SSTORE:
		caller := scope.Address()
		slot := common.Hash(stackData[stackLen-1].Bytes32())
		value := common.Hash(stackData[stackLen-2].Bytes32())

		if _, ok := t.SSTORE[caller]; !ok {
		    t.SSTORE[caller] = &account{
				Code : "",
		        StateDiff : make(map[common.Hash]common.Hash),
		    }
	    }
		if _, ok := t.PRE[caller]; !ok {
		    t.PRE[caller] = &account2{
		        StateDiff : make(map[common.Hash]common.Hash),
		        Initialized : make(map[common.Hash]bool),
		    }
	    }
	    
	    if !t.PRE[caller].Initialized[slot]{
	        t.PRE[caller].StateDiff[slot] = t.env.StateDB.GetState(caller, slot)
	        t.PRE[caller].Initialized[slot] = true
	    }
	    
		t.SSTORE[caller].StateDiff[slot] = value
    case op == vm.RETURN || op == vm.STOP || op == vm.SELFDESTRUCT :
        t.Op = "RETURN"
    case op == vm.INVALID :
        t.Op = "INVALID"
    case op == vm.REVERT:
        t.Op = "REVERT"
	case op == vm.CREATE:
		caller := scope.Address()
		nonce := t.env.StateDB.GetNonce(caller)
		addr := crypto.CreateAddress(caller, nonce)

		if _, ok := t.SSTORE[addr]; !ok {
		    t.SSTORE[addr] = &account{
				Code : "1",
		        StateDiff : make(map[common.Hash]common.Hash),
		    }
	    }
	    t.SSTORE[addr].Code = "1"

	case stackLen >= 4 && op == vm.CREATE2:
		caller := scope.Address()
		offset := stackData[stackLen-2]
		size := stackData[stackLen-3]
		init, err := internal.GetMemoryCopyPadded(scope.MemoryData(), int64(offset.Uint64()), int64(size.Uint64()))
		if err != nil {
			log.Warn("failed to copy CREATE2 input", "err", err, "tracer", "prestateTracer", "offset", offset, "size", size)
			return
		}

		inithash := crypto.Keccak256(init)
		salt := stackData[stackLen-4]
		addr := crypto.CreateAddress2(caller, salt.Bytes32(), inithash)
		if _, ok := t.SSTORE[addr]; !ok {
		    t.SSTORE[addr] = &account{
				Code : "1",
		        StateDiff : make(map[common.Hash]common.Hash),
		    }
	    }
	    t.SSTORE[addr].Code = "1"
    case op == vm.LT || op == vm.GT:
        if op == vm.LT {
            t.data.Op = "LT"
        }
        
        if op == vm.GT {
            t.data.Op = "GT"
        }

        t.data.Amount0 = common.Hash(stackData[stackLen-1].Bytes32())
        t.data.Amount1 = common.Hash(stackData[stackLen-2].Bytes32())
        
    case op == vm.GASPRICE:
        t.data.Gas = true
    default:
        t.Op = "other"
	}
}

// CaptureEnter is called when EVM enters a new scope (via call, create or selfdestruct).
func (t *prestateTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
    t.List[to] = to
	call := callFrame{
		Type:  vm.OpCode(typ),
		From:  from,
		To:    to,
		Input: bytesToHex(input),
		Gas:   gas,
		Value: value,
	}
	t.callstack = append(t.callstack, call)
}

// CaptureExit is called when EVM exits a scope, even if the scope didn't
// execute any code.
func (t *prestateTracer) OnExit(depth int, output []byte, gasUsed uint64, err error, reverted bool) {

	if depth == 0 {
		t.CaptureEnd(output, gasUsed, err)
		return
	}
	size := len(t.callstack)
	if size <= 1 {
		return
	}
	// pop call
	call := t.callstack[size-1]
	t.callstack = t.callstack[:size-1]
	size -= 1

	call.GasUsed = gasUsed
	call.processOutput(output, err)
	t.callstack[size-1].Calls = append(t.callstack[size-1].Calls, call)
}

func (t *prestateTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.env = env
	t.gasLimit = tx.Gas()
}

func (t *prestateTracer) OnsystemTxEnd(intrinsicGas uint64) {
	t.callstack[0].GasUsed -= intrinsicGas
}

func (t *prestateTracer) OnTxEnd(receipt *types.Receipt, err error) {
    for addr,state := range t.SSTORE{
    	for key,_ := range state.StateDiff {
            
            preVal := t.PRE[addr].StateDiff[key]
    		newVal := t.env.StateDB.GetState(addr, key)
    		if preVal != newVal {
				t.SSTORE[addr].StateDiff[key] = newVal
    		}else{
    		    delete(t.SSTORE[addr].StateDiff,key)
    		}
    	}
    	
    	if(t.SSTORE[addr].Code == "1"){
    	    t.SSTORE[addr].Code = bytesToHex(t.env.StateDB.GetCode(addr))
    	}


    	if len(t.SSTORE[addr].StateDiff) == 0 && len(t.SSTORE[addr].Code) == 0{
    	    delete(t.SSTORE,addr)
    	}
    }
    t.callstack[0].GasUsed = receipt.GasUsed
}

// GetResult returns the json-encoded nested list of call traces, and any
// error arising from the encoding or forceful termination (via `Stop`).
func (t *prestateTracer) GetResult() (json.RawMessage, error) {
	res, err := json.Marshal(struct{
    	SSTORE  prestate    `json:"sstore"`
        List        list                `json:"list"`
        
        Op      string      `json:"op"`
    	Interrupt bool      `json:"interrupt"`
    	Reason    error     `json:"reason"`
    	
    	Data amountData `json:"amountData"`
    	Callstack callFrame `json:"callstack"`
	}{t.SSTORE,t.List,t.Op,t.Interrupt,t.Reason,t.data,t.callstack[0]})
	if err != nil {
		return nil, err
	}
	
	return json.RawMessage(res), t.Reason
}

// Stop terminates execution of the tracer at the first opportune moment.
func (t *prestateTracer) Stop(err error) {
	t.Reason = err
	t.Interrupt = true
}