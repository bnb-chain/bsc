package parlia

import (
	"math/rand"
	"time"

	cmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	wiggleTimeBeforeFork       = 500 * time.Millisecond // Random delay (per signer) to allow concurrent signers
	fixedBackOffTimeBeforeFork = 200 * time.Millisecond
)

func (p *Parlia) delayForRamanujanFork(snap *Snapshot, header *types.Header) time.Duration {
	delay := time.Until(time.UnixMilli(int64(header.MilliTimestamp()))) // nolint: gosimple
	if p.chainConfig.IsRamanujan(header.Number) {
		return delay
	}
	if header.Difficulty.Cmp(diffNoTurn) == 0 {
		// It's not our turn explicitly to sign, delay it a bit
		wiggle := time.Duration(len(snap.Validators)/2+1) * wiggleTimeBeforeFork
		delay += fixedBackOffTimeBeforeFork + time.Duration(rand.Int63n(int64(wiggle)))
	}
	return delay
}

func (p *Parlia) blockTimeForRamanujanFork(snap *Snapshot, header, parent *types.Header) uint64 {
	blockTime := parent.MilliTimestamp() + uint64(snap.BlockInterval)
	if p.chainConfig.IsRamanujan(header.Number) {
		blockTime = blockTime + p.backOffTime(snap, header, p.val)
	}
	if now := uint64(time.Now().UnixMilli()); blockTime < now {
		blockTime = uint64(cmath.CeilDiv(int(now), 250)) * 250
	}
	return blockTime
}

func (p *Parlia) blockTimeVerifyForRamanujanFork(snap *Snapshot, header, parent *types.Header) error {
	if p.chainConfig.IsRamanujan(header.Number) {
		if header.MilliTimestamp() < parent.MilliTimestamp()+uint64(snap.BlockInterval)+p.backOffTime(snap, header, header.Coinbase) {
			return consensus.ErrFutureBlock
		}
	}
	return nil
}
