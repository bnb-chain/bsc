package core

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// B20 end-to-end journeys, modelled on the smoke journeys base-std runs against
// a live chain (script/smoke/journeys). Those drive a node over RPC; these drive
// a real BlockChain — signed transactions, mined blocks, receipts, and the fork
// hook that seeds the registries — which is the closest in-tree equivalent and
// covers what the in-process core/vm tests cannot: the fork boundary itself,
// state carried across blocks, receipt status, and logs as a node reports them.
//
// Calldata here is built from method signatures with go-ethereum's ABI encoder
// rather than the precompile's own helpers. An e2e that reused those would agree
// with the implementation by construction; this agrees with an integrator.

var (
	b20E2EDeployerKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	b20E2EDeployer       = crypto.PubkeyToAddress(b20E2EDeployerKey.PublicKey)
	b20E2EAdminKey, _    = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7f")
	b20E2EAdmin          = crypto.PubkeyToAddress(b20E2EAdminKey.PublicKey)

	b20E2EAlice = common.HexToAddress("0xa11ce00000000000000000000000000000000001")
	b20E2EBob   = common.HexToAddress("0xb0b0000000000000000000000000000000000002")
)

const (
	b20E2EVariantAsset      = 0
	b20E2EVariantStablecoin = 1

	// The fork lands on the block at this timestamp. Genesis is at 0 and each
	// block advances by b20E2EBlockTime, so blocks 1..2 are pre-fork.
	b20E2EPasteurTime = 30
	b20E2EBlockTime   = 10
)

// --- ABI plumbing -----------------------------------------------------------

func b20Sel(sig string) []byte { return crypto.Keccak256([]byte(sig))[:4] }

func b20Topic(sig string) common.Hash { return crypto.Keccak256Hash([]byte(sig)) }

// b20Args builds abi.Arguments from Solidity type names.
func b20Args(t *testing.T, types ...string) abi.Arguments {
	t.Helper()
	out := make(abi.Arguments, len(types))
	for i, ty := range types {
		parsed, err := abi.NewType(ty, "", nil)
		if err != nil {
			t.Fatalf("abi.NewType(%s): %v", ty, err)
		}
		out[i] = abi.Argument{Type: parsed}
	}
	return out
}

// b20Call encodes a call to sig with the given argument values. The types are
// read out of the signature itself, so the two can never drift apart.
func b20Call(t *testing.T, sig string, values ...interface{}) []byte {
	t.Helper()
	open, close := bytes.IndexByte([]byte(sig), '('), len(sig)-1
	if open < 0 || sig[close] != ')' {
		t.Fatalf("malformed signature %q", sig)
	}
	var typeNames []string
	if inner := sig[open+1 : close]; inner != "" {
		for _, ty := range splitTopLevel(inner) {
			typeNames = append(typeNames, ty)
		}
	}
	packed, err := b20Args(t, typeNames...).Pack(values...)
	if err != nil {
		t.Fatalf("pack %s: %v", sig, err)
	}
	return append(b20Sel(sig), packed...)
}

// splitTopLevel splits a signature's argument list on commas that are not
// nested inside a tuple.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// b20StructBlob is abi.encode(someStruct): a dynamic tuple, which carries a
// leading offset word. Packing the members as separate arguments would produce
// the same bytes without it and the factory's decode would fail.
func b20StructBlob(t *testing.T, members []abi.ArgumentMarshaling, value interface{}) []byte {
	t.Helper()
	ty, err := abi.NewType("tuple", "", members)
	if err != nil {
		t.Fatalf("abi.NewType(tuple): %v", err)
	}
	packed, err := abi.Arguments{{Type: ty}}.Pack(value)
	if err != nil {
		t.Fatalf("pack params struct: %v", err)
	}
	return packed
}

// b20AssetParams encodes B20AssetCreateParams as the factory's `params` blob.
func b20AssetParams(t *testing.T, name, symbol string, admin common.Address, decimals uint8) []byte {
	t.Helper()
	return b20StructBlob(t, []abi.ArgumentMarshaling{
		{Name: "version", Type: "uint8"},
		{Name: "name", Type: "string"},
		{Name: "symbol", Type: "string"},
		{Name: "initialAdmin", Type: "address"},
		{Name: "decimals", Type: "uint8"},
	}, struct {
		Version      uint8
		Name         string
		Symbol       string
		InitialAdmin common.Address
		Decimals     uint8
	}{1, name, symbol, admin, decimals})
}

