// Copyright 2015 The go-ethereum Authors
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

package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/internal/telemetry"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

const largeTxGasLimit = 10000000 // 10M Gas, to measure the execution time of large tx

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	chain *HeaderChain // Canonical header chain
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(chain *HeaderChain) *StateProcessor {
	return &StateProcessor{
		chain: chain,
	}
}

// chainConfig returns the chain configuration.
func (p *StateProcessor) chainConfig() *params.ChainConfig {
	return p.chain.Config()
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(ctx context.Context, block *types.Block, statedb *state.StateDB, jumpDestCache vm.JumpDestCache, precompileCache *vm.PrecompileCache, cfg vm.Config, execIndex *atomic.Int64) (*ProcessResult, error) {
	// TODO: Adapt parallel execution to Parlia system transactions, system-contract
	// upgrades and payment-lane validation before enabling it for BSC.
	if p.chainConfig().IsNotInBSC() && supportsParallelExecution(block, p.chainConfig(), statedb.Witness() != nil, cfg.Tracer != nil, cfg.DisableParallelExecution) {
		return p.processParallel(ctx, block, statedb, jumpDestCache, precompileCache, cfg)
	}
	var (
		config      = p.chainConfig()
		receipts    = make([]*types.Receipt, 0, len(block.Transactions()))
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = NewGasPool(block.GasLimit())
		err         error

		// The receipts are digested on a pipeline of their own, fed as the
		// transactions finish, so the block bloom and the receipt root are
		// ready when the validator asks. With a tracer attached the blooms
		// stay in the loop, it is handed every receipt through OnTxEnd.
		eagerBloom = cfg.Tracer != nil
		pipeline   = newDigestPipeline(len(block.Transactions()), eagerBloom)
	)
	// Run out the pipeline on the paths that abandon the block half way through.
	defer pipeline.abandon()

	var tracingStateDB = vm.StateDB(statedb)
	if hooks := cfg.Tracer; hooks != nil {
		tracingStateDB = state.NewHookedState(statedb, hooks)
	}
	// Mutate the block and state according to any hard-fork specs
	if config.DAOForkSupport && config.DAOForkBlock != nil && config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(tracingStateDB)
	}
	parent := p.chain.GetHeader(block.ParentHash(), block.NumberU64()-1)
	if parent == nil {
		return nil, fmt.Errorf("missing parent %#x", block.ParentHash())
	}

	// BEP-703 payment-lane, fastnode skip the payment-lane check.
	lane := &LaneState{}
	if !statedb.NoTries() {
		if lane, err = ResolveLaneState(config, p.chain.Engine(), parent, header, statedb); err != nil {
			return nil, laneReject(err)
		}
	}

	recordSystemBAL := config.IsInBSC() && config.IsAmsterdam(blockNumber, block.Time())

	var (
		context         = NewEVMBlockContext(header, p.chain, nil)
		signer          = types.MakeSigner(config, header.Number, header.Time)
		evm             = vm.NewEVM(context, tracingStateDB, config, cfg)
		blockAccessList = bal.NewConstructionBlockAccessList()
		txNum           = len(block.Transactions())
	)
	defer evm.Release()

	if jumpDestCache != nil {
		evm.SetJumpDestCache(jumpDestCache)
	}
	if precompileCache != nil {
		evm.SetPrecompileCache(precompileCache)
	}
	// Run the pre-execution system calls
	blockAccessList.Merge(PreExecution(ctx, block.BeaconRoot(), parent, config, evm, block.Number(), block.Time()))

	// Iterate over and process the individual transactions
	posa, isPoSA := p.chain.Engine().(consensus.PoSA)
	commonTxs := make([]*types.Transaction, 0, txNum)

	// usually do have two tx, one for validator set contract, another for system reward contract.
	systemTxs := make([]*types.Transaction, 0, 2)

	for i, tx := range block.Transactions() {
		// Publish the progress, letting the prefetcher skip caught up work.
		if execIndex != nil {
			execIndex.Store(int64(i))
		}
		if isPoSA {
			if isSystemTx, err := posa.IsSystemTransaction(tx, block.Header()); err != nil {
				return nil, err
			} else if isSystemTx {
				systemTxs = append(systemTxs, tx)
				continue
			}
		}
		if p.chain.Config().IsCancun(block.Number(), block.Time()) {
			if len(systemTxs) > 0 {
				// systemTxs should be always at the end of block.
				return nil, fmt.Errorf("normal tx %d [%v] after systemTx", i, tx.Hash().Hex())
			}
		}

		msg, err := TransactionToMessage(tx, signer, header.BaseFee)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		laneType := lane.Classify(tx)
		usedBefore := gp.Used()
		statedb.SetTxContext(tx.Hash(), i, uint32(i+1))
		receipt, bal, err := applyTransactionWithEVM(ctx, msg, gp, statedb, blockNumber, blockHash, context.Time, tx, evm, eagerBloom)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		lane.RecordUsedFrom(laneType, gp, usedBefore)
		commonTxs = append(commonTxs, tx)
		receipts = append(receipts, receipt)
		pipeline.feedReceipt(receipt)
		allLogs = append(allLogs, receipt.Logs...)
		blockAccessList.Merge(bal)
	}
	numUserReceipts := len(receipts)
	var requests [][]byte
	if config.IsNotInBSC() {
		var postBAL *bal.ConstructionBlockAccessList
		requests, postBAL, err = PostExecution(ctx, config, block.Number(), block.Time(), allLogs, block.Withdrawals(), evm, uint32(len(block.Transactions())+1))
		if err != nil {
			return nil, err
		}
		blockAccessList.Merge(postBAL)
	}

	gasUsed := gp.Used()
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	body := &types.Body{Transactions: commonTxs, Uncles: block.Uncles(), Withdrawals: block.Withdrawals()}
	finalizeState := tracingStateDB
	if recordSystemBAL {
		finalizeState = &systemTxStateDB{StateDB: tracingStateDB, accessList: blockAccessList}
	}
	err = p.chain.Engine().Finalize(p.chain, header, finalizeState, body, &receipts, &systemTxs, &gasUsed, cfg.Tracer)
	if err != nil {
		return nil, err
	}
	if len(receipts) != len(block.Transactions()) {
		return nil, fmt.Errorf("receipt count mismatch: have %d, want %d", len(receipts), len(block.Transactions()))
	}
	// Digest the system-tx receipts and collect their logs after Finalize.
	for _, receipt := range receipts[numUserReceipts:] {
		pipeline.feedReceipt(receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}
	if config.IsInBSC() {
		var postBAL *bal.ConstructionBlockAccessList
		requests, postBAL, err = PostExecution(ctx, config, block.Number(), block.Time(), allLogs, block.Withdrawals(), evm, uint32(len(block.Transactions())+1))
		if err != nil {
			return nil, err
		}
		blockAccessList.Merge(postBAL)
	}

	if err := lane.Verify(gasUsed); err != nil {
		return nil, laneReject(err)
	}
	lane.recordImported()

	// The access list is final, let the pipeline encode it while the block is
	// validated.
	pipeline.feedBAL(blockAccessList)

	// Join the receipts. They are only complete, and safe to hand back, once
	// the pipeline has filled in their blooms.
	pipeline.joinReceipts()

	return &ProcessResult{
		Receipts: receipts,
		Requests: requests,
		Logs:     allLogs,
		// gp.Used() plus the system-tx gas added by Finalize: the block-level
		// chain (EIP-7778) validated against header.GasUsed.
		GasUsed:  gasUsed,
		pipeline: pipeline,
	}, nil
}

// PreExecution processes pre-execution state changes and system calls.
func PreExecution(ctx context.Context, beaconRoot *common.Hash, parent *types.Header, config *params.ChainConfig, evm *vm.EVM, number *big.Int, time uint64) *bal.ConstructionBlockAccessList {
	_, _, spanEnd := telemetry.StartSpan(ctx, "core.preExecution")
	defer spanEnd(nil)

	var blockAccessList *bal.ConstructionBlockAccessList
	if config.IsAmsterdam(number, time) {
		blockAccessList = bal.NewConstructionBlockAccessList()
	}
	systemcontracts.TryUpdateBuildInSystemContract(config, number, parent.Time, time, evm.StateDB, true)
	if config.IsInBSC() && config.IsAmsterdam(number, time) {
		blockAccessList.Merge(finaliseProtocolChanges(evm.StateDB, evm.GetRules(), 0))
	}
	// EIP-4788
	if beaconRoot != nil {
		ProcessBeaconBlockRoot(*beaconRoot, evm, blockAccessList)
	}
	// EIP-2935
	if config.IsPrague(number, time) || config.IsUBT(number, time) {
		ProcessParentBlockHash(parent.Hash(), evm, blockAccessList)
	}
	return blockAccessList
}

// PostExecution processes the withdrawals and then the post-execution system
// calls when Prague is enabled. If Prague is not activated, it returns null
// requests to differentiate from empty requests.
// On BSC, it collects remaining protocol changes after all system transactions.
func PostExecution(ctx context.Context, config *params.ChainConfig, number *big.Int, time uint64, allLogs []*types.Log, withdrawals types.Withdrawals, evm *vm.EVM, blockAccessIndex uint32) (requests [][]byte, blockAccessList *bal.ConstructionBlockAccessList, err error) {
	_, _, spanEnd := telemetry.StartSpan(ctx, "core.postExecution")
	defer spanEnd(&err)

	if config.IsAmsterdam(number, time) {
		blockAccessList = bal.NewConstructionBlockAccessList()
	}
	if config.IsInBSC() {
		if config.IsAmsterdam(number, time) {
			blockAccessList.Merge(finaliseProtocolChanges(evm.StateDB, evm.GetRules(), blockAccessIndex))
		}
		return nil, blockAccessList, nil
	}
	rules := config.Rules(number, true, time) // IsMerge is always true

	// Withdrawals are applied before the system calls
	if rules.IsShanghai && config.IsNotInBSC() {
		ProcessWithdrawals(withdrawals, evm, blockAccessIndex, blockAccessList)
	}
	// Read requests if Prague is enabled.
	if rules.IsPrague && config.IsNotInBSC() {
		requests = [][]byte{}
		// EIP-6110
		if err := ParseDepositLogs(&requests, allLogs, config); err != nil {
			return nil, nil, fmt.Errorf("failed to parse deposit logs: %w", err)
		}
		// EIP-7002
		if err := ProcessWithdrawalQueue(&requests, rules, evm, blockAccessIndex, blockAccessList); err != nil {
			return nil, nil, fmt.Errorf("failed to process withdrawal queue: %w", err)
		}
		// EIP-7251
		if err := ProcessConsolidationQueue(&requests, rules, evm, blockAccessIndex, blockAccessList); err != nil {
			return nil, nil, fmt.Errorf("failed to process consolidation queue: %w", err)
		}
	}

	if rules.IsAmsterdam && config.IsNotInBSC() {
		// EIP-8282
		if err := ProcessBuilderDepositQueue(&requests, rules, evm, blockAccessIndex, blockAccessList); err != nil {
			return nil, nil, fmt.Errorf("failed to process builder deposit queue: %w", err)
		}
		if err := ProcessBuilderExitQueue(&requests, rules, evm, blockAccessIndex, blockAccessList); err != nil {
			return nil, nil, fmt.Errorf("failed to process builder exit queue: %w", err)
		}
	}
	return requests, blockAccessList, nil
}

// ProcessWithdrawals credits the given withdrawals to their recipients.
func ProcessWithdrawals(withdrawals types.Withdrawals, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) {
	rules := evm.GetRules()
	evm.StateDB.Prepare(rules, common.Address{}, common.Address{}, nil, nil, nil)
	evm.StateDB.SetTxContext(common.Hash{}, 0, blockAccessIndex)
	for _, w := range withdrawals {
		// Convert amount from gwei to wei.
		amount := new(uint256.Int).SetUint64(w.Amount)
		amount = amount.Mul(amount, uint256.NewInt(params.GWei))
		evm.StateDB.AddBalance(w.Address, amount, tracing.BalanceIncreaseWithdrawal)

		if rules.IsEIP4762 {
			evm.StateDB.AccessEvents().AddAccount(w.Address, true, math.MaxUint64)
		}
	}
	accessList := evm.StateDB.Finalise(rules)
	if rules.IsAmsterdam {
		blockAccessList.Merge(accessList)
	}
}

// ApplyTransactionWithEVM attempts to apply a transaction to the given state database
// and uses the input parameters for its environment similar to ApplyTransaction. However,
// this method takes an already created EVM instance as input.
func ApplyTransactionWithEVM(ctx context.Context, msg *Message, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, evm *vm.EVM) (*types.Receipt, *bal.ConstructionBlockAccessList, error) {
	return applyTransactionWithEVM(ctx, msg, gp, statedb, blockNumber, blockHash, blockTime, tx, evm, true)
}

// applyTransactionWithEVM is ApplyTransactionWithEVM with the receipt bloom
// filter optional. The block processor leaves it out and lets its receipt
// pipeline hash the logs instead.
func applyTransactionWithEVM(ctx context.Context, msg *Message, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, evm *vm.EVM, withBloom bool) (receipt *types.Receipt, bal *bal.ConstructionBlockAccessList, err error) {
	_, _, spanEnd := telemetry.StartSpan(ctx, "core.ApplyTransactionWithEVM",
		telemetry.StringAttribute("tx.hash", tx.Hash().Hex()),
		telemetry.IntAttribute("tx.index", statedb.TxIndex()),
	)
	defer spanEnd(&err)

	// Add timing measurement
	var result *ExecutionResult
	if tx.Gas() > largeTxGasLimit {
		start := time.Now()
		defer func() {
			if result != nil && result.UsedGas > largeTxGasLimit {
				elapsed := time.Since(start)
				log.Info("LargeTX execution time", "block", blockNumber, "tx", tx.Hash(), "gasUsed", result.UsedGas, "elapsed", elapsed)
			}
		}()
	}

	if hooks := evm.Config.Tracer; hooks != nil {
		if hooks.OnTxStart != nil {
			hooks.OnTxStart(evm.GetVMContext(), tx, msg.From)
		}
		if hooks.OnTxEnd != nil {
			defer func() { hooks.OnTxEnd(receipt, err) }()
		}
	}
	// Apply the transaction to the current state (included in the env).
	result, err = ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, nil, err
	}
	// Update the state with pending changes.
	var root []byte
	if evm.ChainConfig().IsByzantium(blockNumber) {
		bal = evm.StateDB.Finalise(evm.GetRules())
	} else {
		root = statedb.IntermediateRoot(evm.GetRules()).Bytes()
	}
	// Merge the tx-local access event into the "block-local" one, in order to collect
	// all values, so that the witness can be built.
	if statedb.Database().Type().Is(state.TypeUBT) {
		statedb.AccessEvents().Merge(evm.AccessEvents)
	}
	receipt = makeReceipt(evm, result, statedb, blockNumber, blockHash, blockTime, tx, gp.CumulativeUsed(), root)
	if withBloom {
		receipt.Bloom = types.CreateBloom(receipt)
	}
	return receipt, bal, nil
}

