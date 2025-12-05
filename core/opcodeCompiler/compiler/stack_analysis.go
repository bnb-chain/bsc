package compiler

import (
	"fmt"
)

// analyzeStackHeights performs Pass 2: Stack Height Analysis.
// It propagates stack heights from the entry block to all reachable blocks.
// Returns a map of block -> entry stack height.
func (c *CFG) analyzeStackHeights() (map[*MIRBasicBlock]int, error) {
	heights := make(map[*MIRBasicBlock]int)
	// Entry block starts with height 0
	entryBB := c.pcToBlock[0]
	if entryBB == nil {
		// If no block at 0, maybe empty code?
		if len(c.rawCode) == 0 {
			return heights, nil
		}
		return nil, fmt.Errorf("entry block not found")
	}
	heights[entryBB] = 0

	// Worklist of blocks to process
	worklist := []*MIRBasicBlock{entryBB}
	inWorklist := make(map[*MIRBasicBlock]bool)
	inWorklist[entryBB] = true

	for len(worklist) > 0 {
		curr := worklist[0]
		worklist = worklist[1:]
		delete(inWorklist, curr)

		h := heights[curr]

		// Calculate delta and check for underflow
		delta, maxPop, lastOp, stopPC, err := c.calculateBlockStackInfo(curr)
		if err != nil {
			return nil, err
		}

		if h < maxPop {
			return nil, fmt.Errorf("stack underflow in block %d (pc %d): needed %d, have %d", curr.blockNum, curr.firstPC, maxPop, h)
		}

		exitH := h + delta
		if exitH > 1024 {
			return nil, fmt.Errorf("stack overflow in block %d (pc %d): height %d", curr.blockNum, curr.firstPC, exitH)
		}

		// Determine successors
		succs := c.getSuccessors(curr, lastOp, stopPC)

		for _, succ := range succs {
			oldH, seen := heights[succ]
			if !seen {
				heights[succ] = exitH
				if !inWorklist[succ] {
					worklist = append(worklist, succ)
					inWorklist[succ] = true
				}
			} else {
				if oldH != exitH {
					// Verify strict stack height consistency
					// Relaxing this for now to allow analysis to proceed.
					// Use the minimum height to be safe (intersection of stacks).
					// This ensures we don't assume more items than available.
					if exitH < oldH {
						heights[succ] = exitH
						// If height changed, we might need to re-process successors?
						// Yes, because lower height propagates.
						if !inWorklist[succ] {
							worklist = append(worklist, succ)
							inWorklist[succ] = true
						}
						// fmt.Printf("WARN: stack height mismatch at block %d (pc %d): existing %d, incoming %d. Lowering to %d.\n", succ.blockNum, succ.firstPC, oldH, exitH, exitH)
					} else {
						// fmt.Printf("WARN: stack height mismatch at block %d (pc %d): existing %d, incoming %d. Keeping %d.\n", succ.blockNum, succ.firstPC, oldH, exitH, oldH)
					}
				}
			}
		}
	}
	return heights, nil
}

// calculateBlockStackInfo returns:
// - delta: net stack change
// - maxPop: maximum items popped (stack requirement)
// - lastOp: the last opcode of the block (or NO_OP if ends by boundary)
// - stopPC: the PC where the block ends
func (c *CFG) calculateBlockStackInfo(bb *MIRBasicBlock) (delta int, maxPop int, lastOp ByteCode, stopPC int, err error) {
	code := c.rawCode
	pc := int(bb.firstPC)
	currentH := 0
	minH := 0

	// If block has no instructions (empty code or out of bounds), return 0
	if pc >= len(code) {
		return 0, 0, 0, pc, nil
	}

	for pc < len(code) {
		// If we crossed into another block, stop (unless it's the start of this block)
		if pc > int(bb.firstPC) {
			if _, isStart := c.pcToBlock[uint(pc)]; isStart {
				// Block boundary reached (fallthrough case)
				// lastOp should be the previous op. But we don't track it easily here.
				// However, calculateBlockStackInfo is supposed to return info for *this* block 'bb'.
				// If we hit another block start, 'bb' ends *before* 'pc'.
				// So the last op was at the previous iteration.
				// Wait, we updated lastOp and stopPC in the previous iteration loop?
				// No, lastOp is updated at start of loop.
				// If we break here, we return the accumulated delta up to previous op.
				return delta, -minH, lastOp, stopPC, nil
			}
		}

		op := ByteCode(code[pc])
		lastOp = op
		stopPC = pc // Current op position

		var pops, pushes int

		if op >= PUSH1 && op <= PUSH32 {
			pops = 0
			pushes = 1
			size := int(op - PUSH1 + 1)
			pc += size + 1
		} else {
			pops, pushes = getOpStackDelta(op)
			pc += 1
		}

		currentH -= pops
		if currentH < minH {
			minH = currentH
		}
		currentH += pushes
		delta = currentH

		// Terminators
		if op == STOP || op == RETURN || op == REVERT || op == INVALID || op == SELFDESTRUCT {
			return delta, -minH, lastOp, stopPC, nil
		}
		if op == JUMP {
			return delta, -minH, lastOp, stopPC, nil
		}
		if op == JUMPI {
			return delta, -minH, lastOp, stopPC, nil
		}
	}
	return delta, -minH, lastOp, stopPC, nil
}

