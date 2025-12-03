package core

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"golang.org/x/sync/errgroup"
)

// ProcessResultWithMetrics wraps ProcessResult with some metrics that are
// emitted when executing blocks containing access lists.
type ProcessResultWithMetrics struct {
	ProcessResult *ProcessResult
	// the time it took to load modified prestate accounts from disk and instantiate statedbs for execution
	PreProcessTime time.Duration
	// the time it took to validate the block post transaction execution and state root calculation
	PostProcessTime time.Duration
	// the time it took to hash the state root, including intermediate node reads
	RootCalcTime time.Duration
	// the time that it took to load the prestate for accounts that were updated as part of
	// the state root update
	PrestateLoadTime time.Duration
	// the time it took to execute all txs in the block
	ExecTime time.Duration
}

// ParallelStateProcessor is used to execute and verify blocks containing
// access lists.
type ParallelStateProcessor struct {
	*StateProcessor
	vmCfg *vm.Config
}

// NewParallelStateProcessor returns a new ParallelStateProcessor instance.
func NewParallelStateProcessor(config *params.ChainConfig, chain *HeaderChain, cfg *vm.Config) ParallelStateProcessor {
	res := NewStateProcessor(config, chain)
	return ParallelStateProcessor{
		res,
		cfg,
	}
}

// called by resultHandler when all transactions have successfully executed.
// performs post-tx state transition (system contracts and withdrawals)
// and calculates the ProcessResult, returning it to be sent on resCh
// by resultHandler
func (p *ParallelStateProcessor) prepareExecResult(block *types.Block, allStateReads *bal.StateAccesses, tExecStart time.Time, postTxState *state.StateDB, receipts types.Receipts, cfg vm.Config) *ProcessResultWithMetrics {
	tExec := time.Since(tExecStart)
	var requests [][]byte
	tPostprocessStart := time.Now()
	header := block.Header()

	balTracer, hooks := NewBlockAccessListTracer()
	tracingStateDB := state.NewHookedState(postTxState, hooks)
	context := NewEVMBlockContext(header, p.chain, nil)

	evm := vm.NewEVM(context, tracingStateDB, p.config, *p.vmCfg)

	// 1. order the receipts by tx index
	// 2. correctly calculate the cumulative gas used per receipt, returning bad block error if it goes over the allowed
	slices.SortFunc(receipts, func(a, b *types.Receipt) int {
		return cmp.Compare(a.TransactionIndex, b.TransactionIndex)
	})

	var cumulativeGasUsed uint64
	var allLogs []*types.Log
	for _, receipt := range receipts {
		receipt.CumulativeGasUsed = cumulativeGasUsed + receipt.GasUsed
		cumulativeGasUsed += receipt.GasUsed
		if receipt.CumulativeGasUsed > header.GasLimit {
			return &ProcessResultWithMetrics{
				ProcessResult: &ProcessResult{Error: fmt.Errorf("gas limit exceeded")},
			}
		}
	}

	// Read requests if Prague is enabled.
	if p.config.IsPrague(block.Number(), block.Time()) && p.chain.config.Parlia == nil {
		requests = [][]byte{}
		// EIP-6110
		if err := ParseDepositLogs(&requests, allLogs, p.config); err != nil {
			return &ProcessResultWithMetrics{
				ProcessResult: &ProcessResult{Error: err},
			}
		}

		// EIP-7002
		err := ProcessWithdrawalQueue(&requests, evm)
		if err != nil {
			return &ProcessResultWithMetrics{
				ProcessResult: &ProcessResult{Error: err},
			}
		}

		// EIP-7251
		err = ProcessConsolidationQueue(&requests, evm)
		if err != nil {
			return &ProcessResultWithMetrics{
				ProcessResult: &ProcessResult{Error: err},
			}
		}
	}

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	// Classify transactions into common and system transactions
	posa, isPoSA := p.chain.engine.(consensus.PoSA)
	commonTxs := make([]*types.Transaction, 0, len(block.Transactions()))
	systemTxs := make([]*types.Transaction, 0, 2) // usually 2 system txs: validator set + system reward

	for _, tx := range block.Transactions() {
		if isPoSA {
			if isSystemTx, err := posa.IsSystemTransaction(tx, block.Header()); err != nil {
				return &ProcessResultWithMetrics{
					ProcessResult: &ProcessResult{Error: fmt.Errorf("could not check if tx is system tx [%v]: %w", tx.Hash().Hex(), err)},
				}
			} else if isSystemTx {
				systemTxs = append(systemTxs, tx)
				continue
			}
		}
		commonTxs = append(commonTxs, tx)
	}
	systemTxCount := len(systemTxs)

	var usedGas uint64 = cumulativeGasUsed
	err := p.chain.engine.Finalize(p.chain, header, tracingStateDB, &commonTxs, block.Uncles(), block.Withdrawals(), (*[]*types.Receipt)(&receipts), &systemTxs, &usedGas, cfg.Tracer)
	if err != nil {
		log.Error("Finalize failed", "error", err.Error())
	}
	// invoke Finalise so that withdrawals are accounted for in the state diff
	postTxState.Finalise(true)

	balTracer.OnBlockFinalization()
	diff, stateReads := balTracer.builder.FinalizedIdxChanges()
	allStateReads.Merge(stateReads)
	balIdx := len(block.Transactions()) - systemTxCount + 1
	if err := postTxState.BlockAccessList().ValidateStateDiffRange(balIdx, len(block.Transactions())+1, diff); err != nil {
		log.Error("validate state diff on post-tx", "idx", balIdx, "err", err)
		return &ProcessResultWithMetrics{
			ProcessResult: &ProcessResult{Error: fmt.Errorf("validate state diff on post-tx: idx %d, err %w", balIdx, err)},
		}
	}

	if err := postTxState.BlockAccessList().ValidateStateReads(*allStateReads); err != nil {
		return &ProcessResultWithMetrics{
			ProcessResult: &ProcessResult{Error: err},
		}
	}

	tPostprocess := time.Since(tPostprocessStart)

	for _, receipt := range receipts {
		allLogs = append(allLogs, receipt.Logs...)
	}

	return &ProcessResultWithMetrics{
		ProcessResult: &ProcessResult{
			Receipts: receipts,
			Requests: requests,
			Logs:     allLogs,
			GasUsed:  usedGas,
		},
		PostProcessTime: tPostprocess,
		ExecTime:        tExec,
	}
}