// MakeReceipt generates the receipt object for a transaction given its execution result.
func MakeReceipt(evm *vm.EVM, result *ExecutionResult, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, cumulativeGas uint64, root []byte, receiptProcessors ...ReceiptProcessor) *types.Receipt {
	receipt := makeReceipt(evm, result, statedb, blockNumber, blockHash, blockTime, tx, cumulativeGas, root)
	receipt.Bloom = types.CreateBloom(receipt)
	for _, processor := range receiptProcessors {
		processor.Apply(receipt)
	}
	return receipt
}

// makeReceipt generates the receipt object without its bloom filter, which the
// caller either computes itself or leaves to the receipt pipeline.
func makeReceipt(evm *vm.EVM, result *ExecutionResult, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, cumulativeGas uint64, root []byte) *types.Receipt {
	// Create a new receipt for the transaction, storing the intermediate root
	// and gas used by the tx.
	//
	// The cumulative gas used equals the sum of gasUsed across all preceding
	// txs with refunded gas deducted.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: cumulativeGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()

	// GasUsed = max(tx_gas_used - gas_refund, calldata_floor_gas_cost), unchanged
	// in the Amsterdam fork.
	receipt.GasUsed = result.UsedGas

	if tx.Type() == types.BlobTxType {
		receipt.BlobGasUsed = uint64(len(tx.BlobHashes()) * params.BlobTxBlobGasPerBlob)
		receipt.BlobGasPrice = evm.Context.BlobBaseFee
	}

	// If the transaction created a contract, store the creation address in the receipt.
	if tx.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce())
	}

	// Set the receipt logs.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockNumber.Uint64(), blockHash, blockTime)
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	return receipt
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(ctx context.Context, evm *vm.EVM, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction) (*types.Receipt, *bal.ConstructionBlockAccessList, error) {
	msg, err := TransactionToMessage(tx, types.MakeSigner(evm.ChainConfig(), header.Number, header.Time), header.BaseFee)
	if err != nil {
		return nil, nil, err
	}
	// Create a new context to be used in the EVM environment
	return ApplyTransactionWithEVM(ctx, msg, gp, statedb, header.Number, header.Hash(), header.Time, tx, evm)
}