// b20StablecoinParams encodes B20StablecoinCreateParams. There is no decimals
// field: the variant fixes it at 6.
func b20StablecoinParams(t *testing.T, name, symbol string, admin common.Address, currency string) []byte {
	t.Helper()
	return b20StructBlob(t, []abi.ArgumentMarshaling{
		{Name: "version", Type: "uint8"},
		{Name: "name", Type: "string"},
		{Name: "symbol", Type: "string"},
		{Name: "initialAdmin", Type: "address"},
		{Name: "currency", Type: "string"},
	}, struct {
		Version      uint8
		Name         string
		Symbol       string
		InitialAdmin common.Address
		Currency     string
	}{1, name, symbol, admin, currency})
}

func b20CreateCall(t *testing.T, variant uint8, salt common.Hash, createParams []byte, initCalls [][]byte) []byte {
	t.Helper()
	if initCalls == nil {
		initCalls = [][]byte{}
	}
	return b20Call(t, "createB20(uint8,bytes32,bytes,bytes[])",
		variant, [32]byte(salt), createParams, initCalls)
}

// --- chain harness ----------------------------------------------------------

// b20Chain is a running BlockChain with a Parlia-shaped config, so IsInBSC holds
// and the fork hook that seeds the B20 registries actually fires.
type b20Chain struct {
	t     *testing.T
	bc    *BlockChain
	gspec *Genesis
	nonce map[common.Address]uint64
}

func newB20Chain(t *testing.T) *b20Chain {
	t.Helper()

	// Every fork before Pasteur is active from genesis, and Pasteur alone lands
	// mid-chain. NewBlockChain validates the ordering, so each one between Cancun
	// (where TestChainConfig stops) and Pasteur has to be named.
	// ParliaTestChainConfig carries Parlia — what IsInBSC keys on, and what B20
	// is gated on alongside IsPasteur — plus every BSC fork through Cancun. The
	// engine is a faker; only the config's shape matters. NewBlockChain rejects a
	// config that enables one fork while an earlier one is off, so every fork
	// between Cancun and Pasteur has to be named, and the chain has to be
	// post-merge for the beacon engine to seal it.
	cfg := *params.ParliaTestChainConfig
	cfg.ChainID = big.NewInt(714)
	cfg.TerminalTotalDifficulty = common.Big0
	zero := uint64(0)
	for _, at := range []**uint64{
		&cfg.HaberTime, &cfg.HaberFixTime, &cfg.BohrTime, &cfg.PascalTime,
		&cfg.PragueTime, &cfg.LorentzTime, &cfg.MaxwellTime, &cfg.FermiTime,
		&cfg.OsakaTime, &cfg.MendelTime,
	} {
		*at = &zero
	}
	cfg.BlobScheduleConfig = &params.BlobScheduleConfig{
		Cancun: params.DefaultCancunBlobConfig,
		Prague: params.DefaultPragueBlobConfig,
		Osaka:  params.DefaultOsakaBlobConfig,
	}
	pasteur := uint64(b20E2EPasteurTime)
	cfg.PasteurTime = &pasteur
	cfg.B20ActivationAdmin = &b20E2EAdmin

	gspec := &Genesis{
		Config:    &cfg,
		Timestamp: 0,
		Alloc: types.GenesisAlloc{
			b20E2EDeployer: {Balance: new(big.Int).Mul(big.NewInt(1000), big.NewInt(params.Ether))},
			b20E2EAdmin:    {Balance: new(big.Int).Mul(big.NewInt(1000), big.NewInt(params.Ether))},
		},
		GasLimit: 30_000_000,
	}
	bc, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, beacon.New(ethash.NewFaker()), nil)
	if err != nil {
		t.Fatalf("NewBlockChain: %v", err)
	}
	t.Cleanup(bc.Stop)
	return &b20Chain{t: t, bc: bc, gspec: gspec, nonce: map[common.Address]uint64{}}
}

// b20Tx is one transaction to include in the next block. A zero `from` means the
// deployer, who holds every role the journeys need; naming the admin instead is
// how a journey checks that an authority is not shared.
type b20Tx struct {
	from common.Address
	to   common.Address
	data []byte
}

