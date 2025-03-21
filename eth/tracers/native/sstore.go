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
	tracers.DefaultDirectory.Register("sstoreTracer", newSstoreTracer, false)
}

type sstoreTracer struct {
    env     *tracing.VMContext
	SSTORE  prestate    `json:"sstore"`
	PRE     prestate2

    Op      string      `json:"op"`
	Interrupt bool      `json:"interrupt"`
	Reason    error     `json:"reason"`
}

func newSstoreTracer(ctx *tracers.Context, cfg json.RawMessage, chainConfig *params.ChainConfig) (*tracers.Tracer, error) {
	// First callframe contains tx context info
	// and is populated on start and end.
	t := &sstoreTracer{SSTORE : prestate{},PRE : prestate2{},Op : "start"}

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
func (t *sstoreTracer) CaptureEnd(output []byte, gasUsed uint64, err error) {
}

// CaptureState implements the EVMLogger interface to trace a single step of VM execution.
func (t *sstoreTracer) OnOpcode(pc uint64, opcode byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {

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
    
    default:
        t.Op = "other"
	}
}

// CaptureFault implements the EVMLogger interface to trace an execution fault.
func (t *sstoreTracer) CaptureFault(pc uint64, op vm.OpCode, gas, cost uint64, _ *vm.ScopeContext, depth int, err error) {
    //t.Reason = err
}

// CaptureEnter is called when EVM enters a new scope (via call, create or selfdestruct).
func (t *sstoreTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
}

// CaptureExit is called when EVM exits a scope, even if the scope didn't
// execute any code.
func (t *sstoreTracer) OnExit(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
}

func (t *sstoreTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.env = env
}

func (t *sstoreTracer) OnTxEnd(receipt *types.Receipt, err error) {
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
}

func (t *sstoreTracer) OnystemTxEnd(intrinsicGas uint64) {}

// GetResult returns the json-encoded nested list of call traces, and any
// error arising from the encoding or forceful termination (via `Stop`).
func (t *sstoreTracer) GetResult() (json.RawMessage, error) {
	res, err := json.Marshal(struct{
    	SSTORE  prestate    `json:"sstore"`
        Op      string      `json:"op"`
    	Interrupt bool      `json:"interrupt"`
    	Reason    error     `json:"reason"`
	}{t.SSTORE,t.Op,t.Interrupt,t.Reason})

	if err != nil {
		return nil, err
	}
	
	return json.RawMessage(res), t.Reason
}

// Stop terminates execution of the tracer at the first opportune moment.
func (t *sstoreTracer) Stop(err error) {
	t.Reason = err
	t.Interrupt = true
}