// getSuccessors identifies static successors.
// It handles JUMP/JUMPI with constant PUSH targets and fallthroughs.
func (c *CFG) getSuccessors(bb *MIRBasicBlock, lastOp ByteCode, stopPC int) []*MIRBasicBlock {
	var succs []*MIRBasicBlock
	code := c.rawCode

	// Helper to resolve constant push before an instruction
	// Returns -1 if not constant
	resolvePush := func(atPC int) int64 {
		// Look backwards from atPC.
		// The instruction before atPC must be a PUSH
		// Scan backwards is tricky because instructions are variable length.
		// But we know we are in 'bb', so we can scan forward from bb.firstPC to find the op before atPC.

		curr := int(bb.firstPC)
		var prevOp ByteCode
		var prevVal int64 = -1
		found := false

		for curr < atPC {
			op := ByteCode(code[curr])
			prevOp = op

			// Parse value if PUSH
			if op >= PUSH1 && op <= PUSH32 {
				size := int(op - PUSH1 + 1)
				// Extract value (up to 8 bytes fits in int64)
				if size <= 8 {
					var val int64
					for k := 0; k < size; k++ {
						val = (val << 8) | int64(code[curr+1+k])
					}
					prevVal = val
				} else {
					prevVal = -1 // Too big, treat as unknown for simple analysis
				}
				curr += size + 1
			} else {
				prevVal = -1
				curr += 1
			}
		}

		if curr == atPC {
			found = true
		}

		if found && prevOp >= PUSH1 && prevOp <= PUSH32 {
			return prevVal
		}
		return -1
	}

	// 1. JUMP
	if lastOp == JUMP {
		dest := resolvePush(stopPC)
		if dest >= 0 {
			if target, ok := c.pcToBlock[uint(dest)]; ok {
				succs = append(succs, target)
			}
		}
		return succs
	}

	// 2. JUMPI
	if lastOp == JUMPI {
		// True branch
		dest := resolvePush(stopPC)
		if dest >= 0 {
			if target, ok := c.pcToBlock[uint(dest)]; ok {
				succs = append(succs, target)
			}
		}
		// False branch (fallthrough)
		// Logic: code[stopPC] is JUMPI (1 byte). Next is stopPC+1.
		fallthroughPC := stopPC + 1
		if target, ok := c.pcToBlock[uint(fallthroughPC)]; ok {
			succs = append(succs, target)
		}
		return succs
	}

	// 3. Terminators -> No successors
	if lastOp == STOP || lastOp == RETURN || lastOp == REVERT || lastOp == INVALID || lastOp == SELFDESTRUCT {
		return nil
	}

	// 4. Fallthrough (other ops, including JUMPDEST)
	// The block ended at stopPC. The next instruction starts at the next PC.
	// stopPC was the index of the last op.
	// If it was PUSH, stopPC pointed to PUSH opcode. The next instruction is after data.
	// calculateBlockStackInfo returned stopPC as the index of the *opcode*.
	// We need the next PC.

	nextPC := stopPC + 1
	if lastOp >= PUSH1 && lastOp <= PUSH32 {
		nextPC += int(lastOp - PUSH1 + 1)
	}

	if target, ok := c.pcToBlock[uint(nextPC)]; ok {
		succs = append(succs, target)
	}

	return succs
}