// mine seals one block containing calls, and returns its receipts in order. A
// reverted call is a failed receipt, not an error: that is how a node reports
// it, and asserting on the status is the point.
func (c *b20Chain) mine(calls ...b20Tx) types.Receipts {
	c.t.Helper()
	parent := c.bc.CurrentBlock()
	signer := types.LatestSigner(c.bc.Config())

	blocks, receipts := GenerateChain(c.bc.Config(), c.bc.GetBlockByHash(parent.Hash()),
		c.bc.Engine(), c.bc.db, 1, func(i int, gen *BlockGen) {
			gen.OffsetTime(0)
			for _, call := range calls {
				key := b20E2EDeployerKey
				if call.from == b20E2EAdmin {
					key = b20E2EAdminKey
				}
				from := crypto.PubkeyToAddress(key.PublicKey)
				tx, err := types.SignNewTx(key, signer, &types.LegacyTx{
					Nonce:    c.nonce[from],
					To:       &call.to,
					Gas:      8_000_000,
					GasPrice: big.NewInt(params.InitialBaseFee),
					Data:     call.data,
				})
				if err != nil {
					c.t.Fatalf("sign: %v", err)
				}
				c.nonce[from]++
				gen.AddTxWithChain(c.bc, tx)
			}
		})
	if _, err := c.bc.InsertChain(blocks); err != nil {
		c.t.Fatalf("InsertChain: %v", err)
	}
	if len(receipts) == 0 {
		return nil
	}
	return receipts[0]
}

// mineEmpty advances the chain by n blocks with no transactions.
func (c *b20Chain) mineEmpty(n int) {
	c.t.Helper()
	for i := 0; i < n; i++ {
		c.mine()
	}
}

// state returns the state at the chain head.
func (c *b20Chain) state() *state.StateDB {
	c.t.Helper()
	sdb, err := c.bc.StateAt(c.bc.CurrentBlock())
	if err != nil {
		c.t.Fatalf("StateAt: %v", err)
	}
	return sdb
}

// call is the eth_call equivalent: a read against head state that never mines.
func (c *b20Chain) call(to common.Address, data []byte) ([]byte, error) {
	c.t.Helper()
	head := c.bc.CurrentBlock()
	sdb, err := c.bc.StateAt(head)
	if err != nil {
		c.t.Fatalf("StateAt: %v", err)
	}
	blockCtx := NewEVMBlockContext(head, c.bc, nil)
	evm := vm.NewEVM(blockCtx, sdb, c.bc.Config(), vm.Config{})
	ret, _, callErr := evm.Call(b20E2EDeployer, to, data, vm.NewGasBudget(10_000_000), uint256.NewInt(0))
	return ret, callErr
}

// callU256 reads a uint256-returning view, failing the test if it reverts.
func (c *b20Chain) callU256(to common.Address, sig string, args ...interface{}) *big.Int {
	c.t.Helper()
	ret, err := c.call(to, b20Call(c.t, sig, args...))
	if err != nil {
		c.t.Fatalf("%s: %v", sig, err)
	}
	return new(big.Int).SetBytes(ret)
}

func (c *b20Chain) callAddress(to common.Address, sig string, args ...interface{}) common.Address {
	c.t.Helper()
	ret, err := c.call(to, b20Call(c.t, sig, args...))
	if err != nil {
		c.t.Fatalf("%s: %v", sig, err)
	}
	return common.BytesToAddress(ret)
}

// --- assertions -------------------------------------------------------------

func mustSucceed(t *testing.T, r *types.Receipt, what string) {
	t.Helper()
	if r == nil {
		t.Fatalf("%s: no receipt", what)
	}
	if r.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("%s: receipt status failed (gas used %d)", what, r.GasUsed)
	}
}

func mustFail(t *testing.T, r *types.Receipt, what string) {
	t.Helper()
	if r == nil {
		t.Fatalf("%s: no receipt", what)
	}
	if r.Status != types.ReceiptStatusSuccessful {
		return
	}
	t.Fatalf("%s: receipt succeeded, want a revert", what)
}

// logTopics lists topic0 of every log in a receipt, in order.
func logTopics(r *types.Receipt) []common.Hash {
	out := make([]common.Hash, 0, len(r.Logs))
	for _, l := range r.Logs {
		out = append(out, l.Topics[0])
	}
	return out
}

func mustBalance(t *testing.T, c *b20Chain, token, who common.Address, want int64, what string) {
	t.Helper()
	if got := c.callU256(token, "balanceOf(address)", who); got.Int64() != want {
		t.Errorf("%s: balanceOf = %s, want %d", what, got, want)
	}
}

// --- journey 1: the fork boundary and the activation switch -----------------

