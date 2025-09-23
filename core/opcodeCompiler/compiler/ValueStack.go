package compiler

import (
	"github.com/holiman/uint256"
)

type ValueKind int

const (
	Konst     ValueKind = 0 + iota
	Arguments           // The input argument
	Variable            // The runtime determined
	Unknown             // Illegal
)

type Value struct {
	kind    ValueKind
	def     *MIR
	use     []*MIR
	payload []byte
	u       *uint256.Int // pre-decoded constant value (for Konst)
}

type ValueStack struct {
	data []Value
}

func (s *ValueStack) push(ptr *Value) {
	if ptr == nil {
		return
	}
	s.data = append(s.data, *ptr)
}

func (s *ValueStack) pop() (value Value) {
	if len(s.data) == 0 {
		// Return a default value if stack is empty
		return Value{kind: Unknown}
	}
	val := s.data[len(s.data)-1]
	s.data = s.data[:len(s.data)-1]
	return val
}

func (s *ValueStack) size() int {
	return len(s.data)
}

// peek returns a pointer to the nth item from the top of the stack (0-indexed)
// peek(0) returns the top item, peek(1) returns the second item, etc.
func (s *ValueStack) peek(n int) *Value {
	if n < 0 || n >= len(s.data) {
		return nil
	}
	// Stack grows from left to right, so top is at the end
	index := len(s.data) - 1 - n
	return &s.data[index]
}

// swap exchanges the items at positions i and j from the top of the stack (0-indexed)
func (s *ValueStack) swap(i, j int) {
	if i < 0 || i >= len(s.data) || j < 0 || j >= len(s.data) {
		return
	}
	// Convert to actual array indices
	indexI := len(s.data) - 1 - i
	indexJ := len(s.data) - 1 - j
	s.data[indexI], s.data[indexJ] = s.data[indexJ], s.data[indexI]
}

func newValue(kind ValueKind, def *MIR, use *MIR, payload []byte) *Value {
	value := new(Value)
	value.kind = kind
	value.def = def
	if use != nil {
		value.use = []*MIR{use}
	}
	value.payload = payload
	if kind == Konst {
		// Pre-decode constant to avoid per-op decoding and cache lookups
		if len(payload) == 0 {
			value.u = uint256.NewInt(0)
		} else {
			value.u = uint256.NewInt(0).SetBytes(payload)
		}
	}
	return value
}

// IsConst returns true if the value is a constant
func (v *Value) IsConst() bool {
	return v.kind == Konst
}

// ConstValue returns the constant value as a uint64
func (v *Value) ConstValue() uint64 {
	if !v.IsConst() {
		return 0
	}
	var result uint64
	for i, b := range v.payload {
		result |= uint64(b) << (i * 8)
	}
	return result
}

// Equal returns true if two values are equal
func (v *Value) Equal(other *Value) bool {
	if v.kind != other.kind {
		return false
	}
	if v.kind == Konst {
		if len(v.payload) != len(other.payload) {
			return false
		}
		for i := range v.payload {
			if v.payload[i] != other.payload[i] {
				return false
			}
		}
		return true
	}
	return v.def == other.def // fallback for non-const
}
