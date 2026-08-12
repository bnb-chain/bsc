package ethapi

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi/override"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// jennerProbeCode is the runtime code of a probe contract returning 64 bytes:
// word0 = the 0x44 opcode value (PREVRANDAO post-merge / DIFFICULTY on BSC,
// via the EIP-4399 switch in opDifficulty), word1 = the word returned by
// staticcalling the BEP-706 precompile at 0x70 (0 when 0x70 is an empty
// account, i.e. pre-Jenner or non-BSC).
//
//	prevrandao/difficulty -> mem[0:32]
//	staticcall(gas, 0x70, 0, 0, 32, 32) -> mem[32:64]
//	return mem[0:64]
var jennerProbeCode = common.Hex2Bytes("44600052602060206000600060705afa5060406000f3")

// jennerAssertCode reverts unless the word returned by 0x70 equals
// calldataload(0). Used to observe the 0x70 value through eth_estimateGas
// (which only reports success/failure, not return data).
var jennerAssertCode = common.Hex2Bytes("602060006000600060705afa5060005160003514601c5760006000fd5b00")

var (
	jennerProbeAddr  = common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	jennerAssertAddr = common.HexToAddress("0x000000000000000000000000000000000000bbbb")
)

// bscJennerTestConfig returns a Parlia config with every fork including
// Jenner active from genesis.
func bscJennerTestConfig() *params.ChainConfig {
	config := *params.ParliaTestChainConfig
	config.HaberTime = new(uint64)
	config.HaberFixTime = new(uint64)
	config.BohrTime = new(uint64)
	config.PascalTime = new(uint64)
	config.PragueTime = new(uint64)
	config.LorentzTime = new(uint64)
	config.MaxwellTime = new(uint64)
	config.FermiTime = new(uint64)
	config.OsakaTime = new(uint64)
	config.MendelTime = new(uint64)
	config.PasteurTime = new(uint64)
	config.JennerTime = new(uint64)
	config.BlobScheduleConfig = &params.BlobScheduleConfig{
		Cancun: params.DefaultCancunBlobConfig,
		Prague: params.DefaultPragueBlobConfigBSC,
		Osaka:  params.DefaultOsakaBlobConfigBSC,
	}
	return &config
}

func newJennerBSCBackend(t *testing.T) *testBackend {
	t.Helper()
	acc := newTestAccount()
	gspec := &core.Genesis{
		Config: bscJennerTestConfig(),
		Alloc: types.GenesisAlloc{
			acc.addr:         {Balance: big.NewInt(params.Ether)},
			jennerProbeAddr:  {Code: jennerProbeCode},
			jennerAssertAddr: {Code: jennerAssertCode},
		},
	}
	// The plain faker rejects Shanghai+ headers; the full fake skips header
	// verification (body/state validation still runs), same as the
	// chain-level Jenner test in core.
	return newTestBackend(t, 2, gspec, ethash.NewFullFaker(), func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{1})
	})
}

func padMilli(milli uint64) []byte {
	return common.LeftPadBytes(new(big.Int).SetUint64(milli).Bytes(), 32)
}

// callJennerProbe issues an eth_call against the probe contract and returns
// (word at 0x44, word returned by 0x70).
func callJennerProbe(t *testing.T, api *BlockChainAPI, blockOverrides *override.BlockOverrides) (randao common.Hash, milli uint64) {
	t.Helper()
	latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	res, err := api.Call(context.Background(), TransactionArgs{To: &jennerProbeAddr}, &latest, nil, blockOverrides)
	if err != nil {
		t.Fatalf("eth_call(probe): %v", err)
	}
	if len(res) != 64 {
		t.Fatalf("probe must return 64 bytes, got %d", len(res))
	}
	return common.BytesToHash(res[:32]), new(big.Int).SetBytes(res[32:64]).Uint64()
}

