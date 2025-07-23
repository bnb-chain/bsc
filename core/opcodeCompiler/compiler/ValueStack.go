package compiler

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

func newValue(kind ValueKind, def *MIR, use *MIR, payload []byte) *Value {
	value := new(Value)
	value.kind = kind
	value.def = def
	if use != nil {
		value.use = []*MIR{use}
	}
	value.payload = payload
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
