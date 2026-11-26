// Copyright 2026 The go-ethereum Authors
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

package core

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

func TestGasPoolAvailable(t *testing.T) {
	// Before Amsterdam the plain remainder is all that counts.
	gp := NewGasPool(60_000_000)
	if err := gp.CheckGasLegacy(40_000_000); err != nil {
		t.Fatal(err)
	}
	if err := gp.ChargeGasLegacy(10_000_000, 30_000_000); err != nil {
		t.Fatal(err)
	}
	if have, want := gp.Available(false), uint64(30_000_000); have != want {
		t.Fatalf("legacy: have %d, want %d", have, want)
	}
	// After Amsterdam a transaction reserves min(limit, MaxTxGas) execution
	// and its full limit state gas. With 28M execution left, more than a cap,
	// any limit fits the execution dimension, so only the 55M of state left
	// bounds the limit.
	gp = NewGasPool(60_000_000)
	if err := gp.ChargeGasAmsterdam(32_000_000, 5_000_000, 32_000_000); err != nil {
		t.Fatal(err)
	}
	if have, want := gp.Available(true), uint64(55_000_000); have != want {
		t.Fatalf("amsterdam, execution unbound: have %d, want %d", have, want)
	}
	// With less than a cap of execution gas left, both dimensions bind.
	gp = NewGasPool(60_000_000)
	if err := gp.ChargeGasAmsterdam(50_000_000, 20_000_000, 50_000_000); err != nil {
		t.Fatal(err)
	}
	if have, want := gp.Available(true), uint64(10_000_000); have != want {
		t.Fatalf("amsterdam, execution bound: have %d, want %d", have, want)
	}
	gp = NewGasPool(60_000_000)
	if err := gp.ChargeGasAmsterdam(50_000_000, 55_000_000, 55_000_000); err != nil {
		t.Fatal(err)
	}
	if have, want := gp.Available(true), uint64(5_000_000); have != want {
		t.Fatalf("amsterdam, state bound: have %d, want %d", have, want)
	}
	// Available agrees with the reservation check on either side of the cap.
	gp = NewGasPool(60_000_000)
	if err := gp.ChargeGasAmsterdam(32_000_000, 0, 32_000_000); err != nil {
		t.Fatal(err)
	}
	for _, gas := range []uint64{params.MaxTxGas - 1, params.MaxTxGas, 30_000_000, 60_000_000, 60_000_001} {
		fits := gp.CheckGasAmsterdam(min(gas, params.MaxTxGas), gas) == nil
		if have := gas <= gp.Available(true); have != fits {
			t.Fatalf("gas %d: available says %v, check says %v", gas, have, fits)
		}
	}
}

func TestGasPoolAmsterdamReservation(t *testing.T) {
	const limit uint64 = 60_000_000
	for _, gas := range []uint64{25_000, 30_000_000} {
		t.Run(fmt.Sprint(gas), func(t *testing.T) {
			gp := NewGasPool(limit)
			execution, state := min(gas, params.MaxTxGas), gas
			if err := gp.ReserveGasAmsterdam(execution, state); err != nil {
				t.Fatal(err)
			}
			if gp.Used() != 0 || gp.CumulativeUsed() != 0 {
				t.Fatal("reservation counted as consumption")
			}
			if gp.Available(false) != limit {
				t.Fatal("reservation changed legacy budget")
			}
			for _, usage := range [][2]uint64{{limit - execution + 1, 0}, {0, limit - state + 1}} {
				if err := gp.ChargeGasAmsterdam(usage[0], usage[1], 1); !errors.Is(err, ErrGasLimitReached) {
					t.Fatalf("reserved budget consumed: %v", err)
				}
			}
			before := gp.Snapshot()
			if err := gp.ReserveGasAmsterdam(math.MaxUint64, 0); !errors.Is(err, ErrGasLimitReached) {
				t.Fatal(err)
			}
			if *gp != *before {
				t.Fatal("failed reservation changed pool")
			}
			if err := gp.ReleaseGasAmsterdam(execution+1, state); err == nil {
				t.Fatal("invalid release succeeded")
			}
			if *gp != *before {
				t.Fatal("failed release changed pool")
			}
			gp = NewGasPool(limit)
			gp.Set(before)
			if err := gp.ChargeGasAmsterdam(limit-execution, limit-state, 123); err != nil {
				t.Fatal(err)
			}
			if gp.Available(true) != 0 {
				t.Fatal("reserved capacity exposed")
			}
			consumed := gp.Snapshot()
			if err := gp.ReleaseGasAmsterdam(execution, state); err != nil {
				t.Fatal(err)
			}
			if gp.Used() != consumed.Used() || gp.CumulativeUsed() != 123 {
				t.Fatal("release changed consumption")
			}
			if err := gp.CheckGasAmsterdam(execution, state); err != nil {
				t.Fatalf("payment does not fit: %v", err)
			}
			if err := gp.ChargeGasAmsterdam(execution, state, 456); err != nil {
				t.Fatal(err)
			}
			if gp.CumulativeExecution() != limit || gp.CumulativeState() != limit || gp.CumulativeUsed() != 579 {
				t.Fatal("incorrect cumulative accounting")
			}
		})
	}
}
