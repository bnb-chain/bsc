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
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// b20CallString builds calldata for a fn(string) call.
func b20CallString(sel [4]byte, s string) []byte {
	return append(append([]byte{}, sel[:]...), encString(s)...)
}

// decodeString reads an ABI-encoded single string return value. It follows the
// head offset rather than assuming it, and requires the tail to be padded to a
// word boundary — a helper that skipped those checks would accept payloads a
// real ABI consumer rejects, and would hide exactly the encoder bugs these
// tests exist to catch.
func decodeString(t *testing.T, ret []byte) string {
	t.Helper()
	if len(ret) < 64 {
		t.Fatalf("string return too short: %x", ret)
	}
	off := new(uint256.Int).SetBytes(ret[0:32])
	if !off.Eq(uint256.NewInt(32)) {
		t.Fatalf("string head offset = %s, want 32: %x", off, ret)
	}
	n := new(uint256.Int).SetBytes(ret[32:64]).Uint64()
	if uint64(len(ret)) != 64+(n+31)/32*32 {
		t.Fatalf("string tail is %d bytes, want %d for a %d-byte string: %x",
			len(ret)-64, (n+31)/32*32, n, ret)
	}
	return string(ret[64 : 64+n])
}

func TestB20MetadataUpdates(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	editor := common.HexToAddress("0xed170r")

	statedb, token, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setName("Test Token")
		s.setSymbol("TT")
		s.setRole(roleDefaultAdmin, admin, true)
		s.setAdminCount(uint256.NewInt(1))
	})
	view := newB20Storage(statedb, token)

	// A caller without METADATA_ROLE cannot touch any of the three fields.
	for _, input := range [][]byte{
		b20CallString(selUpdateName, "Hijacked"),
		b20CallString(selUpdateSymbol, "HJK"),
		b20CallString(selUpdateContractURI, "ipfs://hijacked"),
	} {
		if _, err := run(editor, input); !errors.Is(err, ErrExecutionReverted) {
			t.Fatalf("unauthorized metadata write err = %v, want revert", err)
		}
	}
	if got := view.name(); got != "Test Token" {
		t.Fatalf("name after refused update = %q, want unchanged", got)
	}

	if _, err := run(admin, b20Call(selGrantRole, roleMetadata, addrKey(editor))); err != nil {
		t.Fatalf("grant METADATA_ROLE: %v", err)
	}

	// DOMAIN_SEPARATOR is derived from the live name, so renaming must roll it.
	before, err := run(editor, b20Call(selDomainSeparator))
	if err != nil {
		t.Fatalf("DOMAIN_SEPARATOR: %v", err)
	}

	if _, err := run(editor, b20CallString(selUpdateName, "Renamed Token")); err != nil {
		t.Fatalf("updateName: %v", err)
	}
	if _, err := run(editor, b20CallString(selUpdateSymbol, "RNT")); err != nil {
		t.Fatalf("updateSymbol: %v", err)
	}
	if _, err := run(editor, b20CallString(selUpdateContractURI, "ipfs://cid")); err != nil {
		t.Fatalf("updateContractURI: %v", err)
	}

	if got := view.name(); got != "Renamed Token" {
		t.Errorf("name = %q, want Renamed Token", got)
	}
	if got := view.symbol(); got != "RNT" {
		t.Errorf("symbol = %q, want RNT", got)
	}
	ret, err := run(editor, b20Call(selContractURI))
	if err != nil {
		t.Fatalf("contractURI: %v", err)
	}
	if got := decodeString(t, ret); got != "ipfs://cid" {
		t.Errorf("contractURI() = %q, want ipfs://cid", got)
	}

	after, err := run(editor, b20Call(selDomainSeparator))
	if err != nil {
		t.Fatalf("DOMAIN_SEPARATOR: %v", err)
	}
	if bytes.Equal(before, after) {
		t.Error("DOMAIN_SEPARATOR unchanged after updateName — outstanding permits would stay valid")
	}

	// A metadata write is still a write: every one is refused in a read-only
	// frame, not just the first.
	for _, c := range []struct {
		what  string
		input []byte
	}{
		{"updateName", b20CallString(selUpdateName, "Static")},
		{"updateSymbol", b20CallString(selUpdateSymbol, "STC")},
		{"updateContractURI", b20CallString(selUpdateContractURI, "ipfs://static")},
	} {
		gas := NewGasBudget(1_000_000)
		roCtx := &PrecompileContext{StateDB: statedb, Self: token, Caller: editor, DirectCall: true, ReadOnly: true, gas: &gas}
		if _, err := newB20Token(roCtx, 18).dispatch(c.input); !errors.Is(err, ErrWriteProtection) {
			t.Errorf("read-only %s err = %v, want write protection", c.what, err)
		}
	}
}

