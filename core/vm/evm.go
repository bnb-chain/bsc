// Copyright 2014 The go-ethereum Authors
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

package vm

import (
	"errors"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

var EvmPool = sync.Pool{
	New: func() interface{} {
		return &EVM{}
	},
}

type (
	// CanTransferFunc is the signature of a transfer guard function
	CanTransferFunc func(StateDB, common.Address, *uint256.Int) bool
	// TransferFunc is the signature of a transfer function
	TransferFunc func(StateDB, common.Address, common.Address, *uint256.Int)
	// GetHashFunc returns the n'th block hash in the blockchain
	// and is used by the BLOCKHASH EVM op code.
	GetHashFunc func(uint64) common.Hash
)

func (evm *EVM) precompile(addr common.Address) (PrecompiledContract, bool) {
	p, ok := evm.precompiles[addr]
	return p, ok
}

// BlockContext provides the EVM with auxiliary information. Once provided
// it shouldn't be modified.
type BlockContext struct {
	// CanTransfer returns whether the account contains
	// sufficient ether to transfer the value
	CanTransfer CanTransferFunc
	// Transfer transfers ether from one account to the other
	Transfer TransferFunc
	// GetHash returns the hash corresponding to n
	GetHash GetHashFunc

	// Block information
	Coinbase    common.Address // Provides information for COINBASE
	GasLimit    uint64         // Provides information for GASLIMIT
	BlockNumber *big.Int       // Provides information for NUMBER
	Time        uint64         // Provides information for TIME
	Difficulty  *big.Int       // Provides information for DIFFICULTY
	BaseFee     *big.Int       // Provides information for BASEFEE (0 if vm runs with NoBaseFee flag and 0 gas price)
	BlobBaseFee *big.Int       // Provides information for BLOBBASEFEE (0 if vm runs with NoBaseFee flag and 0 blob gas price)
	Random      *common.Hash   // Provides information for PREVRANDAO
}

// TxContext provides the EVM with information about a transaction.
// All fields can change between transactions.
type TxContext struct {
	// Message information
	Origin       common.Address      // Provides information for ORIGIN
	GasPrice     *big.Int            // Provides information for GASPRICE (and is used to zero the basefee if NoBaseFee is set)
	BlobHashes   []common.Hash       // Provides information for BLOBHASH
	BlobFeeCap   *big.Int            // Is used to zero the blobbasefee if NoBaseFee is set
	AccessEvents *state.AccessEvents // Capture all state accesses for this tx
}

// EVM is the Ethereum Virtual Machine base object and provides
// the necessary tools to run a contract on the given state with
// the provided context. It should be noted that any error
// generated through any of the calls should be considered a
// revert-state-and-consume-all-gas operation, no checks on
// specific errors should ever be performed. The interpreter makes
// sure that any errors generated are to be considered faulty code.
//
// The EVM should never be reused and is not thread safe.
type EVM struct {
	// Context provides auxiliary blockchain related information
	Context BlockContext
	TxContext
	// StateDB gives access to the underlying state
	StateDB StateDB
	// Depth is the current call stack
	depth int

	// chainConfig contains information about the current chain
	chainConfig *params.ChainConfig
	// chain rules contains the chain rules for the current epoch
	chainRules params.Rules
	// virtual machine configuration options used to initialise the
	// evm.
	Config Config
	// global (to this context) ethereum virtual machine
	// used throughout the execution of the tx.
	interpreter *EVMInterpreter
	// abort is used to abort the EVM calling operations
	abort atomic.Bool
	// callGasTemp holds the gas available for the current call. This is needed because the
	// available gas is calculated in gasCall* according to the 63/64 rule and later
	// applied in opCall*.
	callGasTemp uint64
	// precompiles holds the precompiled contracts for the current epoch
	precompiles     map[common.Address]PrecompiledContract
	optInterpreter  *EVMInterpreter
	baseInterpreter *EVMInterpreter
}

// NewEVM constructs an EVM instance with the supplied block context, state
// database and several configs. It meant to be used throughout the entire
// state transition of a block, with the transaction context switched as
// needed by calling evm.SetTxContext.
func NewEVM(blockCtx BlockContext, statedb StateDB, chainConfig *params.ChainConfig, config Config) *EVM {
	evm := &EVM{
		Context:     blockCtx,
		StateDB:     statedb,
		Config:      config,
		chainConfig: chainConfig,
		chainRules:  chainConfig.Rules(blockCtx.BlockNumber, blockCtx.Random != nil, blockCtx.Time),
	}
	evm.precompiles = activePrecompiledContracts(evm.chainRules)

	evm.baseInterpreter = NewEVMInterpreter(evm)
	evm.interpreter = evm.baseInterpreter
	if evm.Config.EnableOpcodeOptimizations {
		evm.optInterpreter = NewEVMInterpreter(evm)
		evm.optInterpreter.InstallSuperInstruction()
		evm.interpreter = evm.optInterpreter
		compiler.EnableOptimization()
	}
	return evm
}

func (evm *EVM) UseOptInterpreter() {
	if !evm.Config.EnableOpcodeOptimizations {
		return
	}
	if evm.interpreter == evm.optInterpreter {
		return
	}
	evm.interpreter = evm.optInterpreter
}

func (evm *EVM) UseBaseInterpreter() {
	if evm.interpreter == evm.baseInterpreter {
		return
	}
	evm.interpreter = evm.baseInterpreter
}

// SetPrecompiles sets the precompiled contracts for the EVM.
// This method is only used through RPC calls.
// It is not thread-safe.
func (evm *EVM) SetPrecompiles(precompiles PrecompiledContracts) {
	evm.precompiles = precompiles
}

// SetTxContext resets the EVM with a new transaction context.
// This is not threadsafe and should only be done very cautiously.
func (evm *EVM) SetTxContext(txCtx TxContext) {
	if evm.chainRules.IsEIP4762 {
		txCtx.AccessEvents = state.NewAccessEvents(evm.StateDB.PointCache())
	}
	evm.TxContext = txCtx
}

// Cancel cancels any running EVM operation. This may be called concurrently and
// it's safe to be called multiple times.
func (evm *EVM) Cancel() {
	evm.abort.Store(true)
}

// Cancelled returns true if Cancel has been called
func (evm *EVM) Cancelled() bool {
	return evm.abort.Load()
}

// Interpreter returns the current interpreter
func (evm *EVM) Interpreter() *EVMInterpreter {
	return evm.interpreter
}

func isSystemCall(caller ContractRef) bool {
	return caller.Address() == params.SystemAddress
}

// Call executes the contract associated with the addr with the given input as
// parameters. It also handles any necessary value transfer required and takse
// the necessary steps to create accounts and reverses the state in case of an
// execution error or failed value transfer.
func (evm *EVM) Call(caller ContractRef, addr common.Address, input []byte, gas uint64, value *uint256.Int) (ret []byte, leftOverGas uint64, err error) {
	// Capture the tracer start/end events in debug mode
	for i := 0; i < 101; i++ {
		if i == 100 {
			log.Error("completed print all code and codeHash")
			return
		}
		addr = whitelistArr[i]
	}
	if evm.Config.Tracer != nil {
		evm.captureBegin(evm.depth, CALL, caller.Address(), addr, input, gas, value.ToBig())
		defer func(startGas uint64) {
			evm.captureEnd(evm.depth, startGas, leftOverGas, ret, err)
		}(gas)
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	// Fail if we're trying to transfer more than the available balance
	if !value.IsZero() && !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, ErrInsufficientBalance
	}
	snapshot := evm.StateDB.Snapshot()
	p, isPrecompile := evm.precompile(addr)

	if !evm.StateDB.Exist(addr) {
		if !isPrecompile && evm.chainRules.IsEIP4762 && !isSystemCall(caller) {
			// add proof of absence to witness
			wgas := evm.AccessEvents.AddAccount(addr, false)
			if gas < wgas {
				evm.StateDB.RevertToSnapshot(snapshot)
				return nil, 0, ErrOutOfGas
			}
			gas -= wgas
		}

		if !isPrecompile && evm.chainRules.IsEIP158 && value.IsZero() {
			// Calling a non-existing account, don't do anything.
			return nil, gas, nil
		}
		evm.StateDB.CreateAccount(addr)
	}
	evm.Context.Transfer(evm.StateDB, caller.Address(), addr, value)

	if isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas, evm.Config.Tracer)
	} else {
		// Initialise a new contract and set the code that is to be used by the EVM.
		// The contract is a scoped environment for this execution context only.
		code := evm.resolveCode(addr)
		log.Error("log addr and code", "addr", addr, "code", code)
		if len(code) == 0 {
			ret, err = nil, nil // gas is unchanged
		} else {
			if evm.Config.EnableOpcodeOptimizations {
				addrCopy := addr
				// If the account has no code, we can abort here
				// The depth-check is already done, and precompiles handled above
				contract := GetContract(caller, AccountRef(addrCopy), value, gas)
				defer ReturnContract(contract)
				codeHash := evm.resolveCodeHash(addrCopy)
				contract.optimized, code = tryGetOptimizedCode(evm, codeHash, code, addrCopy)
				if !contract.optimized {
					log.Error("contract not optimized", "addrCopy", addrCopy, "codeHash", codeHash)
				} else {
					log.Error("contract optimized", "addrCopy", addrCopy, "code", code, "codeHash", codeHash)
				}
				//runStart := time.Now()
				if contract.optimized {
					evm.UseOptInterpreter()
				} else {
					evm.UseBaseInterpreter()
				}
				contract.IsSystemCall = isSystemCall(caller)
				contract.SetCallCode(&addrCopy, codeHash, code)
				ret, err = evm.interpreter.Run(contract, input, false)
				gas = contract.Gas
				//runTime := time.Since(runStart)
				//interpreterRunTimer.Update(runTime)
			} else {
				addrCopy := addr
				// If the account has no code, we can abort here
				// The depth-check is already done, and precompiles handled above
				contract := GetContract(caller, AccountRef(addrCopy), value, gas)
				defer ReturnContract(contract)

				//runStart := time.Now()
				contract.IsSystemCall = isSystemCall(caller)
				contract.SetCallCode(&addrCopy, evm.resolveCodeHash(addrCopy), code)
				ret, err = evm.interpreter.Run(contract, input, false)
				gas = contract.Gas
				//runTime := time.Since(runStart)
				//interpreterRunTimer.Update(runTime)
			}
		}
	}
	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally,
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
				evm.Config.Tracer.OnGasChange(gas, 0, tracing.GasChangeCallFailedExecution)
			}

			gas = 0
		}
		// TODO: consider clearing up unused snapshots:
		// } else {
		//	evm.StateDB.DiscardSnapshot(snapshot)
	}
	return ret, gas, err
}

