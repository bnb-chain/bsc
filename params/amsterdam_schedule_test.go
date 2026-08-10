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

import "testing"

// TestBSCForksAreSchedulable pins that every timestamp fork BSC knows about can
// actually be scheduled. CheckConfigForkOrder refuses a configuration that sets a
// fork's timestamp without a blobSchedule entry for it, so a missing entry does
// not surface until a node is started with that fork enabled — at which point it
// is a fatal error at service registration, not a test failure.
//
// This caught Amsterdam being unschedulable on all three BSC configs: they define
// entries through Osaka only.
func TestBSCForksAreSchedulable(t *testing.T) {
	ts := uint64(1) << 40

	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
	}{
		{"bsc", BSCChainConfig},
		{"chapel", ChapelChainConfig},
		{"rialto", RialtoChainConfig},
	} {
		// Schedule the forks still unset, in order, so the ordering check passes and
		// the blobSchedule check is what is actually exercised.
		cfg := *tc.cfg
		for _, f := range []*(*uint64){
			&cfg.FermiTime, &cfg.OsakaTime, &cfg.MendelTime,
			&cfg.PasteurTime, &cfg.AmsterdamTime,
		} {
			if *f == nil {
				*f = &ts
			}
		}
		if err := cfg.CheckConfigForkOrder(); err != nil {
			t.Errorf("%s: scheduling every known fork failed: %v", tc.name, err)
		}
	}
}
