package types

import (
	"bytes"
	"io"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestBALDecoding(t *testing.T) {
	blocks := []*Block{
		makeBALTestBlock(1),
		makeBALTestBlock(2),
		makeBALTestBlock(3),
	}
	var buf bytes.Buffer
	for idx, block := range blocks {
		if err := rlp.Encode(&buf, block); err != nil {
			t.Fatalf("failed to encode block %d: %v", idx, err)
		}
	}
	stream := rlp.NewStream(bytes.NewReader(buf.Bytes()), 0)
	for i := 0; ; i++ {
		var decoded Block
		err := stream.Decode(&decoded)
		if err == io.EOF {
			if i != len(blocks) {
				t.Fatalf("decoded %d blocks, want %d", i, len(blocks))
			}
			break
		}
		if err != nil {
			t.Fatalf("error decoding block %d: %v", i, err)
		}
		if i >= len(blocks) {
			t.Fatalf("decoded more blocks than expected")
		}
		if decoded.NumberU64() != blocks[i].NumberU64() {
			t.Fatalf("unexpected block number %d, want %d", decoded.NumberU64(), blocks[i].NumberU64())
		}
	}
}

func makeBALTestBlock(number uint64) *Block {
	header := &Header{
		ParentHash: common.BigToHash(big.NewInt(int64(number))),
		Number:     new(big.Int).SetUint64(number),
		Nonce:      BlockNonce{byte(number)},
		Time:       1 + number,
		GasLimit:   uint64(30_000_000 + number),
		GasUsed:    21_000,
		Extra:      []byte("bal-test"),
		Difficulty: big.NewInt(1),
	}
	return NewBlockWithHeader(header)
}
