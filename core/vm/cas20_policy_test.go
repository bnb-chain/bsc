package vm

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func encodeUpdateList(sel [4]byte, id uint64, flag bool, addrs []common.Address) []byte {
	out := append([]byte{}, sel[:]...)
	out = append(out, u256hash(id).Bytes()...)   // id
	out = append(out, encBool(flag)...)          // add/remove
	out = append(out, u256hash(0x60).Bytes()...) // offset to array (3-word head)
	out = append(out, u256hash(uint64(len(addrs))).Bytes()...)
	for _, a := range addrs {
		out = append(out, addrKey(a).Bytes()...)
	}
	return out
}

func cas20BlockContext(time uint64) BlockContext {
	return BlockContext{
		Random:      &common.Hash{},
		CanTransfer: func(StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		BlockNumber: big.NewInt(1),
		Time:        time,
	}
}

func newCAS20EVM(t *testing.T) (*state.StateDB, *EVM) {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	cfg := *cas20TestChainConfig()
	bc := cas20BlockContext(1)
	seedActivation(statedb, cas20TestCaller)
	return statedb, NewEVM(bc, statedb, &cfg, Config{})
}

var cas20TestCaller = common.HexToAddress("0x60feed")

// The fork part delegates to SeedCAS20Activation so the harness cannot drift from
// it; opening the features stays local, since the fork opens nothing (BEP-702 3.15).
func seedActivation(statedb *state.StateDB, admin common.Address) {
	SeedCAS20Activation(statedb)
	reg := cas20Storage{state: statedb, token: CAS20ActivationRegistryAddress}
	reg.setWord(actSlot(actSlotAdmin), addrKey(admin))
	for _, f := range []common.Hash{featureCAS20Asset, featureCAS20Stablecoin, featurePolicyRegistry, featureMemoRegistry} {
		reg.setWord(mappingSlot(actSlot(actSlotFeatures), f), common.Hash{31: 1})
	}
}

func TestCAS20PolicyRegistry(t *testing.T) {
	_, evm := newCAS20EVM(t)
	admin := common.HexToAddress("0xad4149")
	reg := CAS20PolicyRegistryAddress

	call := func(caller common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, reg, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	authorized := func(id uint64, a common.Address) bool {
		ret, err := call(admin, cas20Call(selIsAuthorized, u256hash(id), addrKey(a)))
		if err != nil {
			t.Fatalf("isAuthorized: %v", err)
		}
		return bytes.Equal(ret, encBool(true))
	}

	ret, err := call(admin, cas20Call(selCreatePolicy, addrKey(admin), u256hash(cas20PolicyBlocklist)))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	block := new(uint256.Int).SetBytes(ret).Uint64()
	if byte(block>>56) != cas20PolicyBlocklist {
		t.Fatalf("blocklist id %#x has wrong type byte", block)
	}
	if r, _ := call(admin, cas20Call(selPolicyExists, u256hash(block))); !bytes.Equal(r, encBool(true)) {
		t.Fatal("policy should exist")
	}
	if r, _ := call(admin, cas20Call(selPolicyAdmin, u256hash(block))); common.BytesToAddress(r) != admin {
		t.Fatal("policyAdmin mismatch")
	}

	if !authorized(block, cas20Bob) {
		t.Fatal("empty blocklist should allow")
	}
	if _, err := call(admin, encodeUpdateList(selUpdateBlocklist, block, true, []common.Address{cas20Bob})); err != nil {
		t.Fatalf("updateBlocklist: %v", err)
	}
	if authorized(block, cas20Bob) {
		t.Fatal("bob should be blocked")
	}
	if !authorized(block, cas20Carol) {
		t.Fatal("carol should still be allowed")
	}

	ret, _ = call(admin, cas20Call(selCreatePolicy, addrKey(admin), u256hash(cas20PolicyAllowlist)))
	allow := new(uint256.Int).SetBytes(ret).Uint64()
	if byte(allow>>56) != cas20PolicyAllowlist {
		t.Fatalf("allowlist id %#x wrong type", allow)
	}
	if authorized(allow, cas20Carol) {
		t.Fatal("empty allowlist should block")
	}
	if _, err := call(admin, encodeUpdateList(selUpdateAllowlist, allow, true, []common.Address{cas20Carol})); err != nil {
		t.Fatalf("updateAllowlist: %v", err)
	}
	if !authorized(allow, cas20Carol) {
		t.Fatal("carol should be allowed")
	}

	if _, err := call(admin, encodeUpdateList(selUpdateAllowlist, block, true, []common.Address{cas20Alice})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("updateAllowlist on blocklist should revert")
	}
	if _, err := call(cas20Bob, encodeUpdateList(selUpdateBlocklist, block, true, []common.Address{cas20Alice})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("non-admin update should revert")
	}

	newAdmin := common.HexToAddress("0x9ead")
	if _, err := call(admin, cas20Call(selStageUpdateAdmin, u256hash(block), addrKey(newAdmin))); err != nil {
		t.Fatalf("stageUpdateAdmin: %v", err)
	}
	if _, err := call(cas20Alice, cas20Call(selFinalizeUpdateAdmin, u256hash(block))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("finalize by non-nominee should revert")
	}
	if _, err := call(newAdmin, cas20Call(selFinalizeUpdateAdmin, u256hash(block))); err != nil {
		t.Fatalf("finalizeUpdateAdmin: %v", err)
	}
	if _, err := call(admin, encodeUpdateList(selUpdateBlocklist, block, true, []common.Address{cas20Alice})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("old admin should no longer update")
	}

	if _, err := call(newAdmin, cas20Call(selRenounceAdmin, u256hash(block))); err != nil {
		t.Fatalf("renounceAdmin: %v", err)
	}
	if _, err := call(newAdmin, encodeUpdateList(selUpdateBlocklist, block, false, []common.Address{cas20Bob})); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("frozen policy should reject updates")
	}
	if authorized(block, cas20Bob) {
		t.Fatal("frozen policy still evaluates: bob stays blocked")
	}
}

func TestCAS20PolicyIntegration(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	custody := common.HexToAddress("0xc45d1")
	salt := common.HexToHash("0x0c")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	initCalls := [][]byte{
		cas20Call(selGrantRole, roleMint, addrKey(creator)),
		cas20Call(selMint, addrKey(cas20Alice), u256hash(1000)),
	}
	ret, err := call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)

	ret, _ = call(creator, CAS20PolicyRegistryAddress, cas20Call(selCreatePolicy, addrKey(creator), u256hash(cas20PolicyBlocklist)))
	blk := new(uint256.Int).SetBytes(ret).Uint64()
	if _, err := call(creator, CAS20PolicyRegistryAddress, encodeUpdateList(selUpdateBlocklist, blk, true, []common.Address{cas20Bob})); err != nil {
		t.Fatalf("updateBlocklist: %v", err)
	}
	if _, err := call(creator, token, cas20Call(selUpdatePolicy, scopeTransferReceiver, u256hash(blk))); err != nil {
		t.Fatalf("updatePolicy(receiver): %v", err)
	}

	if _, err := call(cas20Alice, token, cas20Call(selTransfer, addrKey(cas20Bob), u256hash(10))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("transfer to blocked receiver err = %v, want revert", err)
	}
	if _, err := call(cas20Alice, token, cas20Call(selTransfer, addrKey(cas20Carol), u256hash(10))); err != nil {
		t.Fatalf("transfer to allowed receiver: %v", err)
	}

	ret, _ = call(creator, CAS20PolicyRegistryAddress, cas20Call(selCreatePolicy, addrKey(creator), u256hash(cas20PolicyAllowlist)))
	al := new(uint256.Int).SetBytes(ret).Uint64()
	if _, err := call(creator, CAS20PolicyRegistryAddress, encodeUpdateList(selUpdateAllowlist, al, true, []common.Address{custody})); err != nil {
		t.Fatalf("updateAllowlist: %v", err)
	}
	if _, err := call(creator, token, cas20Call(selUpdatePolicy, scopeMintReceiver, u256hash(al))); err != nil {
		t.Fatalf("updatePolicy(mint receiver): %v", err)
	}
	if _, err := call(creator, token, cas20Call(selMint, addrKey(cas20Alice), u256hash(1))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("mint to non-listed err = %v, want revert", err)
	}
	if _, err := call(creator, token, cas20Call(selMint, addrKey(custody), u256hash(500))); err != nil {
		t.Fatalf("mint to custody: %v", err)
	}
	view := newUnmeteredCAS20Storage(statedb, token)
	if view.balanceOf(custody).Uint64() != 500 {
		t.Fatalf("custody balance = %d, want 500", view.balanceOf(custody).Uint64())
	}
	if _, err := call(creator, token, cas20Call(selUpdatePolicy, scopeTransferSender, u256hash(0x99999))); !errors.Is(err, ErrExecutionReverted) {
		t.Fatal("binding nonexistent policy should revert")
	}
}