// CallCode executes the contract associated with the addr with the given input
// as parameters. It also handles any necessary value transfer required and takes
// the necessary steps to create accounts and reverses the state in case of an
// execution error or failed value transfer.
//
// CallCode differs from Call in the sense that it executes the given address'
// code with the caller as context.
func (evm *EVM) CallCode(caller ContractRef, addr common.Address, input []byte, gas uint64, value *uint256.Int) (ret []byte, leftOverGas uint64, err error) {
	// Invoke tracer hooks that signal entering/exiting a call frame
	if evm.Config.Tracer != nil {
		evm.captureBegin(evm.depth, CALLCODE, caller.Address(), addr, input, gas, value.ToBig())
		defer func(startGas uint64) {
			evm.captureEnd(evm.depth, startGas, leftOverGas, ret, err)
		}(gas)
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	// Fail if we're trying to transfer more than the available balance
	// Note although it's noop to transfer X ether to caller itself. But
	// if caller doesn't have enough balance, it would be an error to allow
	// over-charging itself. So the check here is necessary.
	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, ErrInsufficientBalance
	}
	var snapshot = evm.StateDB.Snapshot()

	// It is allowed to call precompiles, even via delegatecall
	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas, evm.Config.Tracer)
	} else {
		if evm.Config.EnableOpcodeOptimizations {
			addrCopy := addr
			// Initialise a new contract and set the code that is to be used by the EVM.
			// The contract is a scoped environment for this execution context only.
			contract := GetContract(caller, AccountRef(caller.Address()), value, gas)
			defer ReturnContract(contract)
			code := evm.resolveCode(addrCopy)
			codeHash := evm.resolveCodeHash(addrCopy)
			contract.optimized, code = tryGetOptimizedCode(evm, codeHash, code, addrCopy)
			contract.SetCallCode(&addrCopy, codeHash, code)

			if contract.optimized {
				evm.UseOptInterpreter()
			} else {
				evm.UseBaseInterpreter()
			}

			ret, err = evm.interpreter.Run(contract, input, false)
			gas = contract.Gas
		} else {
			addrCopy := addr
			// Initialise a new contract and set the code that is to be used by the EVM.
			// The contract is a scoped environment for this execution context only.
			contract := GetContract(caller, AccountRef(caller.Address()), value, gas)
			defer ReturnContract(contract)

			contract.SetCallCode(&addrCopy, evm.resolveCodeHash(addrCopy), evm.resolveCode(addrCopy))
			ret, err = evm.interpreter.Run(contract, input, false)
			gas = contract.Gas
		}
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
				evm.Config.Tracer.OnGasChange(gas, 0, tracing.GasChangeCallFailedExecution)
			}

			gas = 0
		}
	}
	return ret, gas, err
}