// TestB20MetadataDuringBootstrap pins that the metadata writers are reachable
// from createB20's privileged init calls without METADATA_ROLE. base-std relies
// on this (its test_post_create_calls_execute_against_token pushes an
// updateName init call and asserts the rename took effect), and it is what lets
// a creator finish configuring a token before any role holder exists.
func TestB20MetadataDuringBootstrap(t *testing.T) {
	statedb, evm := newAmsterdamEVM(t)
	creator := common.HexToAddress("0xdec0de")

	input := encodeCreateB20(b20VariantAsset, common.HexToHash("0xc0"), creator,
		[][]byte{b20CallString(selUpdateName, "Configured")})
	ret, _, err := evm.Call(creator, B20FactoryAddress, input, NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20 with updateName init call: %v", err)
	}
	token := common.BytesToAddress(ret)

	view := newB20Storage(statedb, token)
	if got := view.name(); got != "Configured" {
		t.Errorf("name = %q, want Configured", got)
	}
	// The bypass is the bootstrap window only: it must not outlive creation.
	if view.hasRole(roleMetadata, creator) {
		t.Error("creator holds METADATA_ROLE after bootstrap — the bypass leaked into stored state")
	}
	gas := NewGasBudget(1_000_000)
	ctx := &PrecompileContext{evm: evm, StateDB: statedb, Self: token, Caller: creator, DirectCall: true, gas: &gas}
	if _, err := newB20Token(ctx, 18).dispatch(b20CallString(selUpdateName, "After")); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("post-creation updateName err = %v, want revert", err)
	}
}

// TestB20MetadataEvents pins the log shape of the metadata writers: updateName
// must also signal the EIP-712 domain change, and updateContractURI carries no
// argument at all.
func TestB20MetadataEvents(t *testing.T) {
	editor := common.HexToAddress("0xed170r")
	statedb, token, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setRole(roleMetadata, editor, true)
	})
	txHash := common.HexToHash("0xbeef")
	statedb.SetTxContext(txHash, 0)

	// A name long enough to spill into the string's keccak-derived data region,
	// so the encoders are exercised past the single-word case.
	longName := strings.Repeat("Renamed ", 6) // 48 bytes
	for _, c := range []struct {
		what  string
		input []byte
	}{
		{"updateName", b20CallString(selUpdateName, longName)},
		{"updateSymbol", b20CallString(selUpdateSymbol, "RNT")},
		{"updateContractURI", b20CallString(selUpdateContractURI, "ipfs://cid")},
	} {
		if _, err := run(editor, c.input); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
	}

	logs := statedb.GetLogs(txHash, 1, common.Hash{}, 1)
	// Only updateName touches the EIP-712 domain, so exactly four logs in this
	// order. A spurious EIP712DomainChanged from updateSymbol would break both
	// the count and the sequence.
	wantTopics := []common.Hash{
		b20TopicNameUpdated, b20TopicEIP712DomainChanged,
		b20TopicSymbolUpdated, b20TopicContractURIUpdated,
	}
	if len(logs) != len(wantTopics) {
		t.Fatalf("got %d logs, want %d (NameUpdated, EIP712DomainChanged, SymbolUpdated, ContractURIUpdated)",
			len(logs), len(wantTopics))
	}
	for i, want := range wantTopics {
		if logs[i].Address != token {
			t.Errorf("log %d address = %s, want the token", i, logs[i].Address.Hex())
		}
		if len(logs[i].Topics) != 1 || logs[i].Topics[0] != want {
			t.Errorf("log %d topics = %v, want [%s]", i, logs[i].Topics, want.Hex())
		}
	}
	if got := decodeString(t, logs[0].Data); got != longName {
		t.Errorf("NameUpdated data = %q, want %q", got, longName)
	}
	if len(logs[1].Data) != 0 {
		t.Errorf("EIP712DomainChanged data = %x, want empty", logs[1].Data)
	}
	if got := decodeString(t, logs[2].Data); got != "RNT" {
		t.Errorf("SymbolUpdated data = %q, want RNT", got)
	}
	if len(logs[3].Data) != 0 {
		t.Errorf("ContractURIUpdated data = %x, want empty", logs[3].Data)
	}
}

