package vm

import (
	"testing"
)

// FuzzB20Decoders drives every calldata decoder over arbitrary bytes.
func FuzzB20Decoders(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 32))
	f.Add(make([]byte, 64))
	// A well-formed bytes[] of one empty element.
	good := make([]byte, 0, 128)
	good = append(good, u256hash(32).Bytes()...) // offset to the array
	good = append(good, u256hash(1).Bytes()...)  // one element
	good = append(good, u256hash(32).Bytes()...) // element offset
	good = append(good, u256hash(0).Bytes()...)  // element length
	f.Add(good)
	// The offset word set to the top of uint64, which is what wraps.
	wrap := make([]byte, 96)
	for i := 24; i < 32; i++ {
		wrap[i] = 0xff
	}
	f.Add(wrap)

	f.Fuzz(func(t *testing.T, args []byte) {
		inInput := func(b []byte) bool { return len(b) <= len(args) }

		_, _ = readUint8Array(args)

		for i := 0; i < 4; i++ {
			if _, err := readWord(args, i); err == nil {
				// nothing to check: readWord copies
				_ = err
			}
			_, _ = readAddress(args, i)
			_, _ = readU64(args, i)
			_, _ = readStrictUint8(args, i)

			if s, err := readStringArg(args, i); err == nil && len(s) > len(args) {
				t.Fatalf("readStringArg(%d) returned %d bytes from a %d-byte input", i, len(s), len(args))
			}
			if b, err := readBytesArg(args, i); err == nil && !inInput(b) {
				t.Fatalf("readBytesArg(%d) escaped its input", i)
			}
			if ws, err := readWordArray(args, i); err == nil && len(ws)*32 > len(args) {
				t.Fatalf("readWordArray(%d) returned %d words from a %d-byte input", i, len(ws), len(args))
			}
			if arr, err := readBytesArray(args, i); err == nil {
				for k, elem := range arr {
					if !inInput(elem) {
						t.Fatalf("readBytesArray(%d)[%d] escaped its input", i, k)
					}
				}
			}
		}
	})
}