func TestCAS20SeizeWithMemo(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0x0d")

	call := func(caller, to common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, to, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	initCalls := [][]byte{
		cas20Call(selGrantRole, roleMint, addrKey(creator)),
		cas20Call(selGrantRole, roleSeize, addrKey(creator)),
		cas20Call(selMint, addrKey(cas20Bob), u256hash(1000)),
	}
	ret, err := call(creator, CAS20FactoryAddress, encodeCreateCAS20(cas20VariantAsset, salt, creator, initCalls))
	if err != nil {
		t.Fatalf("createCAS20: %v", err)
	}
	token := common.BytesToAddress(ret)
	view := newUnmeteredCAS20Storage(statedb, token)

	memo := common.HexToHash("0x5e12e")

	if _, err := call(creator, token, cas20Call(selSeizeWithMemo, addrKey(cas20Bob), addrKey(cas20Alice), u256hash(100), memo)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("seize before freeze err = %v, want revert (AccountNotSeizable)", err)
	}

	ret, _ = call(creator, CAS20PolicyRegistryAddress, cas20Call(selCreatePolicy, addrKey(creator), u256hash(cas20PolicyBlocklist)))
	blk := new(uint256.Int).SetBytes(ret).Uint64()
	if _, err := call(creator, CAS20PolicyRegistryAddress, encodeUpdateList(selUpdateBlocklist, blk, true, []common.Address{cas20Bob})); err != nil {
		t.Fatalf("updateBlocklist: %v", err)
	}
	if _, err := call(creator, token, cas20Call(selUpdatePolicy, scopeSeizeHolder, u256hash(blk))); err != nil {
		t.Fatalf("updatePolicy(seizeHolder): %v", err)
	}

	if _, err := call(cas20Alice, token, cas20Call(selSeizeWithMemo, addrKey(cas20Bob), addrKey(cas20Alice), u256hash(100), memo)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("unauthorized seize err = %v, want revert", err)
	}

	if _, err := call(creator, token, cas20Call(selSeizeWithMemo, addrKey(cas20Bob), common.Hash{}, u256hash(100), memo)); !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("seize to zero err = %v, want revert (InvalidReceiver)", err)
	}

	if _, err := call(creator, token, cas20Call(selSeizeWithMemo, addrKey(cas20Bob), addrKey(cas20Alice), u256hash(400), memo)); err != nil {
		t.Fatalf("seizeWithMemo: %v", err)
	}
	if got := view.balanceOf(cas20Bob).Uint64(); got != 600 {
		t.Fatalf("seized account balance = %d, want 600", got)
	}
	if got := view.balanceOf(cas20Alice).Uint64(); got != 400 {
		t.Fatalf("destination balance = %d, want 400", got)
	}
	if got := view.totalSupply().Uint64(); got != 1000 {
		t.Fatalf("totalSupply = %d, want 1000 (seizure moves value, it does not burn)", got)
	}
}