// TestB20PausedFeaturesAndSupplyCap covers the two configuration views and the
// SupplyCapUpdated / Paused log payloads. The uint8[] encodings are written out
// word by word rather than produced by the encoder under test.
func TestB20PausedFeaturesAndSupplyCap(t *testing.T) {
	admin := common.HexToAddress("0xad4149")
	statedb, _, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setRole(roleDefaultAdmin, admin, true)
		s.setRole(rolePause, admin, true)
		s.setRole(roleUnpause, admin, true)
		s.setAdminCount(uint256.NewInt(1))
		s.setSupplyCap(uint256.NewInt(1000))
	})
	txHash := common.HexToHash("0xfeed")
	statedb.SetTxContext(txHash, 0)

	ret, err := run(admin, b20Call(selSupplyCap))
	if err != nil {
		t.Fatalf("supplyCap: %v", err)
	}
	if got := new(uint256.Int).SetBytes(ret).Uint64(); got != 1000 {
		t.Errorf("supplyCap() = %d, want 1000", got)
	}

	// No feature paused yet: an empty uint8[] is offset ++ zero length.
	ret, err = run(admin, b20Call(selPausedFeatures))
	if err != nil {
		t.Fatalf("pausedFeatures: %v", err)
	}
	wantEmpty := append(u256hash(0x20).Bytes(), u256hash(0).Bytes()...)
	if !bytes.Equal(ret, wantEmpty) {
		t.Errorf("pausedFeatures() = %x, want %x", ret, wantEmpty)
	}

	// Pause SEIZE, BURN and TRANSFER. SEIZE is the highest feature id, so
	// including it pins the scan's upper bound; the report comes back ordered by
	// feature id, not in the order they were requested.
	if _, err := run(admin, b20CallU8Array(selPause, byte(b20PauseSeize), byte(b20PauseBurn), byte(b20PauseTransfer))); err != nil {
		t.Fatalf("pause: %v", err)
	}
	ret, err = run(admin, b20Call(selPausedFeatures))
	if err != nil {
		t.Fatalf("pausedFeatures: %v", err)
	}
	want := append([]byte{}, u256hash(0x20).Bytes()...)
	want = append(want, u256hash(3).Bytes()...)
	want = append(want, u256hash(uint64(b20PauseTransfer)).Bytes()...)
	want = append(want, u256hash(uint64(b20PauseBurn)).Bytes()...)
	want = append(want, u256hash(uint64(b20PauseSeize)).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Errorf("pausedFeatures() = %x, want %x", ret, want)
	}

	// Unpause SEIZE only: the remaining two must stay set, and the log must be
	// Unpaused rather than Paused.
	if _, err := run(admin, b20CallU8Array(selUnpause, byte(b20PauseSeize))); err != nil {
		t.Fatalf("unpause: %v", err)
	}
	ret, err = run(admin, b20Call(selPausedFeatures))
	if err != nil {
		t.Fatalf("pausedFeatures: %v", err)
	}
	want = append([]byte{}, u256hash(0x20).Bytes()...)
	want = append(want, u256hash(2).Bytes()...)
	want = append(want, u256hash(uint64(b20PauseTransfer)).Bytes()...)
	want = append(want, u256hash(uint64(b20PauseBurn)).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Errorf("pausedFeatures() after unpause = %x, want %x", ret, want)
	}

	if _, err := run(admin, b20Call(selUpdateSupplyCap, u256hash(5000))); err != nil {
		t.Fatalf("updateSupplyCap: %v", err)
	}
	// Read the cap back: emitting SupplyCapUpdated is not evidence the cap moved.
	ret, err = run(admin, b20Call(selSupplyCap))
	if err != nil {
		t.Fatalf("supplyCap: %v", err)
	}
	if got := new(uint256.Int).SetBytes(ret).Uint64(); got != 5000 {
		t.Errorf("supplyCap() after update = %d, want 5000", got)
	}

	// ALWAYS_BLOCK is a sentinel id, so binding it needs no registry entry.
	if _, err := run(admin, b20Call(selUpdatePolicy, scopeTransferSender, wU64(b20PolicyAlwaysBlock))); err != nil {
		t.Fatalf("updatePolicy: %v", err)
	}
	// Same again: the binding must be observable, not just logged.
	ret, err = run(admin, b20Call(selPolicyId, scopeTransferSender))
	if err != nil {
		t.Fatalf("policyId: %v", err)
	}
	if got := new(uint256.Int).SetBytes(ret).Uint64(); got != b20PolicyAlwaysBlock {
		t.Errorf("policyId(TRANSFER_SENDER) = %d, want %d", got, b20PolicyAlwaysBlock)
	}

	logs := statedb.GetLogs(txHash, 1, common.Hash{}, 1)
	if len(logs) != 4 {
		t.Fatalf("got %d logs, want 4 (Paused, Unpaused, SupplyCapUpdated, PolicyUpdated)", len(logs))
	}
	// Paused(address indexed updater, uint8[] features): the payload echoes the
	// requested order, so it stays a record of the action rather than of state.
	if len(logs[0].Topics) != 2 || logs[0].Topics[0] != b20TopicPaused || logs[0].Topics[1] != addrKey(admin) {
		t.Errorf("Paused topics = %v, want [Paused, admin]", logs[0].Topics)
	}
	wantPaused := append([]byte{}, u256hash(0x20).Bytes()...)
	wantPaused = append(wantPaused, u256hash(3).Bytes()...)
	wantPaused = append(wantPaused, u256hash(uint64(b20PauseSeize)).Bytes()...)
	wantPaused = append(wantPaused, u256hash(uint64(b20PauseBurn)).Bytes()...)
	wantPaused = append(wantPaused, u256hash(uint64(b20PauseTransfer)).Bytes()...)
	if !bytes.Equal(logs[0].Data, wantPaused) {
		t.Errorf("Paused data = %x, want %x", logs[0].Data, wantPaused)
	}
	// Unpaused carries its own topic0 and the single feature it released.
	if len(logs[1].Topics) != 2 || logs[1].Topics[0] != b20TopicUnpaused || logs[1].Topics[1] != addrKey(admin) {
		t.Errorf("Unpaused topics = %v, want [Unpaused, admin]", logs[1].Topics)
	}
	wantUnpaused := append([]byte{}, u256hash(0x20).Bytes()...)
	wantUnpaused = append(wantUnpaused, u256hash(1).Bytes()...)
	wantUnpaused = append(wantUnpaused, u256hash(uint64(b20PauseSeize)).Bytes()...)
	if !bytes.Equal(logs[1].Data, wantUnpaused) {
		t.Errorf("Unpaused data = %x, want %x", logs[1].Data, wantUnpaused)
	}
	// SupplyCapUpdated(uint256 previousCap, uint256 newCap): both non-indexed.
	if logs[2].Topics[0] != b20TopicSupplyCapUpdated || len(logs[2].Topics) != 1 {
		t.Errorf("SupplyCapUpdated topics = %v", logs[2].Topics)
	}
	wantCap := append(u256hash(1000).Bytes(), u256hash(5000).Bytes()...)
	if !bytes.Equal(logs[2].Data, wantCap) {
		t.Errorf("SupplyCapUpdated data = %x, want %x", logs[2].Data, wantCap)
	}
	// PolicyUpdated(bytes32 indexed scope, uint64 policyId).
	if len(logs[3].Topics) != 2 || logs[3].Topics[0] != b20TopicPolicyUpdated || logs[3].Topics[1] != scopeTransferSender {
		t.Errorf("PolicyUpdated topics = %v, want [PolicyUpdated, TRANSFER_SENDER]", logs[3].Topics)
	}
	if !bytes.Equal(logs[3].Data, wU64(b20PolicyAlwaysBlock).Bytes()) {
		t.Errorf("PolicyUpdated data = %x, want %x", logs[3].Data, wU64(b20PolicyAlwaysBlock).Bytes())
	}
}