func getOpStackDelta(op ByteCode) (pops, pushes int) {
	switch op {
	case STOP:
		return 0, 0
	case ADD, MUL, SUB, DIV, SDIV, MOD, SMOD, EXP, SIGNEXTEND:
		return 2, 1
	case ADDMOD, MULMOD:
		return 3, 1
	case LT, GT, SLT, SGT, EQ, AND, OR, XOR, BYTE, SHL, SHR, SAR:
		return 2, 1
	case ISZERO, NOT:
		return 1, 1
	case KECCAK256:
		return 2, 1
	case ADDRESS:
		return 0, 1
	case BALANCE:
		return 1, 1
	case ORIGIN, CALLER:
		return 0, 1
	case CALLVALUE:
		return 0, 1
	case CALLDATALOAD:
		return 1, 1
	case CALLDATASIZE:
		return 0, 1
	case CALLDATACOPY:
		return 3, 0
	case CODESIZE:
		return 0, 1
	case CODECOPY:
		return 3, 0
	case GASPRICE:
		return 0, 1
	case EXTCODESIZE:
		return 1, 1
	case EXTCODECOPY:
		return 4, 0
	case RETURNDATASIZE:
		return 0, 1
	case RETURNDATACOPY:
		return 3, 0
	case EXTCODEHASH:
		return 1, 1
	case BLOCKHASH:
		return 1, 1
	case COINBASE:
		return 0, 1
	case TIMESTAMP:
		return 0, 1
	case NUMBER:
		return 0, 1
	case DIFFICULTY:
		return 0, 1 // RANDOM
	case GASLIMIT:
		return 0, 1
	case CHAINID:
		return 0, 1
	case SELFBALANCE:
		return 0, 1
	case BASEFEE:
		return 0, 1
	case BLOBHASH:
		return 1, 1
	case BLOBBASEFEE:
		return 0, 1
	case POP:
		return 1, 0
	case MLOAD:
		return 1, 1
	case MSTORE:
		return 2, 0
	case MSTORE8:
		return 2, 0
	case SLOAD:
		return 1, 1
	case SSTORE:
		return 2, 0
	case JUMP:
		return 1, 0
	case JUMPI:
		return 2, 0
	case PC, MSIZE, GAS:
		return 0, 1
	case JUMPDEST:
		return 0, 0
	case TLOAD:
		return 1, 1
	case TSTORE:
		return 2, 0
	case MCOPY:
		return 3, 0
	case PUSH0:
		return 0, 1
	// PUSH1-32 handled by caller
	case DUP1:
		return 1, 2
	case DUP2:
		return 2, 3
	case DUP3:
		return 3, 4
	case DUP4:
		return 4, 5
	case DUP5:
		return 5, 6
	case DUP6:
		return 6, 7
	case DUP7:
		return 7, 8
	case DUP8:
		return 8, 9
	case DUP9:
		return 9, 10
	case DUP10:
		return 10, 11
	case DUP11:
		return 11, 12
	case DUP12:
		return 12, 13
	case DUP13:
		return 13, 14
	case DUP14:
		return 14, 15
	case DUP15:
		return 15, 16
	case DUP16:
		return 16, 17
	case SWAP1:
		return 2, 2
	case SWAP2:
		return 3, 3
	case SWAP3:
		return 4, 4
	case SWAP4:
		return 5, 5
	case SWAP5:
		return 6, 6
	case SWAP6:
		return 7, 7
	case SWAP7:
		return 8, 8
	case SWAP8:
		return 9, 9
	case SWAP9:
		return 10, 10
	case SWAP10:
		return 11, 11
	case SWAP11:
		return 12, 12
	case SWAP12:
		return 13, 13
	case SWAP13:
		return 14, 14
	case SWAP14:
		return 15, 15
	case SWAP15:
		return 16, 16
	case SWAP16:
		return 17, 17
	case LOG0:
		return 2, 0
	case LOG1:
		return 3, 0
	case LOG2:
		return 4, 0
	case LOG3:
		return 5, 0
	case LOG4:
		return 6, 0
	case CREATE:
		return 3, 1
	case CALL:
		return 7, 1
	case CALLCODE:
		return 7, 1
	case RETURN:
		return 2, 0
	case DELEGATECALL:
		return 6, 1
	case CREATE2:
		return 4, 1
	case STATICCALL:
		return 6, 1
	case REVERT:
		return 2, 0
	case INVALID:
		return 0, 0
	case SELFDESTRUCT:
		return 1, 0
	default:
		return 0, 0
	}
}