// TestB20E2EForkAndActivation walks what base-std calls the activation preflight,
// but from one block earlier than a live chain can: the fork itself.
//
// Before Pasteur the reserved addresses are ordinary accounts. The fork seeds the
// registries and installs the configured admin — and opens nothing. A feature
// then has to be activated by that admin, by transaction, before the factory will
// build anything.
func TestB20E2EForkAndActivation(t *testing.T) {
	c := newB20Chain(t)
	featureAsset := b20Topic("bsc.b20_asset")
	salt := common.HexToHash("0x01")
	createAsset := b20CreateCall(t, b20E2EVariantAsset, salt,
		b20AssetParams(t, "Pre Fork", "PF", b20E2EDeployer, 18), nil)

	// Pre-fork: the factory address holds no code and dispatches to nothing, so
	// a call to it is a plain value-less call to an empty account and succeeds.
	c.mineEmpty(1)
	if c.bc.CurrentBlock().Time >= b20E2EPasteurTime {
		t.Fatalf("block %d is already past the fork; the pre-fork half tests nothing",
			c.bc.CurrentBlock().Number)
	}
	if admin := vm.B20ActivationAdmin(c.state()); admin != (common.Address{}) {
		t.Errorf("pre-fork activation admin = %s, want zero — nothing seeds it yet", admin.Hex())
	}
	pre := c.mine(b20Tx{to: vm.B20FactoryAddress, data: createAsset})
	mustSucceed(t, pre[0], "pre-fork call to the factory address")
	if len(pre[0].Logs) != 0 {
		t.Errorf("pre-fork call emitted %d logs; the address is not a precompile yet", len(pre[0].Logs))
	}

	// Cross the fork. The hook seeds both registry sentinels and the admin.
	c.mineEmpty(3)
	if c.bc.CurrentBlock().Time < b20E2EPasteurTime {
		t.Fatalf("chain never reached the fork (head time %d)", c.bc.CurrentBlock().Time)
	}
	if admin := vm.B20ActivationAdmin(c.state()); admin != b20E2EAdmin {
		t.Fatalf("post-fork activation admin = %s, want the configured %s", admin.Hex(), b20E2EAdmin.Hex())
	}
	if got := c.callAddress(vm.B20ActivationRegistryAddress, "admin()"); got != b20E2EAdmin {
		t.Errorf("admin() = %s, want %s", got.Hex(), b20E2EAdmin.Hex())
	}

	// The fork opens no feature, so creation is still refused.
	shut := c.mine(b20Tx{to: vm.B20FactoryAddress, data: createAsset})
	mustFail(t, shut[0], "createB20 before the feature is activated")

	// Only the admin may open it.
	notAdmin := c.mine(b20Tx{to: vm.B20ActivationRegistryAddress,
		data: b20Call(t, "activate(bytes32)", [32]byte(featureAsset))})
	mustFail(t, notAdmin[0], "activate from a non-admin account")

	opened := c.mine(b20Tx{from: b20E2EAdmin, to: vm.B20ActivationRegistryAddress,
		data: b20Call(t, "activate(bytes32)", [32]byte(featureAsset))})
	mustSucceed(t, opened[0], "activate from the admin")
	if got, want := logTopics(opened[0]), b20Topic("FeatureActivated(bytes32,address)"); len(got) != 1 || got[0] != want {
		t.Errorf("activate logs = %v, want one FeatureActivated", got)
	}

	// And now the same transaction that failed twice goes through.
	made := c.mine(b20Tx{to: vm.B20FactoryAddress, data: createAsset})
	mustSucceed(t, made[0], "createB20 once the feature is open")

	token := c.callAddress(vm.B20FactoryAddress,
		"getB20Address(uint8,address,bytes32)", uint8(b20E2EVariantAsset), b20E2EDeployer, [32]byte(salt))
	if !vm.IsB20Address(token) {
		t.Fatalf("predicted address %s is outside the reserved space", token.Hex())
	}
	// B20Created comes from the factory, not the token, so one address carries
	// every creation — and it comes last, after the token's own initial-admin
	// grant, since the factory emits it once the token is fully built.
	created := findLog(made[0], b20Topic("B20Created(address,uint8,string,string,uint8,bytes)"))
	if created == nil {
		t.Fatal("no B20Created log")
	}
	if created.Address != vm.B20FactoryAddress {
		t.Errorf("B20Created was emitted by %s, want the factory %s",
			created.Address.Hex(), vm.B20FactoryAddress.Hex())
	}
	if got := common.BytesToAddress(created.Topics[1].Bytes()); got != token {
		t.Errorf("B20Created names %s, but the prediction was %s", got.Hex(), token.Hex())
	}
	if last := made[0].Logs[len(made[0].Logs)-1]; last != created {
		t.Errorf("B20Created is log %d of %d, want the last one",
			indexOfLog(made[0], created), len(made[0].Logs))
	}
	// The token announced its own initial admin before that.
	if granted := findLog(made[0], b20Topic("RoleGranted(bytes32,address,address)")); granted == nil {
		t.Error("no RoleGranted for the initial admin")
	} else if granted.Address != token {
		t.Errorf("RoleGranted came from %s, want the token %s", granted.Address.Hex(), token.Hex())
	}
}

