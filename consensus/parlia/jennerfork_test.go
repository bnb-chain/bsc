package parlia

import (
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func jennerTestConfig() *params.ChainConfig {
	config := *params.ParliaTestChainConfig
	zero, fork := uint64(0), uint64(1000)
	config.BohrTime, config.LorentzTime, config.MaxwellTime = &zero, &zero, &zero
	config.JennerTime = &fork
	return &config
}

func jennerTestSnapshot(parent *types.Header) *Snapshot {
	snap := &Snapshot{
		Number: parent.Number.Uint64(), Hash: parent.Hash(), EpochLength: 1000,
		TurnLength: 4, BlockInterval: 450, Recents: make(map[uint64]common.Address),
		Validators: make(map[common.Address]*ValidatorInfo),
	}
	for i := 1; i <= 21; i++ {
		snap.Validators[common.BigToAddress(big.NewInt(int64(i)))] = &ValidatorInfo{}
	}
	return snap
}

func TestJennerBackoffAndTimestampBoundary(t *testing.T) {
	config := jennerTestConfig()
	p := &Parlia{chainConfig: config}
	for _, recent := range []bool{false, true} {
		for _, parentTime := range []uint64{999, 1000, 1001} {
			parent := &types.Header{Number: big.NewInt(998), Time: parentTime}
			header := &types.Header{Number: big.NewInt(999), Time: 1005}
			snap := jennerTestSnapshot(parent)
			if recent {
				for i := uint64(0); i < uint64(snap.TurnLength); i++ {
					snap.Recents[snap.Number-i] = snap.inturnValidator()
				}
			}
			var delays []uint64
			for _, validator := range snap.validators() {
				if snap.inturn(validator) {
					if delay := p.backOffTime(snap, parent, header, validator); delay != 0 {
						t.Fatalf("in-turn backoff = %d", delay)
					}
					continue
				}
				delay := p.backOffTime(snap, parent, header, validator)
				delays = append(delays, delay)
				header.Coinbase = validator
				timestamp := parent.MilliTimestamp() + snap.BlockInterval + delay
				header.Time = timestamp / 1000
				header.SetMilliseconds(timestamp)
				if err := p.blockTimeVerifyForRamanujanFork(snap, header, parent); err != nil {
					t.Fatal(err)
				}
				timestamp--
				header.Time = timestamp / 1000
				header.SetMilliseconds(timestamp)
				if err := p.blockTimeVerifyForRamanujanFork(snap, header, parent); err == nil {
					t.Fatal("accepted a block before its eligible backup timestamp")
				}
			}
			slices.Sort(delays)
			initial := uint64(2000)
			if parentTime >= 1000 {
				initial = 1000
			}
			for rank, delay := range delays {
				want := initial + uint64(rank)*wiggleTime
				if recent {
					if rank == 0 {
						want = 0
					} else {
						want = initial + uint64(rank-1)*wiggleTime
					}
				}
				if delay != want {
					t.Fatalf("recent=%t parentTime=%d rank=%d: delay=%d want=%d", recent, parentTime, rank, delay, want)
				}
			}
		}
	}
}