// systemCallGasBudget returns the gas budget for system calls.
func systemCallGasBudget(evm *vm.EVM) (gasLimit uint64, gasBudget vm.GasBudget) {
	if !evm.GetRules().IsAmsterdam {
		gasLimit = 30_000_000
		gasBudget = vm.NewGasBudget(gasLimit, 0)
	} else {
		// SYSTEM_MAX_SSTORES_PER_CALL = 16 is the upper bound on the number of
		// new storage slots a single system call is expected to write.
		stateBudget := params.SystemMaxSStoresPerCall * evm.Context.CostPerStateByte * params.StorageCreationSize
		gasLimit = 30_000_000
		gasBudget = vm.NewGasBudget(gasLimit, stateBudget)
	}
	return gasLimit, gasBudget
}

// ProcessBeaconBlockRoot applies the EIP-4788 system call to the beacon block root
// contract. This method is exported to be used in tests.
func ProcessBeaconBlockRoot(beaconRoot common.Hash, evm *vm.EVM, blockAccessList *bal.ConstructionBlockAccessList) {
	// Return immediately if beaconRoot equals the zero hash when using the Parlia engine.
	if beaconRoot == (common.Hash{}) {
		if chainConfig := evm.ChainConfig(); chainConfig != nil && chainConfig.IsInBSC() {
			return
		}
	}
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	gasLimit, gasBudget := systemCallGasBudget(evm)
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  gasLimit,
		GasPrice:  uint256.NewInt(0),
		GasFeeCap: uint256.NewInt(0),
		GasTipCap: uint256.NewInt(0),
		To:        &params.BeaconRootsAddress,
		Data:      beaconRoot[:],
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.Prepare(evm.GetRules(), common.Address{}, common.Address{}, nil, nil, nil)
	evm.StateDB.SetTxContext(common.Hash{}, 0, 0)
	evm.StateDB.AddAddressToAccessList(params.BeaconRootsAddress)

	// Unchecked system call (EIP-4788), failures are ignored as spec
	_, _, _ = evm.Call(msg.From, *msg.To, msg.Data, gasBudget, common.U2560)
	if evm.StateDB.AccessEvents() != nil {
		evm.StateDB.AccessEvents().Merge(evm.AccessEvents)
	}
	accessList := evm.StateDB.Finalise(evm.GetRules())
	if evm.GetRules().IsAmsterdam {
		blockAccessList.Merge(accessList)
	}
}