// findLog returns the first log in r with the given topic0, or nil.
func findLog(r *types.Receipt, topic0 common.Hash) *types.Log {
	for _, l := range r.Logs {
		if len(l.Topics) > 0 && l.Topics[0] == topic0 {
			return l
		}
	}
	return nil
}

func indexOfLog(r *types.Receipt, want *types.Log) int {
	for i, l := range r.Logs {
		if l == want {
			return i
		}
	}
	return -1
}

// --- journey 2: the Asset lifecycle -----------------------------------------

// TestB20E2EAssetLifecycle follows base-std's asset_lifecycle journey: create,
// mint, transfer, delegated spend, an announced batch mint, an announced rebase,
// metadata, and burn — each as its own transaction, with balances read back
// between blocks.
func TestB20E2EAssetLifecycle(t *testing.T) {
	c := newB20Chain(t)
	token := b20E2EOpenAssetToken(t, c, common.HexToHash("0x0a"))

	if got := c.callU256(token, "decimals()"); got.Int64() != 18 {
		t.Errorf("decimals = %s, want 18", got)
	}
	if got, err := c.call(vm.B20FactoryAddress,
		b20Call(t, "isB20Initialized(address)", token)); err != nil || got[31] != 1 {
		t.Fatalf("isB20Initialized = %x, %v; want true after creation", got, err)
	}

	// mint and transfer
	r := c.mine(
		b20Tx{to: token, data: b20Call(t, "mint(address,uint256)", b20E2EAlice, big.NewInt(1000))},
		b20Tx{to: token, data: b20Call(t, "mint(address,uint256)", b20E2EDeployer, big.NewInt(500))},
		b20Tx{to: token, data: b20Call(t, "transfer(address,uint256)", b20E2EBob, big.NewInt(200))},
	)
	for i, what := range []string{"mint alice", "mint deployer", "transfer bob"} {
		mustSucceed(t, r[i], what)
	}
	mustBalance(t, c, token, b20E2EAlice, 1000, "after mint")
	mustBalance(t, c, token, b20E2EBob, 200, "after transfer")
	mustBalance(t, c, token, b20E2EDeployer, 300, "after transfer")
	if got := c.callU256(token, "totalSupply()"); got.Int64() != 1500 {
		t.Errorf("totalSupply = %s, want 1500", got)
	}

	// transferWithMemo: the Memo log must follow the Transfer of the same call.
	memo := common.HexToHash("0x1234")
	r = c.mine(b20Tx{to: token,
		data: b20Call(t, "transferWithMemo(address,uint256,bytes32)", b20E2EBob, big.NewInt(1), [32]byte(memo))})
	mustSucceed(t, r[0], "transferWithMemo")
	if got := logTopics(r[0]); len(got) != 2 ||
		got[0] != b20Topic("Transfer(address,address,uint256)") ||
		got[1] != b20Topic("Memo(address,bytes32)") {
		t.Errorf("transferWithMemo logs = %v, want Transfer then Memo", got)
	}

	// delegated spend
	r = c.mine(b20Tx{to: token, data: b20Call(t, "approve(address,uint256)", b20E2EAdmin, big.NewInt(50))})
	mustSucceed(t, r[0], "approve")
	r = c.mine(b20Tx{from: b20E2EAdmin, to: token,
		data: b20Call(t, "transferFrom(address,address,uint256)", b20E2EDeployer, b20E2EBob, big.NewInt(50))})
	mustSucceed(t, r[0], "transferFrom within the allowance")
	mustBalance(t, c, token, b20E2EBob, 251, "after the delegated spend")
	if got := c.callU256(token, "allowance(address,address)", b20E2EDeployer, b20E2EAdmin); got.Sign() != 0 {
		t.Errorf("allowance after a full spend = %s, want 0", got)
	}

	// The allowance is gone, so the same call must now fail.
	r = c.mine(b20Tx{from: b20E2EAdmin, to: token,
		data: b20Call(t, "transferFrom(address,address,uint256)", b20E2EDeployer, b20E2EBob, big.NewInt(1))})
	mustFail(t, r[0], "transferFrom with the allowance spent")

	// An announced batch mint: the disclosure and the act land in one transaction.
	batch := b20Call(t, "batchMint(address[],uint256[])",
		[]common.Address{b20E2EAlice, b20E2EBob}, []*big.Int{big.NewInt(10), big.NewInt(20)})
	r = c.mine(b20Tx{to: token, data: b20Call(t, "announce(bytes[],string,string,string)",
		[][]byte{batch}, "2026-Q1-ISSUE", "quarterly issuance", "ipfs://QmIssue")})
	mustSucceed(t, r[0], "announce + batchMint")
	mustBalance(t, c, token, b20E2EAlice, 1010, "after the announced batch mint")
	mustBalance(t, c, token, b20E2EBob, 271, "after the announced batch mint")

	// The bracket: Announcement first, EndAnnouncement last, the batch between.
	topics := logTopics(r[0])
	if len(topics) < 3 ||
		topics[0] != b20Topic("Announcement(address,string,string,string)") ||
		topics[len(topics)-1] != b20Topic("EndAnnouncement(string)") {
		t.Errorf("announce logs = %v, want the bracket around the bundle", topics)
	}
	if used := c.callU256(token, "isAnnouncementIdUsed(string)", "2026-Q1-ISSUE"); used.Int64() != 1 {
		t.Error("the announcement id is not marked used")
	}

	// Reusing the id must fail, and roll the bundle back with it.
	r = c.mine(b20Tx{to: token, data: b20Call(t, "announce(bytes[],string,string,string)",
		[][]byte{batch}, "2026-Q1-ISSUE", "replay", "")})
	mustFail(t, r[0], "announce with a used id")
	mustBalance(t, c, token, b20E2EAlice, 1010, "after the refused replay")

	// An announced rebase. Raw balances do not move; the scaled view doubles.
	rawAlice := c.callU256(token, "balanceOf(address)", b20E2EAlice)
	r = c.mine(b20Tx{to: token, data: b20Call(t, "announce(bytes[],string,string,string)",
		[][]byte{b20Call(t, "updateMultiplier(uint256)", b20E2EWad(2))},
		"2026-Q1-NAV", "NAV doubled", "ipfs://QmNav")})
	mustSucceed(t, r[0], "announce + updateMultiplier")
	if got := c.callU256(token, "multiplier()"); got.Cmp(b20E2EWad(2)) != 0 {
		t.Errorf("multiplier = %s, want 2e18", got)
	}
	if got := c.callU256(token, "balanceOf(address)", b20E2EAlice); got.Cmp(rawAlice) != 0 {
		t.Errorf("raw balance moved on a rebase: %s, want %s", got, rawAlice)
	}
	if got, want := c.callU256(token, "scaledBalanceOf(address)", b20E2EAlice),
		new(big.Int).Mul(rawAlice, big.NewInt(2)); got.Cmp(want) != 0 {
		t.Errorf("scaledBalanceOf = %s, want %s", got, want)
	}

	// metadata, then burn
	r = c.mine(
		b20Tx{to: token, data: b20Call(t, "updateExtraMetadata(string,string)", "category", "rwa")},
		b20Tx{to: token, data: b20Call(t, "burn(uint256)", big.NewInt(100))},
	)
	mustSucceed(t, r[0], "updateExtraMetadata")
	mustSucceed(t, r[1], "burn")
	ret, err := c.call(token, b20Call(t, "extraMetadata(string)", "category"))
	if err != nil {
		t.Fatalf("extraMetadata: %v", err)
	}
	if got, _ := b20Args(t, "string").Unpack(ret); len(got) != 1 || got[0].(string) != "rwa" {
		t.Errorf("extraMetadata(category) = %v, want rwa", got)
	}

	// A role the deployer does not hold is refused even after all of the above.
	r = c.mine(b20Tx{from: b20E2EAdmin, to: token,
		data: b20Call(t, "mint(address,uint256)", b20E2EBob, big.NewInt(1))})
	mustFail(t, r[0], "mint from an account without MINT_ROLE")
}