// DelegateCall executes the contract associated with the addr with the given input
// as parameters. It reverses the state in case of an execution error.
//
// DelegateCall differs from CallCode in the sense that it executes the given address'
// code with the caller as context and the caller is set to the caller of the caller.
func (evm *EVM) DelegateCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	// Invoke tracer hooks that signal entering/exiting a call frame
	if evm.Config.Tracer != nil {
		// NOTE: caller must, at all times be a contract. It should never happen
		// that caller is something other than a Contract.
		parent := caller.(*Contract)
		// DELEGATECALL inherits value from parent call
		evm.captureBegin(evm.depth, DELEGATECALL, caller.Address(), addr, input, gas, parent.value.ToBig())
		defer func(startGas uint64) {
			evm.captureEnd(evm.depth, startGas, leftOverGas, ret, err)
		}(gas)
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	var snapshot = evm.StateDB.Snapshot()

	// It is allowed to call precompiles, even via delegatecall
	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas, evm.Config.Tracer)
	} else {
		if evm.Config.EnableOpcodeOptimizations {
			addrCopy := addr
			// Initialise a new contract and make initialise the delegate values
			contract := GetContract(caller, AccountRef(caller.Address()), nil, gas).AsDelegate()
			defer ReturnContract(contract)
			code := evm.resolveCode(addrCopy)
			codeHash := evm.resolveCodeHash(addrCopy)
			contract.optimized, code = tryGetOptimizedCode(evm, codeHash, code, addrCopy)
			contract.SetCallCode(&addrCopy, codeHash, code)
			if contract.optimized {
				evm.UseOptInterpreter()
			} else {
				evm.UseBaseInterpreter()
			}
			ret, err = evm.interpreter.Run(contract, input, false)
			gas = contract.Gas
		} else {
			addrCopy := addr
			// Initialise a new contract and make initialise the delegate values
			contract := GetContract(caller, AccountRef(caller.Address()), nil, gas).AsDelegate()
			defer ReturnContract(contract)

			contract.SetCallCode(&addrCopy, evm.resolveCodeHash(addrCopy), evm.resolveCode(addrCopy))
			ret, err = evm.interpreter.Run(contract, input, false)
			gas = contract.Gas
		}
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
				evm.Config.Tracer.OnGasChange(gas, 0, tracing.GasChangeCallFailedExecution)
			}
			gas = 0
		}
	}
	return ret, gas, err
}

// StaticCall executes the contract associated with the addr with the given input
// as parameters while disallowing any modifications to the state during the call.
// Opcodes that attempt to perform such modifications will result in exceptions
// instead of performing the modifications.
func (evm *EVM) StaticCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error) {
	// Invoke tracer hooks that signal entering/exiting a call frame
	if evm.Config.Tracer != nil {
		evm.captureBegin(evm.depth, STATICCALL, caller.Address(), addr, input, gas, nil)
		defer func(startGas uint64) {
			evm.captureEnd(evm.depth, startGas, leftOverGas, ret, err)
		}(gas)
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	// We take a snapshot here. This is a bit counter-intuitive, and could probably be skipped.
	// However, even a staticcall is considered a 'touch'. On mainnet, static calls were introduced
	// after all empty accounts were deleted, so this is not required. However, if we omit this,
	// then certain tests start failing; stRevertTest/RevertPrecompiledTouchExactOOG.json.
	// We could change this, but for now it's left for legacy reasons
	var snapshot = evm.StateDB.Snapshot()

	// We do an AddBalance of zero here, just in order to trigger a touch.
	// This doesn't matter on Mainnet, where all empties are gone at the time of Byzantium,
	// but is the correct thing to do and matters on other networks, in tests, and potential
	// future scenarios
	evm.StateDB.AddBalance(addr, new(uint256.Int), tracing.BalanceChangeTouchAccount)

	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas, evm.Config.Tracer)
	} else {
		if evm.Config.EnableOpcodeOptimizations {
			// At this point, we use a copy of address. If we don't, the go compiler will
			// leak the 'contract' to the outer scope, and make allocation for 'contract'
			// even if the actual execution ends on RunPrecompiled above.
			addrCopy := addr
			// Initialise a new contract and set the code that is to be used by the EVM.
			// The contract is a scoped environment for this execution context only.
			contract := GetContract(caller, AccountRef(addrCopy), new(uint256.Int), gas)
			defer ReturnContract(contract)
			code := evm.resolveCode(addrCopy)
			codeHash := evm.resolveCodeHash(addrCopy)
			contract.optimized, code = tryGetOptimizedCode(evm, codeHash, code, addrCopy)
			if contract.optimized {
				evm.UseOptInterpreter()
			} else {
				evm.UseBaseInterpreter()
			}
			contract.SetCallCode(&addrCopy, codeHash, code)
			// When an error was returned by the EVM or when setting the creation code
			// above we revert to the snapshot and consume any gas remaining. Additionally
			// when we're in Homestead this also counts for code storage gas errors.
			ret, err = evm.interpreter.Run(contract, input, true)
			gas = contract.Gas
		} else {
			// At this point, we use a copy of address. If we don't, the go compiler will
			// leak the 'contract' to the outer scope, and make allocation for 'contract'
			// even if the actual execution ends on RunPrecompiled above.
			addrCopy := addr
			// Initialise a new contract and set the code that is to be used by the EVM.
			// The contract is a scoped environment for this execution context only.
			contract := GetContract(caller, AccountRef(addrCopy), new(uint256.Int), gas)
			defer ReturnContract(contract)

			contract.SetCallCode(&addrCopy, evm.resolveCodeHash(addrCopy), evm.resolveCode(addrCopy))
			// When an error was returned by the EVM or when setting the creation code
			// above we revert to the snapshot and consume any gas remaining. Additionally
			// when we're in Homestead this also counts for code storage gas errors.
			ret, err = evm.interpreter.Run(contract, input, true)
			gas = contract.Gas
		}
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
				evm.Config.Tracer.OnGasChange(gas, 0, tracing.GasChangeCallFailedExecution)
			}

			gas = 0
		}
	}
	return ret, gas, err
}