// TestJennerBlockOverrides_Call covers the Apply path (eth_call and, by the
// same code path, debug_traceCall): every override field applies exactly when
// provided — time syncs the millisecond timestamp to time*1000, prevRandao
// applies independently, and omitted fields keep the block's original values.
func TestJennerBlockOverrides_Call(t *testing.T) {
	t.Parallel()
	backend := newJennerBSCBackend(t)
	api := NewBlockChainAPI(backend)
	head := backend.CurrentHeader()
	headMilli := head.MilliTimestamp()
	headRandao := common.Hash{} // Random is nil on BSC headers: 0x44 falls back to difficulty
	headRandao = common.BigToHash(head.Difficulty)

	// No overrides: block's own values.
	randao, milli := callJennerProbe(t, api, nil)
	if milli != headMilli {
		t.Fatalf("no override: 0x70 = %d, want head milli %d", milli, headMilli)
	}
	if randao != headRandao {
		t.Fatalf("no override: 0x44 = %x, want difficulty %x", randao, headRandao)
	}

	// time only: milli follows the override, 0x44 untouched.
	overrideTime := hexutil.Uint64(5_000)
	randao, milli = callJennerProbe(t, api, &override.BlockOverrides{Time: &overrideTime})
	if milli != 5_000*1000 {
		t.Fatalf("time override: 0x70 = %d, want %d", milli, uint64(5_000)*1000)
	}
	if randao != headRandao {
		t.Fatalf("time override: 0x44 = %x, want unchanged %x", randao, headRandao)
	}

	// prevRandao only: 0x44 follows the override, milli keeps the block's
	// original value ("omitted fields keep original values").
	prevRandao := common.HexToHash("0xdeadbeef00000000000000000000000000000000000000000000000000001234")
	randao, milli = callJennerProbe(t, api, &override.BlockOverrides{PrevRandao: &prevRandao})
	if randao != prevRandao {
		t.Fatalf("prevRandao override: 0x44 = %x, want %x", randao, prevRandao)
	}
	if milli != headMilli {
		t.Fatalf("prevRandao override: 0x70 = %d, want unchanged head milli %d", milli, headMilli)
	}

	// time + prevRandao together: BOTH take effect, independently.
	randao, milli = callJennerProbe(t, api, &override.BlockOverrides{Time: &overrideTime, PrevRandao: &prevRandao})
	if milli != 5_000*1000 {
		t.Fatalf("combined override: 0x70 = %d, want %d", milli, uint64(5_000)*1000)
	}
	if randao != prevRandao {
		t.Fatalf("combined override: 0x44 = %x, want %x", randao, prevRandao)
	}
}

// TestJennerBlockOverrides_EstimateGas observes the same Apply path through
// eth_estimateGas: the assert contract reverts unless 0x70 returns the
// expected value passed as calldata.
func TestJennerBlockOverrides_EstimateGas(t *testing.T) {
	t.Parallel()
	backend := newJennerBSCBackend(t)
	api := NewBlockChainAPI(backend)
	latest := rpc.LatestBlockNumber
	nrOrHash := rpc.BlockNumberOrHash{BlockNumber: &latest}

	overrideTime := hexutil.Uint64(5_000)
	blockOverrides := override.BlockOverrides{Time: &overrideTime}

	right := hexutil.Bytes(padMilli(5_000 * 1000))
	if _, err := api.EstimateGas(context.Background(), TransactionArgs{To: &jennerAssertAddr, Input: &right}, &nrOrHash, nil, &blockOverrides); err != nil {
		t.Fatalf("estimateGas must succeed when 0x70 == overridden time*1000: %v", err)
	}
	wrong := hexutil.Bytes(padMilli(5_000*1000 + 1))
	if _, err := api.EstimateGas(context.Background(), TransactionArgs{To: &jennerAssertAddr, Input: &wrong}, &nrOrHash, nil, &blockOverrides); err == nil {
		t.Fatalf("estimateGas must fail when the asserted milli value mismatches")
	}
}