type txExecResult struct {
	idx     int // transaction index
	receipt *types.Receipt
	err     error // non-EVM error which would render the block invalid

	stateReads bal.StateAccesses
}

type txExecRequest struct {
	idx    int
	balIdx int
	tx     *types.Transaction
}

// resultHandler polls until all transactions have finished executing and the
// state root calculation is complete. The result is emitted on resCh.
func (p *ParallelStateProcessor) resultHandler(block *types.Block, preTxStateReads bal.StateAccesses, postTxState *state.StateDB, tExecStart time.Time, txResCh <-chan txExecResult, stateRootCalcResCh <-chan stateRootCalculationResult, resCh chan *ProcessResultWithMetrics, cfg vm.Config, expectedResults int) {
	// 1. if the block has transactions, receive the execution results from all of them and return an error on resCh if any txs err'd
	// 2. once all txs are executed, compute the post-tx state transition and produce the ProcessResult sending it on resCh (or an error if the post-tx state didn't match what is reported in the BAL)
	var receipts []*types.Receipt
	gp := new(GasPool)
	gp.SetGas(block.GasLimit())
	var execErr error
	var numTxComplete int

	allReads := make(bal.StateAccesses)
	allReads.Merge(preTxStateReads)
	if expectedResults > 0 {
	loop:
		for {
			select {
			case res := <-txResCh:
				if execErr == nil {
					if res.err != nil {
						execErr = res.err
					} else {
						if err := gp.SubGas(res.receipt.GasUsed); err != nil {
							execErr = err
						} else {
							receipts = append(receipts, res.receipt)
							allReads.Merge(res.stateReads)
						}
					}
				}
				numTxComplete++
				if numTxComplete == expectedResults {
					break loop
				}
			}
		}

		if execErr != nil {
			resCh <- &ProcessResultWithMetrics{ProcessResult: &ProcessResult{Error: execErr}}
			return
		}
	}

	execResults := p.prepareExecResult(block, &allReads, tExecStart, postTxState, receipts, cfg)
	rootCalcRes := <-stateRootCalcResCh

	if execResults.ProcessResult.Error != nil {
		resCh <- execResults
	} else if rootCalcRes.err != nil {
		resCh <- &ProcessResultWithMetrics{ProcessResult: &ProcessResult{Error: rootCalcRes.err}}
	} else {
		execResults.RootCalcTime = rootCalcRes.rootCalcTime
		execResults.PrestateLoadTime = rootCalcRes.prestateLoadTime
		resCh <- execResults
	}
}