// ProcessParentBlockHash stores the parent block hash in the history storage contract
// as per EIP-2935/7709.
func ProcessParentBlockHash(prevHash common.Hash, evm *vm.EVM, blockAccessList *bal.ConstructionBlockAccessList) {
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	gasLimit, gasBudget := systemCallGasBudget(evm)
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  gasLimit,
		GasPrice:  uint256.NewInt(0),
		GasFeeCap: uint256.NewInt(0),
		GasTipCap: uint256.NewInt(0),
		To:        &params.HistoryStorageAddress,
		Data:      prevHash.Bytes(),
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.Prepare(evm.GetRules(), common.Address{}, common.Address{}, nil, nil, nil)
	evm.StateDB.SetTxContext(common.Hash{}, 0, 0)
	evm.StateDB.AddAddressToAccessList(params.HistoryStorageAddress)

	// Unchecked system call (EIP-2935), failures are ignored as spec
	_, _, _ = evm.Call(msg.From, *msg.To, msg.Data, gasBudget, common.U2560)
	if evm.StateDB.AccessEvents() != nil {
		evm.StateDB.AccessEvents().Merge(evm.AccessEvents)
	}
	accessList := evm.StateDB.Finalise(evm.GetRules())
	if evm.GetRules().IsAmsterdam {
		blockAccessList.Merge(accessList)
	}
}

