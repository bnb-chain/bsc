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

package vm

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20ABIBaseline cross-checks every function / event / error signature the
// Go implementation registers (via selector / eventTopic / b20ErrorSel) against
// the canonical surface in testdata/abi_baseline.json, whose source of
// truth is core/vm/b20std/B20Std.sol.
//
// The check is exact in both directions:
//   - a registered signature absent from the baseline is a divergence, unless
//     listed in knownDivergent with the target it must converge to;
//   - a baseline signature not registered must appear in the pending lists, so
//     an unimplemented selector cannot be forgotten silently, and implementing
//     one forces its removal here.
func TestB20ABIBaseline(t *testing.T) {
	raw, err := os.ReadFile("testdata/b20_abi_baseline.json")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var baseline struct {
		Functions []string `json:"functions"`
		Events    []string `json:"events"`
		Errors    []string `json:"errors"`
	}
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}

	// Implemented signatures that intentionally differ from the baseline while
	// a work package is in flight. Key: what the Go code registers today;
	// value: the baseline signature it must converge to.
	// No implemented signature currently diverges from the baseline.
	knownDivergent := map[string]string{}

	// Baseline signatures with no implementation yet. Implementing one MUST
	// remove it from this list.
	pendingFunctions := []string{}
	pendingEvents := []string{}
	pendingErrors := []string{}

	check := func(kind string, baselineSigs []string, registered map[string]bool, pending []string, divergent map[string]string) {
		base := map[string]bool{}
		for _, sig := range baselineSigs {
			if base[sig] {
				t.Errorf("%s baseline lists %q twice", kind, sig)
			}
			base[sig] = true
		}
		pend := map[string]bool{}
		for _, sig := range pending {
			if !base[sig] {
				t.Errorf("%s pending entry %q is not in the baseline", kind, sig)
			}
			pend[sig] = true
		}
		// Direction 1: everything registered must be canonical (or tracked).
		for sig := range registered {
			if base[sig] {
				continue
			}
			if target, ok := divergent[sig]; ok {
				if !base[target] {
					t.Errorf("%s knownDivergent target %q is not in the baseline", kind, target)
				}
				continue
			}
			t.Errorf("%s signature %q is implemented but not in the baseline — divergence from BEP-702", kind, sig)
		}
		// Direction 2: everything canonical must be implemented or pending.
		for sig := range base {
			if registered[sig] == pend[sig] {
				if registered[sig] {
					t.Errorf("%s signature %q is implemented but still listed as pending", kind, sig)
				} else {
					t.Errorf("%s signature %q is neither implemented nor listed as pending", kind, sig)
				}
			}
		}
	}

	fns := map[string]bool{}
	for sig := range b20FnSigs {
		fns[sig] = true
	}
	events := map[string]bool{}
	for sig := range b20EventSigs {
		events[sig] = true
	}
	errs := map[string]bool{}
	for sig := range b20ErrSigs {
		errs[sig] = true
	}

	check("function", baseline.Functions, fns, pendingFunctions, knownDivergent)
	check("event", baseline.Events, events, pendingEvents, nil)
	check("error", baseline.Errors, errs, pendingErrors, nil)
}

// TestB20RevertData verifies typed revert payloads travel end to end: a
// business-rule failure inside a B20 precompile surfaces through evm.Call as
// (ABI-encoded error, ErrExecutionReverted), exactly like a Solidity
// `revert CustomError(...)`.
func TestB20RevertData(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	initCalls := [][]byte{
		b20Call(selGrantRole, rolePause, addrKey(creator)),
		b20Call(selGrantRole, roleUnpause, addrKey(creator)),
		b20Call(selGrantRole, roleMint, addrKey(creator)),
		b20Call(selMint, addrKey(b20Alice), u256hash(100)),
	}
	ret, err := call(creator, B20FactoryAddress, encodeCreateB20(b20VariantAsset, common.HexToHash("0x5e"), creator, initCalls))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// ContractPaused(TRANSFER): selector ++ uint8 word.
	if _, err := call(creator, token, b20CallU8Array(selPause, byte(b20PauseTransfer))); err != nil {
		t.Fatalf("pause: %v", err)
	}
	ret, err = call(b20Alice, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(1)))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("paused transfer err = %v, want ErrExecutionReverted", err)
	}
	want := append(append([]byte{}, errSelContractPaused[:]...), wU8(b20PauseTransfer).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Fatalf("revert data = %x, want ContractPaused(TRANSFER) = %x", ret, want)
	}

	// InsufficientBalance(sender, balance, needed) carries the observed values.
	if _, err := call(creator, token, b20CallU8Array(selUnpause, byte(b20PauseTransfer))); err != nil {
		t.Fatalf("unpause: %v", err)
	}
	ret, err = call(b20Alice, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(1000)))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("over-balance transfer err = %v, want ErrExecutionReverted", err)
	}
	want = append([]byte{}, errSelInsufficientBalance[:]...)
	want = append(want, addrKey(b20Alice).Bytes()...)
	want = append(want, u256hash(100).Bytes()...)
	want = append(want, u256hash(1000).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Fatalf("revert data = %x, want InsufficientBalance(alice,100,1000) = %x", ret, want)
	}

	// NonPayable(): a value-bearing call is refused across the routed space.
	ret, _, err = evm.Call(creator, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(1)), NewGasBudget(5_000_000), uint256.NewInt(7))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("value-bearing call err = %v, want ErrExecutionReverted", err)
	}
	if !bytes.Equal(ret, errSelNonPayable[:]) {
		t.Fatalf("revert data = %x, want NonPayable() = %x", ret, errSelNonPayable)
	}
}

// TestB20UndecodableCalldataRevertsEmpty pins BEP-702 3.2's second failure kind:
// calldata that cannot be decoded reverts with no returndata at all, across
// every entry point.
//
// This is a deliberate divergence from base-std, which echoes the caller's four
// selector bytes for an unknown selector and a selector plus a UTF-8 diagnostic
// for a decode failure. Neither is ABI-encoded, so reproducing them would make
// one implementation's error strings consensus data — and four bytes would be
// worse than none, being shaped exactly like an argument-less custom error.
func TestB20UndecodableCalldataRevertsEmpty(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xdec0de")
	call := func(to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(creator, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// Open the Asset feature and create a token so the token path is live too.
	if _, err := call(B20ActivationRegistryAddress, b20Call(selActivate, featureB20Asset)); err != nil {
		// The harness seeds every feature already; activating again is fine to skip.
		_ = err
	}
	ret, err := call(B20FactoryAddress, encodeCreateB20(b20VariantAsset, common.HexToHash("0xd0"), creator, nil))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	targets := map[string]common.Address{
		"factory":             B20FactoryAddress,
		"policy registry":     B20PolicyRegistryAddress,
		"activation registry": B20ActivationRegistryAddress,
		"token":               token,
	}
	inputs := map[string][]byte{
		"unknown selector":      {0xde, 0xad, 0xbe, 0xef},
		"empty calldata":        {},
		"shorter than selector": {0x01, 0x02, 0x03},
		"selector, no args":     append([]byte{}, selBalanceOf[:]...),
	}
	for tname, to := range targets {
		for iname, input := range inputs {
			got, err := call(to, input)
			if !errors.Is(err, ErrExecutionReverted) {
				t.Errorf("%s / %s: err = %v, want revert", tname, iname, err)
				continue
			}
			if len(got) != 0 {
				t.Errorf("%s / %s: returndata = %x, want empty", tname, iname, got)
			}
		}
	}
}