// TestB20EIP712DomainEncoding pins eip712Domain() against a byte vector built
// by hand. The return is a 7-member tuple with three dynamic members, and an
// encoder mistake in the head/tail split would be invisible to a test that
// produced the expectation with the same encoder.
func TestB20EIP712DomainEncoding(t *testing.T) {
	_, token, run := newTokenWithEVM(t, 1, func(s b20Storage) {
		s.setName("Tok")
	})

	ret, err := run(b20Alice, b20Call(selEIP712Domain))
	if err != nil {
		t.Fatalf("eip712Domain: %v", err)
	}

	chainID, _ := uint256.FromBig(params.TestChainConfig.ChainID)
	pad := func(s string) []byte {
		out := make([]byte, 32)
		copy(out, s)
		return out
	}
	// Head is 7 words (224 bytes); the tails follow in declaration order:
	// name at 224, version at 224+64=288, extensions at 288+64=352.
	var want []byte
	want = append(want, common.Hash{0: 0x0f}.Bytes()...) // bytes1 fields, left-aligned
	want = append(want, u256hash(224).Bytes()...)        // -> name
	want = append(want, u256hash(288).Bytes()...)        // -> version
	want = append(want, common.Hash(chainID.Bytes32()).Bytes()...)
	want = append(want, addrKey(token).Bytes()...)
	want = append(want, common.Hash{}.Bytes()...) // salt: unused
	want = append(want, u256hash(352).Bytes()...) // -> extensions
	want = append(want, u256hash(3).Bytes()...)   // len("Tok")
	want = append(want, pad("Tok")...)            //
	want = append(want, u256hash(1).Bytes()...)   // len("1")
	want = append(want, pad(b20EIP712Version)...) //
	want = append(want, u256hash(0).Bytes()...)   // extensions: empty

	if !bytes.Equal(ret, want) {
		t.Fatalf("eip712Domain() =\n%x\nwant\n%x", ret, want)
	}
}

