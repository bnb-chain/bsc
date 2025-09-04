package compiler

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TODO - dav: we can have more detailed info about the memory/state
// access of each method based on method selector analysis

type AccessRecord struct {
	contract *common.Address
	offset   Value
	size     Value
	Value    Value
}

type MemoryAccessor struct {
	reads   []*AccessRecord
	writes  []*AccessRecord
	buffers *bytes.Buffer
}

func (a *MemoryAccessor) recordLoad(offset Value, size Value) {
	a.reads = append(a.reads, &AccessRecord{contract: nil, offset: offset, size: size})
}

func (a *MemoryAccessor) recordStore(offset Value, size Value, value Value) {
	a.writes = append(a.writes, &AccessRecord{contract: nil, offset: offset, size: size, Value: value})
}

func (a *MemoryAccessor) getValueWithOffset(offset *uint256.Int, size *uint256.Int) Value {
	// if the place is known, return the konst value, else return unknown value.
	if val, ok := a.tryGetRecord(offset, size); ok {
		return val
	}
	return Value{kind: Variable}
}

func (a *MemoryAccessor) tryGetRecord(offset *uint256.Int, size *uint256.Int) (Value, bool) {
	// First check writes since they take precedence
	for _, record := range a.writes {
		if record.offset.IsConst() && record.size.IsConst() {
			recordOffset := uint256.NewInt(0).SetBytes(record.offset.payload)
			recordSize := uint256.NewInt(0).SetBytes(record.size.payload)
			if recordOffset.Eq(offset) && recordSize.Eq(size) {
				return record.Value, true
			}
		}
	}

	// Then check reads
	for _, record := range a.reads {
		if record.offset.IsConst() && record.size.IsConst() {
			recordOffset := uint256.NewInt(0).SetBytes(record.offset.payload)
			recordSize := uint256.NewInt(0).SetBytes(record.size.payload)
			if recordOffset.Eq(offset) && recordSize.Eq(size) {
				return record.Value, true
			}
		}
	}

	return Value{}, false
}

// rangeIsKnown checks if the memory range at the given offset and size is known
func (a *MemoryAccessor) rangeIsKnown(offset *uint256.Int, size *uint256.Int) bool {
	_, ok := a.tryGetRecord(offset, size)
	return ok
}

type StateAccessor struct {
	reads  []*AccessRecord
	writes []*AccessRecord
}

// recordStateLoad records a storage read for the given key.
func (a *StateAccessor) recordStateLoad(key Value) {
	a.reads = append(a.reads, &AccessRecord{contract: nil, offset: key})
}

// recordStateStore records a storage write for the given key and value.
func (a *StateAccessor) recordStateStore(key Value, value Value) {
	a.writes = append(a.writes, &AccessRecord{contract: nil, offset: key, Value: value})
}