// --- journey 3: policy enforcement ------------------------------------------

// TestB20E2EPolicyEnforcement follows the enforcement half of base-std's
// policy_registry journey: a policy created in one transaction, bound to a token
// in another, and enforced on a third — across blocks, which is where a registry
// read that only works inside one call would show up.
func TestB20E2EPolicyEnforcement(t *testing.T) {
	c := newB20Chain(t)
	token := b20E2EOpenAssetToken(t, c, common.HexToHash("0x0b"))

	// The PolicyRegistry is gated on its own feature.
	feature := b20Topic("bsc.policy_registry")
	shut := c.mine(b20Tx{to: vm.B20PolicyRegistryAddress,
		data: b20Call(t, "createPolicy(address,uint8)", b20E2EDeployer, uint8(1))})
	mustFail(t, shut[0], "createPolicy before the registry feature is activated")

	open := c.mine(b20Tx{from: b20E2EAdmin, to: vm.B20ActivationRegistryAddress,
		data: b20Call(t, "activate(bytes32)", [32]byte(feature))})
	mustSucceed(t, open[0], "activate the policy registry")

	// An allowlist seeded with alice only.
	r := c.mine(b20Tx{to: vm.B20PolicyRegistryAddress,
		data: b20Call(t, "createPolicyWithAccounts(address,uint8,address[])",
			b20E2EDeployer, uint8(1), []common.Address{b20E2EAlice})})
	mustSucceed(t, r[0], "createPolicyWithAccounts")
	policyID := new(big.Int).SetBytes(r[0].Logs[0].Topics[1].Bytes()).Uint64()

	if got, err := c.call(vm.B20PolicyRegistryAddress,
		b20Call(t, "isAuthorized(uint64,address)", policyID, b20E2EAlice)); err != nil || got[31] != 1 {
		t.Fatalf("alice is not authorized by the allowlist she was seeded into: %x %v", got, err)
	}
	if got, err := c.call(vm.B20PolicyRegistryAddress,
		b20Call(t, "isAuthorized(uint64,address)", policyID, b20E2EBob)); err != nil || got[31] != 0 {
		t.Fatalf("bob is authorized by an allowlist he is not in: %x %v", got, err)
	}

	// Bind it to the token's mint-receiver scope, in a later block.
	scope := b20Topic("MINT_RECEIVER_POLICY")
	r = c.mine(b20Tx{to: token,
		data: b20Call(t, "updatePolicy(bytes32,uint64)", [32]byte(scope), policyID)})
	mustSucceed(t, r[0], "updatePolicy(MINT_RECEIVER)")

	// Enforcement, one block later still: alice may receive, bob may not.
	r = c.mine(
		b20Tx{to: token, data: b20Call(t, "mint(address,uint256)", b20E2EAlice, big.NewInt(100))},
		b20Tx{to: token, data: b20Call(t, "mint(address,uint256)", b20E2EBob, big.NewInt(100))},
	)
	mustSucceed(t, r[0], "mint to an allowlisted receiver")
	mustFail(t, r[1], "mint to a receiver outside the allowlist")
	mustBalance(t, c, token, b20E2EAlice, 100, "allowlisted receiver")
	mustBalance(t, c, token, b20E2EBob, 0, "denied receiver")

	// Adding bob to the policy makes the same mint succeed — the token reads the
	// registry live rather than caching the membership at bind time.
	r = c.mine(b20Tx{to: vm.B20PolicyRegistryAddress,
		data: b20Call(t, "updateAllowlist(uint64,bool,address[])",
			policyID, true, []common.Address{b20E2EBob})})
	mustSucceed(t, r[0], "updateAllowlist")
	r = c.mine(b20Tx{to: token, data: b20Call(t, "mint(address,uint256)", b20E2EBob, big.NewInt(100))})
	mustSucceed(t, r[0], "mint after bob was added to the allowlist")
	mustBalance(t, c, token, b20E2EBob, 100, "after the membership change")
}