// ProcessWithdrawalQueue calls the EIP-7002 withdrawal queue contract.
// It returns the opaque request data returned by the contract.
func ProcessWithdrawalQueue(requests *[][]byte, rules params.Rules, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	return processRequestsSystemCall(requests, rules, evm, 0x01, params.WithdrawalQueueAddress, blockAccessIndex, blockAccessList)
}

// ProcessConsolidationQueue calls the EIP-7251 consolidation queue contract.
// It returns the opaque request data returned by the contract.
func ProcessConsolidationQueue(requests *[][]byte, rules params.Rules, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	return processRequestsSystemCall(requests, rules, evm, 0x02, params.ConsolidationQueueAddress, blockAccessIndex, blockAccessList)
}

// ProcessBuilderDepositQueue calls the EIP-8282 builder deposit contract.
// It returns the opaque request data returned by the contract.
func ProcessBuilderDepositQueue(requests *[][]byte, rules params.Rules, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	return processRequestsSystemCall(requests, rules, evm, 0x03, params.BuilderDepositAddress, blockAccessIndex, blockAccessList)
}

// ProcessBuilderExitQueue calls the EIP-8282 builder exit contract.
// It returns the opaque request data returned by the contract.
func ProcessBuilderExitQueue(requests *[][]byte, rules params.Rules, evm *vm.EVM, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	return processRequestsSystemCall(requests, rules, evm, 0x04, params.BuilderExitAddress, blockAccessIndex, blockAccessList)
}