// TestB20ABIEncodingOracle re-checks the two non-trivial new encodings against
// go-ethereum's own ABI packer. The hand-built vectors above pin the layout and
// this pins conformance: a shared mistake between the B20 encoder and my
// reading of the spec survives the first check but not the second.
func TestB20ABIEncodingOracle(t *testing.T) {
	mustType := func(s string) abi.Type {
		t.Helper()
		ty, err := abi.NewType(s, "", nil)
		if err != nil {
			t.Fatalf("NewType(%s): %v", s, err)
		}
		return ty
	}

	// eip712Domain(): a 7-member tuple with three dynamic members. Both a
	// single-word name and one spanning several words, since a name longer than
	// 32 bytes moves the version and extensions tails — a fixed-size tail
	// assumption survives the short case.
	domainArgs := abi.Arguments{
		{Type: mustType("bytes1")}, {Type: mustType("string")}, {Type: mustType("string")},
		{Type: mustType("uint256")}, {Type: mustType("address")}, {Type: mustType("bytes32")},
		{Type: mustType("uint256[]")},
	}
	for _, name := range []string{"Tok", strings.Repeat("Long Name ", 7)} {
		_, token, run := newTokenWithEVM(t, 1, func(s b20Storage) { s.setName(name) })
		got, err := run(b20Alice, b20Call(selEIP712Domain))
		if err != nil {
			t.Fatalf("eip712Domain(%d-byte name): %v", len(name), err)
		}
		want, err := domainArgs.Pack([1]byte{0x0f}, name, b20EIP712Version,
			params.TestChainConfig.ChainID, token, [32]byte{}, []*big.Int{})
		if err != nil {
			t.Fatalf("pack domain: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("eip712Domain(%d-byte name)\n got = %x\nwant = %x", len(name), got, want)
		}
	}

	// pausedFeatures(): a bare dynamic uint8[].
	admin := common.HexToAddress("0xad4149")
	_, _, runPause := newTokenWithEVM(t, 1, func(s b20Storage) { s.setRole(rolePause, admin, true) })
	if _, err := runPause(admin, b20CallU8Array(selPause, byte(b20PauseSeize), byte(b20PauseTransfer))); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, err := runPause(admin, b20Call(selPausedFeatures))
	if err != nil {
		t.Fatalf("pausedFeatures: %v", err)
	}
	arrayArgs := abi.Arguments{{Type: mustType("uint8[]")}}
	want, err := arrayArgs.Pack([]uint8{uint8(b20PauseTransfer), uint8(b20PauseSeize)})
	if err != nil {
		t.Fatalf("pack uint8[]: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("pausedFeatures()\n got = %x\nwant = %x", got, want)
	}
}
