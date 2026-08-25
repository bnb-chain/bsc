// Copyright 2026 The go-ethereum Authors
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

package tracers

import (
	"context"
	"encoding/json"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// parallelTestTracer is registered as a JS tracer so that requests asking for it
// are dispatched to traceBlockParallel, without pulling the JS engine into this
// package. It reads state back through the tracing StateDB, the way the bundled
// JS tracers do via their `db` binding.
const parallelTestTracer = "parallelTestTracer"

func init() {
	DefaultDirectory.Register(parallelTestTracer, newParallelTestTracer, true)
}

func newParallelTestTracer(ctx *Context, _ json.RawMessage, _ *params.ChainConfig) (*Tracer, error) {
	var (
		env  *tracing.VMContext
		slot common.Hash
	)
	return &Tracer{
		GetResult: func() (json.RawMessage, error) {
			return json.Marshal(slot)
		},
		Stop: func(err error) {},
		Hooks: &tracing.Hooks{
			OnTxStart: func(vmctx *tracing.VMContext, tx *types.Transaction, from common.Address) {
				env = vmctx
			},
			OnTxEnd: func(receipt *types.Receipt, err error) {
				if env != nil {
					slot = env.StateDB.GetState(storageReaderAddr, common.Hash{})
				}
			},
		},
	}, nil
}

// storageReaderAddr is the address of the contract every transaction of the
// traced test block calls into.
var storageReaderAddr = common.HexToAddress("0x00000000000000000000000000000000c0ffee01")

// slotsPerTx is the number of storage slots each transaction of the traced test
// block reads.
const slotsPerTx = 64

// storageReaderCode loads slotsPerTx consecutive storage slots, starting at the
// offset passed in as call data, and finally writes to slot zero. Giving each
// transaction of a block a distinct offset means every trace has to resolve its
// own slots, which is what fills the origin-storage cache of the state object
// backing the contract.
var storageReaderCode = []byte{
	byte(vm.PUSH1), 0x00,
	byte(vm.CALLDATALOAD),      // stack: [base]
	byte(vm.PUSH1), slotsPerTx, // stack: [i, base]
	byte(vm.JUMPDEST), // loop start, pc = 5
	byte(vm.DUP2),
	byte(vm.DUP2),
	byte(vm.ADD),
	byte(vm.SLOAD),
	byte(vm.POP),
	byte(vm.PUSH1), 0x01,
	byte(vm.SWAP1),
	byte(vm.SUB),
	byte(vm.DUP1),
	byte(vm.PUSH1), 0x05, // jump back to the loop start while i != 0
	byte(vm.JUMPI),
	byte(vm.PUSH1), 0x2a,
	byte(vm.PUSH1), 0x00,
	byte(vm.SSTORE),
	byte(vm.STOP),
}

// newStorageReaderBackend assembles a chain whose last block contains one
// transaction per sender, all of them touching the storage of a single contract
// but each of them reading a distinct range of slots.
func newStorageReaderBackend(t *testing.T, senders int) (*testBackend, uint64) {
	t.Helper()

	accounts := newAccounts(senders)
	storage := make(map[common.Hash]common.Hash)
	for i := 1; i <= (senders+1)*slotsPerTx; i++ {
		storage[common.BigToHash(big.NewInt(int64(i)))] = common.BigToHash(big.NewInt(int64(i * 7)))
	}
	alloc := types.GenesisAlloc{
		storageReaderAddr: {
			Nonce:   1,
			Balance: new(big.Int),
			Code:    storageReaderCode,
			Storage: storage,
		},
	}
	for _, acc := range accounts {
		alloc[acc.addr] = types.Account{Balance: big.NewInt(params.Ether)}
	}
	var (
		signer  = types.HomesteadSigner{}
		genesis = &core.Genesis{Config: params.TestChainConfig, Alloc: alloc}
		backend = newTestBackend(t, 1, genesis, func(i int, b *core.BlockGen) {
			for j, acc := range accounts {
				base := common.BigToHash(big.NewInt(int64(j * slotsPerTx)))
				tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{
					Nonce:    0,
					To:       &storageReaderAddr,
					Value:    new(big.Int),
					Gas:      1_000_000,
					GasPrice: b.BaseFee(),
					Data:     base.Bytes(),
				}), signer, acc.key)
				b.AddTx(tx)
			}
		})
	)
	if have := len(backend.chain.GetBlockByNumber(1).Transactions()); have != senders {
		backend.teardown()
		t.Fatalf("unexpected number of transactions: have %d, want %d", have, senders)
	}
	return backend, 1
}

// TestTraceBlockParallelSharedContract traces a block whose transactions all
// touch the storage of a single contract. The tracing work is spread over
// concurrent workers by traceBlockParallel, so any state accidentally shared
// between the per-task StateDB copies surfaces as a data race here (or, without
// the race detector, as a "concurrent map writes" fatal error).
//
// See https://github.com/bnb-chain/bsc/issues/3797.
func TestTraceBlockParallelSharedContract(t *testing.T) {
	backend, number := newStorageReaderBackend(t, 8)
	defer backend.teardown()

	var (
		block  = backend.chain.GetBlockByNumber(number)
		parent = backend.chain.GetBlockByNumber(number - 1)
	)
	statedb, release, err := backend.StateAtBlock(context.Background(), parent, nil, true, false)
	if err != nil {
		t.Fatalf("failed to retrieve the parent state: %v", err)
	}
	defer release()

	// traceBlockParallel is the path taken by debug_traceBlockByNumber whenever
	// a JS tracer is requested. Invoke it directly with the default tracer to
	// keep the state access under test free of tracer specific behaviour.
	api := NewAPI(backend)
	results, err := api.traceBlockParallel(context.Background(), block, statedb, &TraceConfig{})
	if err != nil {
		t.Fatalf("failed to trace block: %v", err)
	}
	assertTraces(t, results, len(block.Transactions()))
}

// TestTraceBlockByNumberConcurrent replays the workload reported in
// bnb-chain/bsc#3797: a sustained stream of debug_traceBlockByNumber requests
// with a JS tracer against a block whose transactions all read and write the
// storage of one contract. Every request fans out over traceBlockParallel, so
// state shared either between the workers of a request or between concurrent
// requests is reported by the race detector.
func TestTraceBlockByNumberConcurrent(t *testing.T) {
	backend, number := newStorageReaderBackend(t, 4)
	defer backend.teardown()

	var (
		api     = NewAPI(backend)
		tracer  = parallelTestTracer
		config  = &TraceConfig{Tracer: &tracer}
		wantTxs = len(backend.chain.GetBlockByNumber(number).Transactions())
		wg      sync.WaitGroup
	)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := api.TraceBlockByNumber(context.Background(), rpc.BlockNumber(number), config)
			if err != nil {
				t.Errorf("failed to trace block: %v", err)
				return
			}
			assertTraces(t, results, wantTxs)
		}()
	}
	wg.Wait()
}

func assertTraces(t *testing.T, results []*txTraceResult, want int) {
	t.Helper()

	if len(results) != want {
		t.Errorf("unexpected number of traces: have %d, want %d", len(results), want)
		return
	}
	for i, res := range results {
		switch {
		case res == nil:
			t.Errorf("trace %d is missing", i)
		case res.Error != "":
			t.Errorf("trace %d failed: %v", i, res.Error)
		case res.Result == nil:
			t.Errorf("trace %d has no result", i)
		}
	}
}
