package override

import (
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// MaxBSCMilliRemainder is the exclusive upper bound for BSC millisecond remainders.
const MaxBSCMilliRemainder = 1000

// BSCMilliRemainder validates and reads the remainder carried by MixDigest.
func BSCMilliRemainder(prevRandao *common.Hash) (uint64, error) {
	v := new(big.Int).SetBytes(prevRandao[:])
	if !v.IsUint64() || v.Uint64() >= MaxBSCMilliRemainder {
		return 0, fmt.Errorf(`block override "prevRandao" on BSC must be less than %d, got %s`, MaxBSCMilliRemainder, v)
	}
	return v.Uint64(), nil
}

// BSCMilliTimestamp combines seconds and a millisecond remainder, checking overflow.
func BSCMilliTimestamp(seconds, remainder uint64) (uint64, error) {
	if remainder >= MaxBSCMilliRemainder {
		return 0, fmt.Errorf("BSC millisecond remainder %d is out of range", remainder)
	}
	if seconds > (math.MaxUint64-remainder)/1000 {
		return 0, fmt.Errorf("BSC millisecond timestamp overflows uint64")
	}
	return seconds*1000 + remainder, nil
}

// ApplyBSC applies BSC's MixDigest and millisecond timestamp semantics.
func (o *BlockOverrides) ApplyBSC(blockCtx *vm.BlockContext) error {
	if o == nil {
		return nil
	}
	if err := o.Apply(blockCtx); err != nil {
		return err
	}
	if o.Time == nil && o.PrevRandao == nil {
		return nil
	}
	var remainder uint64
	if o.PrevRandao != nil {
		var err error
		remainder, err = BSCMilliRemainder(o.PrevRandao)
		if err != nil {
			return err
		}
	}
	milli, err := BSCMilliTimestamp(blockCtx.Time, remainder)
	if err != nil {
		return err
	}
	blockCtx.MilliTimestamp = milli
	return nil
}

// ApplyFor applies BSC-specific block overrides only on BSC chains.
func (o *BlockOverrides) ApplyFor(cfg *params.ChainConfig, blockCtx *vm.BlockContext) error {
	if cfg != nil && cfg.IsInBSC() {
		return o.ApplyBSC(blockCtx)
	}
	return o.Apply(blockCtx)
}

// MakeHeaderBSC keeps a time override consistent with BSC's MixDigest field.
func (o *BlockOverrides) MakeHeaderBSC(header *types.Header) *types.Header {
	h := o.MakeHeader(header)
	if o != nil && o.Time != nil && o.PrevRandao == nil {
		h.MixDigest = common.Hash{}
	}
	return h
}

// MakeHeaderFor applies BSC-specific header overrides only on BSC chains.
func (o *BlockOverrides) MakeHeaderFor(cfg *params.ChainConfig, header *types.Header) *types.Header {
	if cfg != nil && cfg.IsInBSC() {
		return o.MakeHeaderBSC(header)
	}
	return o.MakeHeader(header)
}
