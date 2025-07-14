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
	"strings"
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
	if _, ok := whiteListContracts[strings.ToLower(addr.String())]; ok {
		return true
	}
	return false
}

func tryGetOptimizedCode(evm *EVM, codeHash common.Hash, rawCode []byte, addr common.Address) (bool, []byte) {
	if !isWhitelistedContract(addr) {
		return false, rawCode
	}
	log.Error("tryGetOptimizedCode in whitelist", "addr", addr.String())
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

var whiteListContracts = map[string]struct{}{
	"0x55d398326f99059ff775485246999027b3197955": {},
	"0xff7d6a96ae471bbcd7713af9cb1feeb16cf56b41": {},
	"0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c": {},
	"0xe6df05ce8c8301223373cf5b969afcb1498c5528": {},
	"0xb300000b72deaeb607a12d5f54773d1c19c7028d": {},
	"0x4fa7c69a7b69f8bc48233024d546bc299d6b03bf": {},
	"0xba5fe23f8a3a24bed3236f05f2fcf35fd0bf0b5c": {},
	"0x5efc784d444126ecc05f22c49ff3fbd7d9f4868a": {},
	"0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d": {},
	"0x10a8fb1170b7790ef284b611f149376b6b8a8a9a": {},
	"0xca852767b43a395ac1dd54737193eba5e20c78bd": {},
	"0x8d0d000ee44948fc98c9b98a4fa4921476f08b0d": {},
	"0x1b81d678ffb9c0263b24a97847620c99d213eb14": {},
	"0x3398385c205c060ef54744ee817c1487e28a6616": {},
	"0x380aadf63d84d3a434073f1d5d95f02fb23d5228": {},
	"0x2f714d7b9a035d4ce24af8d9b6091c07e37f43fb": {},
	"0xa234b8924bb2707195664e4c4cf17668db9b7286": {},
	"0x3c45adf480174a3a2f472d61a3c69b86e4d576fb": {},
	"0x0bfbcf9fa4f9c56b0f40a671ad40e0805a091865": {},
	"0x10ed43c718714eb63d5aa57b78b54704e256024e": {},
	"0x3d90f66b534dd8482b181e24655a9e8265316be9": {},
	"0x452c625f0a946422ecd977242ebedcb7eeca95a5": {},
	"0x8157a9d65807521fbb8db8f37eeecefdd247e9b1": {},
	"0x2a9ea6526dd711308f18926a91db13d1e354a774": {},
	"0x7a7ad9aa93cd0a2d0255326e5fb145cec14997ff": {},
	"0xde9e4fe32b049f821c7f3e9802381aa470ffca73": {},
	"0x9b9efa5efa731ea9bbb0369e91fa17abf249cfd4": {},
	"0x13f4ea83d0bd40e75c8222255bc855a974568dd4": {},
	"0x02006d91709d6b52936f971e24abd71467ee3942": {},
	"0xcf59b8c8baa2dea520e3d549f97d4e49ade17057": {},
	"0x556b9306565093c855aea9ae92a594704c2cd59e": {},
	"0x802b65b5d9016621e66003aed0b16615093f328b": {},
	"0x8a37394a7403bddeb73c97bbdbc92d05d0189183": {},
	"0x6aba0315493b7e6989041c91181337b662fb1b90": {},
	"0xdaecee3c08e953bd5f89a5cc90ac560413d709e3": {},
	"0xce94aba9086f408b3f2af4e8b5ac1abc42f4da9a": {},
	"0x28e2ea090877bf75740558f6bfb36a5ffee9e9df": {},
	"0x07672f5a5553249d65a20d05e1aca78924fd5476": {},
	"0xd6b48ccf41a62eb3891e58d0f006b19b01d50cca": {},
	"0xe85ca0002e1f01f13db39eed700a7a308a0d0371": {},
	"0x172fcd41e0913e95784454622d1c3724f546f849": {},
	"0x000000000000000000636f6e736f6c652e6c6f67": {},
	"0x66edfb97c65aad26fea25af192661b3480f1418d": {},
	"0x51363f073b1e4920fda7aa9e9d84ba97ede1560e": {},
	"0xf486ad071f3bee968384d2e39e2d8af0fcf6fd46": {},
	"0x9558a9254890b2a8b057a789f413631b9084f4a3": {},
	"0x194b302a4b0a79795fb68e2adf1b8c9ec5ff8d1f": {},
	"0xce7c3b5e058c196a0eaaa21f8e4bf8c2c07c2935": {},
	"0x6b3affc894b0d2d566e5ff03ad79188f1b65bfff": {},
	"0x389ad4bb96d0d6ee5b6ef0efaf4b7db0ba2e02a0": {},
	"0xb8638d3a15ac1403d4716a323f0954048e939c05": {},
	"0x595e21b20e78674f8a64c1566a20b2b316bc3511": {},
	"0x73d8bd54f7cf5fab43fe4ef40a62d390644946db": {},
	"0x503fa24b7972677f00c4618e5fbe237780c1df53": {},
	"0xfcaf676fa34bfbeedeb5062c136715cbc27183e7": {},
	"0x0f757969e9a982732bda9440228a831ff21f6955": {},
	"0x971435fc38eed5e0aaff0dd717d0d16a02a4110e": {},
	"0x2a917aaa1cc49556a011422b28521a828ecab47e": {},
	"0xd17f20b0c7e7294affe15b2129450dbe34d5ce1b": {},
	"0xd84005dd553ecd7521f37f76b6bdaa6dd35390cd": {},
	"0xb9e1d99884633f5712946dc5291861ff8cd222a7": {},
	"0x9485ff32b6b4444c21d5abe4d9a2283d127075a2": {},
	"0x2170ed0880ac9a755fd29b2688956bd959f933f8": {},
	"0xd99cae3fac551f6b6ba7b9f19bdd316951eeee98": {},
	"0x2c34a2fb1d0b4f55de51e1d0bdefaddce6b7cdd6": {},
	"0xb971ef87ede563556b2ed4b1c0b0019111dd85d2": {},
	"0x06238c1b8e618abedf17669228dc95fb2d2e210b": {},
	"0xc1b45ec6ed9b65d76a49ff0c38794d1daa135050": {},
	"0xe9e7cea3dedca5984780bafc599bd69add087d56": {},
	"0xb5435cfc219d263e1854f9c13b4392f5a0889f05": {},
	"0x7130d2a12b9bcbfae4f2634d864a1ee1ce3ead9c": {},
	"0x5c8daeabc57e9249606d3bd6d1e097ef492ea3c5": {},
	"0x3cf30c1d8decbf45cd36c9492f6395d47357b91c": {},
	"0xb80ccaa913d36757fe7b6251333113e650a11867": {},
	"0x22b1458e780f8fa71e2f84502cee8b5a3cc731fa": {},
	"0x5d3f233b3335106953bbb7dd693d4f1da0363701": {},
	"0x47a90a2d92a8367a91efa1906bfc8c1e05bf10c4": {},
	"0x51220a0fdd6235a6b426a0090111f45a3b05e708": {},
	"0x95034f653d5d161890836ad2b6b8cc49d14e029a": {},
	"0xd9c500dff816a1da21a48a732d3498bf09dc9aeb": {},
	"0xac175d676d9d4027aa72f13bc9406fd05c28cf1e": {},
	"0x4848489f0b2bedd788c696e2d79b6b69d7484848": {},
	"0x45eb49d2cfc594caaebe5e3d37c9cc14bd1704bc": {},
	"0x258474cd00b4f42842ed424e6c2c1da0087031b8": {},
	"0x0000000000000000000000000000000000001000": {},
	"0x90869b3a42e399951bd5f5ff278b8cc5ee1dc0fe": {},
	"0xc15efeacdc18ac3ad0dd1b7a4fa47bb639014444": {},
	"0xc08cd26474722ce93f4d0c34d16201461c10aa8c": {},
	"0x387a86d863420ffa2ef88b2524e54513a0ded845": {},
	"0xb994882a1b9bd98a71dd6ea5f61577c42848b0e8": {},
	"0x0b5f474ad0e3f7ef629bd10dbf9e4a8fd60d9a48": {},
	"0x9d1d2c29547f74228a5fd5c098c50cdc66e0d710": {},
	"0xe88144af81a5f123dbb9bd7baa9e6a3180fd560e": {},
	"0x16b9a82891338f9ba80e2d6970fdda79d1eb0dae": {},
	"0x5037861a294192e60cdff83c6d90f9e06914e7d7": {},
	"0x208bf3e7da9639f1eaefa2de78c23396b0682025": {},
	"0x46a15b0b27311cedf172ab29e4f4766fbe7f4364": {},
	"0x41fe132a53fe38305323b9dc2dfaa413b7f55010": {},
	"0x238a358808379702088667322f80ac48bad5e6c4": {},
	"0x20482b0b4d9d8f60d3ab432b92f4c4b901a0d10c": {},
	"0x8b4329947e34b6d56d71a3385cac122bade7d78d": {},
	"0xf0e8d1e1bddf02f68acf5cdb3fb339228f326315": {},
	"0x48bc32f212d539b23e1459e57dc27724aea6677a": {},
	"0x5b5a48d1dc819d8faccc3f7ea1cc3cee3b2fe439": {},
	"0x961967b4f759e3f2a8352c0fd3d2111a1ce75033": {},
	"0x3fefe29da25bea166fb5f6ade7b5976d2b0e586b": {},
	"0xf2688fb5b81049dfb7703ada5e770543770612c4": {},
	"0x00000000000dd5e8403de99fecd58d24c7633826": {},
	"0x7f9f70da4af54671a6abac58e705b5634cac8819": {},
	"0xf4b385849f2e817e92bffbfb9aeb48f950ff4444": {},
	"0xb035723d62e0e2ea7499d76355c9d560f13ba404": {},
	"0x111111125421ca6dc452d289314280a0f8842a65": {},
	"0xa4b68d48d7bc6f04420e8077e6f74bdef809dea3": {},
	"0x783c3f003f172c6ac5ac700218a357d2d66ee2a2": {},
	"0x519f83bb314a4b18c9884b91b3407785f2aace0e": {},
	"0x3d7c319090edf2293608a0f9a786317c66d320f8": {},
	"0xf4262c4dbf524f53851a5176bdc7d6c1e0fa82d8": {},
	"0xca143ce32fe78f1f7019d7d551a6402fc5350c73": {},
	"0xab0cd4d8ef4713a20c9926c646a14445556ca314": {},
	"0xdb25c09d96c165b62f6e6f9d9b17174738d897ba": {},
	"0x779a74436eda060911b2c4f209d34ea155f3df09": {},
	"0xf6fd1b383bbf5975ca6e27ea6abc4ddca085b875": {},
	"0xb897b5c2db30db088a07c3706b2e2dd68d07ea3a": {},
	"0xb080b94052f039ec2ca8bbaf7ec13329d1926973": {},
	"0xd55c9fb62e176a8eb6968f32958fefdd0962727e": {},
	"0x38a183074870bc7a89616e5cf4a0565bfda5e133": {},
	"0x19683a9d2a31508847a026dfe77f53a05a084444": {},
	"0x0486c6421b7c8e916198b9f570338a04ec195b5c": {},
	"0xb6f6d86a8f9879a9c87f643768d9efc38c1da6e7": {},
	"0x621199f6beb2ba6fbd962e8a52a320ea4f6d4aa3": {},
	"0xca8ca49c933536c09d4bdc15af846bb2713a6a86": {},
	"0x00000047bb99ea4d791bb749d970de71ee0b1a34": {},
	"0x87d00066cf131ff54b72b134a217d5401e5392b6": {},
	"0x4e100f73a329277bc6203c83055677def6aa3a48": {},
	"0x29f4c35559b40c7ea2c591f80844da738ba5dac6": {},
	"0xdc84096074269d8f304d476124101249d105b60d": {},
	"0x1d2f0da169ceb9fc7b3144628db156f3f6c60dbe": {},
	"0x6521aa52f19187817e5aa4c4802857db03bf1554": {},
	"0x78c3cf75d9fc21aa3abec3684a2934eb78e38917": {},
	"0x000000000000000000000000000000000000dead": {},
	"0x0000000000000000000000000000000000001002": {},
	"0x47ebcf27421858d4019238620a44a4ea10ebb5d9": {},
	"0x3595afff15a7ccaeeeb787fd676f7a297319c24c": {},
	"0x1266c6be60392a8ff346e8d5eccd3e69dd9c5f20": {},
	"0x2861ae8265c84e9f62015c60d637bfa0fcb049a0": {},
	"0x095483a98b6d864d510bccfe5d78991305ca5240": {},
	"0x5b620eabc2aba77d2bc4092f6d1fad6515872c34": {},
	"0x7f8c5e730121657e17e452c5a1ba3fa1ef96f22a": {},
	"0x62b652a210eeaa6023dba46878ad9c0d8240e7b4": {},
	"0x8665a78ccc84d6df2acaa4b207d88c6bc9b70ec5": {},
	"0x5dbb1220bc24b462d75e66012bb70cda8d3d1ad5": {},
	"0x103071da56e7cd95b415320760d6a0ddc4da1ca5": {},
	"0x3c79593e01a7f7fed5d0735b16621e2d52a6bc58": {},
	"0x63e1e81573f8667fba28706e94741ad2291e1eb9": {},
	"0xfffffffffffffffffffffffffffffffffffffffe": {},
	"0xc242e7c7d4bc5e920ab2199c407298514b16c92f": {},
	"0x31c2f6fcff4f8759b3bd5bf0e1084a055615c768": {},
	"0x149dd5112144205b9daf6a868e67c127a6fea05a": {},
	"0x92b7807bf19b7dddf89b706143896d05228f3121": {},
	"0x3dd696bc5bf81ec3032260b981be4cb10645dac7": {},
	"0x391eaa90f931c6330132efe6c73ebdf77d782ef5": {},
	"0x36696169c63e42cd08ce11f5deebbcebae652050": {},
	"0x9c4ee895e4f6ce07ada631c508d1306db7502cce": {},
	"0xd49bd24e4a34cdd0f78ef37baf30b43f82b6070e": {},
	"0x357b03be21b57e867c825ae3b8769daf7f330d7a": {},
	"0xfb858ceb0f432179c8c2aee559fff4649da4c9a6": {},
	"0xa6e8fee84f9bd528ad71917c9ddbb1fd3214f280": {},
	"0x0567f2323251f0aab15c8dfb1967e4e8a7d42aee": {},
	"0xd5b642646a6e40090d5d61ca11a78ee2f6e0ef14": {},
	"0x8f3930b7594232805dd780dc3b02f02cbf44016a": {},
	"0x466b045700dc241828787adad35cd868ec041898": {},
	"0xed98014847d567d561c04c6b681f486804fc4527": {},
	"0x2d3755bb69a208fe7c8ee9961a79aa1a2c328939": {},
	"0x4cc318290743b35d6c9063e58c3ef2faa01a81a9": {},
	"0x3c8d20001fe883934a15c949a3355a65ca984444": {},
	"0xd5eeec1a534c35c418b44bb28e4d5a602f1a22de": {},
	"0x3d4f0513e8a29669b960f9dbca61861548a9a760": {},
	"0xb7e548c4f133adbb910914d7529d5cb00c2e9051": {},
	"0x9fca4c864a30c03c21cc8743ee0c73312deda0ec": {},
	"0xf62ab041a0706cb64190c8c471b5cb4db5869594": {},
	"0x4b54900d3801b7a27657a0e63ce7a819365e0940": {},
	"0xdf83b8cc5f70edf17cf903be53f2842f39ef4d72": {},
	"0x24b1a66aeb151e2811e9fa048d608f33087e0dbb": {},
	"0xff5e6b040c4d1787349abb71a98d0ddba94aba9c": {},
	"0x7b4bf9feccff207ef2cb7101ceb15b8516021acd": {},
	"0x83381422ce030a90c170f6ec1bdb9f28d54b13e7": {},
	"0x3aee7602b612de36088f3ffed8c8f10e86ebf2bf": {},
	"0x918adf1f2c03b244823cd712e010b6e3cd653dba": {},
	"0xb5cb0555f07fd97c42b6dd5a46f82df2bde40777": {},
	"0x2a03ddd31911874200b481fb9cd39832b964c814": {},
	"0x75bb6eee1d11d23dd2a8620251a2f830913883bc": {},
	"0x0000000000001ff3684f28c67538d4d072c22734": {},
	"0xc0fab674ff7ddf8b891495ba9975b0fe1dcac735": {},
	"0x6d3ebc288a9ff9aa2d852d52b79946760eb17671": {},
	"0x6baa8dee0b357f01933b7bc969aaf9e222b768b1": {},
	"0x3fa6a5438cfe9901d8d9d9678aa9b255d3a57ef4": {},
	"0x701add4311e85c1f9c1549319fe2c476bc8a1b8b": {},
	"0xa95d87b443e1929465931611b7951b92c1746dae": {},
	"0x669a661d0d5856d05b7854164f1c979c85228888": {},
	"0xfbd46de044cf34dfd7386c6915cce74ef186c23e": {},
	"0x92aa03137385f18539301349dcfc9ebc923ffb10": {},
	"0xb5cb0550957bc59bc000d1183326a6215f3e4dd0": {},
	"0x5acdf322ce2e6a784a530ea4ae24b65bbdce131c": {},
	"0x2eed12578a9228dd0088948e0b170d8d6c3add62": {},
	"0xc46ccb4bc728f0a64103dad7f2c711f68902fe7e": {},
	"0x1987d8109638a028bb8be654531b15642a8708e3": {},
	"0xd4e3132ff005297a7ce45cb35d0fcf218b935492": {},
	"0x40b020ac2fc0c7e37684f70d44955eb8cb2028d1": {},
	"0xe2461367e562df374acf8d8a012729721ad5b486": {},
	"0x102c967f5bd0ad6c57a6989106579cb32546bb7b": {},
	"0x2c85728f3365383d4168aa2434f947e5e510499f": {},
	"0xf197da97df68f6e72264d340a820e8efd301e1a4": {},
	"0x6c58e4a513d3a8062e57f41a1442e003af14ebb5": {},
	"0x978fb308274ce3f3af669f36f068c100b9cb0ec4": {},
	"0xc75aabd3c47b91c897c5c885ab4ae691c34cd48a": {},
	"0xedbe4ede631d89edd350afab4c466f2ce869d7fa": {},
	"0x4bdb60d179d4145bd81ecbb0d29b8a7442c429a9": {},
	"0x0e09fabb73bd3ade0a17ecc321fd13a19e81ce82": {},
	"0xd82544bf0dfe8385ef8fa34d67e6e4940cc63e16": {},
	"0x9c8b5ca345247396bdfac0395638ca9045c6586e": {},
	"0x31dba3c96481fde3cd81c2aaf51f2d8bf618c742": {},
	"0x8ac53c6fa24cf243a1ad429de7046336e648de83": {},
	"0xc3924deddf4732855d9302875e45a4454fad430e": {},
	"0xafa381d908fd736ca58f232a9908b4ebb2dd412c": {},
	"0xe0ad8db26ea39ac29fe31b3621dd31c8ee9aefc8": {},
	"0x1758e12cee5ec99a5923f8c6ac14e2b715efe758": {},
	"0x7ef0cacfb15d369b0c4460d86dc2c68fe96988ad": {},
	"0x3cfed764cfed47926afd792a388823514135137f": {},
	"0x1dd95d7e28a8df15e617ebf2b99b2346d9c928f9": {},
	"0xeaf08974b76abfa199d1de3973b0b55e0c0871b7": {},
	"0x3ee2200efb3400fabb9aacf31297cbdd1d435d47": {},
	"0x00f71afe867b2dbd2ad4ba14fd139bc6bc659ccd": {},
	"0x10d8612d9d8269e322ab551c18a307cb4d6bc07b": {},
	"0x7f51bbf34156ba802deb0e38b7671dc4fa32041d": {},
	"0x779a588e5db467b6cb67bbae1d09bdb5ebb20a0e": {},
	"0xba2ae424d960c26247dd6c32edc70b295c744c43": {},
	"0xf12ba806c378407790607faee5e816c2dec0d03a": {},
	"0xa0ffb9c1ce1fe56963b0321b32e7a0302114058b": {},
	"0xdd2238b71da81f7b7c63ee637232596afd22f88c": {},
	"0x072f94ccc5c062ff35ca00b2dcf85fd49d31f737": {},
	"0xfb5b838b6cfeedc2873ab27866079ac55363d37e": {},
	"0x6bf62ca91e397b5a7d1d6bce97d9092065d7a510": {},
	"0x6ea8211a1e47dbd8b55c487c0b906ebc57b94444": {},
	"0xabed9f9d0c667f0f8c9edb6f6a8e12bd2bf6145c": {},
	"0xdb1d10011ad0ff90774d0c6bb92e5c5c8b4461f7": {},
	"0x6bdcce4a559076e37755a78ce0c06214e59e4444": {},
	"0x4515b64147b8eff871929c90795ae9ecb542f162": {},
	"0x60ffd33c33ae05d4f535e51117c9040000000000": {},
	"0x8a1ccab4be87a7e1a6ee54e666e2fc23725c18f9": {},
	"0x461f6989943a0820c12db66bd962d245b587eb3c": {},
	"0xd86e6ef14b96d942ef0abf0720c549197ea8c528": {},
	"0x7ed1c4d5fef23980c20804b33ca6ad381e68e3f2": {},
	"0xd1c0e791303824d45fdbb74524917829a4137808": {},
	"0xa6820deaeb44c70bf42dbe0b561d399297c05db8": {},
	"0xac5157b98f4f9921dd442c911ba4d3e19f08e2d4": {},
	"0xe1acb466421ed24dd8bd381d1205bad0ad43ca9c": {},
	"0x42407fb33b3487fd7b644d84f35818a63e22b03c": {},
	"0xe50e3d1a46070444f44df911359033f2937fcc13": {},
	"0xee6ff918a1f68b5d2fdecb14b367fa2eb5c6951c": {},
	"0x47423dbe87f337a4a5c17cc57d09b971d501dd14": {},
	"0x1dc78243f307c5ff39d1f304686f430c3e10d905": {},
	"0xa987c9a050800196291fb3020a0b12a89379be23": {},
	"0x7a4420381162847a2d98511d41be1b210a1284f5": {},
	"0xdff151afc14c7fe8542b623efdd275f8564f5f2e": {},
	"0xf927f4aa69c3f9e77305751eb663363bad764444": {},
	"0xd54409fc7aa268f18c369b08e3684ba4bb73ac8b": {},
	"0xc3bbd49bc1277ee2a3745d059e47f77ca35170a4": {},
	"0x5dbde81fce337ff4bcaaee4ca3466c00aecae274": {},
	"0x67ed61bb8cf639c8fd72b0cb95e8b76dc425ae3d": {},
	"0x6045479ab46df901e7c86977ed22ddeb80728165": {},
	"0xd60c6fb1401c752687ea0da8aa73c0d7e70aea5f": {},
	"0xeb606919ea12779ad5362baaae59f7c34930415a": {},
	"0xca29cd653f18c702156aaac27528dc5f4db1f2c4": {},
	"0x52b7b9e768900e2cc509ff0109b900660431b5d7": {},
	"0xfd7ac7e9dfbb78a0ab17b2476bb890f178120d1d": {},
	"0x3b4de3c7855c03bb9f50ea252cd2c9fa1125ab07": {},
	"0x144d395b5562c742259932d2ee6e1d8d092a21b8": {},
	"0x76b2a23f2a94c894fa9eb6c60688e8e38bd95e9a": {},
	"0xecfb0a9f3ff87a4b984a3ac36d75e2f9c21d2b99": {},
	"0xd5091f84e9226d83a7692d904adc34164c7d362c": {},
	"0xe5e9b0cab984b58b7e7ae17707d633295d5a4c4d": {},
	"0xb5cbfa41c00005562560d6e7a9e3d6a028ed46e5": {},
	"0x18dfde99f578a0735410797e949e8d3e2afcb9d2": {},
	"0x5a923dc4a4f175e723dbdd263106a34d25ee45a9": {},
	"0x98d6e81f14b2278455311738267d6cf93160be35": {},
	"0x3c879579c774a34e06e829d515f1c55d7a781a46": {},
	"0x2aa909dfdc82917b433bf9699ebb88731efe7d56": {},
	"0xf2a6e80aa129d94c841d0af7239e671467c5d0bc": {},
	"0x2d024d2205dcce8cd48f4236910bd26159bcf086": {},
	"0xa79252a092c43329e499025a58553f23329c7033": {},
	"0xd7af60112d7dfe0f914724e3407dd54424aaa19b": {},
	"0x899357e54c2c4b014ea50a9a7bf140ba6df2ec73": {},
	"0xdcc6317a6d1e22569206d23ca206438fcf505676": {},
	"0x3635de912598ab441c279950a85825902efbb631": {},
	"0x5042c0fd01adb0e068687297caa9a160f453cdd2": {},
	"0x44f161ae29361e332dea039dfa2f404e0bc5b5cc": {},
	"0xfe1a06260b3b68f49862ae5d617686d78f454dea": {},
	"0xc90778420931cdf087c58b61ae43847693d33dd1": {},
	"0xe9f6214825b794ae7df56aad51e3f6cea826d42e": {},
	"0xbfa21c09dda2f68f0016d06f7bd3ac28ffd3c476": {},
	"0x00901a076785e0906d1028c7d6372d247bec7d61": {},
	"0xb769c69c00e6bd26216791aa94628854dc7f54a2": {},
	"0xd758c807cf0bd82b737c2fc14935fefe49c44141": {},
	"0xc206c2764a9dbf27d599613b8f9a63acd1160ab4": {},
	"0xbf5140a22578168fd562dccf235e5d43a02ce9b1": {},
	"0x1906c1d672b88cd1b9ac7593301ca990f94eae07": {},
	"0x555294d5d028af3d62aa4521924609dbbd111aa4": {},
	"0xcb7f73bfb32f573a255534815429627ebb1547d5": {},
	"0x0ab9405c009fe75e19ac98125d7cef67e012c47a": {},
	"0xfe723495f73714426493384eb5e49aa5b827e1d5": {},
	"0x6807dc923806fe8fd134338eabca509979a7e0cb": {},
	"0xddac2b348b191d39da0eb3866c8df5d48868ab11": {},
	"0x74da4c5f8c254dd4fb39f9804c0924f52a808318": {},
	"0xf37bbb513a584e940a471a792010c06b2dce49de": {},
	"0xf95f84e2bad9c234f93dd66614b82f9a854b452e": {},
	"0xc1353d3ee02fdbd4f65f92eee543cfd709049cb1": {},
	"0x5e45912bbf46b9b83d949dd1611d13b7f51f8416": {},
	"0x51bc76457c4a02eff67abcf4d300651ec467488a": {},
	"0x0557e8cb169f90f6ef421a54e29d7dd0629ca597": {},
	"0x8c55dd82205f90575bd7368c696d7a2e8b21991d": {},
	"0x0ba6d224169bbfad4eb2d38c500d2c8e8ede696c": {},
	"0xabbee6e231010de7aaedfc4a8f18e5e1d6f92d88": {},
	"0xde6e2fa9e8dda7eb498692e48064532bc54671ec": {},
	"0x74836cc0e821a6be18e407e6388e430b689c66e9": {},
	"0xcf39e58ed4decc9ef7bdb2f9335602dc22be99b4": {},
	"0x37c15772077fb3bf3dfa2b806f784acac7999f45": {},
	"0x39c145ef5ca969e060802b50a99623909d73e394": {},
	"0xd26d38408a6ac6faff6fdfa92b5910ced2e5fd31": {},
	"0xd699b97d13d84e81c995a80bbc33c02d04d54bde": {},
	"0x6d76e7bb743fee795a2f00a317760acf822ee2be": {},
	"0xb4c437aca9f5e831af81150479226f79bcd343f5": {},
	"0x03f83fdfb168e7a2a621691e4b15297b1917ab91": {},
	"0x4d67dc640f5327d0e1c7c6537ed8542aaf46cf99": {},
	"0x1771cd9739fe55f28f7e94be52cbfb8fda2274c0": {},
	"0x570a5d26f7765ecb712c0924e4de545b89fd43df": {},
	"0x824b87e7939b3f287ef2c90109f1b20c6df9da61": {},
	"0x4e60285e8ae543efbe3af21b7377f614d7eac80e": {},
	"0xbaf9f711a39271701b837c5cc4f470d533bacf33": {},
	"0xc76979538aad5c50ede8fe561830e35ab49c8888": {},
	"0xf4dcd494377bfc545bc02fa4b5d0db746e2a5834": {},
	"0x890e659a81d8ab0113238d0ece4b3f986d8c03a3": {},
	"0xdd58470515331c24622d3ac5c67a981d684cc313": {},
	"0x59b53dd6f0727a34d79243380278e3b2056b3562": {},
	"0x75a5863a19af60ec0098d62ed8c34cc594fb470f": {},
	"0xd615eb3ea3db2f994dce7d471a02d521b8e1d22d": {},
	"0xda7630a141dbb6382ddb6409aea7331da543b0bb": {},
	"0x356f760ea9ae370096a84f3e16f1da956a151c8f": {},
	"0x57df399cace62f98a74bffdffbb264e6f31bd982": {},
	"0x40113a4e92445f5a2ce2890f80c7863f4ecd50cf": {},
	"0x3e5c63644e683549055b9be8653de26e0b4cd36e": {},
	"0x000066320a467de62b1548f46465abbb82662331": {},
	"0xc37a3dcb6a9edd97dbdf2a68c136c5d4bdb86005": {},
	"0xc71b5f631354be6853efe9c3ab6b9590f8302e81": {},
	"0xeb7c5c1fd1d1a6e8e197133fbce400e80197821e": {},
	"0x8c072554f59c10aa7a9178ba7069ffc9fccbafc4": {},
	"0x1b9a1120a17617d8ec4dc80b921a9a1c50caef7d": {},
	"0xd5cad1821392d83eb1557be48221b6592c0d1d6f": {},
	"0x0ed943ce24baebf257488771759f9bf482c39706": {},
	"0x8399a024ff430017e4c9709161c48b6c73d50f10": {},
	"0x1d37c6689ba65245e68f3c1e0112de37fd527adf": {},
	"0xc1a780989734a0e5df875cebe410748562e1c5e6": {},
	"0x5263972ce5312d9a25c54b80896b55aed6136e50": {},
	"0x86ab1c62a8bf868e1b3e1ab87d587aba6fbcbdc5": {},
	"0x53470afb52a16284afa524776cd3530b4c93b05e": {},
	"0x72ffb0bd5771e3365cec3483535723cf0ed6d263": {},
	"0xfdffb411c4a70aa7c95d5c981a6fb4da867e1111": {},
	"0xe55a5aa2fa59c07961cab70f96938de20dc51eed": {},
	"0x8a2bdfc69f64ae211a1f1e64017b924d6a84691f": {},
	"0x1831bb2723ced46e1b6c08d2f3ae50b2ab9427b9": {},
	"0xff75b6da14ffbbfd355daf7a2731456b3562ba6d": {},
	"0xd0101aaa05ef00c6f1128cf7fadd486b7e1d9251": {},
	"0x4bd8d57b9f64fae735617457e13909af028f6e87": {},
	"0xf150d29d92e7460a1531cbc9d1abeab33d6998e4": {},
	"0xda6cef7f667d992a60eb823ab215493aa0c6b360": {},
	"0x0da21d330f3f75d730a1b5f1535ca75061cabf61": {},
	"0x3d7ab4d0fd0b1fe69b5e55d1d8ea2de29d9182cd": {},
	"0xa8acdd81f46633b69acb6ec5c16ee7e00cc8938d": {},
	"0xefe7a50f92b089b06d0e6cbcc85d7584424921b2": {},
	"0xa4e409177c5a3acf66b7748d4a2f34eb8474f785": {},
	"0x0000000000000000000000000000000000000004": {},
	"0x997a58129890bbda032231a52ed1ddc845fc18e1": {},
	"0x88a573484a2bb61dc830cd41cfca2f7b75622ec9": {},
	"0x46b9217342cdc50c89ffa84a12be45b2639eaf4a": {},
	"0x62fcb3c1794fb95bd8b1a97f6ad5d8a7e4943a1e": {},
	"0xf334df78c5bae66abea56a2cc8dd3ca789613616": {},
	"0xd2d1e8dc32c4429bdfce2d0b866df5454c9d9503": {},
	"0x6bb8ec43d852c10022911140d478ba0284aa747d": {},
	"0x6e61579c22f9a6da63a33e819f29b6697d2a126e": {},
	"0xda77c035e4d5a748b4ab6674327fa446f17098a2": {},
	"0x53432759174c57c88cea35850a4fbf825042bc8c": {},
	"0x32118353be16ce03584c3be37e32ed4a323cf539": {},
	"0x3aa0bc0182209c37f52a512e119cfe663f014398": {},
	"0x757651b39d4d13509b60b5784ba0e504a8ef6691": {},
	"0xb05500ea96db17d28cd0eb30bcb797708f279d36": {},
	"0x2383596efa3a0fc13efdcd776410deff25017417": {},
	"0x4f36319c1e35c5b8e20c1d20879980d17d908507": {},
	"0x8725fb6603d1e518c0078d9de68365f22f2eee85": {},
	"0x201fe9632d49ac0f847f498fa229cd871812ee4b": {},
	"0xa08d24349fae0727eb4e60b70e9237f3b30a2a96": {},
	"0xa6cc226dba6e28ae74cdf7d83324ff9b0f3e4cac": {},
	"0xa05405b6340a7f43dc5835351bfc4f5b1f028359": {},
	"0xd10384ebdc33662b60557f97bfaff08f7e589465": {},
	"0xb60223c0bc097b05dd61d233b163675c33cad1d0": {},
	"0x560c6f82effbcf2aea0f6a102ab7b5854acb08b5": {},
	"0x19113a761706a5914bb272690dc5d691554c12b5": {},
	"0x92412e023114641a066ea83458d11cb5e50927c9": {},
	"0x881966aa7c3ca232b8797afe3e29e8b9ce005795": {},
	"0xad56fdbe2cf6226dbdb6d08f36b08ef2e3936c68": {},
	"0x578e948ec7f0fd1182aae4b2480e761b2f765825": {},
	"0x6d47bdb56e31cdf2ad7e9af1290bd9340ffe7066": {},
	"0x8794b8bfc2b6d2e2fada0f76a9e5b5296efae0f4": {},
	"0x28b9766177b69c08b5df587b75cc61d545e69ebf": {},
	"0x2ab0e9e4ee70fff1fb9d67031e44f6410170d00e": {},
	"0x76f2999f025daa985062188e0ef3ffb8d36af8fc": {},
	"0x5759b5a2ed4e9f6d4e63879cf78f367ddc31ca6f": {},
	"0xa5ea35c42503878274dea73fe5bd57e951e2833f": {},
	"0xa8246f6e92dc9fce2d53ab7b0581cbdaef9f9097": {},
	"0x6162ff51406fbd5a68cccc5158d6ae5e18497476": {},
	"0x5532cdb3c0c4278f9848fc4560b495b70ba67455": {},
	"0x8b10830f86295295717b71c74b3ef7699d1f2825": {},
	"0x3d24650a973c35f4ee406be5e62ce3a54cfb2f1e": {},
	"0x55e71e5c44a454b2b2cad9f80676c1320704b4c6": {},
	"0xd317b5480faf6ef228c502d9c4d0c04599c5b74b": {},
	"0xc23ce8253dfa1628a68c29a3c41de904fe27a51c": {},
	"0x1a515bf4e35aa2df67109281de6b3b00ec37675e": {},
	"0x6952c5408b9822295ba4a7e694d0c5ffdb8fe320": {},
	"0xed5e17e13715df35ffb81650c1121ce6f319d714": {},
	"0x1111111254eeb25477b68fb85ed929f73a960582": {},
	"0x751817452f48fb01980f7931290a7f4514764353": {},
	"0x5fa1e1c899f1cc8599c45c45214c4cb801c112a9": {},
	"0x1345f06ea30c09eead17c857928cf808355fe48d": {},
	"0x0f8b686cbf4af0bd55982e63307a16a14e067443": {},
	"0x0f0cf38f67ed6fb00881cdb815e25441f7f022e0": {},
	"0xd24825d7a3be9b443c26ae104233d00a6f8c04c8": {},
	"0xeb6aa1466259aeda40d56841f00abce33799188f": {},
	"0x29fcb43b46531bca003ddc8fcb67ffe91900c762": {},
	"0xc9882def23bc42d53895b8361d0b1edc7570bc6a": {},
	"0xf86af2fbcf6a0479b21b1d3a4af3893f63207fe7": {},
	"0x743cb7d6d8fbbf93806dec9b7700743a2641dae2": {},
	"0xff3eccb9316c4a3527ea7c227f56f0eef1697986": {},
	"0x559fa5b568f1d3458640a66d3eb2f1464d6c9bc2": {},
	"0x4f9d657c127c143f987c77e01ae361532bf0751d": {},
	"0x4141325bac36affe9db165e854982230a14e6d48": {},
	"0x2c953c51a6d62cf4d35a9982b4aa09fd791a8521": {},
	"0x1ef34b91afc368174f579067d1db94325cdc7946": {},
	"0x193e9b8be2b768c431c5a2da524a1ff2a7c61d75": {},
	"0xa6f7444d2b92aa9f94a2165c77aaf2b671e63994": {},
	"0xc6f14b67e4ebe665df253c975d031062db40cbd8": {},
	"0xaa41acdb7f4a71313d735fc6e1cc6bbe42b0b0b6": {},
	"0xf7ff246b6d54bf702e0a4e45852a1190dc3a8f32": {},
	"0xcb021ea66295fd4f86a7c239fa576d14ba3bb828": {},
	"0x69b86059c5fb3a44355937e7b505a659443b9a22": {},
	"0xbca03cc53ff2d77fb8c06682d5b7581132f2c885": {},
	"0x64e312b0f7f1975a468e54c62d08cda9e31ba73e": {},
	"0x7aaaa5b10f97321345acd76945083141be1c5631": {},
	"0x50061047b9c7150f0dc105f79588d1b07d2be250": {},
	"0x7299bc7e8beb9fa14c55905d94f963bc9f70434b": {},
	"0x900650c66c8d317df5cacc2ea0d2f39549ba8632": {},
	"0x2b6e6e4def77583229299cf386438a227e683b28": {},
	"0xcdbbed5606d9c5c98eeedd67933991dc17f0c68d": {},
	"0xb40f2e5291c3db45abb0ca8df76f1c21e9f112a9": {},
	"0x80ed447db8ab6f3ba469589af474e2b6114ada33": {},
	"0xe3040b2f5d1f04dd0ed333ab8a373e3332cb8229": {},
	"0x799a290f9cc4085a0ce5b42b5f2c30193a7a872b": {},
	"0xf0876ae463bdd657e3e9d40587ed63ccbb61ff2b": {},
	"0x881772cc17ff69a1730b658c4ecc32e0852d985d": {},
	"0x30d59a44930b3994c116846efe55fc8fcf608aa8": {},
	"0x8a0db359c38414b5f145f65cc1c69d9253067c43": {},
	"0x4e2de7adc9de0fb1ffef1ad7b8acc9e97fb9b992": {},
	"0x045a5a1bf7fc80e230c4e324263a7e9f2d9018ef": {},
	"0xbaa5583cbfe94605e948abbdbb227043dc69daa0": {},
	"0xeebe0094ab63869d20c8e270fb18e89056a8bb7a": {},
	"0x07c9bf6dfc9db18cd478d3b3fe7b1312b5dd0c05": {},
	"0xba53da030f3504db8cf0309bb89791950d0ce5e6": {},
	"0xdb6f1f098b55e36b036603c8e54663a8d907d6e1": {},
	"0x5b82b90ca88f803f5ebd63a782fec5e9f62249cf": {},
	"0xe270d1858d85d3f05f058028dcc175fc0f7feab4": {},
	"0x4bab2cb2ed6ee2d564ba5398cab4161d95e58f1b": {},
	"0xbbd372407ead5729657aeb2deb86468a0d593520": {},
	"0xc708ec07d9d6f981f9ebbaaf6580a33ad2755177": {},
	"0x247f51881d1e3ae0f759afb801413a6c948ef442": {},
	"0xfff4857a955b2b460c27fd431aa5a85fabca5559": {},
	"0xe1b865c2593bf7d3a071bb1f162fc1d78919d5a3": {},
	"0x6ec31af1bb9a72aacec12e4ded508861b05f4503": {},
	"0x9b87f667891ea75b7e258ce705289daff812f893": {},
	"0xd03074e2ef40b053fbe2a6b694af067a7454046e": {},
	"0x9ea0f51fd2133d995cf00229bc523737415ad318": {},
	"0xf53353d30def87e5c87be6ccf425b61c39faef37": {},
	"0xef7d88d12b6393fe06f5f07d48d7b76511909e6b": {},
	"0x2c694b204813397041b0d18949ee9493faaa123d": {},
	"0x43256d0dcc2571e564311edb6d7e8f076a72fc46": {},
	"0xbcc9a8ce6322f64b6385a6316b958698219eba23": {},
	"0x4e0e76829d749b3301e41670b0f4fa2e93713e9d": {},
	"0x1641a967c88203113a28ea604d6f82151ff23f37": {},
	"0xa9b6a8e08f2b413b36484128442da9ef28dd5778": {},
	"0xfd5840cd36d94d7229439859c0112a4185bc0255": {},
	"0xd3cffc6b02a34dfc72f1350339031043a854599f": {},
	"0x194fb07e51eeb6fec9eae4edad6523f8dff2c7ed": {},
	"0x68c775c35d6b8197167923b839f191af68bb6d48": {},
	"0xcf6bb5389c92bdda8a3747ddb454cb7a64626c63": {},
}