type stateRootCalculationResult struct {
	err              error
	prestateLoadTime time.Duration
	rootCalcTime     time.Duration
	root             common.Hash
}

// calcAndVerifyRoot performs the post-state root hash calculation, verifying
// it against what is reported by the block and returning a result on resCh.
func (p *ParallelStateProcessor) calcAndVerifyRoot(preState *state.StateDB, block *types.Block, resCh chan stateRootCalculationResult) {
	// calculate and apply the block state modifications
	root, prestateLoadTime, rootCalcTime := preState.BlockAccessList().StateRoot(preState)

	res := stateRootCalculationResult{
		root:             root,
		prestateLoadTime: prestateLoadTime,
		rootCalcTime:     rootCalcTime,
	}

	if root != block.Root() {
		res.err = fmt.Errorf("state root mismatch. local: %x. remote: %x, db error: %v", root, block.Root(), preState.Error())
	}
	resCh <- res
}

// execTx executes single transaction returning a result which includes state accessed/modified
func (p *ParallelStateProcessor) execTx(block *types.Block, tx *types.Transaction, idx int, balIdx int, db *state.StateDB, signer types.Signer) *txExecResult {
	header := block.Header()
	balTracer, hooks := NewBlockAccessListTracer()
	tracingStateDB := state.NewHookedState(db, hooks)
	context := NewEVMBlockContext(header, p.chain, nil)
	cfg := vm.Config{
		Tracer:                  hooks,
		NoBaseFee:               p.vmCfg.NoBaseFee,
		EnablePreimageRecording: p.vmCfg.EnablePreimageRecording,
		ExtraEips:               slices.Clone(p.vmCfg.ExtraEips),
		StatelessSelfValidation: p.vmCfg.StatelessSelfValidation,
	}
	cfg.Tracer = hooks
	evm := vm.NewEVM(context, tracingStateDB, p.config, cfg)

	msg, err := TransactionToMessage(tx, signer, header.BaseFee)
	if err != nil {
		err = fmt.Errorf("could not apply tx %d [%v]: %w", idx, tx.Hash().Hex(), err)
		return &txExecResult{err: err}
	}
	sender, _ := types.Sender(signer, tx)
	db.SetTxSender(sender)
	db.SetTxContext(tx.Hash(), idx)
	db.SetAccessListIndex(balIdx)

	gp := new(GasPool)
	gp.SetGas(block.GasLimit())
	var gasUsed uint64
	receipt, err := ApplyTransactionWithEVM(msg, gp, db, block.Number(), block.Hash(), context.Time, tx, &gasUsed, evm)
	if err != nil {
		err := fmt.Errorf("could not apply tx %d [%v]: %w", idx, tx.Hash().Hex(), err)
		return &txExecResult{err: err}
	}
	diff, accesses := balTracer.builder.FinalizedIdxChanges()
	if err := db.BlockAccessList().ValidateStateDiff(balIdx, diff); err != nil {
		return &txExecResult{err: fmt.Errorf("validate state diff on tx: idx %d, balIdx %d, err %w", idx+1, balIdx, err)}
	}

	receipt.Bloom = types.CreateBloom(receipt)
	return &txExecResult{
		idx:        idx,
		receipt:    receipt,
		stateReads: accesses,
	}
}

