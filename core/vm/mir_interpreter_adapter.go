package vm

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/tracing"
	coretypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// warmSlotKey identifies a warmed storage slot for the current contract address
type warmSlotKey struct {
	addr [20]byte
	slot [32]byte
}

// mirGasProbe is an optional test hook to observe MIR gas after each instruction
var mirGasProbe func(pc uint64, op byte, gasLeft uint64)

// mirGasTimingHook, when set (testing only), receives time spent inside the
// adapter's pre-op hook (i.e., gas accounting for the originating EVM opcode).
var mirGasTimingHook func(pc uint64, op byte, dur time.Duration)

// SetMIRGasTimingHook installs a callback to observe MIR gas calculation time per-op (testing only).
func SetMIRGasTimingHook(cb func(pc uint64, op byte, dur time.Duration)) { mirGasTimingHook = cb }

// SetMIRGasProbe installs a callback to observe MIR gas after each instruction (testing only)
func SetMIRGasProbe(cb func(pc uint64, op byte, gasLeft uint64)) {
	mirGasProbe = cb
}

// MIRInterpreterAdapter adapts MIRInterpreter to work with EVM's interpreter interface
type MIRInterpreterAdapter struct {
	evm             *EVM
	mirInterpreter  *compiler.MIRInterpreter
	currentSelf     common.Address
	currentContract *Contract
	table           *JumpTable
	memShadow       *Memory
	// Warm caches to avoid repeated EIP-2929 checks within a single Run
	warmAccounts map[[20]byte]struct{}
	warmSlots    map[warmSlotKey]struct{}
	// storageCache caches SLOAD values within a single Run (key is 32-byte slot)
	storageCache map[[32]byte][32]byte
	// Track block entry gas charges per block (for GAS opcode to add back)
	blockEntryGasCharges map[*compiler.MIRBasicBlock]uint64
	// Current block being executed (for GAS opcode to know which block entry charges to add back)
	currentBlock *compiler.MIRBasicBlock
	// Track whether the last executed opcode was a control transfer (JUMP/JUMPI)
	lastWasJump bool
	// Dedup JUMPDEST charge at first instruction of landing block
	lastJdPC           uint32
	lastJdBlockFirstPC uint32
}

// countOpcodesInRange counts EVM opcodes in a given PC range
func countOpcodesInRange(code []byte, firstPC, lastPC uint) map[byte]uint32 {
	counts := make(map[byte]uint32)
	if code == nil || firstPC >= uint(len(code)) {
		return counts
	}
	// If lastPC is 0 or invalid, find the next block boundary
	if lastPC == 0 || lastPC <= firstPC || lastPC > uint(len(code)) {
		// Find the next JUMPDEST or block terminator after firstPC
		pc := firstPC
		for pc < uint(len(code)) {
			op := OpCode(code[pc])
			// If we hit a JUMPDEST, this is likely the start of the next block
			// (unless it's the first instruction, in which case it's the current block)
			if op == JUMPDEST && pc > firstPC {
				lastPC = pc
				break
			}
			// If we hit a block terminator (other than JUMPDEST), include it and stop
			if op == STOP || op == RETURN || op == REVERT || op == SELFDESTRUCT {
				lastPC = pc
				break
			}
			// JUMP and JUMPI also terminate blocks (include them)
			if op == JUMP || op == JUMPI {
				lastPC = pc
				break
			}
			// Skip PUSH opcodes
			if op >= PUSH1 && op <= PUSH32 {
				pushSize := int(op - PUSH1 + 1)
				pc += uint(pushSize) + 1
			} else {
				pc++
			}
			// Safety: if we've gone too far, use end of code
			if pc > uint(len(code)) {
				lastPC = uint(len(code))
				break
			}
		}
		// If we didn't find a boundary, use end of code
		if lastPC == 0 || lastPC <= firstPC {
			lastPC = uint(len(code))
		}
	}
	pc := firstPC
	for pc <= lastPC && pc < uint(len(code)) {
		op := OpCode(code[pc])
		// Count the opcode
		counts[byte(op)]++
		// Skip PUSH opcodes (they have data bytes)
		if op >= PUSH1 && op <= PUSH32 {
			pushSize := int(op - PUSH1 + 1)
			pc += uint(pushSize) + 1
		} else {
			pc++
		}
	}
	return counts
}