// BEP-702 3.17, asserted on raw slots rather than on what the ABI reports.
func TestCAS20PolicyStorageLayout(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	admin := common.HexToAddress("0xad4149")

	root := new(uint256.Int).SetBytes(erc7201Root("bsc.policy_registry").Bytes())
	for offset, want := range map[uint64]uint64{
		polSlotPolicies: 0, polSlotMembers: 1, polSlotPendingAdmins: 2, polSlotCounter: 3,
	} {
		if offset != want {
			t.Errorf("slot constant = %d, want %d", offset, want)
		}
		got := new(uint256.Int).SetBytes(polSlot(offset).Bytes())
		if exp := new(uint256.Int).AddUint64(root, offset); !got.Eq(exp) {
			t.Errorf("slot %d = %x, want root+%d = %x", offset, got, offset, exp)
		}
	}

	ret, _, err := evm.Call(admin, CAS20PolicyRegistryAddress,
		cas20Call(selCreatePolicy, addrKey(admin), u256hash(cas20PolicyAllowlist)),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	id := new(uint256.Int).SetBytes(ret).Uint64()

	view := newUnmeteredCAS20Storage(statedb, CAS20PolicyRegistryAddress)
	word := view.getWord(mappingSlot(polSlot(polSlotPolicies), idKey(id)))
	if word[0]&0x80 == 0 {
		t.Errorf("policy word %x has no exists bit", word)
	}
	if got := common.BytesToAddress(word[12:]); got != admin {
		t.Errorf("packed admin = %s, want %s", got.Hex(), admin.Hex())
	}
	for _, b := range word[1:12] { // bits 254:160 are reserved and must stay zero
		if b != 0 {
			t.Errorf("policy word %x has dirty reserved bits", word)
			break
		}
	}
	// Two sentinels are seeded first, so the first caller id draws counter 2.
	if got := new(uint256.Int).SetBytes(view.getWord(polSlot(polSlotCounter)).Bytes()).Uint64(); got != 3 {
		t.Errorf("counter = %d, want 3", got)
	}
	if id != uint64(cas20PolicyAllowlist)<<56|2 {
		t.Errorf("first allowlist id = %#x, want type 1 counter 2", id)
	}
}

func TestCAS20PolicySentinels(t *testing.T) {
	_, evm := newCAS20EVM(t)
	caller := common.HexToAddress("0xad4149")
	ask := func(sel [4]byte, args ...common.Hash) []byte {
		ret, _, err := evm.Call(caller, CAS20PolicyRegistryAddress, cas20Call(sel, args...),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		return ret
	}

	// Before anything is created; ALWAYS_ALLOW is what every unset policy field holds.
	for _, id := range []uint64{cas20PolicyAlwaysAllow, cas20PolicyAlwaysBlock} {
		if !bytes.Equal(ask(selPolicyExists, u256hash(id)), encBool(true)) {
			t.Errorf("policyExists(%#x) = false, want true", id)
		}
		if got := common.BytesToAddress(ask(selPolicyAdmin, u256hash(id))); got != (common.Address{}) {
			t.Errorf("policyAdmin(%#x) = %s, want zero", id, got.Hex())
		}
		if got := common.BytesToAddress(ask(selPendingPolicyAdmin, u256hash(id))); got != (common.Address{}) {
			t.Errorf("pendingPolicyAdmin(%#x) = %s, want zero", id, got.Hex())
		}
	}
	if !bytes.Equal(ask(selIsAuthorized, u256hash(cas20PolicyAlwaysAllow), addrKey(cas20Bob)), encBool(true)) {
		t.Error("ALWAYS_ALLOW must authorize")
	}
	if !bytes.Equal(ask(selIsAuthorized, u256hash(cas20PolicyAlwaysBlock), addrKey(cas20Bob)), encBool(false)) {
		t.Error("ALWAYS_BLOCK must refuse")
	}

	bad := uint64(5) << 56
	if !bytes.Equal(ask(selPolicyExists, u256hash(bad)), encBool(false)) {
		t.Error("a malformed type byte must not exist")
	}
	if !bytes.Equal(ask(selIsAuthorized, u256hash(bad), addrKey(cas20Bob)), encBool(false)) {
		t.Error("a malformed type byte must not authorize")
	}
	if got := common.BytesToAddress(ask(selPolicyAdmin, u256hash(bad))); got != (common.Address{}) {
		t.Errorf("policyAdmin(malformed) = %s, want zero", got.Hex())
	}

	for _, id := range []uint64{cas20PolicyAlwaysAllow, cas20PolicyAlwaysBlock} {
		_, _, err := evm.Call(caller, CAS20PolicyRegistryAddress,
			cas20Call(selStageUpdateAdmin, u256hash(id), addrKey(caller)),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("stageUpdateAdmin(%#x) err = %v, want revert", id, err)
		}
	}
}

// existence -> type -> admin -> batch, observable through which error the caller receives.
func TestCAS20PolicyCheckOrder(t *testing.T) {
	_, evm := newCAS20EVM(t)
	admin := common.HexToAddress("0xad4149")
	stranger := common.HexToAddress("0x57ra9e")
	call := func(caller common.Address, input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, CAS20PolicyRegistryAddress, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	revertsWith := func(what string, caller common.Address, input []byte, sel [4]byte) {
		t.Helper()
		ret, err := call(caller, input)
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s: err = %v, want revert", what, err)
			return
		}
		if len(ret) < 4 || !bytes.Equal(ret[:4], sel[:]) {
			t.Errorf("%s: revert data %x, want selector %x", what, ret, sel)
		}
	}

	ghost := uint64(cas20PolicyAllowlist)<<56 | 999
	revertsWith("nonexistent policy", stranger,
		encodeUpdateList(selUpdateAllowlist, ghost, true, []common.Address{cas20Bob}), errSelPolicyNotFound)

	ret, err := call(admin, cas20Call(selCreatePolicy, addrKey(admin), u256hash(cas20PolicyBlocklist)))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	block := new(uint256.Int).SetBytes(ret).Uint64()

	revertsWith("wrong type, wrong caller", stranger,
		encodeUpdateList(selUpdateAllowlist, block, true, []common.Address{cas20Bob}), errSelIncompatibleType)
	revertsWith("right type, wrong caller", stranger,
		encodeUpdateList(selUpdateBlocklist, block, true, []common.Address{cas20Bob}), errSelUnauthorized)
	oversized := make([]common.Address, cas20PolicyBatchMax+1)
	for i := range oversized {
		oversized[i] = common.BigToAddress(new(big.Int).SetUint64(uint64(i + 1)))
	}
	revertsWith("oversized batch", admin,
		encodeUpdateList(selUpdateBlocklist, block, true, oversized), errSelBatchTooLarge)

	revertsWith("stage on nonexistent", stranger,
		cas20Call(selStageUpdateAdmin, u256hash(ghost), addrKey(stranger)), errSelPolicyNotFound)
	revertsWith("finalize on nonexistent", stranger,
		cas20Call(selFinalizeUpdateAdmin, u256hash(ghost)), errSelPolicyNotFound)
	revertsWith("renounce on nonexistent", stranger,
		cas20Call(selRenounceAdmin, u256hash(ghost)), errSelPolicyNotFound)
}

// Emptiness alone would give the same answers, so a membership-derived sentinel
// looks correct until something writes membership under its id.
func TestCAS20PolicySentinelsIgnoreMembership(t *testing.T) {
	statedb, evm := newCAS20EVM(t)

	view := policyReg{s: newUnmeteredCAS20Storage(statedb, CAS20PolicyRegistryAddress)}
	view.setMember(cas20PolicyAlwaysAllow, cas20Bob, true) // "block bob" on ALWAYS_ALLOW
	view.setMember(cas20PolicyAlwaysBlock, cas20Bob, true) // "allow bob" on ALWAYS_BLOCK

	ask := func(id uint64) []byte {
		ret, _, err := evm.Call(cas20Alice, CAS20PolicyRegistryAddress,
			cas20Call(selIsAuthorized, u256hash(id), addrKey(cas20Bob)),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("isAuthorized: %v", err)
		}
		return ret
	}
	if !bytes.Equal(ask(cas20PolicyAlwaysAllow), encBool(true)) {
		t.Error("ALWAYS_ALLOW stopped authorizing after a membership write — it is not constant")
	}
	if !bytes.Equal(ask(cas20PolicyAlwaysBlock), encBool(false)) {
		t.Error("ALWAYS_BLOCK started authorizing after a membership write — it is not constant")
	}
}

// The counter is driven to the bound directly; a carry into the type byte would
// change an id's type or land on a sentinel.
func TestCAS20PolicyCounterExhaustion(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	admin := common.HexToAddress("0xad4149")
	view := policyReg{s: newUnmeteredCAS20Storage(statedb, CAS20PolicyRegistryAddress)}

	create := func() ([]byte, error) {
		ret, _, err := evm.Call(admin, CAS20PolicyRegistryAddress,
			cas20Call(selCreatePolicy, addrKey(admin), u256hash(cas20PolicyBlocklist)),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	view.setCounter(cas20PolicyCounterMax - 1)
	ret, err := create()
	if err != nil {
		t.Fatalf("createPolicy at counter max-1: %v", err)
	}
	if id := new(uint256.Int).SetBytes(ret).Uint64(); polIDType(id) != cas20PolicyBlocklist {
		t.Errorf("id %#x escaped its type byte", id)
	}

	view.setCounter(cas20PolicyCounterMax)
	ret, err = create()
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createPolicy at counter max err = %v, want revert", err)
	}
	want := append(append([]byte{}, errSelPanic[:]...), wU8(0x11).Bytes()...)
	if !bytes.Equal(ret, want) {
		t.Errorf("revert data = %x, want Panic(0x11) = %x", ret, want)
	}
}

// The membership events mix a static bool with a dynamic address[], so their data
// is also checked against go-ethereum's packer.
func TestCAS20PolicyEvents(t *testing.T) {
	statedb, evm := newCAS20EVM(t)
	admin := common.HexToAddress("0xad4149")
	heir := common.HexToAddress("0x8e14")

	txSeq := 0
	logsOf := func(caller common.Address, input []byte) []*types.Log {
		t.Helper()
		txSeq++
		hash := common.BigToHash(new(big.Int).SetUint64(uint64(txSeq)))
		statedb.SetTxContext(hash, txSeq)
		if _, _, err := evm.Call(caller, CAS20PolicyRegistryAddress, input,
			NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
			t.Fatalf("call: %v", err)
		}
		return statedb.GetLogs(hash, 1, common.Hash{}, 1)
	}
	wantTopics := func(what string, got *types.Log, expect ...common.Hash) {
		t.Helper()
		if len(got.Topics) != len(expect) {
			t.Errorf("%s: %d topics, want %d", what, len(got.Topics), len(expect))
			return
		}
		for i := range expect {
			if got.Topics[i] != expect[i] {
				t.Errorf("%s: topic %d = %s, want %s", what, i, got.Topics[i].Hex(), expect[i].Hex())
			}
		}
	}

	// The initial admin is a transition from nobody, so creation lands in the same
	// stream as every later handover.
	logs := logsOf(admin, cas20Call(selCreatePolicy, addrKey(admin), u256hash(cas20PolicyBlocklist)))
	if len(logs) != 2 {
		t.Fatalf("createPolicy emitted %d logs, want 2", len(logs))
	}
	block := uint64(cas20PolicyBlocklist)<<56 | 2
	wantTopics("PolicyCreated", logs[0], cas20TopicPolicyCreated, idKey(block), addrKey(admin))
	if !bytes.Equal(logs[0].Data, wU8(cas20PolicyBlocklist).Bytes()) {
		t.Errorf("PolicyCreated data = %x, want the type byte", logs[0].Data)
	}
	wantTopics("PolicyAdminUpdated", logs[1],
		cas20TopicPolicyAdminUpdated, idKey(block), addrKey(common.Address{}), addrKey(admin))
	if len(logs[1].Data) != 0 {
		t.Errorf("PolicyAdminUpdated data = %x, want empty", logs[1].Data)
	}

	// creator is the caller, not the nominated admin.
	logs = logsOf(cas20Alice, cas20Call(selCreatePolicy, addrKey(heir), u256hash(cas20PolicyBlocklist)))
	if len(logs) != 2 {
		t.Fatalf("createPolicy (third party) emitted %d logs, want 2", len(logs))
	}
	third := uint64(cas20PolicyBlocklist)<<56 | 3
	wantTopics("PolicyCreated (third party)", logs[0],
		cas20TopicPolicyCreated, idKey(third), addrKey(cas20Alice))
	wantTopics("PolicyAdminUpdated (third party)", logs[1],
		cas20TopicPolicyAdminUpdated, idKey(third), addrKey(common.Address{}), addrKey(heir))

	accounts := []common.Address{cas20Bob, cas20Carol}
	logs = logsOf(admin, encodeUpdateList(selUpdateBlocklist, block, true, accounts))
	if len(logs) != 1 {
		t.Fatalf("updateBlocklist emitted %d logs, want 1", len(logs))
	}
	wantTopics("BlocklistUpdated", logs[0], cas20TopicBlocklistUpdated, idKey(block), addrKey(admin))

	boolType, err := abi.NewType("bool", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	addrsType, err := abi.NewType("address[]", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := abi.Arguments{{Type: boolType}, {Type: addrsType}}.Pack(true, accounts)
	if err != nil {
		t.Fatalf("pack membership payload: %v", err)
	}
	if !bytes.Equal(logs[0].Data, oracle) {
		t.Errorf("BlocklistUpdated data\n got = %x\nwant = %x", logs[0].Data, oracle)
	}

	ret, _, err := evm.Call(admin, CAS20PolicyRegistryAddress,
		cas20Call(selCreatePolicy, addrKey(admin), u256hash(cas20PolicyAllowlist)),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createPolicy allowlist: %v", err)
	}
	allow := new(uint256.Int).SetBytes(ret).Uint64()
	logs = logsOf(admin, encodeUpdateList(selUpdateAllowlist, allow, false, accounts))
	if len(logs) != 1 || logs[0].Topics[0] != cas20TopicAllowlistUpdated {
		t.Fatalf("updateAllowlist logs = %v", logs)
	}
	oracle, _ = abi.Arguments{{Type: boolType}, {Type: addrsType}}.Pack(false, accounts)
	if !bytes.Equal(logs[0].Data, oracle) {
		t.Errorf("AllowlistUpdated data\n got = %x\nwant = %x", logs[0].Data, oracle)
	}

	// Emitted for an empty batch too: the call form is part of the record.
	logs = logsOf(admin, encodeCreatePolicyWithAccounts(admin, cas20PolicyAllowlist, nil))
	if len(logs) != 3 {
		t.Fatalf("createPolicyWithAccounts emitted %d logs, want 3", len(logs))
	}
	if logs[2].Topics[0] != cas20TopicAllowlistUpdated {
		t.Errorf("third log = %s, want AllowlistUpdated", logs[2].Topics[0].Hex())
	}
	oracle, _ = abi.Arguments{{Type: boolType}, {Type: addrsType}}.Pack(true, []common.Address{})
	if !bytes.Equal(logs[2].Data, oracle) {
		t.Errorf("seed AllowlistUpdated data\n got = %x\nwant = %x", logs[2].Data, oracle)
	}

	logs = logsOf(admin, cas20Call(selStageUpdateAdmin, u256hash(block), addrKey(heir)))
	if len(logs) != 1 {
		t.Fatalf("stageUpdateAdmin emitted %d logs, want 1", len(logs))
	}
	wantTopics("PolicyAdminStaged", logs[0],
		cas20TopicPolicyAdminStaged, idKey(block), addrKey(admin), addrKey(heir))

	logs = logsOf(admin, cas20Call(selStageUpdateAdmin, u256hash(block), addrKey(common.Address{})))
	wantTopics("PolicyAdminStaged (cancel)", logs[0],
		cas20TopicPolicyAdminStaged, idKey(block), addrKey(admin), addrKey(common.Address{}))

	logsOf(admin, cas20Call(selStageUpdateAdmin, u256hash(block), addrKey(heir)))
	logs = logsOf(heir, cas20Call(selFinalizeUpdateAdmin, u256hash(block)))
	if len(logs) != 1 {
		t.Fatalf("finalizeUpdateAdmin emitted %d logs, want 1", len(logs))
	}
	wantTopics("PolicyAdminUpdated (finalize)", logs[0],
		cas20TopicPolicyAdminUpdated, idKey(block), addrKey(admin), addrKey(heir))

	logs = logsOf(heir, cas20Call(selRenounceAdmin, u256hash(block)))
	if len(logs) != 1 {
		t.Fatalf("renounceAdmin emitted %d logs, want 1", len(logs))
	}
	wantTopics("PolicyAdminUpdated (renounce)", logs[0],
		cas20TopicPolicyAdminUpdated, idKey(block), addrKey(heir), addrKey(common.Address{}))
}

func encodeCreatePolicyWithAccounts(admin common.Address, ptype byte, accounts []common.Address) []byte {
	out := append([]byte{}, selCreatePolicyWithAccounts[:]...)
	out = append(out, addrKey(admin).Bytes()...)
	out = append(out, u256hash(uint64(ptype)).Bytes()...)
	out = append(out, u256hash(0x60).Bytes()...) // offset past the 3-word head
	out = append(out, u256hash(uint64(len(accounts))).Bytes()...)
	for _, a := range accounts {
		out = append(out, addrKey(a).Bytes()...)
	}
	return out
}

func TestCAS20MembershipArgsDecodeStrictly(t *testing.T) {
	_, evm := newCAS20EVM(t)
	admin := cas20TestCaller
	call := func(input []byte) ([]byte, error) {
		ret, _, err := evm.Call(admin, CAS20PolicyRegistryAddress, input,
			NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	ret, err := call(cas20Call(selCreatePolicy, addrKey(admin), u256hash(cas20PolicyAllowlist)))
	if err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	id := new(uint256.Int).SetBytes(ret).Uint64()

	dirty := addrKey(cas20Alice)
	dirty[0] = 0x01 // a byte above the low twenty

	// A dirty element would add an account other than the one the encoding names.
	if _, err := call(encodeUpdateListRaw(selUpdateAllowlist, id, u256hash(1), []common.Hash{dirty})); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("updateAllowlist with a dirty address element: err = %v, want a revert", err)
	}
	if _, err := call(encodeUpdateListRaw(selUpdateAllowlist, id, u256hash(2), []common.Hash{addrKey(cas20Alice)})); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("updateAllowlist with allowed = 2: err = %v, want a revert", err)
	}
	if _, err := call(encodeUpdateListRaw(selUpdateAllowlist, id, u256hash(1), []common.Hash{addrKey(cas20Alice)})); err != nil {
		t.Fatalf("updateAllowlist with clean args: %v", err)
	}
	if got, err := call(cas20Call(selIsAuthorized, wU64(id), addrKey(cas20Alice))); err != nil {
		t.Fatalf("isAuthorized: %v", err)
	} else if !bytes.Equal(got, encBool(true)) {
		t.Error("alice was not added by the clean call")
	}

	if _, err := call(encodeCreatePolicyWithAccountsRaw(addrKey(admin),
		u256hash(cas20PolicyAllowlist), []common.Hash{dirty})); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("createPolicyWithAccounts with a dirty element: err = %v, want a revert", err)
	}
}

func encodeUpdateListRaw(sel [4]byte, id uint64, flag common.Hash, accounts []common.Hash) []byte {
	out := append([]byte{}, sel[:]...)
	out = append(out, wU64(id).Bytes()...)
	out = append(out, flag.Bytes()...)
	out = append(out, u256hash(0x60).Bytes()...)
	out = append(out, u256hash(uint64(len(accounts))).Bytes()...)
	for _, a := range accounts {
		out = append(out, a.Bytes()...)
	}
	return out
}

func encodeCreatePolicyWithAccountsRaw(admin, ptype common.Hash, accounts []common.Hash) []byte {
	out := append([]byte{}, selCreatePolicyWithAccounts[:]...)
	out = append(out, admin.Bytes()...)
	out = append(out, ptype.Bytes()...)
	out = append(out, u256hash(0x60).Bytes()...)
	out = append(out, u256hash(uint64(len(accounts))).Bytes()...)
	for _, a := range accounts {
		out = append(out, a.Bytes()...)
	}
	return out
}

func TestCAS20SentinelErrorDoesNotDependOnInitialization(t *testing.T) {
	_, evm := newCAS20EVM(t)
	caller := common.HexToAddress("0xca11e4")
	call := func(input []byte) ([]byte, error) {
		ret, _, err := evm.Call(caller, CAS20PolicyRegistryAddress, input,
			NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}

	// The sentinels' words are unwritten; administering one must still fail as
	// unauthorized, not as missing.
	want := func(input []byte, sel [4]byte, what string) {
		t.Helper()
		ret, err := call(input)
		if !errors.Is(err, ErrExecutionReverted) {
			t.Fatalf("%s: err = %v, want a revert", what, err)
		}
		if len(ret) < 4 || [4]byte(ret[:4]) != sel {
			t.Errorf("%s: revert = %x, want %x", what, ret[:min(4, len(ret))], sel)
		}
	}
	for _, id := range []uint64{cas20PolicyAlwaysAllow, cas20PolicyAlwaysBlock} {
		want(cas20Call(selStageUpdateAdmin, wU64(id), addrKey(caller)),
			errSelUnauthorized, "stageUpdateAdmin on a sentinel before initialization")
	}

	// Creating anything runs ensureInitialized; the answer must not change.
	if _, err := call(cas20Call(selCreatePolicy, addrKey(caller), u256hash(cas20PolicyAllowlist))); err != nil {
		t.Fatalf("createPolicy: %v", err)
	}
	for _, id := range []uint64{cas20PolicyAlwaysAllow, cas20PolicyAlwaysBlock} {
		want(cas20Call(selStageUpdateAdmin, wU64(id), addrKey(caller)),
			errSelUnauthorized, "stageUpdateAdmin on a sentinel after initialization")
	}
}

func encodeComposite(sel [4]byte, head []common.Hash, kids []uint64) []byte {
	out := append([]byte{}, sel[:]...)
	for _, w := range head {
		out = append(out, w.Bytes()...)
	}
	out = append(out, u256hash(uint64((len(head)+1)*32)).Bytes()...)
	out = append(out, u256hash(uint64(len(kids))).Bytes()...)
	for _, k := range kids {
		out = append(out, wU64(k).Bytes()...)
	}
	return out
}

// A set holding both a missing child and an ineligible one owes PolicyNotFound
// whatever their order; a per-child validator would pass every other assertion here.
func TestCAS20CompositeChildValidation(t *testing.T) {
	_, evm := newCAS20EVM(t)
	admin := common.HexToAddress("0xad4149")
	call := func(input []byte) ([]byte, error) {
		ret, _, err := evm.Call(admin, CAS20PolicyRegistryAddress, input, NewGasBudget(5_000_000), uint256.NewInt(0))
		return ret, err
	}
	create := func(ptype uint64) uint64 {
		t.Helper()
		ret, err := call(cas20Call(selCreatePolicy, addrKey(admin), u256hash(ptype)))
		if err != nil {
			t.Fatalf("createPolicy(%d): %v", ptype, err)
		}
		return new(uint256.Int).SetBytes(ret).Uint64()
	}
	revertsWith := func(what string, input []byte, sel [4]byte, args ...common.Hash) {
		t.Helper()
		ret, err := call(input)
		if !errors.Is(err, ErrExecutionReverted) {
			t.Errorf("%s: err = %v, want revert", what, err)
			return
		}
		want := append([]byte{}, sel[:]...)
		for _, a := range args {
			want = append(want, a.Bytes()...)
		}
		if !bytes.Equal(ret, want) {
			t.Errorf("%s: revert data %x, want %x", what, ret, want)
		}
	}

	block, allow := create(cas20PolicyBlocklist), create(cas20PolicyAllowlist)
	unionHead := []common.Hash{addrKey(admin), u256hash(cas20PolicyUnion)}

	ret, err := call(encodeComposite(selCreateComposite, unionHead, []uint64{block, allow}))
	if err != nil {
		t.Fatalf("createCompositePolicy: %v", err)
	}
	composite := new(uint256.Int).SetBytes(ret).Uint64()

	ghost := uint64(cas20PolicyAllowlist)<<56 | 999

	revertsWith("composite child before missing child",
		encodeComposite(selCreateComposite, unionHead, []uint64{composite, ghost}), errSelPolicyNotFound)
	// Both entry points, in case a refactor specializes either path.
	revertsWith("composite child before missing child, on update",
		encodeComposite(selUpdateComposite, []common.Hash{u256hash(composite)},
			[]uint64{composite, ghost}), errSelPolicyNotFound)
	revertsWith("missing child alone",
		encodeComposite(selCreateComposite, unionHead, []uint64{ghost, block}), errSelPolicyNotFound)

	// Only a simple policy the registry minted can be a child.
	revertsWith("composite as child",
		encodeComposite(selCreateComposite, unionHead, []uint64{composite, block}),
		errSelInvalidChildPolicy, wU64(composite))
	revertsWith("always-allow sentinel as child",
		encodeComposite(selCreateComposite, unionHead, []uint64{cas20PolicyAlwaysAllow, block}),
		errSelInvalidChildPolicy, wU64(cas20PolicyAlwaysAllow))
	revertsWith("always-block sentinel as child",
		encodeComposite(selCreateComposite, unionHead, []uint64{block, cas20PolicyAlwaysBlock}),
		errSelInvalidChildPolicy, wU64(cas20PolicyAlwaysBlock))

	// The count bound precedes both passes.
	revertsWith("count bound precedes child checks",
		encodeComposite(selCreateComposite, unionHead, []uint64{ghost}), errSelChildrenOutOfRange)

	// Zero admin before type, as in the simple constructors.
	revertsWith("zero admin outranks incompatible type",
		encodeComposite(selCreateComposite,
			[]common.Hash{{}, u256hash(cas20PolicyBlocklist)}, []uint64{block, allow}),
		errSelZeroAddress)

	revertsWith("updateComposite on a missing composite",
		encodeComposite(selUpdateComposite, []common.Hash{u256hash(ghost)}, []uint64{block, allow}),
		errSelPolicyNotFound)
	revertsWith("updateComposite on a simple policy",
		encodeComposite(selUpdateComposite, []common.Hash{u256hash(block)}, []uint64{block, allow}),
		errSelIncompatibleType)

	if _, err := call(encodeComposite(selUpdateComposite,
		[]common.Hash{u256hash(composite)}, []uint64{allow, block})); err != nil {
		t.Fatalf("updateComposite with a valid child set: %v", err)
	}
}

// An empty composite is reachable only through an id the registry never minted.
// OR over nothing is false and AND over nothing is true, so a never-created
// INTERSECT behaves as ALWAYS_ALLOW; binding checks existence, so no token can
// reference either.
func TestCAS20EmptyCompositeEvaluation(t *testing.T) {
	_, evm := newCAS20EVM(t)
	caller := cas20TestCaller
	ask := func(id uint64) bool {
		t.Helper()
		ret, _, err := evm.Call(caller, CAS20PolicyRegistryAddress,
			cas20Call(selIsAuthorized, wU64(id), addrKey(cas20Alice)),
			NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("isAuthorized(%#x): %v", id, err)
		}
		return bytes.Equal(ret, encBool(true))
	}

	ghostUnion := uint64(cas20PolicyUnion)<<56 | 7
	ghostIntersect := uint64(cas20PolicyIntersect)<<56 | 7
	ghostBlocklist := uint64(cas20PolicyBlocklist)<<56 | 7
	ghostAllowlist := uint64(cas20PolicyAllowlist)<<56 | 7

	if ask(ghostUnion) {
		t.Error("a never-created UNION authorized an account: an OR over no children is false")
	}
	if !ask(ghostIntersect) {
		t.Error("a never-created INTERSECT refused an account: an AND over no children is " +
			"vacuously true, so it authorizes everyone")
	}
	// The simple types for contrast, under the same emptiness rule.
	if !ask(ghostBlocklist) {
		t.Error("a never-created BLOCKLIST refused an account: an empty blocklist blocks no one")
	}
	if ask(ghostAllowlist) {
		t.Error("a never-created ALLOWLIST authorized an account: an empty allowlist admits no one")
	}
	// None of them exists, which is what keeps the tolerance unreachable from a token.
	for _, id := range []uint64{ghostUnion, ghostIntersect, ghostBlocklist, ghostAllowlist} {
		ret, _, err := evm.Call(caller, CAS20PolicyRegistryAddress,
			cas20Call(selPolicyExists, wU64(id)), NewGasBudget(5_000_000), uint256.NewInt(0))
		if err != nil {
			t.Fatalf("policyExists(%#x): %v", id, err)
		}
		if !bytes.Equal(ret, encBool(false)) {
			t.Errorf("policyExists(%#x) = true, want false", id)
		}
	}
}
