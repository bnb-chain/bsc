// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package params

import (
	"strings"
	"testing"
)

var bscConfigs = []struct {
	name string
	cfg  *ChainConfig
}{
	{"bsc", BSCChainConfig},
	{"chapel", ChapelChainConfig},
	{"rialto", RialtoChainConfig},
}

// TestBSCForksAreSchedulable pins that every timestamp fork BSC is ready to ship
// can actually be scheduled. CheckConfigForkOrder refuses a configuration that
// sets a fork's timestamp without a blobSchedule entry for it, so a missing entry
// does not surface until a node is started with that fork enabled — at which
// point it is a fatal error at service registration, not a test failure.
func TestBSCForksAreSchedulable(t *testing.T) {
	ts := uint64(1) << 40

	for _, tc := range bscConfigs {
		// Schedule the forks still unset, in order, so the ordering check passes and
		// the blobSchedule check is what is actually exercised. Rialto leaves
		// MendelTime unset, so Pasteur cannot be scheduled there on its own.
		cfg := *tc.cfg
		for _, f := range []*(*uint64){
			&cfg.FermiTime, &cfg.OsakaTime, &cfg.MendelTime, &cfg.PasteurTime,
		} {
			if *f == nil {
				*f = &ts
			}
		}
		if err := cfg.CheckConfigForkOrder(); err != nil {
			t.Errorf("%s: scheduling every shippable fork failed: %v", tc.name, err)
		}
	}
}

// TestBSCAmsterdamIsNotSchedulable is a tripwire, not an invariant we want to
// keep. Amsterdam brings EIP-7928 block access lists, and this tree carries only
// the scaffolding: headers are validated for BlockAccessListHash but never built
// with one. Scheduling Amsterdam therefore halts block production at the fork
// block — the first Amsterdam block is rejected by its own peers with
//
//	rlp: input string too short for common.Hash, decoding into
//	(types.Header).BlockAccessListHash
//
// and sealing stops with "unknown ancestor". The missing blobSchedule entry is
// what currently stands between an operator and that halt, so it stays missing
// until BAL block production is wired up.
//
// If this test starts failing, the entry has been added. That is the right move
// once headers carry a real BlockAccessListHash — verify that they do, then
// delete this test rather than working around it.
func TestBSCAmsterdamIsNotSchedulable(t *testing.T) {
	ts := uint64(1) << 40

	for _, tc := range bscConfigs {
		cfg := *tc.cfg
		for _, f := range []*(*uint64){
			&cfg.FermiTime, &cfg.OsakaTime, &cfg.MendelTime, &cfg.PasteurTime,
			&cfg.AmsterdamTime,
		} {
			if *f == nil {
				*f = &ts
			}
		}
		// Assert on the reason, not just on failure: an ordering error would let
		// this pass while Amsterdam was in fact schedulable.
		err := cfg.CheckConfigForkOrder()
		if err == nil || !strings.Contains(err.Error(), `missing entry for fork "amsterdam" in blobSchedule`) {
			t.Errorf("%s: Amsterdam is now schedulable (err = %v) — confirm block "+
				"production fills in header.BlockAccessListHash before allowing it", tc.name, err)
		}
	}
}