func processRequestsSystemCall(requests *[][]byte, rules params.Rules, evm *vm.EVM, requestType byte, addr common.Address, blockAccessIndex uint32, blockAccessList *bal.ConstructionBlockAccessList) error {
	if evm.StateDB.GetCodeSize(addr) == 0 {
		return fmt.Errorf("empty system contract: no code at %v", addr)
	}
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	gasLimit, gasBudget := systemCallGasBudget(evm)
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  gasLimit,
		GasPrice:  uint256.NewInt(0),
		GasFeeCap: uint256.NewInt(0),
		GasTipCap: uint256.NewInt(0),
		To:        &addr,
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.Prepare(evm.GetRules(), common.Address{}, common.Address{}, nil, nil, nil)
	evm.StateDB.SetTxContext(common.Hash{}, 0, blockAccessIndex)
	evm.StateDB.AddAddressToAccessList(addr)
	ret, _, err := evm.Call(msg.From, *msg.To, msg.Data, gasBudget, common.U2560)
	if evm.StateDB.AccessEvents() != nil {
		evm.StateDB.AccessEvents().Merge(evm.AccessEvents)
	}
	bal := evm.StateDB.Finalise(evm.GetRules())
	if err != nil {
		return fmt.Errorf("system call failed to execute: %v", err)
	}
	if rules.IsAmsterdam {
		blockAccessList.Merge(bal)
	}
	if len(ret) == 0 {
		return nil // skip empty output
	}
	// Append prefixed requestsData to the requests list.
	requestsData := make([]byte, len(ret)+1)
	requestsData[0] = requestType
	copy(requestsData[1:], ret)
	*requests = append(*requests, requestsData)
	return nil
}