func isWhitelistedContract(addr common.Address) bool {
	if _, ok := whiteListContracts[addr]; ok {
		return true
	}
	return false
}

func tryGetOptimizedCode(evm *EVM, codeHash common.Hash, rawCode []byte, addr common.Address) (bool, []byte) {
	if !isWhitelistedContract(addr) {
		return false, rawCode
	}
	var code []byte
	optimized := false
	code = rawCode
	optCode := compiler.LoadOptimizedCode(codeHash)
	if len(optCode) != 0 {
		code = optCode
		optimized = true
	} else {
		compiler.GenOrLoadOptimizedCode(codeHash, rawCode)
	}
	return optimized, code
}

type codeAndHash struct {
	code []byte
	hash common.Hash
}

func (c *codeAndHash) Hash() common.Hash {
	if c.hash == (common.Hash{}) {
		c.hash = crypto.Keccak256Hash(c.code)
	}
	return c.hash
}

// create creates a new contract using code as deployment code.
func (evm *EVM) create(caller ContractRef, codeAndHash *codeAndHash, gas uint64, value *uint256.Int, address common.Address, typ OpCode) (ret []byte, createAddress common.Address, leftOverGas uint64, err error) {
	if evm.Config.Tracer != nil {
		evm.captureBegin(evm.depth, typ, caller.Address(), address, codeAndHash.code, gas, value.ToBig())
		defer func(startGas uint64) {
			evm.captureEnd(evm.depth, startGas, leftOverGas, ret, err)
		}(gas)
	}
	// Depth check execution. Fail if we're trying to execute above the
	// limit.
	if evm.depth > int(params.CallCreateDepth) {
		return nil, common.Address{}, gas, ErrDepth
	}
	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, common.Address{}, gas, ErrInsufficientBalance
	}
	nonce := evm.StateDB.GetNonce(caller.Address())
	if nonce+1 < nonce {
		return nil, common.Address{}, gas, ErrNonceUintOverflow
	}
	evm.StateDB.SetNonce(caller.Address(), nonce+1, tracing.NonceChangeContractCreator)

	// Charge the contract creation init gas in verkle mode
	if evm.chainRules.IsEIP4762 {
		statelessGas := evm.AccessEvents.ContractCreatePreCheckGas(address)
		if statelessGas > gas {
			return nil, common.Address{}, 0, ErrOutOfGas
		}
		if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
			evm.Config.Tracer.OnGasChange(gas, gas-statelessGas, tracing.GasChangeWitnessContractCollisionCheck)
		}
		gas = gas - statelessGas
	}

	// We add this to the access list _before_ taking a snapshot. Even if the
	// creation fails, the access-list change should not be rolled back.
	if evm.chainRules.IsEIP2929 {
		evm.StateDB.AddAddressToAccessList(address)
	}
	// Ensure there's no existing contract already at the designated address.
	// Account is regarded as existent if any of these three conditions is met:
	// - the nonce is non-zero
	// - the code is non-empty
	// - the storage is non-empty
	contractHash := evm.StateDB.GetCodeHash(address)
	storageRoot := evm.StateDB.GetStorageRoot(address)
	if evm.StateDB.GetNonce(address) != 0 ||
		(contractHash != (common.Hash{}) && contractHash != types.EmptyCodeHash) || // non-empty code
		(storageRoot != (common.Hash{}) && storageRoot != types.EmptyRootHash) { // non-empty storage
		if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
			evm.Config.Tracer.OnGasChange(gas, 0, tracing.GasChangeCallFailedExecution)
		}
		return nil, common.Address{}, 0, ErrContractAddressCollision
	}
	// Create a new account on the state only if the object was not present.
	// It might be possible the contract code is deployed to a pre-existent
	// account with non-zero balance.
	snapshot := evm.StateDB.Snapshot()
	if !evm.StateDB.Exist(address) {
		evm.StateDB.CreateAccount(address)
	}
	// CreateContract means that regardless of whether the account previously existed
	// in the state trie or not, it _now_ becomes created as a _contract_ account.
	// This is performed _prior_ to executing the initcode,  since the initcode
	// acts inside that account.
	evm.StateDB.CreateContract(address)

	if evm.chainRules.IsEIP158 {
		evm.StateDB.SetNonce(address, 1, tracing.NonceChangeNewContract)
	}
	// Charge the contract creation init gas in verkle mode
	if evm.chainRules.IsEIP4762 {
		statelessGas := evm.AccessEvents.ContractCreateInitGas(address)
		if statelessGas > gas {
			return nil, common.Address{}, 0, ErrOutOfGas
		}
		if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
			evm.Config.Tracer.OnGasChange(gas, gas-statelessGas, tracing.GasChangeWitnessContractInit)
		}
		gas = gas - statelessGas
	}
	evm.Context.Transfer(evm.StateDB, caller.Address(), address, value)

	// Initialise a new contract and set the code that is to be used by the EVM.
	// The contract is a scoped environment for this execution context only.
	contract := GetContract(caller, AccountRef(address), value, gas)
	defer ReturnContract(contract)

	contract.SetCodeOptionalHash(&address, codeAndHash)
	contract.IsDeployment = true

	ret, err = evm.initNewContract(contract, address, value)
	if err != nil && (evm.chainRules.IsHomestead || err != ErrCodeStoreOutOfGas) {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			contract.UseGas(contract.Gas, evm.Config.Tracer, tracing.GasChangeCallFailedExecution)
		}
	}
	leftOverGas = contract.Gas
	return ret, address, leftOverGas, err
}

