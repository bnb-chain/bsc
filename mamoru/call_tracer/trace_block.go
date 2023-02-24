package call_tracer

import (
	"context"
	"errors"
	"fmt"

	"math/big"
	"runtime"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
	"github.com/panjf2000/ants/v2"

	"github.com/ethereum/go-ethereum/mamoru"
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
	Result []*mamoru.CallFrame `json:"result,omitempty"` // Trace results produced by the tracer
	Error  string              `json:"error,omitempty"`  // Trace failure produced by the tracer
}

func TraceBlock(ctx context.Context,
	config *Config,
	block *types.Block,
) ([]*TxTraceResult, error) {
	defer ants.Release()
	if block.NumberU64() == 0 {
		return nil, errors.New("genesis is not traceable")
	}
	// Execute all the transaction contained within the block concurrently
	var (
		signer         = types.MakeSigner(config.chainConfig, block.Number())
		txs            = block.Transactions()
		results        = make([]*TxTraceResult, len(txs))
		stateDB        = config.stateDB
		blockHash      = block.Hash()
		pend           = new(sync.WaitGroup)
		jobs           = make(chan *txTraceTask, len(txs))
		blockCtx       = core.NewEVMBlockContext(block.Header(), config.chainContext, nil)
		defaultPool, _ = ants.NewPool(ants.DefaultAntsPoolSize, ants.WithExpiryDuration(10*time.Second))
	)
	threads := runtime.NumCPU()
	if threads > len(txs) {
		threads = len(txs)
	}

	for th := 0; th < threads; th++ {
		pend.Add(1)
		_ = defaultPool.Submit(func() {
			defer pend.Done()
			// Fetch and execute the next transaction trace tasks
			for task := range jobs {
				msg, _ := txs[task.index].AsMessage(signer, block.BaseFee())
				txctx := &tracers.Context{
					BlockHash: blockHash,
					TxIndex:   task.index,
					TxHash:    txs[task.index].Hash(),
				}
				res, err := traceTx(ctx, config.chainConfig, msg, txctx, blockCtx, task.statedb, config.engin)
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
	//blockCtx := core.NewEVMBlockContext(block.Header(), config.chainContext, nil)
	for i, tx := range txs {
		// Send the trace task over for execution
		jobs <- &txTraceTask{statedb: stateDB.Copy(), index: i}

		if posa, ok := config.engin.(consensus.PoSA); ok {
			if isSystem, _ := posa.IsSystemTransaction(tx, block.Header()); isSystem {
				balance := stateDB.GetBalance(consensus.SystemAddress)
				if balance.Cmp(common.Big0) > 0 {
					stateDB.SetBalance(consensus.SystemAddress, big.NewInt(0))
					stateDB.AddBalance(block.Header().Coinbase, balance)
				}
			}
		}
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
	statedb *state.StateDB,
	engine consensus.Engine) ([]*mamoru.CallFrame, error) {
	var (
		err       error
		timeout   = 15 * time.Second
		txContext = core.NewEVMTxContext(message)
	)
	// Creating CallTracer
	tracer, err := mamoru.NewCallTracer(true)
	if err != nil {
		return nil, err
	}

	// Run the transaction with tracing enabled.
	vmenv := vm.NewEVM(vmctx, txContext, statedb, chainConfig, vm.Config{Debug: true, Tracer: tracer, NoBaseFee: true})

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	go func() {
		<-deadlineCtx.Done()
		if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
			tracer.Stop(errors.New("execution timeout"))
			vmenv.Cancel()
		}
	}()
	defer cancel()

	if posa, ok := engine.(consensus.PoSA); ok && message.From() == vmctx.Coinbase &&
		posa.IsSystemContract(message.To()) && message.GasPrice().Cmp(big.NewInt(0)) == 0 {
		balance := statedb.GetBalance(consensus.SystemAddress)
		if balance.Cmp(common.Big0) > 0 {
			statedb.SetBalance(consensus.SystemAddress, big.NewInt(0))
			statedb.AddBalance(vmctx.Coinbase, balance)
		}
	}

	// Call Prepare to clear out the statedb access list
	statedb.Prepare(txctx.TxHash, txctx.TxIndex)
	if _, err = core.ApplyMessage(vmenv, message, new(core.GasPool).AddGas(message.Gas())); err != nil {
		return nil, fmt.Errorf("tracing failed: %w", err)
	}

	return tracer.GetResult()
}