var depositTopic = common.HexToHash("0x649bbc62d0e31342afea4e5cd82d4049e7e1ee912fc0889aa790803be39038c5")

// ParseDepositLogs extracts the EIP-6110 deposit values from logs emitted by
// BeaconDepositContract.
func ParseDepositLogs(requests *[][]byte, logs []*types.Log, config *params.ChainConfig) error {
	deposits := make([]byte, 1) // note: first byte is 0x00 (== deposit request type)
	for _, log := range logs {
		if log.Address == config.DepositContractAddress && len(log.Topics) > 0 && log.Topics[0] == depositTopic {
			request, err := types.DepositLogToRequest(log.Data)
			if err != nil {
				return fmt.Errorf("unable to parse deposit data: %v", err)
			}
			deposits = append(deposits, request...)
		}
	}
	if len(deposits) > 1 {
		*requests = append(*requests, deposits)
	}
	return nil
}

func onSystemCallStart(tracer *tracing.Hooks, ctx *tracing.VMContext) {
	if tracer.OnSystemCallStartV2 != nil {
		tracer.OnSystemCallStartV2(ctx)
	} else if tracer.OnSystemCallStart != nil {
		tracer.OnSystemCallStart()
	}
}

// blockAssembler is implemented by consensus engines that support FinalizeAndAssemble.
type blockAssembler interface {
	FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, body *types.Body, receipts []*types.Receipt, tracer *tracing.Hooks) (*types.Block, []*types.Receipt, error)
}

// AssembleBlock finalizes the state and assembles the block with provided
// body and receipts.
func AssembleBlock(engine consensus.Engine, chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, body *types.Body, receipts []*types.Receipt, blockAccessLists ...*bal.ConstructionBlockAccessList) (*types.Block, []*types.Receipt, error) {
	if p, ok := engine.(blockAssembler); ok {
		// TODO: Include Parlia finalization in BAL construction and attach the
		// completed access list here before enabling Amsterdam on BSC.
		block, receipts, err := p.FinalizeAndAssemble(chain, header, state, body, receipts, nil)
		if err != nil {
			return nil, nil, err
		}
		return block, receipts, nil
	}
	if err := engine.Finalize(chain, header, state, body, &receipts, nil, &header.GasUsed, nil); err != nil {
		return nil, nil, err
	}
	header.Root = state.IntermediateRoot(chain.Config().Rules(header.Number, header.Difficulty.Sign() == 0, header.Time))
	if chain.Config().IsAmsterdam(header.Number, header.Time) {
		if len(blockAccessLists) != 1 || blockAccessLists[0] == nil {
			return nil, nil, errors.New("missing block access list for Amsterdam assembly")
		}
		list := blockAccessLists[0].ToEncodingObj()
		hash := list.Hash()
		header.BlockAccessListHash = &hash
		return types.NewBlock(header, body, receipts, trie.NewStackTrie(nil)).WithAccessListUnsafe(list), receipts, nil
	}
	return types.NewBlock(header, body, receipts, trie.NewStackTrie(nil)), receipts, nil
}