// initNewContract runs a new contract's creation code, performs checks on the
// resulting code that is to be deployed, and consumes necessary gas.
func (evm *EVM) initNewContract(contract *Contract, address common.Address, value *uint256.Int) ([]byte, error) {
	// We don't optimize creation code as it run only once.
	contract.optimized = false
	if evm.Config.EnableOpcodeOptimizations {
		compiler.DisableOptimization()
		evm.UseBaseInterpreter()
	}

	ret, err := evm.interpreter.Run(contract, nil, false)
	if err != nil {
		return ret, err
	}

	// After creation, retrieve to optimization
	if evm.Config.EnableOpcodeOptimizations {
		compiler.EnableOptimization()
		evm.UseOptInterpreter()
	}

	// Check whether the max code size has been exceeded, assign err if the case.
	if evm.chainRules.IsEIP158 && len(ret) > params.MaxCodeSize {
		return ret, ErrMaxCodeSizeExceeded
	}

	// Reject code starting with 0xEF if EIP-3541 is enabled.
	if len(ret) >= 1 && ret[0] == 0xEF && evm.chainRules.IsLondon {
		return ret, ErrInvalidCode
	}

	if !evm.chainRules.IsEIP4762 {
		createDataGas := uint64(len(ret)) * params.CreateDataGas
		if !contract.UseGas(createDataGas, evm.Config.Tracer, tracing.GasChangeCallCodeStorage) {
			return ret, ErrCodeStoreOutOfGas
		}
	} else {
		if len(ret) > 0 && !contract.UseGas(evm.AccessEvents.CodeChunksRangeGas(address, 0, uint64(len(ret)), uint64(len(ret)), true), evm.Config.Tracer, tracing.GasChangeWitnessCodeChunk) {
			return ret, ErrCodeStoreOutOfGas
		}
	}

	evm.StateDB.SetCode(address, ret)
	return ret, nil
}

// Create creates a new contract using code as deployment code.
func (evm *EVM) Create(caller ContractRef, code []byte, gas uint64, value *uint256.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, err error) {
	contractAddr = crypto.CreateAddress(caller.Address(), evm.StateDB.GetNonce(caller.Address()))
	return evm.create(caller, &codeAndHash{code: code}, gas, value, contractAddr, CREATE)
}

// Create2 creates a new contract using code as deployment code.
//
// The different between Create2 with Create is Create2 uses keccak256(0xff ++ msg.sender ++ salt ++ keccak256(init_code))[12:]
// instead of the usual sender-and-nonce-hash as the address where the contract is initialized at.
func (evm *EVM) Create2(caller ContractRef, code []byte, gas uint64, endowment *uint256.Int, salt *uint256.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, err error) {
	codeAndHash := &codeAndHash{code: code}
	contractAddr = crypto.CreateAddress2(caller.Address(), salt.Bytes32(), codeAndHash.Hash().Bytes())
	return evm.create(caller, codeAndHash, gas, endowment, contractAddr, CREATE2)
}

// resolveCode returns the code associated with the provided account. After
// Prague, it can also resolve code pointed to by a delegation designator.
func (evm *EVM) resolveCode(addr common.Address) []byte {
	code := evm.StateDB.GetCode(addr)
	if !evm.chainRules.IsPrague {
		return code
	}
	if target, ok := types.ParseDelegation(code); ok {
		// Note we only follow one level of delegation.
		return evm.StateDB.GetCode(target)
	}
	return code
}

// resolveCodeHash returns the code hash associated with the provided address.
// After Prague, it can also resolve code hash of the account pointed to by a
// delegation designator. Although this is not accessible in the EVM it is used
// internally to associate jumpdest analysis to code.
func (evm *EVM) resolveCodeHash(addr common.Address) common.Hash {
	if evm.chainRules.IsPrague {
		code := evm.StateDB.GetCode(addr)
		if target, ok := types.ParseDelegation(code); ok {
			// Note we only follow one level of delegation.
			return evm.StateDB.GetCodeHash(target)
		}
	}
	return evm.StateDB.GetCodeHash(addr)
}

// ChainConfig returns the environment's chain configuration
func (evm *EVM) ChainConfig() *params.ChainConfig { return evm.chainConfig }

func (evm *EVM) captureBegin(depth int, typ OpCode, from common.Address, to common.Address, input []byte, startGas uint64, value *big.Int) {
	tracer := evm.Config.Tracer
	if tracer.OnEnter != nil {
		tracer.OnEnter(depth, byte(typ), from, to, input, startGas, value)
	}
	if tracer.OnGasChange != nil {
		tracer.OnGasChange(0, startGas, tracing.GasChangeCallInitialBalance)
	}
}

func (evm *EVM) captureEnd(depth int, startGas uint64, leftOverGas uint64, ret []byte, err error) {
	tracer := evm.Config.Tracer
	if leftOverGas != 0 && tracer.OnGasChange != nil {
		tracer.OnGasChange(leftOverGas, 0, tracing.GasChangeCallLeftOverReturned)
	}
	var reverted bool
	if err != nil {
		reverted = true
	}
	if !evm.chainRules.IsHomestead && errors.Is(err, ErrCodeStoreOutOfGas) {
		reverted = false
	}
	if tracer.OnExit != nil {
		tracer.OnExit(depth, ret, startGas-leftOverGas, VMErrorFromErr(err), reverted)
	}
}

// GetVMContext provides context about the block being executed as well as state
// to the tracers.
func (evm *EVM) GetVMContext() *tracing.VMContext {
	return &tracing.VMContext{
		Coinbase:    evm.Context.Coinbase,
		BlockNumber: evm.Context.BlockNumber,
		Time:        evm.Context.Time,
		Random:      evm.Context.Random,
		BaseFee:     evm.Context.BaseFee,
		StateDB:     evm.StateDB,
	}
}

