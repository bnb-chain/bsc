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
	"bytes"
	"errors"
	"github.com/holiman/uint256"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
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
		//compiler.EnableOptimization()
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
				contract.optimized, code = true, PreloadUSDT
				//if !contract.optimized {
				//	log.Error("contract not optimized", "addrCopy", addrCopy, "codeHash", codeHash.String())
				//} else {
				//	log.Error("contract optimized", "addrCopy", addrCopy, "codeHash", codeHash.String(), "code matches preload", bytes.Equal(code, getPreloadedOptimization(codeHash)))
				//}
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
			contract.optimized, code = tryGetOptimizedCode(evm, codeHash, code)
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
			contract.optimized, code = tryGetOptimizedCode(evm, codeHash, code)
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
			contract.optimized, code = tryGetOptimizedCode(evm, codeHash, code)
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

//func isWhitelistedContract(addr common.Address) bool {
//	if _, ok := whiteListContracts[addr]; ok {
//		return true
//	}
//	return false
//}

func getPreloadedOptimization(codeHash common.Hash) []byte {
	if value, ok := PreloadOptimizeCodeHash[codeHash]; ok {
		return value
	}
	return []byte{}
}

func tryGetOptimizedCode(evm *EVM, codeHash common.Hash, rawCode []byte) (bool, []byte) {
	preloadedCode := getPreloadedOptimization(codeHash)
	if bytes.Equal(preloadedCode, []byte{}) {
		return false, rawCode
	} else {
		return true, preloadedCode
	}

	//var code []byte
	//optimized := false
	//code = rawCode
	//optCode := compiler.LoadOptimizedCode(codeHash)
	//if len(optCode) != 0 {
	//	code = optCode
	//	optimized = true
	//} else {
	//	compiler.GenOrLoadOptimizedCode(codeHash, rawCode)
	//}
	//return optimized, code
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
		//compiler.DisableOptimization()
		evm.UseBaseInterpreter()
	}

	ret, err := evm.interpreter.Run(contract, nil, false)
	if err != nil {
		return ret, err
	}

	// After creation, retrieve to optimization
	if evm.Config.EnableOpcodeOptimizations {
		//compiler.EnableOptimization()
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

var PreloadUSDT = []byte{183, 128, 176, 64, 82, 52, 128, 193, 176, 0, 16, 176, 186, 0, 176, 253, 91, 80, 96, 4, 54, 16, 182, 1, 44, 176, 197, 0, 176, 176, 224, 176, 176, 176, 137, 61, 32, 232, 176, 176, 0, 173, 87, 128, 99, 169, 5, 156, 187, 17, 182, 0, 113, 176, 196, 176, 169, 5, 156, 187, 176, 176, 3, 90, 87, 196, 176, 176, 159, 18, 102, 176, 176, 3, 134, 87, 196, 176, 210, 141, 136, 82, 176, 176, 3, 142, 87, 196, 176, 221, 98, 237, 62, 176, 176, 3, 150, 87, 196, 176, 242, 253, 227, 139, 176, 176, 3, 196, 87, 181, 1, 44, 176, 91, 196, 176, 137, 61, 32, 232, 176, 176, 2, 221, 87, 196, 176, 141, 165, 203, 91, 176, 176, 3, 1, 87, 196, 176, 149, 216, 155, 65, 176, 176, 3, 9, 87, 196, 176, 160, 113, 45, 104, 176, 176, 3, 17, 87, 196, 176, 164, 87, 194, 215, 176, 176, 3, 46, 87, 181, 1, 44, 176, 91, 128, 99, 50, 66, 74, 163, 17, 182, 0, 244, 176, 196, 176, 50, 66, 74, 163, 176, 176, 2, 92, 87, 196, 176, 57, 80, 147, 81, 176, 176, 2, 100, 87, 196, 176, 66, 150, 108, 104, 176, 176, 2, 144, 87, 196, 176, 112, 160, 130, 49, 176, 176, 2, 173, 87, 196, 176, 113, 80, 24, 166, 176, 176, 2, 211, 87, 181, 1, 44, 176, 91, 196, 176, 6, 253, 222, 3, 176, 176, 1, 49, 87, 196, 176, 9, 94, 167, 179, 176, 176, 1, 174, 87, 196, 176, 24, 22, 13, 221, 176, 176, 1, 238, 87, 196, 176, 35, 184, 114, 221, 176, 176, 2, 8, 87, 196, 176, 49, 60, 229, 103, 176, 176, 2, 62, 87, 91, 186, 0, 176, 253, 91, 97, 1, 57, 181, 3, 234, 176, 91, 186, 64, 176, 81, 186, 32, 176, 130, 82, 131, 81, 129, 131, 1, 82, 131, 81, 145, 146, 131, 146, 144, 131, 1, 145, 133, 1, 144, 128, 131, 131, 96, 0, 91, 131, 192, 176, 193, 176, 1, 115, 176, 129, 129, 1, 81, 131, 130, 1, 82, 184, 32, 176, 181, 1, 91, 176, 91, 189, 176, 189, 176, 187, 176, 144, 129, 1, 144, 96, 31, 22, 128, 193, 176, 1, 160, 176, 128, 130, 3, 128, 81, 96, 1, 131, 96, 32, 3, 97, 1, 0, 10, 3, 25, 22, 195, 176, 176, 32, 176, 191, 176, 91, 80, 146, 189, 176, 80, 96, 64, 81, 128, 145, 3, 144, 243, 91, 97, 1, 218, 186, 4, 176, 54, 3, 96, 64, 192, 176, 193, 176, 1, 196, 176, 186, 0, 176, 253, 91, 80, 198, 1, 176, 1, 176, 160, 176, 176, 129, 53, 22, 144, 184, 32, 176, 53, 181, 4, 128, 176, 91, 186, 64, 176, 81, 145, 21, 21, 130, 82, 81, 144, 129, 144, 3, 184, 32, 176, 144, 243, 91, 97, 1, 246, 181, 4, 157, 176, 91, 186, 64, 176, 81, 145, 130, 82, 81, 144, 129, 144, 3, 184, 32, 176, 144, 243, 91, 97, 1, 218, 186, 4, 176, 54, 3, 96, 96, 192, 176, 193, 176, 2, 30, 176, 186, 0, 176, 253, 91, 80, 198, 1, 176, 1, 176, 160, 176, 176, 129, 53, 129, 22, 145, 96, 32, 129, 1, 53, 144, 145, 22, 144, 184, 64, 176, 53, 181, 4, 163, 176, 91, 97, 2, 70, 181, 5, 48, 176, 91, 186, 64, 176, 81, 96, 255, 144, 146, 22, 130, 82, 81, 144, 129, 144, 3, 184, 32, 176, 144, 243, 91, 97, 2, 70, 181, 5, 57, 176, 91, 97, 1, 218, 186, 4, 176, 54, 3, 96, 64, 192, 176, 193, 176, 2, 122, 176, 186, 0, 176, 253, 91, 80, 198, 1, 176, 1, 176, 160, 176, 176, 129, 53, 22, 144, 184, 32, 176, 53, 181, 5, 66, 176, 91, 97, 1, 218, 186, 4, 176, 54, 3, 96, 32, 192, 176, 193, 176, 2, 166, 176, 186, 0, 176, 253, 91, 80, 53, 181, 5, 150, 176, 91, 97, 1, 246, 186, 4, 176, 54, 3, 96, 32, 192, 176, 193, 176, 2, 195, 176, 186, 0, 176, 253, 91, 80, 53, 198, 1, 176, 1, 176, 160, 176, 176, 22, 181, 5, 177, 176, 91, 97, 2, 219, 181, 5, 204, 176, 91, 0, 91, 97, 2, 229, 181, 6, 128, 176, 91, 186, 64, 176, 81, 198, 1, 176, 1, 176, 160, 176, 176, 144, 146, 22, 130, 82, 81, 144, 129, 144, 3, 184, 32, 176, 144, 243, 91, 97, 2, 229, 181, 6, 143, 176, 91, 97, 1, 57, 181, 6, 158, 176, 91, 97, 1, 218, 186, 4, 176, 54, 3, 96, 32, 192, 176, 193, 176, 3, 39, 176, 186, 0, 176, 253, 91, 80, 53, 181, 6, 255, 176, 91, 97, 1, 218, 186, 4, 176, 54, 3, 96, 64, 192, 176, 193, 176, 3, 68, 176, 186, 0, 176, 253, 91, 80, 198, 1, 176, 1, 176, 160, 176, 176, 129, 53, 22, 144, 184, 32, 176, 53, 181, 7, 124, 176, 91, 97, 1, 218, 186, 4, 176, 54, 3, 96, 64, 192, 176, 193, 176, 3, 112, 176, 186, 0, 176, 253, 91, 80, 198, 1, 176, 1, 176, 160, 176, 176, 129, 53, 22, 144, 184, 32, 176, 53, 181, 7, 234, 176, 91, 97, 1, 57, 181, 7, 254, 176, 91, 97, 1, 57, 181, 8, 140, 176, 91, 97, 1, 246, 186, 4, 176, 54, 3, 96, 64, 192, 176, 193, 176, 3, 172, 176, 186, 0, 176, 253, 91, 80, 198, 1, 176, 1, 176, 160, 176, 176, 129, 53, 129, 22, 145, 184, 32, 176, 53, 22, 181, 8, 231, 176, 91, 97, 2, 219, 186, 4, 176, 54, 3, 96, 32, 192, 176, 193, 176, 3, 218, 176, 186, 0, 176, 253, 91, 80, 53, 198, 1, 176, 1, 176, 160, 176, 176, 22, 181, 9, 18, 176, 91, 186, 6, 176, 84, 186, 64, 176, 81, 183, 32, 176, 31, 183, 2, 176, 0, 25, 97, 1, 0, 96, 1, 136, 22, 21, 2, 1, 144, 149, 22, 148, 144, 148, 4, 147, 132, 1, 129, 144, 4, 129, 2, 130, 1, 129, 1, 144, 146, 82, 130, 129, 82, 96, 96, 147, 144, 146, 144, 145, 131, 1, 130, 130, 128, 193, 176, 4, 118, 176, 128, 96, 31, 16, 182, 4, 75, 176, 97, 1, 0, 128, 131, 84, 4, 2, 131, 82, 145, 184, 32, 176, 145, 181, 4, 118, 176, 91, 130, 1, 190, 176, 96, 0, 82, 183, 32, 176, 0, 32, 144, 91, 129, 84, 129, 82, 144, 184, 1, 176, 144, 184, 32, 176, 128, 131, 17, 182, 4, 89, 176, 130, 144, 3, 96, 31, 22, 130, 1, 145, 91, 189, 176, 189, 176, 80, 187, 176, 144, 86, 91, 96, 0, 97, 4, 148, 97, 4, 141, 181, 9, 136, 176, 91, 132, 132, 181, 9, 140, 176, 91, 80, 96, 1, 146, 191, 176, 188, 176, 91, 96, 3, 84, 144, 86, 91, 96, 0, 97, 4, 176, 132, 132, 132, 181, 10, 120, 176, 91, 97, 5, 38, 132, 97, 4, 188, 181, 9, 136, 176, 91, 97, 5, 33, 133, 96, 64, 81, 128, 184, 96, 176, 96, 64, 82, 128, 96, 40, 195, 176, 176, 32, 176, 97, 16, 14, 96, 40, 145, 57, 198, 1, 176, 1, 176, 160, 176, 176, 138, 22, 96, 0, 144, 129, 82, 183, 2, 176, 32, 82, 96, 64, 129, 32, 144, 97, 4, 250, 181, 9, 136, 176, 91, 198, 1, 176, 1, 176, 160, 176, 176, 22, 129, 82, 96, 32, 129, 1, 190, 176, 145, 82, 184, 64, 176, 96, 0, 32, 84, 190, 176, 99, 255, 255, 255, 255, 97, 11, 214, 22, 86, 91, 181, 9, 140, 176, 91, 80, 96, 1, 147, 146, 189, 176, 188, 176, 91, 96, 4, 84, 96, 255, 22, 144, 86, 91, 96, 4, 84, 96, 255, 22, 129, 86, 91, 96, 0, 97, 4, 148, 97, 5, 79, 181, 9, 136, 176, 91, 132, 97, 5, 33, 133, 183, 2, 176, 0, 97, 5, 96, 181, 9, 136, 176, 91, 198, 1, 176, 1, 176, 160, 176, 176, 144, 129, 22, 130, 82, 186, 32, 176, 131, 1, 147, 144, 147, 82, 96, 64, 145, 130, 1, 96, 0, 144, 129, 32, 145, 140, 22, 129, 82, 146, 82, 144, 32, 84, 144, 99, 255, 255, 255, 255, 97, 12, 109, 22, 86, 91, 96, 0, 97, 5, 169, 97, 5, 163, 181, 9, 136, 176, 91, 131, 181, 12, 206, 176, 91, 80, 96, 1, 178, 176, 176, 176, 91, 198, 1, 176, 1, 176, 160, 176, 176, 22, 96, 0, 144, 129, 82, 183, 1, 176, 32, 82, 96, 64, 144, 32, 84, 144, 86, 91, 97, 5, 212, 181, 9, 136, 176, 91, 96, 0, 84, 198, 1, 176, 1, 176, 160, 176, 176, 144, 129, 22, 145, 22, 20, 182, 6, 54, 176, 186, 64, 176, 81, 98, 70, 27, 205, 185, 229, 176, 129, 82, 183, 32, 176, 4, 130, 1, 129, 144, 82, 96, 36, 130, 1, 82, 127, 79, 119, 110, 97, 98, 108, 101, 58, 32, 99, 97, 108, 108, 101, 114, 32, 105, 115, 32, 110, 111, 116, 32, 116, 104, 101, 32, 111, 119, 110, 101, 114, 96, 68, 130, 1, 82, 144, 81, 144, 129, 144, 3, 184, 100, 176, 144, 253, 91, 186, 0, 176, 84, 96, 64, 81, 198, 1, 176, 1, 176, 160, 176, 176, 144, 145, 22, 144, 127, 139, 224, 7, 156, 83, 22, 89, 20, 19, 68, 205, 31, 208, 164, 242, 132, 25, 73, 127, 151, 34, 163, 218, 175, 227, 180, 24, 111, 107, 100, 87, 224, 144, 131, 144, 163, 186, 0, 176, 84, 198, 1, 176, 1, 176, 160, 176, 176, 25, 22, 144, 85, 86, 91, 96, 0, 97, 6, 138, 181, 6, 143, 176, 91, 187, 176, 144, 86, 91, 96, 0, 84, 198, 1, 176, 1, 176, 160, 176, 176, 22, 144, 86, 91, 186, 5, 176, 84, 186, 64, 176, 81, 183, 32, 176, 31, 183, 2, 176, 0, 25, 97, 1, 0, 96, 1, 136, 22, 21, 2, 1, 144, 149, 22, 148, 144, 148, 4, 147, 132, 1, 129, 144, 4, 129, 2, 130, 1, 129, 1, 144, 146, 82, 130, 129, 82, 96, 96, 147, 144, 146, 144, 145, 131, 1, 130, 130, 128, 193, 176, 4, 118, 176, 128, 96, 31, 16, 182, 4, 75, 176, 97, 1, 0, 128, 131, 84, 4, 2, 131, 82, 145, 184, 32, 176, 145, 181, 4, 118, 176, 91, 96, 0, 97, 7, 9, 181, 9, 136, 176, 91, 96, 0, 84, 198, 1, 176, 1, 176, 160, 176, 176, 144, 129, 22, 145, 22, 20, 182, 7, 107, 176, 186, 64, 176, 81, 98, 70, 27, 205, 185, 229, 176, 129, 82, 183, 32, 176, 4, 130, 1, 129, 144, 82, 96, 36, 130, 1, 82, 127, 79, 119, 110, 97, 98, 108, 101, 58, 32, 99, 97, 108, 108, 101, 114, 32, 105, 115, 32, 110, 111, 116, 32, 116, 104, 101, 32, 111, 119, 110, 101, 114, 96, 68, 130, 1, 82, 144, 81, 144, 129, 144, 3, 184, 100, 176, 144, 253, 91, 97, 5, 169, 97, 7, 118, 181, 9, 136, 176, 91, 131, 181, 13, 202, 176, 91, 96, 0, 97, 4, 148, 97, 7, 137, 181, 9, 136, 176, 91, 132, 97, 5, 33, 133, 96, 64, 81, 128, 184, 96, 176, 96, 64, 82, 128, 96, 37, 195, 176, 176, 32, 176, 97, 16, 127, 96, 37, 145, 57, 183, 2, 176, 0, 97, 7, 179, 181, 9, 136, 176, 91, 198, 1, 176, 1, 176, 160, 176, 176, 144, 129, 22, 130, 82, 186, 32, 176, 131, 1, 147, 144, 147, 82, 96, 64, 145, 130, 1, 96, 0, 144, 129, 32, 145, 141, 22, 129, 82, 146, 82, 144, 32, 84, 190, 176, 99, 255, 255, 255, 255, 97, 11, 214, 22, 86, 91, 96, 0, 97, 4, 148, 97, 7, 247, 181, 9, 136, 176, 91, 132, 132, 181, 10, 120, 176, 91, 186, 5, 176, 84, 186, 64, 176, 81, 183, 32, 176, 2, 96, 1, 133, 22, 194, 176, 1, 0, 2, 96, 0, 25, 1, 144, 148, 22, 147, 144, 147, 4, 96, 31, 129, 1, 132, 144, 4, 132, 2, 130, 1, 132, 1, 144, 146, 82, 129, 129, 82, 146, 145, 131, 1, 130, 130, 128, 193, 176, 8, 132, 176, 128, 96, 31, 16, 182, 8, 89, 176, 97, 1, 0, 128, 131, 84, 4, 2, 131, 82, 145, 184, 32, 176, 145, 181, 8, 132, 176, 91, 130, 1, 190, 176, 96, 0, 82, 183, 32, 176, 0, 32, 144, 91, 129, 84, 129, 82, 144, 184, 1, 176, 144, 184, 32, 176, 128, 131, 17, 182, 8, 103, 176, 130, 144, 3, 96, 31, 22, 130, 1, 145, 91, 189, 176, 189, 176, 80, 129, 86, 91, 186, 6, 176, 84, 186, 64, 176, 81, 183, 32, 176, 2, 96, 1, 133, 22, 194, 176, 1, 0, 2, 96, 0, 25, 1, 144, 148, 22, 147, 144, 147, 4, 96, 31, 129, 1, 132, 144, 4, 132, 2, 130, 1, 132, 1, 144, 146, 82, 129, 129, 82, 146, 145, 131, 1, 130, 130, 128, 193, 176, 8, 132, 176, 128, 96, 31, 16, 182, 8, 89, 176, 97, 1, 0, 128, 131, 84, 4, 2, 131, 82, 145, 184, 32, 176, 145, 181, 8, 132, 176, 91, 198, 1, 176, 1, 176, 160, 176, 176, 145, 130, 22, 96, 0, 144, 129, 82, 183, 2, 176, 32, 144, 129, 82, 186, 64, 176, 131, 32, 147, 144, 148, 22, 130, 82, 190, 176, 145, 82, 32, 84, 144, 86, 91, 97, 9, 26, 181, 9, 136, 176, 91, 96, 0, 84, 198, 1, 176, 1, 176, 160, 176, 176, 144, 129, 22, 145, 22, 20, 182, 9, 124, 176, 186, 64, 176, 81, 98, 70, 27, 205, 185, 229, 176, 129, 82, 183, 32, 176, 4, 130, 1, 129, 144, 82, 96, 36, 130, 1, 82, 127, 79, 119, 110, 97, 98, 108, 101, 58, 32, 99, 97, 108, 108, 101, 114, 32, 105, 115, 32, 110, 111, 116, 32, 116, 104, 101, 32, 111, 119, 110, 101, 114, 96, 68, 130, 1, 82, 144, 81, 144, 129, 144, 3, 184, 100, 176, 144, 253, 91, 97, 9, 133, 129, 181, 14, 188, 176, 91, 188, 176, 91, 51, 144, 86, 91, 198, 1, 176, 1, 176, 160, 176, 176, 131, 22, 182, 9, 209, 176, 96, 64, 81, 98, 70, 27, 205, 185, 229, 176, 195, 176, 176, 4, 176, 128, 128, 184, 32, 176, 130, 129, 3, 130, 82, 96, 36, 195, 176, 176, 32, 176, 128, 97, 15, 196, 96, 36, 145, 57, 184, 64, 176, 191, 176, 80, 96, 64, 81, 128, 145, 3, 144, 253, 91, 198, 1, 176, 1, 176, 160, 176, 176, 130, 22, 182, 10, 22, 176, 96, 64, 81, 98, 70, 27, 205, 185, 229, 176, 195, 176, 176, 4, 176, 128, 128, 184, 32, 176, 130, 129, 3, 130, 82, 96, 34, 195, 176, 176, 32, 176, 128, 97, 16, 231, 96, 34, 145, 57, 184, 64, 176, 191, 176, 80, 96, 64, 81, 128, 145, 3, 144, 253, 91, 198, 1, 176, 1, 176, 160, 176, 176, 128, 132, 22, 96, 0, 129, 129, 82, 183, 2, 176, 32, 144, 129, 82, 186, 64, 176, 131, 32, 148, 135, 22, 128, 132, 82, 148, 130, 82, 145, 130, 144, 32, 133, 144, 85, 129, 81, 133, 129, 82, 145, 81, 127, 140, 91, 225, 229, 235, 236, 125, 91, 209, 79, 113, 66, 125, 30, 132, 243, 221, 3, 20, 192, 247, 178, 41, 30, 91, 32, 10, 200, 199, 195, 185, 37, 146, 129, 144, 3, 144, 145, 1, 144, 163, 189, 176, 188, 176, 91, 198, 1, 176, 1, 176, 160, 176, 176, 131, 22, 182, 10, 189, 176, 96, 64, 81, 98, 70, 27, 205, 185, 229, 176, 195, 176, 176, 4, 176, 128, 128, 184, 32, 176, 130, 129, 3, 130, 82, 96, 37, 195, 176, 176, 32, 176, 128, 97, 15, 159, 96, 37, 145, 57, 184, 64, 176, 191, 176, 80, 96, 64, 81, 128, 145, 3, 144, 253, 91, 198, 1, 176, 1, 176, 160, 176, 176, 130, 22, 182, 11, 2, 176, 96, 64, 81, 98, 70, 27, 205, 185, 229, 176, 195, 176, 176, 4, 176, 128, 128, 184, 32, 176, 130, 129, 3, 130, 82, 96, 35, 195, 176, 176, 32, 176, 128, 97, 16, 92, 96, 35, 145, 57, 184, 64, 176, 191, 176, 80, 96, 64, 81, 128, 145, 3, 144, 253, 91, 97, 11, 69, 129, 96, 64, 81, 128, 184, 96, 176, 96, 64, 82, 128, 96, 38, 195, 176, 176, 32, 176, 97, 16, 54, 96, 38, 145, 57, 198, 1, 176, 1, 176, 160, 176, 176, 134, 22, 96, 0, 144, 129, 82, 183, 1, 176, 32, 82, 96, 64, 144, 32, 84, 190, 176, 99, 255, 255, 255, 255, 97, 11, 214, 22, 86, 91, 198, 1, 176, 1, 176, 160, 176, 176, 128, 133, 22, 96, 0, 144, 129, 82, 183, 1, 176, 32, 82, 186, 64, 176, 130, 32, 147, 144, 147, 85, 144, 132, 22, 129, 82, 32, 84, 97, 11, 122, 144, 130, 99, 255, 255, 255, 255, 97, 12, 109, 22, 86, 91, 198, 1, 176, 1, 176, 160, 176, 176, 128, 132, 22, 96, 0, 129, 129, 82, 183, 1, 176, 32, 144, 129, 82, 96, 64, 145, 130, 144, 32, 148, 144, 148, 85, 128, 81, 133, 129, 82, 144, 81, 145, 147, 146, 135, 22, 146, 127, 221, 242, 82, 173, 27, 226, 200, 155, 105, 194, 176, 104, 252, 55, 141, 170, 149, 43, 167, 241, 99, 196, 161, 22, 40, 245, 90, 77, 245, 35, 179, 239, 146, 145, 130, 144, 3, 1, 144, 163, 189, 176, 188, 176, 91, 96, 0, 129, 132, 132, 17, 193, 176, 12, 101, 176, 96, 64, 81, 98, 70, 27, 205, 185, 229, 176, 195, 176, 176, 4, 176, 128, 128, 184, 32, 176, 130, 129, 3, 130, 82, 131, 129, 129, 81, 195, 176, 176, 32, 176, 191, 176, 128, 81, 144, 184, 32, 176, 144, 128, 131, 131, 96, 0, 91, 131, 192, 176, 193, 176, 12, 42, 176, 129, 129, 1, 81, 131, 130, 1, 82, 184, 32, 176, 181, 12, 18, 176, 91, 189, 176, 189, 176, 187, 176, 144, 129, 1, 144, 96, 31, 22, 128, 193, 176, 12, 87, 176, 128, 130, 3, 128, 81, 96, 1, 131, 96, 32, 3, 97, 1, 0, 10, 3, 25, 22, 195, 176, 176, 32, 176, 191, 176, 91, 80, 146, 189, 176, 80, 96, 64, 81, 128, 145, 3, 144, 253, 91, 189, 176, 80, 144, 3, 144, 86, 91, 96, 0, 130, 130, 1, 131, 192, 176, 193, 176, 12, 199, 176, 186, 64, 176, 81, 98, 70, 27, 205, 185, 229, 176, 129, 82, 183, 32, 176, 4, 130, 1, 82, 183, 27, 176, 36, 130, 1, 82, 127, 83, 97, 102, 101, 77, 97, 116, 104, 58, 32, 97, 100, 100, 105, 116, 105, 111, 110, 32, 111, 118, 101, 114, 102, 108, 111, 119, 0, 0, 0, 0, 0, 96, 68, 130, 1, 82, 144, 81, 144, 129, 144, 3, 184, 100, 176, 144, 253, 91, 147, 146, 189, 176, 188, 176, 91, 198, 1, 176, 1, 176, 160, 176, 176, 130, 22, 182, 13, 19, 176, 96, 64, 81, 98, 70, 27, 205, 185, 229, 176, 195, 176, 176, 4, 176, 128, 128, 184, 32, 176, 130, 129, 3, 130, 82, 96, 33, 195, 176, 176, 32, 176, 128, 97, 16, 164, 96, 33, 145, 57, 184, 64, 176, 191, 176, 80, 96, 64, 81, 128, 145, 3, 144, 253, 91, 97, 13, 86, 129, 96, 64, 81, 128, 184, 96, 176, 96, 64, 82, 128, 96, 34, 195, 176, 176, 32, 176, 97, 16, 197, 96, 34, 145, 57, 198, 1, 176, 1, 176, 160, 176, 176, 133, 22, 96, 0, 144, 129, 82, 183, 1, 176, 32, 82, 96, 64, 144, 32, 84, 190, 176, 99, 255, 255, 255, 255, 97, 11, 214, 22, 86, 91, 198, 1, 176, 1, 176, 160, 176, 176, 131, 22, 96, 0, 144, 129, 82, 183, 1, 176, 32, 82, 96, 64, 144, 32, 85, 96, 3, 84, 97, 13, 130, 144, 130, 99, 255, 255, 255, 255, 97, 15, 92, 22, 86, 91, 96, 3, 85, 186, 64, 176, 81, 130, 129, 82, 144, 81, 96, 0, 145, 198, 1, 176, 1, 176, 160, 176, 176, 133, 22, 145, 127, 221, 242, 82, 173, 27, 226, 200, 155, 105, 194, 176, 104, 252, 55, 141, 170, 149, 43, 167, 241, 99, 196, 161, 22, 40, 245, 90, 77, 245, 35, 179, 239, 145, 129, 144, 3, 184, 32, 176, 144, 163, 189, 176, 86, 91, 198, 1, 176, 1, 176, 160, 176, 176, 130, 22, 182, 14, 37, 176, 186, 64, 176, 81, 98, 70, 27, 205, 185, 229, 176, 129, 82, 183, 32, 176, 4, 130, 1, 82, 183, 31, 176, 36, 130, 1, 82, 127, 66, 69, 80, 50, 48, 58, 32, 109, 105, 110, 116, 32, 116, 111, 32, 116, 104, 101, 32, 122, 101, 114, 111, 32, 97, 100, 100, 114, 101, 115, 115, 0, 96, 68, 130, 1, 82, 144, 81, 144, 129, 144, 3, 184, 100, 176, 144, 253, 91, 96, 3, 84, 97, 14, 56, 144, 130, 99, 255, 255, 255, 255, 97, 12, 109, 22, 86, 91, 96, 3, 85, 198, 1, 176, 1, 176, 160, 176, 176, 130, 22, 96, 0, 144, 129, 82, 183, 1, 176, 32, 82, 96, 64, 144, 32, 84, 97, 14, 100, 144, 130, 99, 255, 255, 255, 255, 97, 12, 109, 22, 86, 91, 198, 1, 176, 1, 176, 160, 176, 176, 131, 22, 96, 0, 129, 129, 82, 183, 1, 176, 32, 144, 129, 82, 186, 64, 176, 131, 32, 148, 144, 148, 85, 131, 81, 133, 129, 82, 147, 81, 146, 147, 145, 146, 127, 221, 242, 82, 173, 27, 226, 200, 155, 105, 194, 176, 104, 252, 55, 141, 170, 149, 43, 167, 241, 99, 196, 161, 22, 40, 245, 90, 77, 245, 35, 179, 239, 146, 129, 144, 3, 144, 145, 1, 144, 163, 189, 176, 86, 91, 198, 1, 176, 1, 176, 160, 176, 176, 129, 22, 182, 15, 1, 176, 96, 64, 81, 98, 70, 27, 205, 185, 229, 176, 195, 176, 176, 4, 176, 128, 128, 184, 32, 176, 130, 129, 3, 130, 82, 96, 38, 195, 176, 176, 32, 176, 128, 97, 15, 232, 96, 38, 145, 57, 184, 64, 176, 191, 176, 80, 96, 64, 81, 128, 145, 3, 144, 253, 91, 186, 0, 176, 84, 96, 64, 81, 198, 1, 176, 1, 176, 160, 176, 176, 128, 133, 22, 147, 146, 22, 145, 127, 139, 224, 7, 156, 83, 22, 89, 20, 19, 68, 205, 31, 208, 164, 242, 132, 25, 73, 127, 151, 34, 163, 218, 175, 227, 180, 24, 111, 107, 100, 87, 224, 145, 163, 186, 0, 176, 84, 198, 1, 176, 1, 176, 160, 176, 176, 25, 22, 198, 1, 176, 1, 176, 160, 176, 176, 146, 144, 146, 22, 190, 176, 145, 23, 144, 85, 86, 91, 96, 0, 97, 12, 199, 131, 131, 96, 64, 81, 128, 184, 64, 176, 96, 64, 82, 128, 96, 30, 195, 176, 176, 32, 176, 127, 83, 97, 102, 101, 77, 97, 116, 104, 58, 32, 115, 117, 98, 116, 114, 97, 99, 116, 105, 111, 110, 32, 111, 118, 101, 114, 102, 108, 111, 119, 0, 0, 129, 82, 80, 181, 11, 214, 176, 254, 66, 69, 80, 50, 48, 58, 32, 116, 114, 97, 110, 115, 102, 101, 114, 32, 102, 114, 111, 109, 32, 116, 104, 101, 32, 122, 101, 114, 111, 32, 97, 100, 100, 114, 101, 115, 115, 66, 69, 80, 50, 48, 58, 32, 97, 112, 112, 114, 111, 118, 101, 32, 102, 114, 111, 109, 32, 116, 104, 101, 32, 122, 101, 114, 111, 32, 97, 100, 100, 114, 101, 115, 115, 79, 119, 110, 97, 98, 108, 101, 58, 32, 110, 101, 119, 32, 111, 119, 110, 101, 114, 32, 105, 115, 32, 116, 104, 101, 32, 122, 101, 114, 111, 32, 97, 100, 100, 114, 101, 115, 115, 66, 69, 80, 50, 48, 58, 32, 116, 114, 97, 110, 115, 102, 101, 114, 32, 97, 109, 111, 117, 110, 116, 32, 101, 120, 99, 101, 101, 100, 115, 32, 97, 108, 108, 111, 119, 97, 110, 99, 101, 66, 69, 80, 50, 48, 58, 32, 116, 114, 97, 110, 115, 102, 101, 114, 32, 97, 109, 111, 117, 110, 116, 32, 101, 120, 99, 101, 101, 100, 115, 32, 98, 97, 108, 97, 110, 99, 101, 66, 69, 80, 50, 48, 58, 32, 116, 114, 97, 110, 115, 102, 101, 114, 32, 116, 111, 32, 116, 104, 101, 32, 122, 101, 114, 111, 32, 97, 100, 100, 114, 101, 115, 115, 66, 69, 80, 50, 48, 58, 32, 100, 101, 99, 114, 101, 97, 115, 101, 100, 32, 97, 108, 108, 111, 119, 97, 110, 99, 101, 32, 98, 101, 108, 111, 119, 32, 122, 101, 114, 111, 66, 69, 80, 50, 48, 58, 32, 98, 117, 114, 110, 32, 102, 114, 111, 109, 32, 116, 104, 101, 32, 122, 101, 114, 111, 32, 97, 100, 100, 114, 101, 115, 115, 66, 69, 80, 50, 48, 58, 32, 98, 117, 114, 110, 32, 97, 109, 111, 117, 110, 116, 32, 101, 120, 99, 101, 101, 100, 115, 32, 98, 97, 108, 97, 110, 99, 101, 66, 69, 80, 50, 48, 58, 32, 97, 112, 112, 114, 111, 118, 101, 32, 116, 111, 32, 116, 104, 101, 32, 122, 101, 114, 111, 32, 97, 100, 100, 114, 101, 115, 115, 162, 101, 98, 122, 122, 114, 49, 88, 32, 203, 189, 87, 10, 228, 120, 246, 183, 171, 249, 201, 165, 200, 198, 136, 76, 243, 246, 77, 222, 215, 79, 126, 195, 233, 182, 208, 180, 17, 34, 234, 255, 100, 115, 111, 108, 99, 67, 0, 5, 16, 0, 50}
