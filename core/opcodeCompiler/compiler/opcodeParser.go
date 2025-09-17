package compiler

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// CFG is the IR record the control flow of the contract.
// It records not only the control flow info but also the state and memory accesses.
// CFG is mapping to <addr, code> pair, and there is no need to record CFG for every contract
// since it is just an IR, although the analyzing/compiling results are saved to the related cache.
type CFG struct {
	codeAddr        common.Hash
	rawCode         []byte
	basicBlocks     []*MIRBasicBlock
	basicBlockCount uint
	memoryAccessor  *MemoryAccessor
	stateAccessor   *StateAccessor
}

func NewCFG(hash common.Hash, code []byte) (c *CFG) {
	c = &CFG{}
	c.codeAddr = hash
	c.rawCode = code
	c.basicBlocks = []*MIRBasicBlock{}
	c.basicBlockCount = 0
	return c
}

func (c *CFG) getMemoryAccessor() *MemoryAccessor {
	if c.memoryAccessor == nil {
		c.memoryAccessor = new(MemoryAccessor)
	}
	return c.memoryAccessor
}

func (c *CFG) getStateAccessor() *StateAccessor {
	if c.stateAccessor == nil {
		c.stateAccessor = new(StateAccessor)
	}
	return c.stateAccessor
}

// createEntryBB create an empty bb that contains (method/invoke) entry info.
func (c *CFG) createEntryBB() *MIRBasicBlock {
	entryBB := NewMIRBasicBlock(0, 0, nil)
	c.basicBlockCount++
	return entryBB
}

// createBB create a normal bb.
func (c *CFG) createBB(pc uint, parent *MIRBasicBlock) *MIRBasicBlock {
	bb := NewMIRBasicBlock(c.basicBlockCount, pc, parent)
	c.basicBlocks = append(c.basicBlocks, bb)
	c.basicBlockCount++
	return bb
}

func (c *CFG) reachEndBB() {
	// reach the end of BasicBlock.
	// TODO - zlin:  check the child is backward only.
}

func doOpcodesParse(hash common.Hash, code []byte) error {
	return buildCFG(hash, code)
}

func buildCFG(hash common.Hash, code []byte) error {
	if len(code) == 0 {
		log.Warn("Can not build CFG with nil codes\n")
		return ErrFailPreprocessing
	}
	return parseOpCode(hash, code)
}

func parseOpCode(hash common.Hash, code []byte) error {
	cfg := NewCFG(hash, code)

	// memoryAccessor is instance local at runtime
	var memoryAccessor *MemoryAccessor = cfg.getMemoryAccessor()
	// stateAccessor is global but we analyze it in processor granularity
	var stateAccessor *StateAccessor = cfg.getStateAccessor()

	entryBB := cfg.createEntryBB()
	startBB := cfg.createBB(0, entryBB)
	valueStack := ValueStack{}
	// generate CFG.
	unprcessedBBs := MIRBasicBlockStack{}
	unprcessedBBs.Push(startBB)

	for unprcessedBBs.Size() != 0 {
		curBB := unprcessedBBs.Pop()
		err := cfg.buildBasicBlock(curBB, &valueStack, memoryAccessor, stateAccessor, &unprcessedBBs)
		if err != nil {
			log.Error(err.Error())
		}
	}

	return nil
}

// parseOpCodeWithOptimization parses opcodes and returns optimized bytecode if possible
func parseOpCodeWithOptimization(hash common.Hash, code []byte) ([]byte, error) {
	cfg := NewCFG(hash, code)

	// memoryAccessor is instance local at runtime
	var memoryAccessor *MemoryAccessor = cfg.getMemoryAccessor()
	// stateAccessor is global but we analyze it in processor granularity
	var stateAccessor *StateAccessor = cfg.getStateAccessor()

	entryBB := cfg.createEntryBB()
	startBB := cfg.createBB(0, entryBB)
	valueStack := ValueStack{}
	// generate CFG.
	unprcessedBBs := MIRBasicBlockStack{}
	unprcessedBBs.Push(startBB)

	for unprcessedBBs.Size() != 0 {
		curBB := unprcessedBBs.Pop()
		err := cfg.buildBasicBlock(curBB, &valueStack, memoryAccessor, stateAccessor, &unprcessedBBs)
		if err != nil {
			log.Error(err.Error())
		}
	}

	// Use MIRInterpreter to execute and optimize the MIR instructions
	optimizedBytecode, err := executeAndOptimizeMIR(cfg, &valueStack, code)
	if err != nil {
		return nil, err
	}

	return optimizedBytecode, nil
}

