// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package parlia

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// The validator-set calls behind SealerRole decode through one shared helper.
// A decode failure there silently disables evidence-based revocation, so both
// output shapes are pinned here.

func testValidatorSetABI(t *testing.T) abi.ABI {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(validatorSetABI))
	if err != nil {
		t.Fatalf("parse validatorSetABI: %v", err)
	}
	return parsed
}

func TestValidatorSetUnpackSingleOutput(t *testing.T) {
	vABI := testValidatorSetABI(t)

	for _, method := range []string{"numOfCabinets", "maxNumOfWorkingCandidates"} {
		want := big.NewInt(21)
		packed, err := vABI.Methods[method].Outputs.Pack(want)
		if err != nil {
			t.Fatalf("%s: pack: %v", method, err)
		}

		var got *big.Int
		if err := unpackValidatorSet(vABI, method, packed, &got); err != nil {
			t.Fatalf("%s: unpack: %v", method, err)
		}
		if got == nil || got.Cmp(want) != 0 {
			t.Fatalf("%s: got %v, want %v", method, got, want)
		}
	}
}

func TestValidatorSetUnpackMultipleOutputs(t *testing.T) {
	vABI := testValidatorSetABI(t)

	wantAddrs := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}
	wantVotes := [][]byte{make([]byte, types.BLSPublicKeyLength), make([]byte, types.BLSPublicKeyLength)}
	wantVotes[1][0] = 0xaa

	packed, err := vABI.Methods["getMiningValidators"].Outputs.Pack(wantAddrs, wantVotes)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	var (
		gotAddrs []common.Address
		gotVotes []types.BLSPublicKey
	)
	if err := unpackValidatorSet(vABI, "getMiningValidators", packed, &gotAddrs, &gotVotes); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if len(gotAddrs) != len(wantAddrs) {
		t.Fatalf("got %d addresses, want %d", len(gotAddrs), len(wantAddrs))
	}
	for i := range wantAddrs {
		if gotAddrs[i] != wantAddrs[i] {
			t.Fatalf("address %d: got %s, want %s", i, gotAddrs[i], wantAddrs[i])
		}
	}
	if len(gotVotes) != len(wantVotes) || gotVotes[1][0] != 0xaa {
		t.Fatalf("vote addresses did not round-trip: %v", gotVotes)
	}
}
