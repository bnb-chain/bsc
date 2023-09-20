package types

import (
	"bytes"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
)

func TestSidecarEncodeDecodeRLP(t *testing.T) {

	testCases := []*Sidecar{
		{
			BlockRoot:       []byte{0x01},
			Index:           1,
			Slot:            2,
			BlockParentRoot: []byte{0x03},
			ProposerIndex:   3,
			Blob:            Blob{0x04},
			KZGCommitment:   KZGCommitment{0x05},
			KZGProof:        KZGProof{0x06},
			ReceivedAt:      time.Now(),
			ReceivedFrom:    "test1",
		},
		{
			BlockRoot:       []byte{0x10, 0x11},
			Index:           123,
			Slot:            456,
			BlockParentRoot: []byte{0x12, 0x13},
			ProposerIndex:   789,
			Blob:            Blob{0x14, 0x15},
			KZGCommitment:   KZGCommitment{0x16, 0x17},
			KZGProof:        KZGProof{0x18, 0x19},
			ReceivedAt:      time.Now().Add(-1 * time.Hour),
			ReceivedFrom:    "test2",
		},
	}

	for _, tc := range testCases {
		// Encode to RLP
		var buf bytes.Buffer
		if err := tc.EncodeRLP(&buf); err != nil {
			t.Fatal(err)
		}

		// Decode back to new struct
		sc := &Sidecar{}
		if err := sc.DecodeRLP(rlp.NewStream(&buf, 0)); err != nil {
			t.Fatal(err)
		}

		// Check all fields match
		if !bytes.Equal(tc.BlockRoot, sc.BlockRoot) {
			t.Errorf("BlockRoot mismatch: %v %v", tc.BlockRoot, sc.BlockRoot)
		}
		if tc.Index != sc.Index {
			t.Errorf("Index mismatch: %v %v", tc.Index, sc.Index)
		}
		if !bytes.Equal(tc.BlockParentRoot, sc.BlockParentRoot) {
			t.Errorf("BlockParentRoot mismatch: %v %v", tc.BlockParentRoot, sc.BlockParentRoot)
		}
		//...check other fields

		//// Roundtrip test
		//if !bytes.Equal(tc.Encode(), sc.Encode()) {
		//	t.Errorf("Encoding roundtrip failed")
		//}
	}

}