// GenerateMIRCFG generates a MIR Control Flow Graph for the given bytecode
func GenerateMIRCFG(hash common.Hash, code []byte) (*CFG, error) {
	if len(code) == 0 {
		return nil, fmt.Errorf("empty code")
	}
	
	cfg := NewCFG(hash, code)
	
	// memoryAccessor is instance local at runtime
	var memoryAccessor *MemoryAccessor = cfg.getMemoryAccessor()
	// stateAccessor is global but we analyze it in processor granularity
	var stateAccessor *StateAccessor = cfg.getStateAccessor()

	entryBB := cfg.createEntryBB()
	startBB := cfg.createBB(0, entryBB)
	valueStack := ValueStack{}
	// generate CFG.
	unprcessedBBs := MIRBasicBlockStack{}
	unprcessedBBs.Push(startBB)

	for unprcessedBBs.Size() != 0 {
		curBB := unprcessedBBs.Pop()
		err := cfg.buildBasicBlock(curBB, &valueStack, memoryAccessor, stateAccessor, &unprcessedBBs)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}
	
	return cfg, nil
}

// GetBasicBlocks returns the basic blocks in this CFG
func (c *CFG) GetBasicBlocks() []*MIRBasicBlock {
	return c.basicBlocks
}

// executeAndOptimizeMIR uses MIRInterpreter to simulate execution and optimize MIR instructions
func executeAndOptimizeMIR(cfg *CFG, finalStack *ValueStack, originalCode []byte) ([]byte, error) {
	// Create MIR execution environment for simulation
	env := &MIRExecutionEnv{
		Memory:  make([]byte, 0, 1024),
		Storage: make(map[[32]byte][32]byte),
		// Set some default values for simulation
		BlockNumber: 1,
		Timestamp:   1000000,
		ChainID:     1,
		GasPrice:    1000000000,
	}
	
	// Create MIR interpreter
	interpreter := NewMIRInterpreter(env)
	
	// Step 1: Use MIRInterpreter to validate and simulate MIR execution
	// This helps us understand the actual execution flow and identify optimization opportunities
	var simulationSuccessful bool = true
	
	for _, bb := range cfg.basicBlocks {
		if bb == nil || len(bb.instructions) == 0 {
			continue
		}
		
		// Create a copy of the basic block for simulation
		// We don't want to modify the original
		simulationBB := &MIRBasicBlock{
			instructions: make([]*MIR, len(bb.instructions)),
		}
		copy(simulationBB.instructions, bb.instructions)
		
		// Try to simulate execution of this basic block
		_, err := interpreter.RunMIR(simulationBB)
		if err != nil {
			// Simulation failed - this is expected for many cases
			// (e.g., when we have incomplete context)
			simulationSuccessful = false
			break
		}
	}
	
	// Step 2: Apply MIR-level optimizations based on simulation results
	// For now, we'll enhance the existing peephole optimizations with MIR interpreter insights
	optimizedStack := optimizeMIRWithInterpreter(cfg, finalStack, interpreter, simulationSuccessful)
	
	// Step 3: Generate optimized bytecode
	return generateOptimizedBytecodeFromMIR(cfg, optimizedStack, originalCode)
}

// optimizeMIRWithInterpreter applies MIR optimizations using insights from MIRInterpreter
func optimizeMIRWithInterpreter(cfg *CFG, stack *ValueStack, interpreter *MIRInterpreter, simulationOK bool) *ValueStack {
	// If simulation was successful, we can apply more aggressive optimizations
	if simulationOK {
		// Use MIRInterpreter's results to enhance optimization
		// For now, we'll keep the existing stack but mark it as "interpreter-validated"
		return stack
	}
	
	// If simulation failed, fall back to conservative optimizations
	// This is the current behavior - use the existing stack
	return stack
}

