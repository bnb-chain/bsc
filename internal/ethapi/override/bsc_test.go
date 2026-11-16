package override

import (
	"math"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
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

func TestBSCSlotNumberOverrides(t *testing.T) {
	slot := func(v uint64) *uint64 { return &v }
	number := hexutil.Big(*big.NewInt(200))
	before, after := hexutil.Uint64(19), hexutil.Uint64(20)
	for _, test := range []struct {
		name       string
		bsc        bool
		timestamp  uint64
		parentSlot *uint64
		overrides  *BlockOverrides
		want       *uint64
	}{
		{name: "number", bsc: true, timestamp: 20, parentSlot: slot(100), overrides: &BlockOverrides{Number: &number}, want: slot(200)},
		{name: "first-fork", bsc: true, timestamp: 19, overrides: &BlockOverrides{Time: &after}, want: slot(100)},
		{name: "before-fork", bsc: true, timestamp: 20, parentSlot: slot(100), overrides: &BlockOverrides{Time: &before}},
		{name: "both", bsc: true, timestamp: 19, overrides: &BlockOverrides{Number: &number, Time: &after}, want: slot(200)},
		{name: "nil", bsc: true, timestamp: 20, parentSlot: slot(100), want: slot(100)},
		{name: "empty", bsc: true, timestamp: 20, parentSlot: slot(100), overrides: &BlockOverrides{}, want: slot(100)},
		{name: "eth-number", timestamp: 20, parentSlot: slot(500), overrides: &BlockOverrides{Number: &number}, want: slot(500)},
		{name: "eth-first-fork", timestamp: 19, overrides: &BlockOverrides{Time: &after}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fork := uint64(20)
			cfg := &params.ChainConfig{LondonBlock: big.NewInt(0), AmsterdamTime: &fork}
			if test.bsc {
				cfg.Parlia = &params.ParliaConfig{}
			}
			header := &types.Header{Number: big.NewInt(100), Time: test.timestamp, SlotNumber: test.parentSlot}
			original := types.CopyHeader(header)
			ctx := vm.BlockContext{BlockNumber: big.NewInt(100), Time: test.timestamp}
			if header.SlotNumber != nil {
				ctx.SlotNum = *header.SlotNumber
			}
			if err := test.overrides.ApplyFor(cfg, &ctx); err != nil {
				t.Fatal(err)
			}
			got := test.overrides.MakeHeaderFor(cfg, header)
			want := uint64(0)
			if test.want != nil {
				want = *test.want
			}
			if ctx.SlotNum != want || !reflect.DeepEqual(got.SlotNumber, test.want) {
				t.Fatalf("slot context=%d header=%v, want %v", ctx.SlotNum, got.SlotNumber, test.want)
			}
			if ctx.BlockNumber.Cmp(got.Number) != 0 || ctx.Time != got.Time {
				t.Fatal("header/context mismatch")
			}
			if !reflect.DeepEqual(types.CopyHeader(header), original) {
				t.Fatal("override mutated the original header")
			}
		})
	}
}
