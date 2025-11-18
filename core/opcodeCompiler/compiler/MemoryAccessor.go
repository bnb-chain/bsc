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

	// Attempt to assemble a constant range from prior writes (e.g., MSTORE8 sequences)
	// Only attempt for exact 32-byte requests with constant offset
	if size != nil && offset != nil && size.Eq(uint256.NewInt(32)) && offset.IsUint64() {
		base := offset.Uint64()
		// Track which bytes are known
		known := make([]bool, 32)
		out := make([]byte, 32)
		// Apply writes in order (program order), later writes may overwrite earlier ones
		for _, w := range a.writes {
			if !w.offset.IsConst() || !w.size.IsConst() || w.Value.kind != Konst {
				continue
			}
			wOff := uint256.NewInt(0).SetBytes(w.offset.payload)
			wSz := uint256.NewInt(0).SetBytes(w.size.payload)
			if !wOff.IsUint64() || !wSz.IsUint64() {
				continue
			}
			wo := wOff.Uint64()
			ws := wSz.Uint64()
			// Fast paths for 1 and 32 byte writes
			if ws == 0 {
				continue
			}
			// Compute overlap between [wo, wo+ws) and [base, base+32)
			endW := wo + ws
			endT := base + 32
			if endW <= base || wo >= endT {
				continue
			}
			start := wo
			if start < base {
				start = base
			}
			end := endW
			if end > endT {
				end = endT
			}
			// Copy overlapped bytes from write payload
			// For 32-byte payloads, w.Value.payload is right-aligned already
			for pos := start; pos < end; pos++ {
				idx := int(pos - base)
				// Source index within write payload
				if ws == 1 {
					// Single byte write (MSTORE8): payload[0] is the stored byte
					out[idx] = w.Value.payload[0]
					known[idx] = true
				} else if ws == 32 {
					// 32-byte write: payload length should be 32
					// Map pos into payload index
					pIdx := int(32 - (endW - pos))
					if pIdx >= 0 && pIdx < len(w.Value.payload) {
						out[idx] = w.Value.payload[pIdx]
						known[idx] = true
					}
				} else if len(w.Value.payload) >= int(ws) {
					// Generic case: copy byte-wise
					pIdx := int(pos - wo)
					if pIdx >= 0 && pIdx < len(w.Value.payload) {
						out[idx] = w.Value.payload[pIdx]
						known[idx] = true
					}
				}
			}
		}
		// Verify full coverage
		allKnown := true
		for i := 0; i < 32; i++ {
			if !known[i] {
				allKnown = false
				break
			}
		}
		if allKnown {
			return Value{kind: Konst, payload: out, u: uint256.NewInt(0).SetBytes(out)}, true
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