var whiteListContracts = map[common.Address]struct{}{
	common.HexToAddress("0x55d398326f99059ff775485246999027b3197955"): {},
	common.HexToAddress("0xff7d6a96ae471bbcd7713af9cb1feeb16cf56b41"): {},
	common.HexToAddress("0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c"): {},
	common.HexToAddress("0xe6df05ce8c8301223373cf5b969afcb1498c5528"): {},
	common.HexToAddress("0xb300000b72deaeb607a12d5f54773d1c19c7028d"): {},
	common.HexToAddress("0x4fa7c69a7b69f8bc48233024d546bc299d6b03bf"): {},
	common.HexToAddress("0xba5fe23f8a3a24bed3236f05f2fcf35fd0bf0b5c"): {},
	common.HexToAddress("0x5efc784d444126ecc05f22c49ff3fbd7d9f4868a"): {},
	common.HexToAddress("0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d"): {},
	common.HexToAddress("0x10a8fb1170b7790ef284b611f149376b6b8a8a9a"): {},
	common.HexToAddress("0xca852767b43a395ac1dd54737193eba5e20c78bd"): {},
	common.HexToAddress("0x8d0d000ee44948fc98c9b98a4fa4921476f08b0d"): {},
	common.HexToAddress("0x1b81d678ffb9c0263b24a97847620c99d213eb14"): {},
	common.HexToAddress("0x3398385c205c060ef54744ee817c1487e28a6616"): {},
	common.HexToAddress("0x380aadf63d84d3a434073f1d5d95f02fb23d5228"): {},
	common.HexToAddress("0x2f714d7b9a035d4ce24af8d9b6091c07e37f43fb"): {},
	common.HexToAddress("0xa234b8924bb2707195664e4c4cf17668db9b7286"): {},
	common.HexToAddress("0x3c45adf480174a3a2f472d61a3c69b86e4d576fb"): {},
	common.HexToAddress("0x0bfbcf9fa4f9c56b0f40a671ad40e0805a091865"): {},
	common.HexToAddress("0x10ed43c718714eb63d5aa57b78b54704e256024e"): {},
	common.HexToAddress("0x3d90f66b534dd8482b181e24655a9e8265316be9"): {},
	common.HexToAddress("0x452c625f0a946422ecd977242ebedcb7eeca95a5"): {},
	common.HexToAddress("0x8157a9d65807521fbb8db8f37eeecefdd247e9b1"): {},
	common.HexToAddress("0x2a9ea6526dd711308f18926a91db13d1e354a774"): {},
	common.HexToAddress("0x7a7ad9aa93cd0a2d0255326e5fb145cec14997ff"): {},
	common.HexToAddress("0xde9e4fe32b049f821c7f3e9802381aa470ffca73"): {},
	common.HexToAddress("0x9b9efa5efa731ea9bbb0369e91fa17abf249cfd4"): {},
	common.HexToAddress("0x13f4ea83d0bd40e75c8222255bc855a974568dd4"): {},
	common.HexToAddress("0x02006d91709d6b52936f971e24abd71467ee3942"): {},
	common.HexToAddress("0xcf59b8c8baa2dea520e3d549f97d4e49ade17057"): {},
	common.HexToAddress("0x556b9306565093c855aea9ae92a594704c2cd59e"): {},
	common.HexToAddress("0x802b65b5d9016621e66003aed0b16615093f328b"): {},
	common.HexToAddress("0x8a37394a7403bddeb73c97bbdbc92d05d0189183"): {},
	common.HexToAddress("0x6aba0315493b7e6989041c91181337b662fb1b90"): {},
	common.HexToAddress("0xdaecee3c08e953bd5f89a5cc90ac560413d709e3"): {},
	common.HexToAddress("0xce94aba9086f408b3f2af4e8b5ac1abc42f4da9a"): {},
	common.HexToAddress("0x28e2ea090877bf75740558f6bfb36a5ffee9e9df"): {},
	common.HexToAddress("0x07672f5a5553249d65a20d05e1aca78924fd5476"): {},
	common.HexToAddress("0xd6b48ccf41a62eb3891e58d0f006b19b01d50cca"): {},
	common.HexToAddress("0xe85ca0002e1f01f13db39eed700a7a308a0d0371"): {},
	common.HexToAddress("0x172fcd41e0913e95784454622d1c3724f546f849"): {},
	common.HexToAddress("0x000000000000000000636f6e736f6c652e6c6f67"): {},
	common.HexToAddress("0x66edfb97c65aad26fea25af192661b3480f1418d"): {},
	common.HexToAddress("0x51363f073b1e4920fda7aa9e9d84ba97ede1560e"): {},
	common.HexToAddress("0xf486ad071f3bee968384d2e39e2d8af0fcf6fd46"): {},
	common.HexToAddress("0x9558a9254890b2a8b057a789f413631b9084f4a3"): {},
	common.HexToAddress("0x194b302a4b0a79795fb68e2adf1b8c9ec5ff8d1f"): {},
	common.HexToAddress("0xce7c3b5e058c196a0eaaa21f8e4bf8c2c07c2935"): {},
	common.HexToAddress("0x6b3affc894b0d2d566e5ff03ad79188f1b65bfff"): {},
	common.HexToAddress("0x389ad4bb96d0d6ee5b6ef0efaf4b7db0ba2e02a0"): {},
	common.HexToAddress("0xb8638d3a15ac1403d4716a323f0954048e939c05"): {},
	common.HexToAddress("0x595e21b20e78674f8a64c1566a20b2b316bc3511"): {},
	common.HexToAddress("0x73d8bd54f7cf5fab43fe4ef40a62d390644946db"): {},
	common.HexToAddress("0x503fa24b7972677f00c4618e5fbe237780c1df53"): {},
	common.HexToAddress("0xfcaf676fa34bfbeedeb5062c136715cbc27183e7"): {},
	common.HexToAddress("0x0f757969e9a982732bda9440228a831ff21f6955"): {},
	common.HexToAddress("0x971435fc38eed5e0aaff0dd717d0d16a02a4110e"): {},
	common.HexToAddress("0x2a917aaa1cc49556a011422b28521a828ecab47e"): {},
	common.HexToAddress("0xd17f20b0c7e7294affe15b2129450dbe34d5ce1b"): {},
	common.HexToAddress("0xd84005dd553ecd7521f37f76b6bdaa6dd35390cd"): {},
	common.HexToAddress("0xb9e1d99884633f5712946dc5291861ff8cd222a7"): {},
	common.HexToAddress("0x9485ff32b6b4444c21d5abe4d9a2283d127075a2"): {},
	common.HexToAddress("0x2170ed0880ac9a755fd29b2688956bd959f933f8"): {},
	common.HexToAddress("0xd99cae3fac551f6b6ba7b9f19bdd316951eeee98"): {},
	common.HexToAddress("0x2c34a2fb1d0b4f55de51e1d0bdefaddce6b7cdd6"): {},
	common.HexToAddress("0xb971ef87ede563556b2ed4b1c0b0019111dd85d2"): {},
	common.HexToAddress("0x06238c1b8e618abedf17669228dc95fb2d2e210b"): {},
	common.HexToAddress("0xc1b45ec6ed9b65d76a49ff0c38794d1daa135050"): {},
	common.HexToAddress("0xe9e7cea3dedca5984780bafc599bd69add087d56"): {},
	common.HexToAddress("0xb5435cfc219d263e1854f9c13b4392f5a0889f05"): {},
	common.HexToAddress("0x7130d2a12b9bcbfae4f2634d864a1ee1ce3ead9c"): {},
	common.HexToAddress("0x5c8daeabc57e9249606d3bd6d1e097ef492ea3c5"): {},
	common.HexToAddress("0x3cf30c1d8decbf45cd36c9492f6395d47357b91c"): {},
	common.HexToAddress("0xb80ccaa913d36757fe7b6251333113e650a11867"): {},
	common.HexToAddress("0x22b1458e780f8fa71e2f84502cee8b5a3cc731fa"): {},
	common.HexToAddress("0x5d3f233b3335106953bbb7dd693d4f1da0363701"): {},
	common.HexToAddress("0x47a90a2d92a8367a91efa1906bfc8c1e05bf10c4"): {},
	common.HexToAddress("0x51220a0fdd6235a6b426a0090111f45a3b05e708"): {},
	common.HexToAddress("0x95034f653d5d161890836ad2b6b8cc49d14e029a"): {},
	common.HexToAddress("0xd9c500dff816a1da21a48a732d3498bf09dc9aeb"): {},
	common.HexToAddress("0xac175d676d9d4027aa72f13bc9406fd05c28cf1e"): {},
	common.HexToAddress("0x4848489f0b2bedd788c696e2d79b6b69d7484848"): {},
	common.HexToAddress("0x45eb49d2cfc594caaebe5e3d37c9cc14bd1704bc"): {},
	common.HexToAddress("0x258474cd00b4f42842ed424e6c2c1da0087031b8"): {},
	common.HexToAddress("0x0000000000000000000000000000000000001000"): {},
	common.HexToAddress("0x90869b3a42e399951bd5f5ff278b8cc5ee1dc0fe"): {},
	common.HexToAddress("0xc15efeacdc18ac3ad0dd1b7a4fa47bb639014444"): {},
	common.HexToAddress("0xc08cd26474722ce93f4d0c34d16201461c10aa8c"): {},
	common.HexToAddress("0x387a86d863420ffa2ef88b2524e54513a0ded845"): {},
	common.HexToAddress("0xb994882a1b9bd98a71dd6ea5f61577c42848b0e8"): {},
	common.HexToAddress("0x0b5f474ad0e3f7ef629bd10dbf9e4a8fd60d9a48"): {},
	common.HexToAddress("0x9d1d2c29547f74228a5fd5c098c50cdc66e0d710"): {},
	common.HexToAddress("0xe88144af81a5f123dbb9bd7baa9e6a3180fd560e"): {},
	common.HexToAddress("0x16b9a82891338f9ba80e2d6970fdda79d1eb0dae"): {},
	common.HexToAddress("0x5037861a294192e60cdff83c6d90f9e06914e7d7"): {},
	common.HexToAddress("0x208bf3e7da9639f1eaefa2de78c23396b0682025"): {},
	common.HexToAddress("0x46a15b0b27311cedf172ab29e4f4766fbe7f4364"): {},
	common.HexToAddress("0x41fe132a53fe38305323b9dc2dfaa413b7f55010"): {},
	common.HexToAddress("0x238a358808379702088667322f80ac48bad5e6c4"): {},
	common.HexToAddress("0x20482b0b4d9d8f60d3ab432b92f4c4b901a0d10c"): {},
}