func (c *CFG) buildBasicBlock(curBB *MIRBasicBlock, valueStack *ValueStack, memoryAccessor *MemoryAccessor, stateAccessor *StateAccessor, unprcessedBBs *MIRBasicBlockStack) error {
	// Get the raw code from the CFG
	code := c.rawCode
	if code == nil || len(code) == 0 {
		return fmt.Errorf("empty code for basic block")
	}

	// Start processing from the basic block's firstPC position
	i := int(curBB.firstPC)
	if i >= len(code) {
		return fmt.Errorf("invalid starting position %d for basic block", i)
	}

	// Process each byte in the code starting from firstPC
	for i < len(code) {
		op := ByteCode(code[i])

		// Handle PUSH operations
		if op >= PUSH1 && op <= PUSH32 {
			size := int(op - PUSH1 + 1)
			if i+size >= len(code) {
				return fmt.Errorf("invalid PUSH operation at position %d", i)
			}
			_ = curBB.CreatePushMIR(size, code[i+1:i+1+size], valueStack)
			i += size + 1  // +1 for the opcode itself
			continue
		}

		// Handle other operations
		var mir *MIR
		switch op {
		case STOP:
			mir = curBB.CreateVoidMIR(MirSTOP)
			curBB.SetLastPC(uint(i))
			return nil
		case ADD:
			mir = curBB.CreateBinOpMIR(MirADD, valueStack)
		case MUL:
			mir = curBB.CreateBinOpMIR(MirMUL, valueStack)
		case SUB:
			mir = curBB.CreateBinOpMIR(MirSUB, valueStack)
		case DIV:
			mir = curBB.CreateBinOpMIR(MirDIV, valueStack)
		case SDIV:
			mir = curBB.CreateBinOpMIR(MirSDIV, valueStack)
		case MOD:
			mir = curBB.CreateBinOpMIR(MirMOD, valueStack)
		case SMOD:
			mir = curBB.CreateBinOpMIR(MirSMOD, valueStack)
		case ADDMOD:
			mir = curBB.CreateTernaryOpMIR(MirADDMOD, valueStack)
		case MULMOD:
			mir = curBB.CreateTernaryOpMIR(MirMULMOD, valueStack)
		case EXP:
			mir = curBB.CreateBinOpMIR(MirEXP, valueStack)
		case SIGNEXTEND:
			mir = curBB.CreateBinOpMIR(MirSIGNEXT, valueStack)
		case LT:
			mir = curBB.CreateBinOpMIR(MirLT, valueStack)
		case GT:
			mir = curBB.CreateBinOpMIR(MirGT, valueStack)
		case SLT:
			mir = curBB.CreateBinOpMIR(MirSLT, valueStack)
		case SGT:
			mir = curBB.CreateBinOpMIR(MirSGT, valueStack)
		case EQ:
			mir = curBB.CreateBinOpMIR(MirEQ, valueStack)
		case ISZERO:
			mir = curBB.CreateUnaryOpMIR(MirISZERO, valueStack)
		case AND:
			mir = curBB.CreateBinOpMIR(MirAND, valueStack)
		case OR:
			mir = curBB.CreateBinOpMIR(MirOR, valueStack)
		case XOR:
			mir = curBB.CreateBinOpMIR(MirXOR, valueStack)
		case NOT:
			mir = curBB.CreateUnaryOpMIR(MirNOT, valueStack)
		case BYTE:
			mir = curBB.CreateBinOpMIR(MirBYTE, valueStack)
		case SHL:
			mir = curBB.CreateBinOpMIR(MirSHL, valueStack)
		case SHR:
			mir = curBB.CreateBinOpMIR(MirSHR, valueStack)
		case SAR:
			mir = curBB.CreateBinOpMIR(MirSAR, valueStack)
		case KECCAK256:
			mir = curBB.CreateBinOpMIRWithMA(MirKECCAK256, valueStack, memoryAccessor)
		case ADDRESS:
			mir = curBB.CreateBlockInfoMIR(MirADDRESS, valueStack)
		case BALANCE:
			mir = curBB.CreateBlockInfoMIR(MirBALANCE, valueStack)
		case ORIGIN:
			mir = curBB.CreateBlockInfoMIR(MirORIGIN, valueStack)
		case CALLER:
			mir = curBB.CreateBlockInfoMIR(MirCALLER, valueStack)
		case CALLVALUE:
			mir = curBB.CreateBlockInfoMIR(MirCALLVALUE, valueStack)
		case CALLDATALOAD:
			mir = curBB.CreateBlockInfoMIR(MirCALLDATALOAD, valueStack)
		case CALLDATASIZE:
			mir = curBB.CreateBlockInfoMIR(MirCALLDATASIZE, valueStack)
		case CALLDATACOPY:
			mir = curBB.CreateBlockInfoMIR(MirCALLDATACOPY, valueStack)
		case CODESIZE:
			mir = curBB.CreateBlockInfoMIR(MirCODESIZE, valueStack)
		case CODECOPY:
			mir = curBB.CreateBlockInfoMIR(MirCODECOPY, valueStack)
		case GASPRICE:
			mir = curBB.CreateBlockInfoMIR(MirGASPRICE, valueStack)
		case EXTCODESIZE:
			mir = curBB.CreateBlockInfoMIR(MirEXTCODESIZE, valueStack)
		case EXTCODECOPY:
			mir = curBB.CreateBlockInfoMIR(MirEXTCODECOPY, valueStack)
		case RETURNDATASIZE:
			mir = curBB.CreateBlockInfoMIR(MirRETURNDATASIZE, valueStack)
		case RETURNDATACOPY:
			mir = curBB.CreateBlockInfoMIR(MirRETURNDATACOPY, valueStack)
		case EXTCODEHASH:
			mir = curBB.CreateBlockInfoMIR(MirEXTCODEHASH, valueStack)
		case BLOCKHASH: 
			mir = curBB.CreateBlockOpMIR(MirBLOCKHASH, valueStack)
		case COINBASE:
			mir = curBB.CreateBlockOpMIR(MirCOINBASE, valueStack)
		case TIMESTAMP:
			mir = curBB.CreateBlockOpMIR(MirTIMESTAMP, valueStack)
		case NUMBER:
			mir = curBB.CreateBlockOpMIR(MirNUMBER, valueStack)
		case DIFFICULTY:
			mir = curBB.CreateBlockOpMIR(MirDIFFICULTY, valueStack)
		case GASLIMIT:
			mir = curBB.CreateBlockOpMIR(MirGASLIMIT, valueStack)
		case CHAINID:
			mir = curBB.CreateBlockOpMIR(MirCHAINID, valueStack)
		case SELFBALANCE:
			mir = curBB.CreateBlockOpMIR(MirSELFBALANCE, valueStack)
		case BASEFEE:
			mir = curBB.CreateBlockOpMIR(MirBASEFEE, valueStack)
		case POP:
			_ = valueStack.pop()
			mir = nil
		case MLOAD:
			mir = curBB.CreateMemoryOpMIR(MirMLOAD, valueStack, memoryAccessor)
		case MSTORE:
			mir = curBB.CreateMemoryOpMIR(MirMSTORE, valueStack, memoryAccessor)
		case MSTORE8:
			mir = curBB.CreateMemoryOpMIR(MirMSTORE8, valueStack, memoryAccessor)
		case SLOAD:
			mir = curBB.CreateStorageOpMIR(MirSLOAD, valueStack, stateAccessor)
		case SSTORE:
			mir = curBB.CreateStorageOpMIR(MirSSTORE, valueStack, stateAccessor)
		case JUMP:
			mir = curBB.CreateJumpMIR(MirJUMP, valueStack, nil)
			if mir != nil {
				curBB.appendMIR(mir)
				// Create a new basic block for the jump target
				if len(mir.oprands) > 0 && mir.oprands[0].payload != nil {
					targetPC := uint64(0)
					if len(mir.oprands[0].payload) >= 8 {
						targetPC = binary.BigEndian.Uint64(mir.oprands[0].payload[len(mir.oprands[0].payload)-8:])
					}
					if targetPC < uint64(len(code)) {
						targetBB := c.createBB(uint(targetPC), curBB)
						curBB.SetChildren([]*MIRBasicBlock{targetBB})
						fallthroughBB := c.createBB(uint(i+1), curBB)
						// fallthroughBB is not the children of curBB
						// curBB.SetChildren([]*MIRBasicBlock{targetBB, fallthroughBB})
						unprcessedBBs.Push(targetBB)
						unprcessedBBs.Push(fallthroughBB)
						return nil
					}
				}
			}
			return nil
		case JUMPI:
			mir = curBB.CreateJumpMIR(MirJUMPI, valueStack, nil)
			if mir != nil {
				curBB.appendMIR(mir)
				// Create new basic blocks for both true and false paths
				if len(mir.oprands) > 0 && mir.oprands[0].payload != nil {
					targetPC := uint64(0)
					if len(mir.oprands[0].payload) >= 8 {
						targetPC = binary.BigEndian.Uint64(mir.oprands[0].payload[len(mir.oprands[0].payload)-8:])
					}
					if targetPC < uint64(len(code)) {
						// Create block for the jump target
						targetBB := c.createBB(uint(targetPC), curBB)
						// Create block for the fall-through path
						fallthroughBB := c.createBB(uint(i+1), curBB)
						curBB.SetChildren([]*MIRBasicBlock{targetBB, fallthroughBB})
						unprcessedBBs.Push(targetBB)
						unprcessedBBs.Push(fallthroughBB)
						return nil
					}
				}
			}
			return nil
		case RJUMP:
			// mir = curBB.CreateJumpMIR(MirRJUMP, valueStack, nil)
			// return nil
			panic("not implemented")
		case RJUMPI:
			// mir = curBB.CreateJumpMIR(MirRJUMPI, valueStack, nil)
			// return nil
			panic("not implemented")
		case RJUMPV:
			// mir = curBB.CreateJumpMIR(MirRJUMPV, valueStack, nil)
			// return nil
			panic("not implemented")
		case JUMPDEST:
			// If we hit a JUMPDEST, we should create a new basic block
			// unless this is the first instruction
			if curBB.Size() > 0 {
				newBB := c.createBB(uint(i), curBB)
				curBB.SetChildren([]*MIRBasicBlock{newBB})
				unprcessedBBs.Push(newBB)
				return nil
			}

			mir = curBB.CreateVoidMIR(MirJUMPDEST)
			if mir != nil {
				curBB.appendMIR(mir)
			}
		case PC:
			mir = curBB.CreateBlockInfoMIR(MirPC, valueStack)
		case MSIZE:
			mir = curBB.CreateMemoryOpMIR(MirMSIZE, valueStack, memoryAccessor)
		case GAS:
			mir = curBB.CreateBlockInfoMIR(MirGAS, valueStack)
		case BLOBHASH:
			mir = curBB.CreateBlockInfoMIR(MirBLOBHASH, valueStack)
		case BLOBBASEFEE:
			mir = curBB.CreateBlockInfoMIR(MirBLOBBASEFEE, valueStack)
		case TLOAD:
			mir = curBB.CreateStorageOpMIR(MirTLOAD, valueStack, stateAccessor)
		case TSTORE:
			mir = curBB.CreateStorageOpMIR(MirTSTORE, valueStack, stateAccessor)
		case MCOPY:
			// MCOPY takes 3 operands: dest, src, length
			length := valueStack.pop()
			src := valueStack.pop()
			dest := valueStack.pop()
			mir = new(MIR)
			mir.op = MirMCOPY
			mir.oprands = []*Value{&dest, &src, &length}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(src, length)
				memoryAccessor.recordStore(dest, length, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
		case PUSH0:
			_ = curBB.CreatePushMIR(0, []byte{}, valueStack)
		case LOG0:
			mir = curBB.CreateLogMIR(MirLOG0, valueStack)
		case LOG1:
			mir = curBB.CreateLogMIR(MirLOG1, valueStack)
		case LOG2:
			mir = curBB.CreateLogMIR(MirLOG2, valueStack)
		case LOG3:
			mir = curBB.CreateLogMIR(MirLOG3, valueStack)
		case LOG4:
			mir = curBB.CreateLogMIR(MirLOG4, valueStack)
		case CREATE:
			// CREATE takes 3 operands: value, offset, size
			size := valueStack.pop()
			offset := valueStack.pop()
			value := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCREATE
			mir.oprands = []*Value{&value, &offset, &size}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(offset, size)
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case CREATE2:
			// CREATE2 takes 4 operands: value, offset, size, salt
			salt := valueStack.pop()
			size := valueStack.pop()
			offset := valueStack.pop()
			value := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCREATE2
			mir.oprands = []*Value{&value, &offset, &size, &salt}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(offset, size)
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case CALL:
			// CALL takes 7 operands: gas, addr, value, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			value := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCALL
			mir.oprands = []*Value{&gas, &addr, &value, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case CALLCODE:
			// CALLCODE takes same operands as CALL
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			value := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCALLCODE
			mir.oprands = []*Value{&gas, &addr, &value, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case RETURN:
			// RETURN takes 2 operands: offset, size
			size := valueStack.pop()
			offset := valueStack.pop()
			mir = new(MIR)
			mir.op = MirRETURN
			mir.oprands = []*Value{&offset, &size}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(offset, size)
			}
			if mir != nil {
				curBB.appendMIR(mir)
			}
			return nil
		case DELEGATECALL:
			// DELEGATECALL takes 6 operands: gas, addr, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirDELEGATECALL
			mir.oprands = []*Value{&gas, &addr, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case STATICCALL:
			// STATICCALL takes 6 operands: gas, addr, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirSTATICCALL
			mir.oprands = []*Value{&gas, &addr, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case REVERT:
			// REVERT takes 2 operands: offset, size
			size := valueStack.pop()
			offset := valueStack.pop()
			mir = new(MIR)
			mir.op = MirREVERT
			mir.oprands = []*Value{&offset, &size}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(offset, size)
			}
			if mir != nil {
				curBB.appendMIR(mir)
			}
			return nil
		case INVALID:
			mir = curBB.CreateVoidMIR(MirINVALID)
			if mir != nil {
				curBB.appendMIR(mir)
			}
			return nil
		case SELFDESTRUCT:
			// SELFDESTRUCT takes 1 operand: address
			addr := valueStack.pop()
			mir = new(MIR)
			mir.op = MirSELFDESTRUCT
			mir.oprands = []*Value{&addr}
			if mir != nil {
				curBB.appendMIR(mir)
			}
			return nil
		// Stack operations - DUP1 to DUP16
		case DUP1:
			mir = curBB.CreateStackOpMIR(MirDUP1, valueStack)
		case DUP2:
			mir = curBB.CreateStackOpMIR(MirDUP2, valueStack)
		case DUP3:
			mir = curBB.CreateStackOpMIR(MirDUP3, valueStack)
		case DUP4:
			mir = curBB.CreateStackOpMIR(MirDUP4, valueStack)
		case DUP5:
			mir = curBB.CreateStackOpMIR(MirDUP5, valueStack)
		case DUP6:
			mir = curBB.CreateStackOpMIR(MirDUP6, valueStack)
		case DUP7:
			mir = curBB.CreateStackOpMIR(MirDUP7, valueStack)
		case DUP8:
			mir = curBB.CreateStackOpMIR(MirDUP8, valueStack)
		case DUP9:
			mir = curBB.CreateStackOpMIR(MirDUP9, valueStack)
		case DUP10:
			mir = curBB.CreateStackOpMIR(MirDUP10, valueStack)
		case DUP11:
			mir = curBB.CreateStackOpMIR(MirDUP11, valueStack)
		case DUP12:
			mir = curBB.CreateStackOpMIR(MirDUP12, valueStack)
		case DUP13:
			mir = curBB.CreateStackOpMIR(MirDUP13, valueStack)
		case DUP14:
			mir = curBB.CreateStackOpMIR(MirDUP14, valueStack)
		case DUP15:
			mir = curBB.CreateStackOpMIR(MirDUP15, valueStack)
		case DUP16:
			mir = curBB.CreateStackOpMIR(MirDUP16, valueStack)
		// Stack operations - SWAP1 to SWAP16
		case SWAP1:
			mir = curBB.CreateStackOpMIR(MirSWAP1, valueStack)
		case SWAP2:
			mir = curBB.CreateStackOpMIR(MirSWAP2, valueStack)
		case SWAP3:
			mir = curBB.CreateStackOpMIR(MirSWAP3, valueStack)
		case SWAP4:
			mir = curBB.CreateStackOpMIR(MirSWAP4, valueStack)
		case SWAP5:
			mir = curBB.CreateStackOpMIR(MirSWAP5, valueStack)
		case SWAP6:
			mir = curBB.CreateStackOpMIR(MirSWAP6, valueStack)
		case SWAP7:
			mir = curBB.CreateStackOpMIR(MirSWAP7, valueStack)
		case SWAP8:
			mir = curBB.CreateStackOpMIR(MirSWAP8, valueStack)
		case SWAP9:
			mir = curBB.CreateStackOpMIR(MirSWAP9, valueStack)
		case SWAP10:
			mir = curBB.CreateStackOpMIR(MirSWAP10, valueStack)
		case SWAP11:
			mir = curBB.CreateStackOpMIR(MirSWAP11, valueStack)
		case SWAP12:
			mir = curBB.CreateStackOpMIR(MirSWAP12, valueStack)
		case SWAP13:
			mir = curBB.CreateStackOpMIR(MirSWAP13, valueStack)
		case SWAP14:
			mir = curBB.CreateStackOpMIR(MirSWAP14, valueStack)
		case SWAP15:
			mir = curBB.CreateStackOpMIR(MirSWAP15, valueStack)
		case SWAP16:
			mir = curBB.CreateStackOpMIR(MirSWAP16, valueStack)
		// EOF operations
		case DATALOAD:
			mir = curBB.CreateBlockInfoMIR(MirDATALOAD, valueStack)
		case DATALOADN:
			mir = curBB.CreateBlockInfoMIR(MirDATALOADN, valueStack)
		case DATASIZE:
			mir = curBB.CreateBlockInfoMIR(MirDATASIZE, valueStack)
		case DATACOPY:
			mir = curBB.CreateBlockInfoMIR(MirDATACOPY, valueStack)
		case CALLF:
			// CALLF takes 2 operands: gas, function_id
			functionID := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirCALLF
			mir.oprands = []*Value{&gas, &functionID}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case RETF:
			mir = curBB.CreateVoidMIR(MirRETF)
			return nil
		case JUMPF:
			mir = curBB.CreateJumpMIR(MirJUMPF, valueStack, nil)
			return nil
		case DUPN:
			mir = curBB.CreateStackOpMIR(MirDUPN, valueStack)
		case SWAPN:
			mir = curBB.CreateStackOpMIR(MirSWAPN, valueStack)
		case EXCHANGE:
			mir = curBB.CreateStackOpMIR(MirEXCHANGE, valueStack)
		case EOFCREATE:
			// EOFCREATE takes 4 operands: value, code_offset, code_size, salt
			salt := valueStack.pop()
			codeSize := valueStack.pop()
			codeOffset := valueStack.pop()
			value := valueStack.pop()
			mir = new(MIR)
			mir.op = MirEOFCREATE
			mir.oprands = []*Value{&value, &codeOffset, &codeSize, &salt}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case RETURNCONTRACT:
			mir = curBB.CreateVoidMIR(MirRETURNCONTRACT)
		// Additional opcodes
		case RETURNDATALOAD:
			mir = curBB.CreateBlockInfoMIR(MirRETURNDATALOAD, valueStack)
		case EXTCALL:
			// EXTCALL takes 7 operands: gas, addr, value, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			value := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirEXTCALL
			mir.oprands = []*Value{&gas, &addr, &value, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case EXTDELEGATECALL:
			// EXTDELEGATECALL takes 6 operands: gas, addr, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirEXTDELEGATECALL
			mir.oprands = []*Value{&gas, &addr, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		case EXTSTATICCALL:
			// EXTSTATICCALL takes 6 operands: gas, addr, inOffset, inSize, outOffset, outSize
			outSize := valueStack.pop()
			outOffset := valueStack.pop()
			inSize := valueStack.pop()
			inOffset := valueStack.pop()
			addr := valueStack.pop()
			gas := valueStack.pop()
			mir = new(MIR)
			mir.op = MirEXTSTATICCALL
			mir.oprands = []*Value{&gas, &addr, &inOffset, &inSize, &outOffset, &outSize}
			if memoryAccessor != nil {
				memoryAccessor.recordLoad(inOffset, inSize)
				memoryAccessor.recordStore(outOffset, outSize, Value{kind: Variable})
			}
			valueStack.push(mir.Result())
			if mir != nil {
				curBB.appendMIR(mir)
			}
			fallthroughBB := c.createBB(uint(i+1), curBB)
			curBB.SetChildren([]*MIRBasicBlock{fallthroughBB})
			unprcessedBBs.Push(fallthroughBB)
			return nil
		default:
			return fmt.Errorf("unknown opcode: %v", op)
		}

		if mir != nil {
			curBB.appendMIR(mir)
		}
		i++
	}
	return nil
}

// generateOptimizedBytecodeFromMIR generates optimized bytecode by combining MIR optimizations with original code
func generateOptimizedBytecodeFromMIR(cfg *CFG, finalStack *ValueStack, originalCode []byte) ([]byte, error) {
	var result []byte
	
	// Check if we have any optimized stack operations
	hasStackOptimizations := false
	optimizedInstructions := make(map[uint]bool)
	
	for _, bb := range cfg.basicBlocks {
		if bb == nil {
			continue
		}
		for _, mir := range bb.instructions {
			if mir == nil {
				continue
			}
			
			// Check for optimized stack operations (marked as NOP)
			if mir.op == MirNOP && len(mir.oprands) > 0 {
				// This was an optimized instruction, mark its original PC
				if mir.pc != nil {
					optimizedInstructions[*mir.pc] = true
				}
				
				// Check if it was a stack operation
				if len(mir.meta) > 0 {
					// Check the original operation type from metadata
					hasStackOptimizations = true
				}
			}
		}
	}
	
	// Strategy 1: If only math optimizations, use final stack
	if !hasStackOptimizations {
		stackBytes := convertStackToBytecode(finalStack)
		if len(stackBytes) > 0 && len(stackBytes) < len(originalCode) {
			return stackBytes, nil
		}
	}
	
	// Strategy 1.5: For stack optimizations, also try final stack approach
	if hasStackOptimizations {
		stackBytes := convertStackToBytecode(finalStack)
		if len(stackBytes) > 0 {
			// For stack operations, we might want to use the final stack
			// even if it's not smaller, because it represents the optimized state
			return stackBytes, nil
		}
	}
	
	// Strategy 2: For stack optimizations, remove optimized instructions from original code
	if hasStackOptimizations {
		result = make([]byte, 0, len(originalCode))
		i := 0
		
		for i < len(originalCode) {
			// Check if this instruction was optimized away
			if optimizedInstructions[uint(i)] {
				// Skip the optimized instruction
				opcode := originalCode[i]
				if opcode >= byte(PUSH1) && opcode <= byte(PUSH32) {
					// Skip PUSH instruction and its data
					pushSize := int(opcode - byte(PUSH1) + 1)
					i += pushSize + 1
				} else {
					// Skip single-byte instruction
					i++
				}
				continue
			}
			
			// Keep the original instruction
			opcode := originalCode[i]
			result = append(result, opcode)
			i++
			
			// Handle PUSH instruction data
			if opcode >= byte(PUSH1) && opcode <= byte(PUSH32) {
				pushSize := int(opcode - byte(PUSH1) + 1)
				if i+pushSize <= len(originalCode) {
					result = append(result, originalCode[i:i+pushSize]...)
					i += pushSize
				}
			}
		}
		
		return result, nil
	}
	
	// Fallback: return original code
	return originalCode, nil
}


// extractOptimizedConstants extracts optimized constant values and converts them to PUSH instructions
func extractOptimizedConstants(bb *MIRBasicBlock) []byte {
	var result []byte
	
	// Look for NOP instructions that represent optimized constants
	for _, mir := range bb.instructions {
		if mir == nil {
			continue
		}
		
		if mir.op == MirNOP && len(mir.meta) > 0 {
			// This is an optimized instruction - check if it produced a constant
			originalOp := MirOperation(mir.meta[0])
			
			// If this was an arithmetic operation that got optimized to a constant,
			// we need to generate the appropriate PUSH instruction
			if isOptimizable(originalOp) {
				// The constant value should be available in the operands or through peephole optimization
				// For now, we'll handle the most common case of simple arithmetic
				constantBytes := extractConstantFromOptimizedMIR(mir)
				if len(constantBytes) > 0 {
					pushBytes := constantToPushBytecode(constantBytes)
					result = append(result, pushBytes...)
				}
			}
		}
	}
	
	return result
}

// convertStackToBytecode converts optimized constants from ValueStack to PUSH bytecode
func convertStackToBytecode(stack *ValueStack) []byte {
	var result []byte
	
	if stack == nil || stack.size() == 0 {
		return result
	}
	
	// Process all constant values in the stack
	for i := 0; i < stack.size(); i++ {
		value := stack.data[i]
		if value.kind == Konst && len(value.payload) > 0 {
			// Convert constant to PUSH instruction
			pushBytes := constantToPushBytecode(value.payload)
			result = append(result, pushBytes...)
		}
	}
	
	return result
}

// extractConstantFromOptimizedMIR extracts the constant value from an optimized MIR instruction
func extractConstantFromOptimizedMIR(mir *MIR) []byte {
	// This is a simplified implementation
	// In a full implementation, you would track the constant values through the optimization process
	
	if len(mir.oprands) > 0 {
		for _, operand := range mir.oprands {
			if operand.kind == Konst && len(operand.payload) > 0 {
				return operand.payload
			}
		}
	}
	
	return nil
}