// runJennerSimulation executes eth_simulateV1 blocks against a backend and
// returns per-call (0x44 word, 0x70 word) pairs.
func runJennerSimulation(t *testing.T, backend *testBackend, blocks []simBlock) []*simBlockResult {
	t.Helper()
	ctx := context.Background()
	stateDB, baseHeader, err := backend.StateAndHeaderByNumberOrHash(ctx, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))
	if err != nil {
		t.Fatalf("state and header: %v", err)
	}
	sim := &simulator{
		b:           backend,
		state:       stateDB,
		base:        baseHeader,
		chainConfig: backend.ChainConfig(),
		budget:      newGasBudget(0),
	}
	results, err := sim.execute(ctx, blocks)
	if err != nil {
		t.Fatalf("simulateV1 execute: %v", err)
	}
	return results
}

func decodeJennerProbeResult(t *testing.T, res *simBlockResult, call int) (common.Hash, uint64) {
	t.Helper()
	data := []byte(res.Calls[call].ReturnValue)
	if len(data) != 64 {
		t.Fatalf("probe must return 64 bytes, got %d (error: %v)", len(data), res.Calls[call].Error)
	}
	return common.BytesToHash(data[:32]), new(big.Int).SetBytes(data[32:64]).Uint64()
}

// TestJennerBlockOverrides_SimulateV1 covers the eth_simulateV1 path (fresh
// simulated headers, MakeHeader + the MilliTimestamp pin in processBlock):
//   - time-only override across several chained blocks: 0x70 tracks each
//     block's own overridden time without drift;
//   - time + prevRandao: both apply (0x70 == time*1000 AND 0x44 == prevRandao);
//   - prevRandao without time (a real combination: sanitizeChain fills the
//     default timestamp): 0x44 == prevRandao and 0x70 == sanitizedTime*1000.
func TestJennerBlockOverrides_SimulateV1(t *testing.T) {
	t.Parallel()
	backend := newJennerBSCBackend(t)
	base := backend.CurrentHeader()
	probeCall := TransactionArgs{To: &jennerProbeAddr, Gas: newUint64(500_000)}

	var (
		n1 = (*hexutil.Big)(new(big.Int).Add(base.Number, big.NewInt(1)))
		n2 = (*hexutil.Big)(new(big.Int).Add(base.Number, big.NewInt(2)))
		n3 = (*hexutil.Big)(new(big.Int).Add(base.Number, big.NewInt(3)))
		n4 = (*hexutil.Big)(new(big.Int).Add(base.Number, big.NewInt(4)))

		t1         = hexutil.Uint64(base.Time + 100)
		t2         = hexutil.Uint64(base.Time + 200)
		t3         = hexutil.Uint64(base.Time + 300)
		prevRandao = common.HexToHash("0xdeadbeef00000000000000000000000000000000000000000000000000001234")
	)
	results := runJennerSimulation(t, backend, []simBlock{
		{BlockOverrides: &override.BlockOverrides{Number: n1, Time: &t1}, Calls: []TransactionArgs{probeCall}},
		{BlockOverrides: &override.BlockOverrides{Number: n2, Time: &t2}, Calls: []TransactionArgs{probeCall}},
		{BlockOverrides: &override.BlockOverrides{Number: n3, Time: &t3, PrevRandao: &prevRandao}, Calls: []TransactionArgs{probeCall}},
		{BlockOverrides: &override.BlockOverrides{Number: n4, PrevRandao: &prevRandao}, Calls: []TransactionArgs{probeCall}},
	})

	// Blocks 1-2: time-only, chained. 0x70 tracks each block's own time; the
	// simulated headers are post-merge (difficulty 0), so 0x44 reads the
	// (zero) MixDigest.
	for i, wantTime := range []uint64{uint64(t1), uint64(t2)} {
		randao, milli := decodeJennerProbeResult(t, results[i], 0)
		if milli != wantTime*1000 {
			t.Fatalf("block %d: 0x70 = %d, want %d", i+1, milli, wantTime*1000)
		}
		if randao != (common.Hash{}) {
			t.Fatalf("block %d: 0x44 = %x, want zero (no prevRandao override)", i+1, randao)
		}
	}
	// Block 3: time + prevRandao — both take effect.
	randao, milli := decodeJennerProbeResult(t, results[2], 0)
	if milli != uint64(t3)*1000 {
		t.Fatalf("block 3: 0x70 = %d, want %d", milli, uint64(t3)*1000)
	}
	if randao != prevRandao {
		t.Fatalf("block 3: 0x44 = %x, want %x", randao, prevRandao)
	}
	// Block 4: prevRandao without time. sanitizeChain fills the default
	// timestamp (previous block + timestampIncrement); the pin in
	// processBlock must use that sanitized time, not decode garbage
	// milliseconds out of the prevRandao-carrying MixDigest.
	sanitizedTime := results[3].Block.Time()
	if want := uint64(t3) + timestampIncrement; sanitizedTime != want {
		t.Fatalf("block 4: sanitized time = %d, want %d", sanitizedTime, want)
	}
	randao, milli = decodeJennerProbeResult(t, results[3], 0)
	if milli != sanitizedTime*1000 {
		t.Fatalf("block 4: 0x70 = %d, want sanitized time*1000 = %d", milli, sanitizedTime*1000)
	}
	if randao != prevRandao {
		t.Fatalf("block 4: 0x44 = %x, want %x", randao, prevRandao)
	}
}