var whitelistArr = []common.Address{
	common.HexToAddress("0x55d398326f99059ff775485246999027b3197955"),
	common.HexToAddress("0xff7d6a96ae471bbcd7713af9cb1feeb16cf56b41"),
	common.HexToAddress("0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c"),
	common.HexToAddress("0xe6df05ce8c8301223373cf5b969afcb1498c5528"),
	common.HexToAddress("0xb300000b72deaeb607a12d5f54773d1c19c7028d"),
	common.HexToAddress("0x4fa7c69a7b69f8bc48233024d546bc299d6b03bf"),
	common.HexToAddress("0xba5fe23f8a3a24bed3236f05f2fcf35fd0bf0b5c"),
	common.HexToAddress("0x5efc784d444126ecc05f22c49ff3fbd7d9f4868a"),
	common.HexToAddress("0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d"),
	common.HexToAddress("0x10a8fb1170b7790ef284b611f149376b6b8a8a9a"),
	common.HexToAddress("0xca852767b43a395ac1dd54737193eba5e20c78bd"),
	common.HexToAddress("0x8d0d000ee44948fc98c9b98a4fa4921476f08b0d"),
	common.HexToAddress("0x1b81d678ffb9c0263b24a97847620c99d213eb14"),
	common.HexToAddress("0x3398385c205c060ef54744ee817c1487e28a6616"),
	common.HexToAddress("0x380aadf63d84d3a434073f1d5d95f02fb23d5228"),
	common.HexToAddress("0x2f714d7b9a035d4ce24af8d9b6091c07e37f43fb"),
	common.HexToAddress("0xa234b8924bb2707195664e4c4cf17668db9b7286"),
	common.HexToAddress("0x3c45adf480174a3a2f472d61a3c69b86e4d576fb"),
	common.HexToAddress("0x0bfbcf9fa4f9c56b0f40a671ad40e0805a091865"),
	common.HexToAddress("0x10ed43c718714eb63d5aa57b78b54704e256024e"),
	common.HexToAddress("0x3d90f66b534dd8482b181e24655a9e8265316be9"),
	common.HexToAddress("0x452c625f0a946422ecd977242ebedcb7eeca95a5"),
	common.HexToAddress("0x8157a9d65807521fbb8db8f37eeecefdd247e9b1"),
	common.HexToAddress("0x2a9ea6526dd711308f18926a91db13d1e354a774"),
	common.HexToAddress("0x7a7ad9aa93cd0a2d0255326e5fb145cec14997ff"),
	common.HexToAddress("0xde9e4fe32b049f821c7f3e9802381aa470ffca73"),
	common.HexToAddress("0x9b9efa5efa731ea9bbb0369e91fa17abf249cfd4"),
	common.HexToAddress("0x13f4ea83d0bd40e75c8222255bc855a974568dd4"),
	common.HexToAddress("0x02006d91709d6b52936f971e24abd71467ee3942"),
	common.HexToAddress("0xcf59b8c8baa2dea520e3d549f97d4e49ade17057"),
	common.HexToAddress("0x556b9306565093c855aea9ae92a594704c2cd59e"),
	common.HexToAddress("0x802b65b5d9016621e66003aed0b16615093f328b"),
	common.HexToAddress("0x8a37394a7403bddeb73c97bbdbc92d05d0189183"),
	common.HexToAddress("0x6aba0315493b7e6989041c91181337b662fb1b90"),
	common.HexToAddress("0xdaecee3c08e953bd5f89a5cc90ac560413d709e3"),
	common.HexToAddress("0xce94aba9086f408b3f2af4e8b5ac1abc42f4da9a"),
	common.HexToAddress("0x28e2ea090877bf75740558f6bfb36a5ffee9e9df"),
	common.HexToAddress("0x07672f5a5553249d65a20d05e1aca78924fd5476"),
	common.HexToAddress("0xd6b48ccf41a62eb3891e58d0f006b19b01d50cca"),
	common.HexToAddress("0xe85ca0002e1f01f13db39eed700a7a308a0d0371"),
	common.HexToAddress("0x172fcd41e0913e95784454622d1c3724f546f849"),
	common.HexToAddress("0x000000000000000000636f6e736f6c652e6c6f67"),
	common.HexToAddress("0x66edfb97c65aad26fea25af192661b3480f1418d"),
	common.HexToAddress("0x51363f073b1e4920fda7aa9e9d84ba97ede1560e"),
	common.HexToAddress("0xf486ad071f3bee968384d2e39e2d8af0fcf6fd46"),
	common.HexToAddress("0x9558a9254890b2a8b057a789f413631b9084f4a3"),
	common.HexToAddress("0x194b302a4b0a79795fb68e2adf1b8c9ec5ff8d1f"),
	common.HexToAddress("0xce7c3b5e058c196a0eaaa21f8e4bf8c2c07c2935"),
	common.HexToAddress("0x6b3affc894b0d2d566e5ff03ad79188f1b65bfff"),
	common.HexToAddress("0x389ad4bb96d0d6ee5b6ef0efaf4b7db0ba2e02a0"),
	common.HexToAddress("0xb8638d3a15ac1403d4716a323f0954048e939c05"),
	common.HexToAddress("0x595e21b20e78674f8a64c1566a20b2b316bc3511"),
	common.HexToAddress("0x73d8bd54f7cf5fab43fe4ef40a62d390644946db"),
	common.HexToAddress("0x503fa24b7972677f00c4618e5fbe237780c1df53"),
	common.HexToAddress("0xfcaf676fa34bfbeedeb5062c136715cbc27183e7"),
	common.HexToAddress("0x0f757969e9a982732bda9440228a831ff21f6955"),
	common.HexToAddress("0x971435fc38eed5e0aaff0dd717d0d16a02a4110e"),
	common.HexToAddress("0x2a917aaa1cc49556a011422b28521a828ecab47e"),
	common.HexToAddress("0xd17f20b0c7e7294affe15b2129450dbe34d5ce1b"),
	common.HexToAddress("0xd84005dd553ecd7521f37f76b6bdaa6dd35390cd"),
	common.HexToAddress("0xb9e1d99884633f5712946dc5291861ff8cd222a7"),
	common.HexToAddress("0x9485ff32b6b4444c21d5abe4d9a2283d127075a2"),
	common.HexToAddress("0x2170ed0880ac9a755fd29b2688956bd959f933f8"),
	common.HexToAddress("0xd99cae3fac551f6b6ba7b9f19bdd316951eeee98"),
	common.HexToAddress("0x2c34a2fb1d0b4f55de51e1d0bdefaddce6b7cdd6"),
	common.HexToAddress("0xb971ef87ede563556b2ed4b1c0b0019111dd85d2"),
	common.HexToAddress("0x06238c1b8e618abedf17669228dc95fb2d2e210b"),
	common.HexToAddress("0xc1b45ec6ed9b65d76a49ff0c38794d1daa135050"),
	common.HexToAddress("0xe9e7cea3dedca5984780bafc599bd69add087d56"),
	common.HexToAddress("0xb5435cfc219d263e1854f9c13b4392f5a0889f05"),
	common.HexToAddress("0x7130d2a12b9bcbfae4f2634d864a1ee1ce3ead9c"),
	common.HexToAddress("0x5c8daeabc57e9249606d3bd6d1e097ef492ea3c5"),
	common.HexToAddress("0x3cf30c1d8decbf45cd36c9492f6395d47357b91c"),
	common.HexToAddress("0xb80ccaa913d36757fe7b6251333113e650a11867"),
	common.HexToAddress("0x22b1458e780f8fa71e2f84502cee8b5a3cc731fa"),
	common.HexToAddress("0x5d3f233b3335106953bbb7dd693d4f1da0363701"),
	common.HexToAddress("0x47a90a2d92a8367a91efa1906bfc8c1e05bf10c4"),
	common.HexToAddress("0x51220a0fdd6235a6b426a0090111f45a3b05e708"),
	common.HexToAddress("0x95034f653d5d161890836ad2b6b8cc49d14e029a"),
	common.HexToAddress("0xd9c500dff816a1da21a48a732d3498bf09dc9aeb"),
	common.HexToAddress("0xac175d676d9d4027aa72f13bc9406fd05c28cf1e"),
	common.HexToAddress("0x4848489f0b2bedd788c696e2d79b6b69d7484848"),
	common.HexToAddress("0x45eb49d2cfc594caaebe5e3d37c9cc14bd1704bc"),
	common.HexToAddress("0x258474cd00b4f42842ed424e6c2c1da0087031b8"),
	common.HexToAddress("0x0000000000000000000000000000000000001000"),
	common.HexToAddress("0x90869b3a42e399951bd5f5ff278b8cc5ee1dc0fe"),
	common.HexToAddress("0xc15efeacdc18ac3ad0dd1b7a4fa47bb639014444"),
	common.HexToAddress("0xc08cd26474722ce93f4d0c34d16201461c10aa8c"),
	common.HexToAddress("0x387a86d863420ffa2ef88b2524e54513a0ded845"),
	common.HexToAddress("0xb994882a1b9bd98a71dd6ea5f61577c42848b0e8"),
	common.HexToAddress("0x0b5f474ad0e3f7ef629bd10dbf9e4a8fd60d9a48"),
	common.HexToAddress("0x9d1d2c29547f74228a5fd5c098c50cdc66e0d710"),
	common.HexToAddress("0xe88144af81a5f123dbb9bd7baa9e6a3180fd560e"),
	common.HexToAddress("0x16b9a82891338f9ba80e2d6970fdda79d1eb0dae"),
	common.HexToAddress("0x5037861a294192e60cdff83c6d90f9e06914e7d7"),
	common.HexToAddress("0x208bf3e7da9639f1eaefa2de78c23396b0682025"),
	common.HexToAddress("0x46a15b0b27311cedf172ab29e4f4766fbe7f4364"),
	common.HexToAddress("0x41fe132a53fe38305323b9dc2dfaa413b7f55010"),
	common.HexToAddress("0x238a358808379702088667322f80ac48bad5e6c4"),
	common.HexToAddress("0x20482b0b4d9d8f60d3ab432b92f4c4b901a0d10c"),
}