// NewMIRInterpreterAdapter creates a new MIR interpreter adapter for EVM
func NewMIRInterpreterAdapter(evm *EVM) *MIRInterpreterAdapter {
	// Create adapter early so closures can reference cached fields
	adapter := &MIRInterpreterAdapter{evm: evm}

	// Create MIR execution environment from EVM context
	env := &compiler.MIRExecutionEnv{
		Memory:      make([]byte, 0, 1024),
		Storage:     make(map[[32]byte][32]byte),
		BlockNumber: evm.Context.BlockNumber.Uint64(),
		Timestamp:   evm.Context.Time,
		ChainID:     evm.ChainConfig().ChainID.Uint64(),
		GasPrice:    0, // Will be set from transaction context
		BaseFee:     0, // Will be set from block context
		SelfBalance: 0, // Will be set from contract context
	}

	// Set values from contexts if available
	if evm.Context.BaseFee != nil {
		env.BaseFee = evm.Context.BaseFee.Uint64()
	}

	// Install runtime linkage hooks once; they read dynamic data from env/evm
	env.SLoadFunc = func(key [32]byte) [32]byte {
		// Use cached currentSelf to avoid per-access address conversions
		val := evm.StateDB.GetState(adapter.currentSelf, common.BytesToHash(key[:]))
		var out [32]byte
		copy(out[:], val[:])
		return out
	}
	env.SStoreFunc = func(key [32]byte, value [32]byte) {
		evm.StateDB.SetState(adapter.currentSelf, common.BytesToHash(key[:]), common.BytesToHash(value[:]))
	}
	env.TLoadFunc = func(key [32]byte) [32]byte {
		// Transient storage (EIP-1153)
		return evm.StateDB.GetTransientState(adapter.currentSelf, common.BytesToHash(key[:]))
	}
	env.TStoreFunc = func(key [32]byte, value [32]byte) {
		// Transient storage (EIP-1153)
		evm.StateDB.SetTransientState(adapter.currentSelf, common.BytesToHash(key[:]), common.BytesToHash(value[:]))
	}
	env.GetBalanceFunc = func(addr20 [20]byte) *uint256.Int {
		addr := common.BytesToAddress(addr20[:])
		b := evm.StateDB.GetBalance(addr)
		if b == nil {
			return uint256.NewInt(0)
		}
		return new(uint256.Int).Set(b)
	}

	adapter.mirInterpreter = compiler.NewMIRInterpreter(env)
	// Install pre-op hook once; it will read the current contract from adapter.currentContract
	adapter.mirInterpreter.SetBeforeOpHook(func(ctx *compiler.MIRPreOpContext) error {
		contract := adapter.currentContract
		if ctx == nil || ctx.M == nil || contract == nil {
			return nil
		}
		var timingStart time.Time
		if mirGasTimingHook != nil {
			timingStart = time.Now()
		}
		// The following body mirrors the per-run innerHook logic
		evmOp := OpCode(ctx.EvmOp)
		// Call EVM tracer for all MIR instructions that correspond to EVM opcodes (not PHI, not NOP)
		if adapter.evm != nil && adapter.evm.Config.Tracer != nil && adapter.evm.Config.Tracer.OnOpcode != nil {
			// Only trace actual EVM opcodes, not internal MIR operations like PHI
			if ctx.M.Op() != compiler.MirPHI && ctx.M.Op() != compiler.MirNOP {
				scope := &ScopeContext{Memory: adapter.memShadow, Stack: nil, Contract: contract}
				adapter.evm.Config.Tracer.OnOpcode(uint64(ctx.M.EvmPC()), byte(evmOp), contract.Gas, 0, scope, nil, adapter.evm.depth, nil)
			}
		}
		// Block entry gas charging is handled by the per-run innerHook (installed in Run method)
		// This hook only handles tracing and dynamic gas
		// Constant gas is charged at block entry, so we don't charge it per instruction
		// Dynamic gas will still be charged per instruction in the switch statement below
		if adapter.memShadow == nil {
			adapter.memShadow = NewMemory()
		}
		resizeShadow := func(sz uint64) {
			if sz > 0 {
				adapter.memShadow.Resize(sz)
			}
		}
		toWord := func(x uint64) uint64 { return (x + 31) / 32 }
		switch evmOp {
		case SLOAD:
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					st := newstack()
					defer returnStack(st)
					st.push(ctx.Operands[0])
					gas, err := gasSLoadEIP2929(adapter.evm, contract, st, adapter.memShadow, 0)
					if err != nil {
						if errors.Is(err, ErrGasUintOverflow) {
							err = nil
						}
						if err != nil {
							return err
						}
					}
					if gas > 0 {
						if contract.Gas < gas {
							return ErrOutOfGas
						}
						contract.Gas -= gas
					}
				}
			}
		case BALANCE, EXTCODESIZE, EXTCODEHASH:
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					st := newstack()
					defer returnStack(st)
					st.push(ctx.Operands[0])
					gas, err := gasEip2929AccountCheck(adapter.evm, contract, st, adapter.memShadow, 0)
					if err != nil {
						if errors.Is(err, ErrGasUintOverflow) {
							err = nil
						}
						if err != nil {
							return err
						}
					}
					if gas > 0 {
						if contract.Gas < gas {
							return ErrOutOfGas
						}
						contract.Gas -= gas
					}
				}
			}
		case EXP:
			if len(ctx.Operands) >= 2 {
				exp := ctx.Operands[1]
				expBytes := uint64((exp.BitLen() + 7) / 8)
				perByte := params.ExpByteFrontier
				if adapter.evm.chainRules.IsEIP158 {
					perByte = params.ExpByteEIP158
				}
				add := params.ExpGas + perByte*expBytes
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
			}
		case MLOAD, MSTORE, MSTORE8, RETURN, REVERT, CREATE, CREATE2:
			// Calculate the required memory size for the operation
			var needed uint64
			switch evmOp {
			case MLOAD, MSTORE:
				if len(ctx.Operands) > 0 {
					off := ctx.Operands[0].Uint64()
					needed = off + 32
				}
			case MSTORE8:
				if len(ctx.Operands) > 0 {
					off := ctx.Operands[0].Uint64()
					needed = off + 1
				}
			case RETURN, REVERT:
				if len(ctx.Operands) >= 2 {
					off := ctx.Operands[0].Uint64()
					size := ctx.Operands[1].Uint64()
					needed = off + size
				}
			case CREATE, CREATE2:
				// CREATE: value, offset, size
				// CREATE2: value, offset, size, salt
				if len(ctx.Operands) >= 3 {
					off := ctx.Operands[1].Uint64()
					size := ctx.Operands[2].Uint64()
					needed = off + size
				}
			}

			// Round up to 32 bytes
			memSize := (needed + 31) / 32 * 32
			if memSize < needed { // overflow check
				return ErrGasUintOverflow
			}

			if memSize > uint64(adapter.memShadow.Len()) {
				gas, err := memoryGasCost(adapter.memShadow, memSize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					}
					if err != nil {
						return err
					}
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
				if evmOp == CREATE || evmOp == CREATE2 {
					if len(ctx.Operands) >= 3 {
						size := ctx.Operands[2].Uint64()
						if size > params.MaxInitCodeSize {
							return fmt.Errorf("%w: size %d", ErrMaxInitCodeSizeExceeded, size)
						}
						more := params.InitCodeWordGas * toWord(size)
						if contract.Gas < more {
							return ErrOutOfGas
						}
						contract.Gas -= more
					}
				}
				resizeShadow(memSize)
				// Pre-size MIR interpreter memory to move resize cost out of handler
				adapter.mirInterpreter.EnsureMemorySize(memSize)
			}
		case CALLDATACOPY, CODECOPY, RETURNDATACOPY:
			// Always charge copy gas per word
			var size uint64
			if len(ctx.Operands) >= 3 {
				size = ctx.Operands[2].Uint64()
			}
			copyGas := toWord(size) * params.CopyGas
			if contract.Gas < copyGas {
				return ErrOutOfGas
			}
			contract.Gas -= copyGas

			if ctx.MemorySize > uint64(adapter.memShadow.Len()) {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
				resizeShadow(ctx.MemorySize)
				adapter.mirInterpreter.EnsureMemorySize(ctx.MemorySize)
			}
		case EXTCODECOPY:
			// Always charge copy gas per word
			var size uint64
			if len(ctx.Operands) >= 4 {
				size = ctx.Operands[3].Uint64()
			}
			copyGas := toWord(size) * params.CopyGas
			if contract.Gas < copyGas {
				return ErrOutOfGas
			}
			contract.Gas -= copyGas

			if ctx.MemorySize > uint64(adapter.memShadow.Len()) {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
				resizeShadow(ctx.MemorySize)
				adapter.mirInterpreter.EnsureMemorySize(ctx.MemorySize)
			}
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					var a [20]byte
					b := ctx.Operands[0].Bytes20()
					copy(a[:], b[:])
					if _, ok := adapter.warmAccounts[a]; !ok {
						st := newstack()
						defer returnStack(st)
						st.push(ctx.Operands[0])
						gas, err := gasEip2929AccountCheck(adapter.evm, contract, st, adapter.memShadow, 0)
						if err != nil {
							if errors.Is(err, ErrGasUintOverflow) {
								err = nil
							}
							if err != nil {
								return err
							}
						}
						if gas > 0 {
							if contract.Gas < gas {
								return ErrOutOfGas
							}
							contract.Gas -= gas
							adapter.warmAccounts[a] = struct{}{}
						}
					}
				}
			}
		case MCOPY:
			// Always charge copy gas per word
			var size uint64
			if len(ctx.Operands) >= 3 {
				size = ctx.Operands[2].Uint64()
			}
			copyGas := toWord(size) * params.CopyGas
			if contract.Gas < copyGas {
				return ErrOutOfGas
			}
			contract.Gas -= copyGas

			if ctx.MemorySize > uint64(adapter.memShadow.Len()) {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
				resizeShadow(ctx.MemorySize)
				adapter.mirInterpreter.EnsureMemorySize(ctx.MemorySize)
			}
		case KECCAK256:
			if ctx.MemorySize > uint64(adapter.memShadow.Len()) {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						// align with base interpreter: don't surface overflow here
					} else {
						return err
					}
				}
				size := ctx.Length
				if size == 0 && len(ctx.Operands) >= 2 {
					size = ctx.Operands[1].Uint64()
				}
				wordGas := toWord(size) * params.Keccak256WordGas
				add := gas + wordGas
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
				resizeShadow(ctx.MemorySize)
				adapter.mirInterpreter.EnsureMemorySize(ctx.MemorySize)
			} else {
				// No growth: only charge per-word keccak cost
				size := ctx.Length
				if size == 0 && len(ctx.Operands) >= 2 {
					size = ctx.Operands[1].Uint64()
				}
				wordGas := toWord(size) * params.Keccak256WordGas
				if contract.Gas < wordGas {
					return ErrOutOfGas
				}
				contract.Gas -= wordGas
			}
		case LOG0, LOG1, LOG2, LOG3, LOG4:
			if ctx.MemorySize > 0 {
				gas, err := memoryGasCost(adapter.memShadow, ctx.MemorySize)
				if err != nil {
					return err
				}
				n := int(evmOp - LOG0)
				add := gas + uint64(n)*params.LogTopicGas
				var size uint64
				if len(ctx.Operands) >= 2 {
					size = ctx.Operands[1].Uint64()
				}
				add += size * params.LogDataGas
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
				resizeShadow(ctx.MemorySize)
				adapter.mirInterpreter.EnsureMemorySize(ctx.MemorySize)
			}
		case SELFDESTRUCT:
			var gas uint64
			var err error
			if adapter.evm.chainRules.IsLondon {
				st := newstack()
				defer returnStack(st)
				gas, err = gasSelfdestructEIP3529(adapter.evm, contract, st, adapter.memShadow, 0)
			} else if adapter.evm.chainRules.IsBerlin {
				st := newstack()
				defer returnStack(st)
				gas, err = gasSelfdestructEIP2929(adapter.evm, contract, st, adapter.memShadow, 0)
			}
			if err != nil {
				return err
			}
			if gas > 0 {
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
			}
		case SSTORE:
			if len(ctx.Operands) >= 2 {
				st := newstack()
				defer returnStack(st)
				st.push(ctx.Operands[1])
				st.push(ctx.Operands[0])
				gas, err := gasSStore(adapter.evm, contract, st, adapter.memShadow, 0)
				if err != nil {
					return err
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
			}
		case CALL, CALLCODE, DELEGATECALL, STATICCALL:
			// Use vm gas calculators to set evm.callGasTemp and deduct dynamic gas
			// Build stack so Back(0)=requestedGas, Back(1)=addr, Back(2)=value (if present)
			st := newstack()
			// Ensure ctx.Operands length checks per variant
			var memSize uint64
			// Calculate required memory size
			switch evmOp {
			case CALL, CALLCODE:
				if len(ctx.Operands) < 7 {
					return nil
				}
				// args: [3] [4] -> [3]+[4]
				// ret: [5] [6] -> [5]+[6]
				argsOff := ctx.Operands[3].Uint64()
				argsSize := ctx.Operands[4].Uint64()
				retOff := ctx.Operands[5].Uint64()
				retSize := ctx.Operands[6].Uint64()
				m1 := argsOff + argsSize
				m2 := retOff + retSize
				needed := m1
				if m2 > m1 {
					needed = m2
				}
				memSize = (needed + 31) / 32 * 32
				if memSize < needed {
					return ErrGasUintOverflow
				}

				st.push(ctx.Operands[2])
				st.push(ctx.Operands[1])
				st.push(ctx.Operands[0])
				var dyn uint64
				var err error
				hadOverflow := false
				if adapter.evm.chainRules.IsBerlin {
					if evmOp == CALL {
						dyn, err = gasCallEIP2929(adapter.evm, contract, st, adapter.memShadow, memSize)
					} else {
						dyn, err = gasCallCodeEIP2929(adapter.evm, contract, st, adapter.memShadow, memSize)
					}
				} else {
					if evmOp == CALL {
						dyn, err = gasCall(adapter.evm, contract, st, adapter.memShadow, memSize)
					} else {
						dyn, err = gasCallCode(adapter.evm, contract, st, adapter.memShadow, memSize)
					}
				}
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						// Match stock interpreter: do not surface overflow from call gas calculation here
						// Effective call stipend/cap handling happens later in the call path
						hadOverflow = true
						err = nil
					}
					if err != nil {
						return err
					}
				}
				if contract.Gas < dyn {
					return ErrOutOfGas
				}
				contract.Gas -= dyn
				if !hadOverflow && memSize > 0 {
					resizeShadow(memSize)
					adapter.mirInterpreter.EnsureMemorySize(memSize)
				}
			case DELEGATECALL, STATICCALL:
				if len(ctx.Operands) < 6 {
					return nil
				}
				// args: [2] [3] -> [2]+[3]
				// ret: [4] [5] -> [4]+[5]
				argsOff := ctx.Operands[2].Uint64()
				argsSize := ctx.Operands[3].Uint64()
				retOff := ctx.Operands[4].Uint64()
				retSize := ctx.Operands[5].Uint64()
				m1 := argsOff + argsSize
				m2 := retOff + retSize
				needed := m1
				if m2 > m1 {
					needed = m2
				}
				memSize = (needed + 31) / 32 * 32
				if memSize < needed {
					return ErrGasUintOverflow
				}

				st.push(ctx.Operands[0])
				var dyn uint64
				var err error
				hadOverflow := false
				if adapter.evm.chainRules.IsBerlin {
					if evmOp == DELEGATECALL {
						dyn, err = gasDelegateCallEIP2929(adapter.evm, contract, st, adapter.memShadow, memSize)
					} else {
						dyn, err = gasStaticCallEIP2929(adapter.evm, contract, st, adapter.memShadow, memSize)
					}
				} else {
					if evmOp == DELEGATECALL {
						dyn, err = gasDelegateCall(adapter.evm, contract, st, adapter.memShadow, memSize)
					} else {
						dyn, err = gasStaticCall(adapter.evm, contract, st, adapter.memShadow, memSize)
					}
				}
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						hadOverflow = true
						err = nil
					}
					if err != nil {
						return err
					}
				}
				if contract.Gas < dyn {
					return ErrOutOfGas
				}
				contract.Gas -= dyn
				if !hadOverflow && memSize > 0 {
					resizeShadow(memSize)
					adapter.mirInterpreter.EnsureMemorySize(memSize)
				}
			}
		}
		if mirGasProbe != nil {
			mirGasProbe(uint64(ctx.M.EvmPC()), ctx.EvmOp, contract.Gas)
		}
		if mirGasTimingHook != nil {
			mirGasTimingHook(uint64(ctx.M.EvmPC()), ctx.EvmOp, time.Since(timingStart))
		}
		return nil
	})
	// Build a jump table matching current chain rules for gas accounting
	switch {
	case evm.chainRules.IsVerkle:
		adapter.table = &verkleInstructionSet
	case evm.chainRules.IsPrague:
		adapter.table = &pragueInstructionSet
	case evm.chainRules.IsCancun:
		adapter.table = &cancunInstructionSet
	case evm.chainRules.IsShanghai:
		adapter.table = &shanghaiInstructionSet
	case evm.chainRules.IsMerge:
		adapter.table = &mergeInstructionSet
	case evm.chainRules.IsLondon:
		adapter.table = &londonInstructionSet
	case evm.chainRules.IsBerlin:
		adapter.table = &berlinInstructionSet
	case evm.chainRules.IsIstanbul:
		adapter.table = &istanbulInstructionSet
	case evm.chainRules.IsConstantinople:
		adapter.table = &constantinopleInstructionSet
	case evm.chainRules.IsByzantium:
		adapter.table = &byzantiumInstructionSet
	case evm.chainRules.IsEIP158:
		adapter.table = &spuriousDragonInstructionSet
	case evm.chainRules.IsEIP150:
		adapter.table = &tangerineWhistleInstructionSet
	case evm.chainRules.IsHomestead:
		adapter.table = &homesteadInstructionSet
	default:
		adapter.table = &frontierInstructionSet
	}
	// Initialize a shadow memory for dynamic memory gas accounting
	adapter.memShadow = NewMemory()
	adapter.warmAccounts = make(map[[20]byte]struct{})
	adapter.warmSlots = make(map[warmSlotKey]struct{})
	adapter.storageCache = make(map[[32]byte][32]byte)
	return adapter
}

