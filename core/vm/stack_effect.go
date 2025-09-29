// Copyright 2025 The go-ethereum Authors
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

import "github.com/ethereum/go-ethereum/params"

// OpStackCounts returns the number of values popped from and pushed to the stack
// by the given opcode. The stack effect is deterministic per opcode across forks,
// so we derive it from the canonical instruction set.
func OpStackCounts(op OpCode) (pops int, pushes int) {
	// Use the Prague instruction set for full opcode coverage.
	jt := newPragueInstructionSet()
	entry := jt[op]
	if entry == nil {
		return 0, 0
	}
	// minStack stores the required pops. maxStack = StackLimit + pops - pushes.
	pops = entry.minStack
	pushes = pops + int(params.StackLimit) - entry.maxStack
	if pushes < 0 {
		pushes = 0
	}
	return
}

// NextStackSize computes the stack height after executing the given opcode,
// given the current stack height before execution.
func NextStackSize(op OpCode, before int) int {
	pops, pushes := OpStackCounts(op)
	return before - pops + pushes
}
