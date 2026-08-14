package vm

import (
	"testing"

	"github.com/holiman/uint256"
)

// TestB20MultiplierRoundTripNeverGains pins the direction the rounding must go.
//
// Both conversions truncate, so raw -> scaled -> raw can only lose. That is the
// safe direction: the opposite would let a holder mint value out of a rebase by
// converting back and forth. Nothing asserted this, and it is not visible from the
// two functions in isolation — each looks like an ordinary fixed-point multiply.
func TestB20MultiplierRoundTripNeverGains(t *testing.T) {
	wad := b20WAD
	muls := []*uint256.Int{
		uint256.NewInt(1),
		new(uint256.Int).Div(wad, uint256.NewInt(3)),
		new(uint256.Int).Sub(wad, uint256.NewInt(1)),
		wad,
		new(uint256.Int).Add(wad, uint256.NewInt(1)),
		new(uint256.Int).Mul(wad, uint256.NewInt(3)),
		new(uint256.Int).Mul(wad, uint256.NewInt(1_000_000)),
	}
	raws := []*uint256.Int{
		uint256.NewInt(0),
		uint256.NewInt(1),
		uint256.NewInt(2),
		uint256.NewInt(999),
		uint256.NewInt(1_000_000),
		new(uint256.Int).Mul(wad, uint256.NewInt(12345)),
		new(uint256.Int).Sub(wad, uint256.NewInt(1)),
	}

	for _, mul := range muls {
		for _, raw := range raws {
			scaled, err := applyMultiplier(raw, mul)
			if err != nil {
				t.Fatalf("applyMultiplier(%s, %s): %v", raw, mul, err)
			}
			back, err := removeMultiplier(scaled, mul)
			if err != nil {
				t.Fatalf("removeMultiplier(%s, %s): %v", scaled, mul, err)
			}
			if back.Gt(raw) {
				t.Errorf("round trip gained: raw %s -> scaled %s -> %s (multiplier %s)",
					raw, scaled, back, mul)
			}
		}
	}
}

// TestB20MultiplierOverflowBoundary locates where the products actually overflow,
// which is not where the bounds suggest.
//
// A raw balance and the multiplier are each bounded by type(uint128).max, and
// (2^128-1)^2 is below 2^256 — so applyMultiplier cannot overflow from any state
// the token can hold, and uint128.max * WAD is only 3.4e56 against a 1.16e77
// ceiling. The reachable case is the other direction: toRawBalance takes a uint256
// from the caller, and scaled * WAD overflows above about 1.16e59. It must revert
// there rather than wrap into an unrelated balance.
func TestB20MultiplierOverflowBoundary(t *testing.T) {
	u128Max := new(uint256.Int).Sub(new(uint256.Int).Lsh(uint256.NewInt(1), 128), uint256.NewInt(1))

	// Neither extreme of the internally reachable range overflows.
	if _, err := applyMultiplier(u128Max, u128Max); err != nil {
		t.Errorf("applyMultiplier at both bounds reverted: %v — (2^128-1)^2 fits in uint256", err)
	}
	if _, err := removeMultiplier(u128Max, uint256.NewInt(1)); err != nil {
		t.Errorf("removeMultiplier(uint128.max, 1) reverted: %v", err)
	}

	// The caller-supplied direction does, and must say so rather than wrap.
	// floor(2^256 / WAD) is the largest scaled value that still fits.
	maxScaled := new(uint256.Int).Div(new(uint256.Int).Not(new(uint256.Int)), b20WAD)
	if _, err := removeMultiplier(maxScaled, b20WAD); err != nil {
		t.Errorf("removeMultiplier at the boundary reverted early: %v", err)
	}
	over := new(uint256.Int).Add(maxScaled, uint256.NewInt(1))
	if _, err := removeMultiplier(over, b20WAD); err == nil {
		t.Error("removeMultiplier wrapped instead of reverting one past the boundary")
	}

	// And a zero multiplier answers zero rather than dividing by it. updateMultiplier
	// rejects zero, so this is only reachable if that guard is ever lost.
	if got, err := removeMultiplier(uint256.NewInt(1000), new(uint256.Int)); err != nil || !got.IsZero() {
		t.Errorf("removeMultiplier with a zero multiplier = %s, %v; want 0, nil", got, err)
	}
}
