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

package b20

import (
	"github.com/ethereum/go-ethereum/core/vm"

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
// truth is core/vm/b20/B20Std.sol.
//
// The check is exact in both directions:
//   - a registered signature absent from the baseline is a divergence, unless
//     listed in knownDivergent with the target it must converge to;
//   - a baseline signature not registered must appear in the pending lists, so
//     an unimplemented selector cannot be forgotten silently, and implementing
//     one forces its removal here.
func TestB20ABIBaseline(t *testing.T) {
	raw, err := os.ReadFile("testdata/abi_baseline.json")
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
	knownDivergent := map[string]string{
		// The factory still takes initialAdmin directly; the BEP takes an
		// ABI-encoded create-params struct (typed params work package).
		"createB20(uint8,bytes32,address,bytes[])": "createB20(uint8,bytes32,bytes,bytes[])",
	}

	// Baseline signatures with no implementation yet. Implementing one MUST
	// remove it from this list.
	pendingFunctions := []string{
		"contractURI()",
		"updateName(string)",
		"updateSymbol(string)",
		"updateContractURI(string)",
		"pausedFeatures()",
		"supplyCap()",
		"eip712Domain()",
		"currency()",
		"variantOf(address)",
		"createB20(uint8,bytes32,bytes,bytes[])",
		// ActivationRegistry is not implemented at all yet.
		"isActivated(bytes32)",
		"checkActivated(bytes32)",
		"admin()",
		"setAdmin(address)",
		"activate(bytes32)",
		"deactivate(bytes32)",
	}
	pendingEvents := []string{
		"NameUpdated(string)",
		"SymbolUpdated(string)",
		"ContractURIUpdated()",
		"LastAdminRenounced(address)",
		"PolicyUpdated(bytes32,uint64)",
		"Paused(address,uint8[])",
		"Unpaused(address,uint8[])",
		"SupplyCapUpdated(uint256,uint256)",
		"EIP712DomainChanged()",
		"B20Created(address,address,uint8,string,string,bytes)",
		"PolicyCreated(uint64,address,uint8)",
		"PolicyAdminStaged(uint64,address)",
		"PolicyAdminUpdated(uint64,address,address)",
		"MembersUpdated(uint64,address,bool,address[])",
		"FeatureActivated(bytes32,address)",
		"FeatureDeactivated(bytes32,address)",
		"AdminChanged(address,address,address)",
	}
	pendingErrors := []string{
		"UnsupportedVersion(uint8,uint8)",
		"InvalidDecimals(uint8)",
		"MissingRequiredField(string)",
		"InvalidCurrency(string)",
		"PolicyNotFound(uint64)",
		"Unauthorized(address)",
		"FeatureNotActivated(bytes32)",
		"AlreadyActivated(bytes32)",
		"ZeroAdminAddress()",
	}

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
// (ABI-encoded error, vm.ErrExecutionReverted), exactly like a Solidity
// `revert CustomError(...)`.
func TestB20RevertData(t *testing.T) {
	_, evm := newAmsterdamEVM(t)
	creator := common.HexToAddress("0xdec0de")
	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, vm.NewGasBudget(5_000_000), uint256.NewInt(0))
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
	if !errors.Is(err, vm.ErrExecutionReverted) {
		t.Fatalf("paused transfer err = %v, want vm.ErrExecutionReverted", err)
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
	if !errors.Is(err, vm.ErrExecutionReverted) {
		t.Fatalf("over-balance transfer err = %v, want vm.ErrExecutionReverted", err)
	}
	want = append([]byte{}, errSelInsufficientBalance[:]...)
	want = append(want, addrKey(b20Alice).Bytes()...)
	want = append(want, u256hash(100).Bytes()...)
	want = append(want, u256hash(1000).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Fatalf("revert data = %x, want InsufficientBalance(alice,100,1000) = %x", ret, want)
	}

	// NonPayable(): a value-bearing call is refused across the routed space.
	ret, _, err = evm.Call(creator, token, b20Call(selTransfer, addrKey(b20Bob), u256hash(1)), vm.NewGasBudget(5_000_000), uint256.NewInt(7))
	if !errors.Is(err, vm.ErrExecutionReverted) {
		t.Fatalf("value-bearing call err = %v, want vm.ErrExecutionReverted", err)
	}
	if !bytes.Equal(ret, errSelNonPayable[:]) {
		t.Fatalf("revert data = %x, want NonPayable() = %x", ret, errSelNonPayable)
	}
}