// --- journey 4: the Stablecoin variant --------------------------------------

// TestB20E2EStablecoinVariant follows base-std's stablecoin_lifecycle far enough
// to pin what makes the variant a variant: a currency fixed at creation and
// decimals that are not a creation parameter at all.
func TestB20E2EStablecoinVariant(t *testing.T) {
	c := newB20Chain(t)
	b20E2EReachFork(t, c)
	b20E2EActivate(t, c, b20Topic("bsc.b20_stablecoin"))

	salt := common.HexToHash("0x0c")
	r := c.mine(b20Tx{to: vm.B20FactoryAddress, data: b20CreateCall(t, b20E2EVariantStablecoin, salt,
		b20StablecoinParams(t, "Test Stable", "TS", b20E2EDeployer, "USD"),
		[][]byte{b20Call(t, "grantRole(bytes32,address)", [32]byte(b20Topic("MINT_ROLE")), b20E2EDeployer)})})
	mustSucceed(t, r[0], "createB20 STABLECOIN")

	token := c.callAddress(vm.B20FactoryAddress, "getB20Address(uint8,address,bytes32)",
		uint8(b20E2EVariantStablecoin), b20E2EDeployer, [32]byte(salt))

	if got := c.callU256(token, "decimals()"); got.Int64() != 6 {
		t.Errorf("stablecoin decimals = %s, want 6 — fixed, not a parameter", got)
	}
	ret, err := c.call(token, b20Call(t, "currency()"))
	if err != nil {
		t.Fatalf("currency(): %v", err)
	}
	if got, _ := b20Args(t, "string").Unpack(ret); len(got) != 1 || got[0].(string) != "USD" {
		t.Errorf("currency() = %v, want USD", got)
	}

	// The Asset extensions are absent from this variant.
	if _, err := c.call(token, b20Call(t, "multiplier()")); err == nil {
		t.Error("multiplier() answered on a Stablecoin; the variant has no multiplier")
	}

	r = c.mine(b20Tx{to: token, data: b20Call(t, "mint(address,uint256)", b20E2EAlice, big.NewInt(1000))})
	mustSucceed(t, r[0], "mint")
	mustBalance(t, c, token, b20E2EAlice, 1000, "stablecoin mint")

	// A duplicate salt is refused rather than overwriting the live token.
	r = c.mine(b20Tx{to: vm.B20FactoryAddress, data: b20CreateCall(t, b20E2EVariantStablecoin, salt,
		b20StablecoinParams(t, "Other", "OT", b20E2EDeployer, "EUR"), nil)})
	mustFail(t, r[0], "createB20 with a salt already used")
	if got := c.callU256(token, "balanceOf(address)", b20E2EAlice); got.Int64() != 1000 {
		t.Errorf("the refused creation disturbed the live token: balance %s", got)
	}
}