// Process performs EVM execution and state root computation for a block which is known
// to contain an access list.
func (p *ParallelStateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (*ProcessResultWithMetrics, error) {
	var (
		header = block.Header()
		resCh  = make(chan *ProcessResultWithMetrics)
		signer = types.MakeSigner(p.config, header.Number, header.Time)
	)

	txResCh := make(chan txExecResult)
	pStart := time.Now()
	var (
		tPreprocess      time.Duration // time to create a set of prestates for parallel transaction execution
		tExecStart       time.Time
		rootCalcResultCh = make(chan stateRootCalculationResult)
	)

	// Mutate the block and state according to any hard-fork specs
	if p.config.DAOForkSupport && p.config.DAOForkBlock != nil && p.config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(statedb)
	}
	alReader := state.NewBALReader(block, statedb)
	statedb.SetBlockAccessList(alReader)

	var (
		context vm.BlockContext
	)
	log.Debug("parallel state processor", "block", block.Number(), "hash", block.Hash(), "signData", common.Bytes2Hex(block.AccessList().SignData))

	balTracer, hooks := NewBlockAccessListTracer()
	tracingStateDB := state.NewHookedState(statedb, hooks)
	originalStateDB := statedb.Copy()
	// TODO: figure out exactly why we need to set the hooks on the TracingStateDB and the vm.Config
	cfg = vm.Config{
		Tracer:                  hooks,
		NoBaseFee:               p.vmCfg.NoBaseFee,
		EnablePreimageRecording: p.vmCfg.EnablePreimageRecording,
		ExtraEips:               slices.Clone(p.vmCfg.ExtraEips),
		StatelessSelfValidation: p.vmCfg.StatelessSelfValidation,
	}
	cfg.Tracer = hooks

	context = NewEVMBlockContext(header, p.chain, nil)
	evm := vm.NewEVM(context, tracingStateDB, p.chain.config, cfg)

	lastBlock := p.chain.GetHeaderByHash(block.ParentHash())
	if lastBlock == nil {
		return nil, errors.New("could not get parent block")
	}
	// Handle upgrade built-in system contract code
	systemcontracts.TryUpdateBuildInSystemContract(p.config, block.Number(), lastBlock.Time, block.Time(), tracingStateDB, true)

	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		ProcessBeaconBlockRoot(*beaconRoot, evm)
	}
	if p.config.IsPrague(block.Number(), block.Time()) || p.config.IsVerkle(block.Number(), block.Time()) {
		ProcessParentBlockHash(block.ParentHash(), evm)
	}
	// TODO: weird that I have to manually call finalize here
	balTracer.OnPreTxExecutionDone()

	diff, stateReads := balTracer.builder.FinalizedIdxChanges()
	if err := statedb.BlockAccessList().ValidateStateDiff(0, diff); err != nil {
		return nil, fmt.Errorf("validate state diff on pre-tx: idx 0 , err %w", err)
	}
	// compute the post-tx state prestate (before applying final block system calls and eip-4895 withdrawals)
	// the post-tx state transition is verified by resultHandler
	postTxState := originalStateDB.Copy()

	posa, isPoSA := p.chain.engine.(consensus.PoSA)
	var systemTxCount int
	execJobs := make([]txExecRequest, 0, len(block.Transactions())-systemTxCount)
	for i, tx := range block.Transactions() {
		if isPoSA {
			isSystemTx, err := posa.IsSystemTransaction(tx, header)
			if err != nil {
				return nil, fmt.Errorf("could not check if tx is system tx [%v]: %w", tx.Hash().Hex(), err)
			}
			if isSystemTx {
				systemTxCount++
				continue
			}
			balIdx := i - systemTxCount + 1
			execJobs = append(execJobs, txExecRequest{idx: i, balIdx: balIdx, tx: tx})
		}
		if p.config.IsCancun(block.Number(), block.Time()) && systemTxCount > 0 {
			return nil, fmt.Errorf("normal tx %d [%v] after systemTx", i, tx.Hash().Hex())
		}
	}

	postTxState.SetAccessListIndex(len(block.Transactions()) - systemTxCount + 1)
	tPreprocess = time.Since(pStart)
	// execute transactions and state root calculation in parallel

	tExecStart = time.Now()

	expectedResults := len(execJobs)
	go p.resultHandler(block, stateReads, postTxState, tExecStart, txResCh, rootCalcResultCh, resCh, cfg, expectedResults)
	var workers errgroup.Group
	startingState := originalStateDB.Copy()
	for _, job := range execJobs {
		workers.Go(func() error {
			res := p.execTx(block, job.tx, job.idx, job.balIdx, startingState.Copy(), signer)
			txResCh <- *res
			return nil
		})
	}

	go p.calcAndVerifyRoot(statedb, block, rootCalcResultCh)

	res := <-resCh
	if res.ProcessResult.Error != nil {
		return nil, res.ProcessResult.Error
	}
	res.PreProcessTime = tPreprocess
	//	res.PreProcessLoadTime = tPreprocessLoad
	return res, nil
}
