// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.

package parlia

import (
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// The validator-set calls behind SealerRole decode through one shared helper.
// A decode failure there silently disables evidence-based revocation, so both
// single-output types are pinned here.

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
	want := big.NewInt(21)
	packed, err := vABI.Methods["numOfCabinets"].Outputs.Pack(want)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	var got *big.Int
	if err := unpackValidatorSet(vABI, "numOfCabinets", packed, &got); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if got == nil || got.Cmp(want) != 0 {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestValidatorSetUnpackValidators(t *testing.T) {
	vABI := testValidatorSetABI(t)
	want := []common.Address{
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}
	packed, err := vABI.Methods["getValidators"].Outputs.Pack(want)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	var got []common.Address
	if err := unpackValidatorSet(vABI, "getValidators", packed, &got); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