// --- shared setup -----------------------------------------------------------

func b20E2EWad(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
}

// b20E2EReachFork advances past Pasteur so the registries are seeded.
func b20E2EReachFork(t *testing.T, c *b20Chain) {
	t.Helper()
	for c.bc.CurrentBlock().Time < b20E2EPasteurTime {
		c.mineEmpty(1)
	}
	if admin := vm.B20ActivationAdmin(c.state()); admin != b20E2EAdmin {
		t.Fatalf("the fork hook did not seed the admin (got %s)", admin.Hex())
	}
}

// b20E2EActivate opens one feature through the admin, by transaction.
func b20E2EActivate(t *testing.T, c *b20Chain, feature common.Hash) {
	t.Helper()
	r := c.mine(b20Tx{from: b20E2EAdmin, to: vm.B20ActivationRegistryAddress,
		data: b20Call(t, "activate(bytes32)", [32]byte(feature))})
	mustSucceed(t, r[0], "activate")
}

// b20E2EOpenAssetToken reaches the fork, opens the Asset feature, and creates a
// token whose deployer holds the roles the journeys exercise.
func b20E2EOpenAssetToken(t *testing.T, c *b20Chain, salt common.Hash) common.Address {
	t.Helper()
	b20E2EReachFork(t, c)
	b20E2EActivate(t, c, b20Topic("bsc.b20_asset"))

	var initCalls [][]byte
	for _, role := range []string{"MINT_ROLE", "BURN_ROLE", "OPERATOR_ROLE", "METADATA_ROLE", "POLICY_ROLE"} {
		initCalls = append(initCalls,
			b20Call(t, "grantRole(bytes32,address)", [32]byte(b20Topic(role)), b20E2EDeployer))
	}
	r := c.mine(b20Tx{to: vm.B20FactoryAddress, data: b20CreateCall(t, b20E2EVariantAsset, salt,
		b20AssetParams(t, "Test Token", "TT", b20E2EDeployer, 18), initCalls)})
	mustSucceed(t, r[0], "createB20 ASSET")

	return c.callAddress(vm.B20FactoryAddress, "getB20Address(uint8,address,bytes32)",
		uint8(b20E2EVariantAsset), b20E2EDeployer, [32]byte(salt))
}