// Run executes the contract using MIR interpreter
// This method should match the signature of EVMInterpreter.Run
func (adapter *MIRInterpreterAdapter) Run(contract *Contract, input []byte, readOnly bool) (ret []byte, err error) {
	// Check if we have MIR-optimized code
	if !contract.HasMIRCode() {
		return nil, fmt.Errorf("MIR code missing for %s", contract.Address())
	}
	// Reset JUMPDEST de-dup guard per top-level run
	adapter.lastJdPC = ^uint32(0)
	adapter.lastJdBlockFirstPC = ^uint32(0)

	// Pre-flight fork gating: if the bytecode contains opcodes not enabled at the current fork,
	// mirror EVM behavior by returning invalid opcode errors instead of running MIR.
	rules := adapter.evm.chainRules
	code := contract.Code
	if !rules.IsConstantinople {
		if bytes.IndexByte(code, byte(SHR)) >= 0 || bytes.IndexByte(code, byte(SHL)) >= 0 || bytes.IndexByte(code, byte(SAR)) >= 0 {
			return nil, fmt.Errorf("invalid opcode: SHR")
		}
	}

	// Get the MIR CFG from the contract (type assertion)
	cfgInterface := contract.GetMIRCFG()
	cfg, ok := cfgInterface.(*compiler.CFG)
	if !ok || cfg == nil {
		return nil, fmt.Errorf("MIR CFG invalid for %s", contract.Address())
	}

	// Set current contract for the pre-installed hook
	adapter.currentContract = contract

	// Save current env state before modifying it (for nested calls)
	env := adapter.mirInterpreter.GetEnv()
	if env == nil {
		return nil, fmt.Errorf("MIR interpreter env is nil")
	}
	oldSelf := env.Self
	oldCaller := env.Caller
	oldOrigin := env.Origin
	oldCallValue := env.CallValue
	oldCalldata := env.Calldata
	oldCode := env.Code
	oldCurrentSelf := adapter.currentSelf

	// Restore env after execution
	defer func() {
		env.Self = oldSelf
		env.Caller = oldCaller
		env.Origin = oldOrigin
		env.CallValue = oldCallValue
		env.Calldata = oldCalldata
		env.Code = oldCode
		adapter.currentSelf = oldCurrentSelf
	}()

	// Save and restore memShadow to prevent pollution across nested calls
	oldMemShadow := adapter.memShadow
	adapter.memShadow = NewMemory() // Each contract gets its own memory shadow
	defer func() {
		adapter.memShadow = oldMemShadow
	}()

	// Set up MIR execution environment with contract-specific data
	adapter.setupExecutionEnvironment(contract, input)

	// Initialize block entry gas charges tracking (clear per contract execution)
	adapter.blockEntryGasCharges = make(map[*compiler.MIRBasicBlock]uint64)
	// Wire gas left getter so MirGAS can read it if needed
	// GAS opcode should return the gas value BEFORE constant gas for future opcodes is charged
	// (In EVM, GAS reads gas before future opcodes' constant gas is charged)
	if adapter.mirInterpreter != nil && adapter.mirInterpreter.GetEnv() != nil {
		env := adapter.mirInterpreter.GetEnv()
		env.GasLeft = func() uint64 {
			if adapter.currentContract != nil {
				gas := adapter.currentContract.Gas
				// GAS opcode should read gas before constant gas for future opcodes is charged
				// Add back block entry gas charges for the current block (if any)
				// This assumes GAS is the first opcode in the block - a more precise solution
				// would track which opcodes come after GAS based on evmPC
				if adapter.currentBlock != nil {
					if blockEntryGas, ok := adapter.blockEntryGasCharges[adapter.currentBlock]; ok {
						// Add back block entry gas charges (for opcodes that come after GAS)
						gas += blockEntryGas
					}
				}
				return gas
			}
			return 0
		}
	}

	// Install a pre-op hook to charge constant gas per opcode and any eliminated-op constants per block entry
	innerHook := func(ctx *compiler.MIRPreOpContext) error {
		if ctx == nil {
			return nil
		}
		// Track if previous op was a JUMP/JUMPI to decide landing-time JUMPDEST charge
		if ctx.M != nil {
			if OpCode(ctx.EvmOp) == JUMP || OpCode(ctx.EvmOp) == JUMPI || ctx.M.Op() == compiler.MirJUMP || ctx.M.Op() == compiler.MirJUMPI {
				adapter.lastWasJump = true
			}
		}
		// On block entry, charge constant gas for all EVM opcodes in the block
		// (including PUSH/DUP/SWAP that don't have MIR instructions)
		// Exception: EXP and KECCAK256 constant gas is charged per instruction (along with dynamic gas)
		// But if EXP has no MIR instruction (optimized away), we need to charge it at block entry
		// Allow block entry gas charging even when ctx.M == nil (for blocks with Size=0)
		if ctx.IsBlockEntry && ctx.Block != nil {
			// Update current block
			adapter.currentBlock = ctx.Block
			// Track total gas charged at block entry (for GAS opcode to add back)
			var blockEntryTotalGas uint64
			// If the first opcode of the underlying bytecode at this block is JUMPDEST, EVM charges 1 gas upon entering.
			// Charge it up-front here (once), and record for GAS to add back.
			if adapter.currentContract != nil {
				code := adapter.currentContract.Code
				firstPC := int(ctx.Block.FirstPC())
				if code != nil && firstPC >= 0 && firstPC < len(code) && OpCode(code[firstPC]) == JUMPDEST {
					jg := params.JumpdestGas
					if adapter.currentContract.Gas < jg {
						return ErrOutOfGas
					}
					adapter.currentContract.Gas -= jg
					blockEntryTotalGas += jg
					// remember we charged this (block-first JUMPDEST)
					adapter.lastJdPC = uint32(ctx.Block.FirstPC())
					adapter.lastJdBlockFirstPC = uint32(ctx.Block.FirstPC())
				}
			}
			// Validate that we're only charging for the current block
			// (ctx.Block should match the block we're entering)
			// Charge block entry gas for all opcodes in the block
			// Get counts from EVMOpCounts(), but validate against actual bytecode in block's PC range
			// This fixes cases where EVMOpCounts() includes opcodes from other blocks
			var counts map[byte]uint32
			// Use currentContract instead of captured contract to handle nested calls correctly
			currentContract := adapter.currentContract
			if currentContract == nil {
				currentContract = contract
			}
			if currentContract.Code != nil && ctx.Block != nil {
				// Count opcodes directly from bytecode in the block's PC range
				firstPC := ctx.Block.FirstPC()
				lastPC := ctx.Block.LastPC()
				validatedCounts := countOpcodesInRange(currentContract.Code, firstPC, lastPC)
				// Use validated counts instead of EVMOpCounts() to ensure accuracy
				counts = validatedCounts
			} else {
				// Fallback to EVMOpCounts() if we can't validate
				counts = ctx.Block.EVMOpCounts()
			}
			if counts != nil {
				// Track which opcodes have MIR instructions in this block
				hasMIRInstruction := make(map[OpCode]bool)
				if ctx.M != nil && ctx.M.EvmOp() != 0 {
					hasMIRInstruction[OpCode(ctx.M.EvmOp())] = true
				}
				// Also check all instructions in the block
				if ctx.Block != nil {
					for _, m := range ctx.Block.Instructions() {
						if m != nil && m.EvmOp() != 0 {
							hasMIRInstruction[OpCode(m.EvmOp())] = true
						}
					}
				}
				for opb, cnt := range counts {
					if cnt == 0 {
						continue
					}
					op := OpCode(opb)
					// Skip KECCAK256 constant gas at block entry (charged per instruction)
					if op == KECCAK256 {
						continue
					}
					// Skip JUMPDEST constant gas at block entry (always charged per instruction when executed)
					// JUMPDEST is a jump target and must be charged when executed, not at block entry
					if op == JUMPDEST {
						continue
					}
					// Skip GAS constant gas at block entry (charged per instruction, but GAS reads gas before its own charge)
					// GAS opcode should return the gas value BEFORE its own constant gas is charged
					if op == GAS {
						continue
					}
					if op == EXP {
						// EXP has both constant and dynamic gas
						// If EXP has a MIR instruction, gas will be charged per instruction
						// If EXP was optimized away (no MIR instruction), charge gas at block entry using operands from bytecode
						if !hasMIRInstruction[EXP] {
							// EXP was optimized away, but we still need to charge gas
							// Get operands from the original bytecode to calculate dynamic gas
							expGas := params.ExpGas // Start with constant gas
							if currentContract.Code != nil {
								code := currentContract.Code
								// Find EXP opcode (0x0a) in the bytecode and get its operands
								for i := 0; i < len(code); i++ {
									if OpCode(code[i]) == EXP {
										// Found EXP at PC i, now get the two operands from PUSH instructions before it
										// Trace back to find the two PUSH values
										// Stack order: last pushed is on top, so EXP pops: top (base) then next (exponent)
										var expVal *uint256.Int
										// Trace backwards from EXP to find PUSH instructions
										// The byte before EXP (i-1) is the data byte of the last PUSH
										// We need to trace back to find both PUSH opcodes
										pc := i - 1 // Last byte before EXP (data byte of first PUSH)
										// Find first PUSH before EXP (base, top of stack)
										// Trace back to find the PUSH opcode
										for pc >= 0 && pc < len(code) {
											// Check if current byte is a PUSH opcode
											op := OpCode(code[pc])
											if op >= PUSH1 && op <= PUSH32 {
												pushSize := int(op - PUSH1 + 1)
												// This is a PUSH opcode, data follows immediately
												if pc+pushSize < len(code) {
													// Move to before this PUSH to find the next one (exponent)
													pc = pc - 1
													// Get second operand (exponent) - second PUSH before EXP
													for pc >= 0 && pc < len(code) {
														op2 := OpCode(code[pc])
														if op2 >= PUSH1 && op2 <= PUSH32 {
															pushSize2 := int(op2 - PUSH1 + 1)
															if pc+pushSize2 < len(code) {
																// Read the pushed value (big-endian, right-aligned)
																valBytes2 := make([]byte, 32)
																copy(valBytes2[32-pushSize2:], code[pc+1:pc+1+pushSize2])
																expVal = uint256.NewInt(0).SetBytes(valBytes2)
															}
															break
														}
														pc--
													}
												}
												break
											}
											pc--
										}
										// Calculate dynamic gas if we have the exponent
										if expVal != nil {
											expBytes := uint64((expVal.BitLen() + 7) / 8)
											perByte := params.ExpByteFrontier
											if adapter.evm.chainRules.IsEIP158 {
												perByte = params.ExpByteEIP158
											}
											expGas = params.ExpGas + perByte*expBytes
										}
										break
									}
								}
							}
							totalGas := expGas * uint64(cnt)
							if currentContract.Gas < totalGas {
								return ErrOutOfGas
							}
							currentContract.Gas -= totalGas
							blockEntryTotalGas += totalGas
						}
						// Skip EXP constant gas at block entry if it has a MIR instruction (will be charged per instruction)
						continue
					}
					jt := (*adapter.table)[op]
					if jt != nil && jt.constantGas > 0 {
						total := jt.constantGas * uint64(cnt)
						if currentContract.Gas < total {
							return ErrOutOfGas
						}
						currentContract.Gas -= total
						blockEntryTotalGas += total
					}
				}
				// Store block entry gas charges for this block (for GAS opcode to add back)
				if blockEntryTotalGas > 0 {
					adapter.blockEntryGasCharges[ctx.Block] = blockEntryTotalGas
				}
			}
		}
		// Determine originating EVM opcode for this MIR
		evmOp := OpCode(ctx.EvmOp)
		// Emit tracer OnOpcode before charging to maintain step-count parity
		if adapter.evm != nil && adapter.evm.Config.Tracer != nil && adapter.evm.Config.Tracer.OnOpcode != nil {
			scope := &ScopeContext{Memory: adapter.memShadow, Stack: nil, Contract: contract}
			adapter.evm.Config.Tracer.OnOpcode(uint64(ctx.M.EvmPC()), byte(evmOp), contract.Gas, 0, scope, nil, adapter.evm.depth, nil)
		}
		// Constant gas is charged at block entry; JUMPDEST is charged at block entry of landing blocks.
		// Charge JUMPDEST exactly when executed (matches base EVM semantics).
		// For zero-size landing blocks, MIR synthesizes a JUMPDEST pre-op at block-entry:
		// in that case, charge at block-entry; for normal blocks, charge on instruction (IsBlockEntry=false).
		// Charge JUMPDEST on landing exactly once.
		// Prefer charging at the instruction itself; if first MIR instruction doesn't carry EvmOp=JUMPDEST,
		// charge at block entry when coming from a jump, based on bytecode and/or first MIR instr.
		if ctx.M != nil && (ctx.EvmOp == byte(JUMPDEST) || ctx.M.Op() == compiler.MirJUMPDEST) {
			lp := uint32(ctx.M.EvmPC())
			var bf uint32
			if ctx.Block != nil {
				bf = uint32(ctx.Block.FirstPC())
			}
			// Skip if we've already charged for this exact landing in this block
			if adapter.lastJdPC == lp && adapter.lastJdBlockFirstPC == bf {
				// no-op
			} else {
				jumpdestGas := params.JumpdestGas
				if contract.Gas < jumpdestGas {
					return ErrOutOfGas
				}
				contract.Gas -= jumpdestGas
				adapter.lastJdPC = lp
				adapter.lastJdBlockFirstPC = bf
			}
		} else if ctx.IsBlockEntry && ctx.Block != nil && adapter.currentContract != nil {
			firstPC := int(ctx.Block.FirstPC())
			isJD := false
			// Check bytecode at firstPC
			code := adapter.currentContract.Code
			if code != nil && firstPC >= 0 && firstPC < len(code) {
				if OpCode(code[firstPC]) == JUMPDEST {
					isJD = true
				}
			}
			// Also check first MIR instruction if available
			if !isJD {
				if instrs := ctx.Block.Instructions(); len(instrs) > 0 && instrs[0] != nil {
					if instrs[0].Op() == compiler.MirJUMPDEST || instrs[0].EvmOp() == byte(JUMPDEST) {
						isJD = true
					}
				}
			}
			if isJD {
				lp := uint32(ctx.Block.FirstPC())
				bf := lp
				// Dedup
				if !(adapter.lastJdPC == lp && adapter.lastJdBlockFirstPC == bf) {
					jumpdestGas := params.JumpdestGas
					if contract.Gas < jumpdestGas {
						return ErrOutOfGas
					}
					contract.Gas -= jumpdestGas
					adapter.lastJdPC = lp
					adapter.lastJdBlockFirstPC = bf
				}
			}
			// Clear jump flag at block entry regardless
			adapter.lastWasJump = false
		}
		// Exception: GAS opcode must read gas BEFORE its own constant gas is charged
		// GAS opcode constant gas is skipped at block entry, so we charge it when executed
		// But GAS opcode reads the gas value, so we need to charge it AFTER it reads
		// Actually, we charge it in the pre-op hook (before execution), but GAS reads in execution
		// So GAS will read the gas AFTER its constant gas is charged, which matches EVM behavior
		if ctx.M != nil && ctx.M.Op() == compiler.MirGAS && !ctx.IsBlockEntry {
			// GAS opcode charges constant gas when executed
			if adapter.table != nil && (*adapter.table)[GAS] != nil {
				gasOpGas := (*adapter.table)[GAS].constantGas
				if gasOpGas > 0 {
					if contract.Gas < gasOpGas {
						return ErrOutOfGas
					}
					contract.Gas -= gasOpGas
				}
			}
		}
		// Dynamic gas will still be charged per instruction in the switch statement below
		// Dynamic gas metering
		// Ensure shadow memory reflects prior expansions
		if adapter.memShadow == nil {
			adapter.memShadow = NewMemory()
		}
		// Helper: resize shadow memory after charging
		resizeShadow := func(sz uint64) {
			if sz > 0 {
				adapter.memShadow.Resize(sz)
			}
		}
		// Helper: toWordSize
		toWord := func(x uint64) uint64 { return (x + 31) / 32 }
		// Handle EXP gas (check MIR opcode, evmOp, and MIR instruction's evmOp field)
		// Following EVM logic: EXP gas is charged via dynamicGas function (constant + dynamic together)
		expHandledByMir := false
		// Check all possible ways EXP might be detected
		isEXP := false
		if ctx.M != nil {
			mirOp := ctx.M.Op()
			mirEvmOp := ctx.M.EvmOp()
			if mirOp == compiler.MirEXP {
				isEXP = true
			} else if mirEvmOp == byte(EXP) {
				// Check MIR instruction's evmOp field directly
				isEXP = true
			}
		}
		if !isEXP && (evmOp == EXP || ctx.EvmOp == byte(EXP)) {
			isEXP = true
		}

		// DEBUG: innerHook trace
		// if true {
		//      opsStr := ""
		//      for _, op := range ctx.Operands {
		//          if op != nil {
		//              opsStr += fmt.Sprintf("%x ", op.Bytes())
		//          }
		//      }
		//
		// }

		if isEXP {
			expHandledByMir = true
			// EXP has both constant and dynamic gas (charged together, following EVM logic)
			if len(ctx.Operands) >= 2 {
				exp := ctx.Operands[1]
				expBytes := uint64((exp.BitLen() + 7) / 8)
				perByte := params.ExpByteFrontier
				if adapter.evm.chainRules.IsEIP158 {
					perByte = params.ExpByteEIP158
				}
				// Charge both constant and dynamic gas for EXP (same as EVM dynamicGas function)
				totalGas := params.ExpGas + perByte*expBytes
				if contract.Gas < totalGas {
					return ErrOutOfGas
				}
				contract.Gas -= totalGas
			} else {
				// Fallback: charge minimum EXP gas if operands not available
				minGas := params.ExpGas
				if contract.Gas < minGas {
					return ErrOutOfGas
				}
				contract.Gas -= minGas
			}
		}
		// EXP gas will be handled in the switch statement below when evmOp == EXP, following EVM logic
		switch evmOp {
		case SLOAD:
			// EIP-2929 SLOAD dynamic gas
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					st := newstack()
					st.push(new(uint256.Int).Set(ctx.Operands[0])) // peek -> slot
					gas, err := gasSLoadEIP2929(adapter.evm, contract, st, adapter.memShadow, 0)
					if err != nil {
						if errors.Is(err, ErrGasUintOverflow) {
							// Mirror base interpreter behavior: do not surface overflow from dynamic calc here
							// Leave callGasTemp as-is; the call execution path will handle effective gas
							err = nil
						}
						return err
					}
					if contract.Gas < gas {
						return ErrOutOfGas
					}
					contract.Gas -= gas
				}
			}
		case BALANCE, EXTCODESIZE, EXTCODEHASH:
			// EIP-2929 account warm/cold surcharge
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					st := newstack()
					st.push(new(uint256.Int).Set(ctx.Operands[0])) // peek -> address
					gas, err := gasEip2929AccountCheck(adapter.evm, contract, st, adapter.memShadow, 0)
					if err != nil {
						if errors.Is(err, ErrGasUintOverflow) {
							err = nil
						}
						return err
					}
					if gas > 0 {
						if contract.Gas < gas {
							return ErrOutOfGas
						}
						contract.Gas -= gas
					}
				}
			}
		case EXP:
			// EXP has both constant and dynamic gas (charged together, following EVM logic)
			// Constant gas is NOT charged at block entry for EXP (already skipped above)
			// Only charge here if MirEXP wasn't already handled above (to avoid double-charging)
			if !expHandledByMir {
				if len(ctx.Operands) >= 2 {
					exp := ctx.Operands[1]
					expBytes := uint64((exp.BitLen() + 7) / 8)
					perByte := params.ExpByteFrontier
					if adapter.evm.chainRules.IsEIP158 {
						perByte = params.ExpByteEIP158
					}
					// Charge both constant and dynamic gas for EXP (same as EVM dynamicGas function)
					totalGas := params.ExpGas + perByte*expBytes
					if contract.Gas < totalGas {
						return ErrOutOfGas
					}
					contract.Gas -= totalGas
				} else {
					// Fallback: charge minimum EXP gas if operands not available
					minGas := params.ExpGas
					if contract.Gas < minGas {
						return ErrOutOfGas
					}
					contract.Gas -= minGas
				}
			}
		case MLOAD, MSTORE, MSTORE8, RETURN, REVERT, CREATE, CREATE2:
			// Calculate the required memory size for the operation
			var needed uint64
			switch evmOp {
			case MLOAD, MSTORE:
				if len(ctx.Operands) > 0 {
					off := ctx.Operands[0].Uint64()
					needed = off + 32
				}
			case MSTORE8:
				if len(ctx.Operands) > 0 {
					off := ctx.Operands[0].Uint64()
					needed = off + 1
				}
			case RETURN, REVERT:
				if len(ctx.Operands) >= 2 {
					off := ctx.Operands[0].Uint64()
					size := ctx.Operands[1].Uint64()
					needed = off + size
				}
			case CREATE, CREATE2:
				// CREATE: value, offset, size
				// CREATE2: value, offset, size, salt
				if len(ctx.Operands) >= 3 {
					off := ctx.Operands[1].Uint64()
					size := ctx.Operands[2].Uint64()
					needed = off + size
				}
			}

			// Round up to 32 bytes
			memSize := (needed + 31) / 32 * 32
			if memSize < needed { // overflow check
				return ErrGasUintOverflow
			}

			// Only charge gas if memory is expanding
			if memSize > uint64(adapter.memShadow.Len()) {
				gas, err := memoryGasCost(adapter.memShadow, memSize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					}
					return err
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
				resizeShadow(memSize)
				adapter.mirInterpreter.EnsureMemorySize(memSize)
			}

			// Additional gas charges
			if evmOp == CREATE2 {
				if len(ctx.Operands) >= 3 {
					size := ctx.Operands[2].Uint64()
					keccak256Gas := toWord(size) * params.Keccak256WordGas
					if contract.Gas < keccak256Gas {
						return ErrOutOfGas
					}
					contract.Gas -= keccak256Gas
				}
			}
			// EIP-3860 initcode per-word gas for CREATE/CREATE2 (only if Shanghai is active)
			if (evmOp == CREATE || evmOp == CREATE2) && adapter.evm.chainRules.IsShanghai {
				if len(ctx.Operands) >= 3 {
					size := ctx.Operands[2].Uint64()
					if size > params.MaxInitCodeSize {
						return fmt.Errorf("%w: size %d", ErrMaxInitCodeSizeExceeded, size)
					}
					more := params.InitCodeWordGas * toWord(size)
					if contract.Gas < more {
						return ErrOutOfGas
					}
					contract.Gas -= more
				}
			}
		case CALLDATACOPY, CODECOPY, RETURNDATACOPY:
			// Always charge copy gas per word
			var memOff, size uint64
			if len(ctx.Operands) >= 3 {
				memOff = ctx.Operands[0].Uint64()
				size = ctx.Operands[2].Uint64()
			}
			copyGas := toWord(size) * params.CopyGas

			needed := memOff + size
			memSize := (needed + 31) / 32 * 32
			if memSize < needed {
				return ErrGasUintOverflow
			}

			// Memory expansion gas if destination grows memory
			var memGas uint64
			if memSize > uint64(adapter.memShadow.Len()) {
				g, err := memoryGasCost(adapter.memShadow, memSize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				memGas = g
			}
			add := memGas + copyGas
			if add > 0 {
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
			}
			if memSize > uint64(adapter.memShadow.Len()) {
				resizeShadow(memSize)
				adapter.mirInterpreter.EnsureMemorySize(memSize)
			}
		case EXTCODECOPY:
			// Always charge copy gas per word; memory expansion if needed
			var memOff, size uint64
			if len(ctx.Operands) >= 4 {
				memOff = ctx.Operands[1].Uint64()
				size = ctx.Operands[3].Uint64()
			}
			copyGas := toWord(size) * params.CopyGas

			needed := memOff + size
			memSize := (needed + 31) / 32 * 32
			if memSize < needed {
				return ErrGasUintOverflow
			}

			var memGas uint64
			if memSize > uint64(adapter.memShadow.Len()) {
				g, err := memoryGasCost(adapter.memShadow, memSize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				memGas = g
			}
			add := memGas + copyGas
			if add > 0 {
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
			}
			if memSize > uint64(adapter.memShadow.Len()) {
				resizeShadow(memSize)
				adapter.mirInterpreter.EnsureMemorySize(memSize)
			}
			// EIP-2929 cold-warm surcharge for EXTCODECOPY
			if adapter.evm.chainRules.IsBerlin {
				if len(ctx.Operands) >= 1 {
					st := newstack()
					st.push(new(uint256.Int).Set(ctx.Operands[0])) // address
					gas, err := gasEip2929AccountCheck(adapter.evm, contract, st, adapter.memShadow, 0)
					if err != nil {
						if errors.Is(err, ErrGasUintOverflow) {
							err = nil
						} else {
							return err
						}
					}
					if gas > 0 {
						if contract.Gas < gas {
							return ErrOutOfGas
						}
						contract.Gas -= gas
					}
				}
			}
		case MCOPY:
			// Always charge copy gas; and memory gas if growing
			// Operands: dest, src, len
			var dest, src, size uint64
			if len(ctx.Operands) >= 3 {
				dest = ctx.Operands[0].Uint64()
				src = ctx.Operands[1].Uint64()
				size = ctx.Operands[2].Uint64()
			}
			copyGas := toWord(size) * params.CopyGas

			// Expansion for both read and write
			m1 := dest + size
			m2 := src + size
			needed := m1
			if m2 > m1 {
				needed = m2
			}
			memSize := (needed + 31) / 32 * 32
			if memSize < needed {
				return ErrGasUintOverflow
			}

			var memGas uint64
			if memSize > uint64(adapter.memShadow.Len()) {
				g, err := memoryGasCost(adapter.memShadow, memSize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						err = nil
					} else {
						return err
					}
				}
				memGas = g
			}
			add := memGas + copyGas
			if add > 0 {
				if contract.Gas < add {
					return ErrOutOfGas
				}
				contract.Gas -= add
			}
			if memSize > uint64(adapter.memShadow.Len()) {
				resizeShadow(memSize)
				adapter.mirInterpreter.EnsureMemorySize(memSize)
			}
		case KECCAK256:
			// KECCAK256 has constant gas (30) + memory gas + word gas
			// Constant gas is charged per instruction (not at block entry)
			var offset, size uint64
			if len(ctx.Operands) >= 2 {
				offset = ctx.Operands[0].Uint64()
				size = ctx.Operands[1].Uint64()
			}
			// Calculate memory size
			needed := offset + size
			memSize := (needed + 31) / 32 * 32
			if memSize < needed {
				return ErrGasUintOverflow
			}

			wordGas := toWord(size) * params.Keccak256WordGas
			totalGas := params.Keccak256Gas + wordGas
			// Memory expansion gas (if any)
			if memSize > uint64(adapter.memShadow.Len()) {
				gas, err := memoryGasCost(adapter.memShadow, memSize)
				if err != nil {
					if errors.Is(err, ErrGasUintOverflow) {
						// align with base interpreter: don't surface overflow from call gas calc here
					} else {
						return err
					}
				}
				totalGas += gas
			}
			if contract.Gas < totalGas {
				return ErrOutOfGas
			}
			contract.Gas -= totalGas
			if memSize > uint64(adapter.memShadow.Len()) {
				resizeShadow(memSize)
				adapter.mirInterpreter.EnsureMemorySize(memSize)
			}
		case LOG0, LOG1, LOG2, LOG3, LOG4:
			var offset, size uint64
			if len(ctx.Operands) >= 2 {
				offset = ctx.Operands[0].Uint64()
				size = ctx.Operands[1].Uint64()
			}
			needed := offset + size
			memSize := (needed + 31) / 32 * 32
			if memSize < needed {
				return ErrGasUintOverflow
			}

			var gas uint64
			if memSize > uint64(adapter.memShadow.Len()) {
				g, err := memoryGasCost(adapter.memShadow, memSize)
				if err != nil {
					return err
				}
				gas = g
			}
			// Topics and data costs
			n := int(evmOp - LOG0)
			add := gas + params.LogGas + uint64(n)*params.LogTopicGas
			// LogDataGas is per byte
			add += size * params.LogDataGas
			if contract.Gas < add {
				return ErrOutOfGas
			}
			contract.Gas -= add
			if memSize > uint64(adapter.memShadow.Len()) {
				resizeShadow(memSize)
				adapter.mirInterpreter.EnsureMemorySize(memSize)
			}
		case SELFDESTRUCT:
			// Charge dynamic gas according to fork rules
			var gas uint64
			var err error
			if len(ctx.Operands) < 1 {
				return fmt.Errorf("SELFDESTRUCT missing operand")
			}
			st := newstack()
			beneficiaryAddr := new(uint256.Int).Set(ctx.Operands[0])
			st.push(beneficiaryAddr) // Push beneficiary address onto stack for gas calculation
			if adapter.evm.chainRules.IsLondon {
				gas, err = gasSelfdestructEIP3529(adapter.evm, contract, st, adapter.memShadow, 0)
			} else if adapter.evm.chainRules.IsBerlin {
				gas, err = gasSelfdestructEIP2929(adapter.evm, contract, st, adapter.memShadow, 0)
			}
			if err != nil {
				return err
			}
			if gas > 0 {
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
			}
		case SSTORE:
			// Build a tiny stack where Back(0)=key, Back(1)=value per gasSStore contract
			if len(ctx.Operands) >= 2 {
				st := newstack()
				// push value then key so Back(0)=key, Back(1)=value
				st.push(new(uint256.Int).Set(ctx.Operands[1]))
				st.push(new(uint256.Int).Set(ctx.Operands[0]))
				// Use EIP-2929/3529 gas functions for Berlin/London
				var gas uint64
				var err error
				if adapter.evm.chainRules.IsLondon {
					gas, err = gasSStoreEIP3529(adapter.evm, contract, st, adapter.memShadow, 0)
				} else if adapter.evm.chainRules.IsBerlin {
					gas, err = gasSStoreEIP2929(adapter.evm, contract, st, adapter.memShadow, 0)
				} else {
					gas, err = gasSStore(adapter.evm, contract, st, adapter.memShadow, 0)
				}
				if err != nil {
					return err
				}
				if contract.Gas < gas {
					return ErrOutOfGas
				}
				contract.Gas -= gas
			}
		case CALL, CALLCODE, DELEGATECALL, STATICCALL:
			// Use vm gas calculators to set evm.callGasTemp and deduct dynamic gas
			// Build stack so Back(0)=requestedGas, Back(1)=addr, Back(2)=value (if present)
			st := newstack()
			// Ensure ctx.Operands length checks per variant
			switch evmOp {
			case CALL, CALLCODE:
				if len(ctx.Operands) < 7 {
					return nil
				}
				reqGas := new(uint256.Int).Set(ctx.Operands[0])
				addr := new(uint256.Int)
				// address at operands[1]
				addr.Set(ctx.Operands[1])
				val := new(uint256.Int).Set(ctx.Operands[2])
				st.push(val)    // Back(2)
				st.push(addr)   // Back(1)
				st.push(reqGas) // Back(0)
				var dyn uint64
				var err error
				if adapter.evm.chainRules.IsBerlin {
					if evmOp == CALL {
						dyn, err = gasCallEIP2929(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					} else {
						dyn, err = gasCallCodeEIP2929(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					}
				} else {
					if evmOp == CALL {
						dyn, err = gasCall(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					} else {
						dyn, err = gasCallCode(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					}
				}
				if err != nil {
					return err
				}
				if contract.Gas < dyn {
					return ErrOutOfGas
				}
				contract.Gas -= dyn
				resizeShadow(ctx.MemorySize)
			case DELEGATECALL, STATICCALL:
				if len(ctx.Operands) < 6 {
					return nil
				}
				st := newstack()
				reqGas := new(uint256.Int).Set(ctx.Operands[0])
				addr := new(uint256.Int).Set(ctx.Operands[1])
				st.push(addr)   // Back(1) - address
				st.push(reqGas) // Back(0) - requested gas
				var dyn uint64
				var err error
				if adapter.evm.chainRules.IsBerlin {
					if evmOp == DELEGATECALL {
						dyn, err = gasDelegateCallEIP2929(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					} else {
						dyn, err = gasStaticCallEIP2929(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					}
				} else {
					if evmOp == DELEGATECALL {
						dyn, err = gasDelegateCall(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					} else {
						dyn, err = gasStaticCall(adapter.evm, contract, st, adapter.memShadow, ctx.MemorySize)
					}
				}
				if err != nil {
					return err
				}
				if contract.Gas < dyn {
					return ErrOutOfGas
				}
				contract.Gas -= dyn
				resizeShadow(ctx.MemorySize)
			}
		}
		// Test probe: allow tests to observe gas left after each MIR step
		if mirGasProbe != nil {
			mirGasProbe(uint64(ctx.M.EvmPC()), ctx.EvmOp, contract.Gas)
		}
		return nil
	}
	// Save current beforeOp hook to restore after this execution
	oldHook := adapter.mirInterpreter.GetBeforeOpHook()
	defer func() {
		// Restore previous hook after execution
		if oldHook != nil {
			adapter.mirInterpreter.SetBeforeOpHook(oldHook)
		}
	}()

	adapter.mirInterpreter.SetBeforeOpHook(func(ctx *compiler.MIRPreOpContext) error {
		err := innerHook(ctx)
		if err != nil && errors.Is(err, ErrGasUintOverflow) {
			return nil
		}
		return err
	})

	// Selector-based direct dispatch is disabled; always fall back to default entry.

	// No selector: execute the first basic block only
	bbs := cfg.GetBasicBlocks()
	// Allow blocks with Size=0 to execute if they have children (e.g., entry block with only PUSH)
	// PUSH operations don't create MIR instructions but are handled via block-level opcode counts
	if len(bbs) > 0 {
		bbByPC := cfg.BlockByPC(0)
		var bbByPCSize uint
		if bbByPC != nil {
			bbByPCSize = bbByPC.Size()
		}
		log.Warn("Adapter.Run checking entry block", "len(bbs)", len(bbs), "bb0.Size", bbs[0].Size(), "bb0.children", len(bbs[0].Children()), "BlockByPC(0)!=nil", bbByPC != nil, "BlockByPC(0).Size", bbByPCSize)
	} else {
		log.Warn("Adapter.Run checking entry block", "len(bbs)", 0)
	}
	entryBlockHasContent := len(bbs) > 0 && bbs[0] != nil && (bbs[0].Size() > 0 || len(bbs[0].Children()) > 0)
	if entryBlockHasContent {
		result, err := adapter.mirInterpreter.RunCFGWithResolver(cfg, bbs[0])
		if err != nil {
			if err == compiler.ErrMIRFallback {
				return nil, fmt.Errorf("MIR fallback requested but disabled: %w", err)
			}
			// Map compiler.errREVERT to vm.ErrExecutionReverted to preserve gas
			if errors.Is(err, compiler.GetErrREVERT()) {
				return result, ErrExecutionReverted
			}
			// Preserve returndata on error (e.g., REVERT) to match EVM semantics
			return result, err
		}
		// If MIR executed without error, return whatever returndata was produced.
		// An empty result (e.g., STOP) should not trigger fallback; mirror EVM semantics
		// where a STOP simply returns empty bytes.
		return result, nil
	}
	// If nothing returned from the entry, return error
	return nil, fmt.Errorf("MIR entry block produced no result")
}

// setupExecutionEnvironment configures the MIR interpreter with contract-specific data
func (adapter *MIRInterpreterAdapter) setupExecutionEnvironment(contract *Contract, input []byte) {
	env := adapter.mirInterpreter.GetEnv()

	// Set calldata (copy to avoid slice reuse issues)
	if input != nil {
		env.Calldata = append([]byte(nil), input...)
	} else {
		env.Calldata = nil
	}

	// Reset interpreter transient state to avoid per-call allocations
	// Reuse memory backing store by truncating length to zero
	if adapter.mirInterpreter != nil {
		// Reset memory view
		if adapter.mirInterpreter.MemoryCap() > 0 {
			adapter.mirInterpreter.TruncateMemory()
		}
		// Reset return data
		adapter.mirInterpreter.ResetReturnData()
	}

	// Reset warm caches per top-level Run
	if adapter.warmAccounts == nil {
		adapter.warmAccounts = make(map[[20]byte]struct{})
	} else {
		for k := range adapter.warmAccounts {
			delete(adapter.warmAccounts, k)
		}
	}
	// Reset storage cache per run
	if adapter.storageCache == nil {
		adapter.storageCache = make(map[[32]byte][32]byte)
	} else {
		for k := range adapter.storageCache {
			delete(adapter.storageCache, k)
		}
	}
	if adapter.warmSlots == nil {
		adapter.warmSlots = make(map[warmSlotKey]struct{})
	} else {
		for k := range adapter.warmSlots {
			delete(adapter.warmSlots, k)
		}
	}

	// Set address context
	{
		addr := contract.Address()
		caller := contract.Caller()
		origin := adapter.evm.TxContext.Origin
		adapter.currentSelf = addr
		copy(env.Self[:], addr[:])
		copy(env.Caller[:], caller[:])
		copy(env.Origin[:], origin[:])
	}

	// Set gas price from transaction context
	if adapter.evm.TxContext.GasPrice != nil {
		env.GasPrice = adapter.evm.TxContext.GasPrice.Uint64()
	}
	// Set blob base fee from block context (for BLOBBASEFEE opcode)
	if adapter.evm.Context.BlobBaseFee != nil {
		env.BlobBaseFee = adapter.evm.Context.BlobBaseFee.Uint64()
	} else {
		env.BlobBaseFee = 0
	}

	// Set call value for CALLVALUE op
	if contract != nil && contract.Value() != nil {
		// MIR will clone when reading, but we also clone here to insulate from Contract mutation
		env.CallValue = new(uint256.Int).Set(contract.Value())
	} else {
		env.CallValue = uint256.NewInt(0)
	}

	// Provide code for CODE* ops
	env.Code = contract.Code

	// External code accessors
	if env.ExtCodeSize == nil {
		env.ExtCodeSize = func(addr [20]byte) uint64 {
			a := common.BytesToAddress(addr[:])
			code := adapter.evm.StateDB.GetCode(a)
			if code == nil {
				return 0
			}
			return uint64(len(code))
		}
	}
	if env.ExtCodeCopy == nil {
		env.ExtCodeCopy = func(addr [20]byte, codeOffset uint64, dest []byte) {
			a := common.BytesToAddress(addr[:])
			code := adapter.evm.StateDB.GetCode(a)
			if code == nil {
				for i := range dest {
					dest[i] = 0
				}
				return
			}
			for i := uint64(0); i < uint64(len(dest)); i++ {
				idx := codeOffset + i
				if idx < uint64(len(code)) {
					dest[i] = code[idx]
				} else {
					dest[i] = 0
				}
			}
		}
	}

	// EXTCODEHASH via StateDB (installed once)
	if env.ExtCodeHash == nil {
		env.ExtCodeHash = func(addr [20]byte) [32]byte {
			a := common.BytesToAddress(addr[:])
			h := adapter.evm.StateDB.GetCodeHash(a)
			var out [32]byte
			copy(out[:], h[:])
			return out
		}
	}
	// Log function to route logs back into EVM (installed once)
	if env.LogFunc == nil {
		env.LogFunc = func(addr [20]byte, topics [][32]byte, data []byte) {
			a := common.BytesToAddress(addr[:])
			hashes := make([]common.Hash, len(topics))
			for i := range topics {
				hashes[i] = common.BytesToHash(topics[i][:])
			}
			if adapter.evm.Context.BlobBaseFee != nil {
				env.BlobBaseFee = adapter.evm.Context.BlobBaseFee.Uint64()
			}
			env.BlobHashFunc = func(index uint64) [32]byte {
				if index < uint64(len(adapter.evm.TxContext.BlobHashes)) {
					h := adapter.evm.TxContext.BlobHashes[index]
					var out [32]byte
					copy(out[:], h[:])
					return out
				}
				return [32]byte{}
			}
			adapter.evm.StateDB.AddLog(&coretypes.Log{
				Address:     a,
				Topics:      hashes,
				Data:        append([]byte(nil), data...),
				BlockNumber: adapter.evm.Context.BlockNumber.Uint64(),
			})
		}
	}

	// Wire external execution to stock EVM for CALL-family ops
	env.ExternalCall = func(kind byte, addr20 [20]byte, value *uint256.Int, callInput []byte, requestedGas uint64) (ret []byte, success bool) {
		to := common.BytesToAddress(addr20[:])
		// Use callGasTemp set by beforeOp hook (gas calculation already done)
		gas := adapter.evm.callGasTemp
		if (kind == 0 || kind == 1) && value != nil && !value.IsZero() {
			gas += params.CallStipend
		}
		var (
			out      []byte
			leftover uint64
			err      error
		)
		switch kind {
		case 0: // CALL
			out, leftover, err = adapter.evm.Call(contract, to, callInput, gas, value)
		case 1: // CALLCODE
			out, leftover, err = adapter.evm.CallCode(contract, to, callInput, gas, value)
		case 2: // DELEGATECALL
			out, leftover, err = adapter.evm.DelegateCall(contract, to, callInput, gas)
		case 3: // STATICCALL
			out, leftover, err = adapter.evm.StaticCall(contract, to, callInput, gas)
		default:
			return nil, false
		}
		// Refund leftover like stock interpreter
		contract.RefundGas(leftover, adapter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)
		if err != nil {
			return out, false
		}
		return out, true
	}

	// Wire CREATE and CREATE2 to stock EVM
	env.CreateContract = func(kind byte, value *uint256.Int, init []byte, salt *[32]byte) (addr [20]byte, success bool, ret []byte) {
		gas := contract.Gas
		// Apply EIP-150: parent gas reduction by 1/64 before passing to child
		gas -= gas / 64
		// Deduct from parent before call, matching opCreate/opCreate2
		// Use correct tracing reason for CREATE vs CREATE2
		tracingReason := tracing.GasChangeCallContractCreation
		if kind == 5 { // CREATE2
			tracingReason = tracing.GasChangeCallContractCreation2
		}
		contract.UseGas(gas, adapter.evm.Config.Tracer, tracingReason)
		var (
			out      []byte
			newAddr  common.Address
			leftover uint64
			err      error
		)
		if kind == 4 { // CREATE
			out, newAddr, leftover, err = adapter.evm.Create(contract, init, gas, value)
		} else { // CREATE2
			var saltU *uint256.Int
			if salt != nil {
				saltU = new(uint256.Int).SetBytes(salt[:])
			} else {
				saltU = uint256.NewInt(0)
			}
			out, newAddr, leftover, err = adapter.evm.Create2(contract, init, gas, value, saltU)
		}
		copy(addr[:], newAddr[:])
		// Refund leftover like stock interpreter
		contract.RefundGas(leftover, adapter.evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)
		if err != nil {
			return addr, false, out
		}
		return addr, true, out
	}

	// Do not override any tracer set by tests; leave as-is.

	// Block info
	env.GasLimit = adapter.evm.Context.GasLimit
	if adapter.evm.Context.Difficulty != nil {
		env.Difficulty = adapter.evm.Context.Difficulty.Uint64()
	}
	copy(env.Coinbase[:], adapter.evm.Context.Coinbase[:])
	env.BlockHashFunc = func(num uint64) [32]byte {
		// Use Context.GetHash if available; else return zero
		h := adapter.evm.Context.GetHash(num)
		var out [32]byte
		copy(out[:], h[:])
		return out
	}

	// Set fork flags from chain rules
	rules := adapter.evm.chainRules
	env.IsByzantium = rules.IsByzantium
	env.IsConstantinople = rules.IsConstantinople
	env.IsIstanbul = rules.IsIstanbul
	env.IsLondon = rules.IsLondon
	// Optionally extend with newer flags if MIR grows support for those ops
	// No-op if not referenced in interpreter.

	// Install jumpdest checker using EVM contract helpers
	env.CheckJumpdest = func(pc uint64) bool {

		// Must be within bounds and at a JUMPDEST and code segment
		if pc >= uint64(len(contract.Code)) {

			return false
		}
		if OpCode(contract.Code[pc]) != JUMPDEST {

			return false
		}
		isC := contract.isCode(pc)

		return isC
	}
}

// CanRun checks if this adapter can run the given contract
func (adapter *MIRInterpreterAdapter) CanRun(contract *Contract) bool {
	return contract.HasMIRCode()
}
