package override

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestBSCMilliRemainder(t *testing.T) {
	for _, test := range []struct {
		name    string
		in      common.Hash
		wantErr bool
	}{
		{name: "max", in: common.BigToHash(new(big.Int).SetUint64(MaxBSCMilliRemainder - 1))},
		{name: "bound", in: common.BigToHash(new(big.Int).SetUint64(MaxBSCMilliRemainder)), wantErr: true},
		{name: "wide", in: common.HexToHash("0x10000000000000000"), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BSCMilliRemainder(&test.in); (err != nil) != test.wantErr {
				t.Fatalf("BSCMilliRemainder(%x) error = %v, want error: %t", test.in, err, test.wantErr)
			}
		})
	}
}

func TestBSCMilliTimestampBounds(t *testing.T) {
	maxSeconds := uint64(math.MaxUint64) / 1000
	maxRemainder := uint64(math.MaxUint64) % 1000

	if got, err := BSCMilliTimestamp(maxSeconds, maxRemainder); err != nil || got != math.MaxUint64 {
		t.Fatalf("maximum timestamp = %d, %v; want %d, nil", got, err, uint64(math.MaxUint64))
	}
	for _, test := range []struct {
		seconds   uint64
		remainder uint64
	}{
		{seconds: maxSeconds, remainder: maxRemainder + 1},
		{seconds: maxSeconds + 1},
		{remainder: MaxBSCMilliRemainder},
	} {
		if _, err := BSCMilliTimestamp(test.seconds, test.remainder); err == nil {
			t.Fatalf("BSCMilliTimestamp(%d, %d) succeeded", test.seconds, test.remainder)
		}
	}
}