// TestJennerBlockOverrides_NonBSCRegression pins down that the change leaks
// nothing into non-Parlia chains: PREVRANDAO override behavior is unchanged
// on a merged (post-merge Ethereum) config, and 0x70 stays an empty account
// there (Jenner never activates outside BSC).
func TestJennerBlockOverrides_NonBSCRegression(t *testing.T) {
	t.Parallel()
	acc := newTestAccount()
	gspec := &core.Genesis{
		Config: params.MergedTestChainConfig,
		Alloc: types.GenesisAlloc{
			acc.addr:        {Balance: big.NewInt(params.Ether)},
			jennerProbeAddr: {Code: jennerProbeCode},
		},
	}
	backend := newTestBackend(t, 2, gspec, beacon.New(ethash.NewFaker()), func(i int, b *core.BlockGen) {
		b.SetPoS()
	})
	api := NewBlockChainAPI(backend)
	head := backend.CurrentHeader()

	// eth_call, time override only: PREVRANDAO keeps the block's own value.
	overrideTime := hexutil.Uint64(head.Time + 5_000)
	randao, milli := callJennerProbe(t, api, &override.BlockOverrides{Time: &overrideTime})
	if randao != head.MixDigest {
		t.Fatalf("non-BSC time override: PREVRANDAO = %x, want header value %x", randao, head.MixDigest)
	}
	if milli != 0 {
		t.Fatalf("non-BSC: 0x70 must stay an empty account, probe word = %d", milli)
	}

	// eth_call, time + prevRandao: the prevRandao override stays effective.
	prevRandao := common.HexToHash("0xdeadbeef00000000000000000000000000000000000000000000000000001234")
	randao, milli = callJennerProbe(t, api, &override.BlockOverrides{Time: &overrideTime, PrevRandao: &prevRandao})
	if randao != prevRandao {
		t.Fatalf("non-BSC combined override: PREVRANDAO = %x, want %x", randao, prevRandao)
	}
	if milli != 0 {
		t.Fatalf("non-BSC: 0x70 must stay an empty account, probe word = %d", milli)
	}

	// eth_simulateV1 with time + prevRandao: unchanged behavior as well.
	n1 := (*hexutil.Big)(new(big.Int).Add(head.Number, big.NewInt(1)))
	t1 := hexutil.Uint64(head.Time + 100)
	results := runJennerSimulation(t, backend, []simBlock{
		{BlockOverrides: &override.BlockOverrides{Number: n1, Time: &t1, PrevRandao: &prevRandao},
			Calls: []TransactionArgs{{To: &jennerProbeAddr, Gas: newUint64(500_000)}}},
	})
	randao, milli = decodeJennerProbeResult(t, results[0], 0)
	if randao != prevRandao {
		t.Fatalf("non-BSC simulateV1: PREVRANDAO = %x, want %x", randao, prevRandao)
	}
	if milli != 0 {
		t.Fatalf("non-BSC simulateV1: 0x70 must stay an empty account, probe word = %d", milli)
	}
}
