package tracer

import (
	"context"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
	"github.com/panjf2000/ants/v2"
	"runtime"
	"sync"
	"time"
)

type Config struct {
	stateDB      *state.StateDB
	chainConfig  *params.ChainConfig
	chainContext core.ChainContext
	engin        consensus.Engine
}

func NewTracerConfig(stateDB *state.StateDB, chainConfig *params.ChainConfig, chainContext core.ChainContext) *Config {
	return &Config{
		stateDB:      stateDB,
		chainConfig:  chainConfig,
		chainContext: chainContext,
		engin:        chainContext.Engine(),
	}
}

func (c *Config) GetChainConfig() *params.ChainConfig {
	return c.chainConfig
}

// txTraceTask represents a single transaction trace task when an entire block
// is being traced.
type txTraceTask struct {
	statedb *state.StateDB // Intermediate state prepped for tracing
	index   int            // Transaction offset in the block
}

// TxTraceResult is the result of a single transaction trace.
type TxTraceResult struct {
	Result []*CallFrame `json:"result,omitempty"` // Trace results produced by the tracer
	Error  string       `json:"error,omitempty"`  // Trace failure produced by the tracer
}

func TraceBlock(ctx context.Context,
	config *Config,
	block *types.Block,
) ([]*TxTraceResult, error) {
	if block.NumberU64() == 0 {
		return nil, errors.New("genesis is not traceable")
	}
	// Execute all the transaction contained within the block concurrently
	var (
		signer  = types.MakeSigner(config.chainConfig, block.Number())
		txs     = block.Transactions()
		results = make([]*TxTraceResult, len(txs))
		stateDB = config.stateDB

		pend = new(sync.WaitGroup)
		jobs = make(chan *txTraceTask, len(txs))
	)
	threads := runtime.NumCPU()
	if threads > len(txs) {
		threads = len(txs)
	}
	blockHash := block.Hash()
	var defaultPool, _ = ants.NewPool(ants.DefaultAntsPoolSize, ants.WithExpiryDuration(10*time.Second))
	//defer defaultPool.Release()
	for th := 0; th < threads; th++ {
		pend.Add(1)
		_ = defaultPool.Submit(func() {
			blockCtx := core.NewEVMBlockContext(block.Header(), config.chainContext, nil)
			defer pend.Done()
			// Fetch and execute the next transaction trace tasks
			for task := range jobs {
				msg, _ := txs[task.index].AsMessage(signer, block.BaseFee())
				txctx := &tracers.Context{
					BlockHash: blockHash,
					TxIndex:   task.index,
					TxHash:    txs[task.index].Hash(),
				}
				res, err := traceTx(ctx, config.chainConfig, msg, txctx, blockCtx, task.statedb)
				if err != nil {
					results[task.index] = &TxTraceResult{Error: err.Error()}
					continue
				}
				results[task.index] = &TxTraceResult{Result: res}
			}
		})
	}
	// EthFeed the transactions into the tracers and return
	var failed error
	blockCtx := core.NewEVMBlockContext(block.Header(), config.chainContext, nil)
	for i, tx := range txs {
		// Send the trace task over for execution
		jobs <- &txTraceTask{statedb: stateDB.Copy(), index: i}

		// Generate the next state snapshot fast without tracing
		msg, _ := tx.AsMessage(signer, block.BaseFee())

		stateDB.Prepare(tx.Hash(), i)
		vmenv := vm.NewEVM(blockCtx, core.NewEVMTxContext(msg), stateDB, config.chainConfig, vm.Config{})
		if _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.Gas())); err != nil {
			failed = err
			break
		}
		// Finalize the state so any modifications are written to the trie
		// Only delete empty objects if EIP158/161 (a.k.a Spurious Dragon) is in effect
		stateDB.Finalise(vmenv.ChainConfig().IsEIP158(block.Number()))
	}
	close(jobs)
	pend.Wait()

	// If execution failed in between, abort
	if failed != nil {
		return nil, failed
	}
	return results, nil
}

// traceTx configures a new tracer according to the provided configuration, and
// executes the given message in the provided environment. The return value will
// be tracer dependent.
func traceTx(ctx context.Context,
	chainConfig *params.ChainConfig,
	message core.Message,
	txctx *tracers.Context,
	vmctx vm.BlockContext,
	statedb *state.StateDB) ([]*CallFrame, error) {
	var (
		err       error
		timeout   = 15 * time.Second
		txContext = core.NewEVMTxContext(message)
	)
	// Creating CallTracer
	tracer, err := NewCallTracer(true)
	if err != nil {
		return nil, err
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	go func() {
		<-deadlineCtx.Done()
		if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
			tracer.Stop(errors.New("execution timeout"))
		}
	}()
	defer cancel()

	// Run the transaction with tracing enabled.
	vmenv := vm.NewEVM(vmctx, txContext, statedb, chainConfig, vm.Config{Debug: true, Tracer: tracer, NoBaseFee: true})

	// Call Prepare to clear out the statedb access list
	statedb.Prepare(txctx.TxHash, txctx.TxIndex)
	if _, err = core.ApplyMessage(vmenv, message, new(core.GasPool).AddGas(message.Gas())); err != nil {
		return nil, fmt.Errorf("tracing failed: %w", err)
	}

	return tracer.GetResult()
}
