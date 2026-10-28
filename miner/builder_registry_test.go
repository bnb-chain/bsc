package miner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/miner/builderclient"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
	"github.com/ethereum/go-ethereum/rpc"
)

type registryTestChain struct{ header *types.Header }

func (c *registryTestChain) CurrentFinalBlock() *types.Header { return c.header }

type registryTestCaller func(context.Context, rpc.BlockNumberOrHash, common.Address, []byte, uint64) ([]byte, error)

func (f registryTestCaller) CallContract(ctx context.Context, block rpc.BlockNumberOrHash, contract common.Address, input []byte, gas uint64) ([]byte, error) {
	return f(ctx, block, contract, input, gas)
}

type registryTestBuilder struct {
	Addr common.Address
	Url  string
	Name string
}

func encodeRegistryBuilders(t *testing.T, builders ...registryTestBuilder) []byte {
	t.Helper()
	output, err := registryABI.Methods["getBuilders"].Outputs.Pack(builders)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func TestBuilderRegistryTransitions(t *testing.T) {
	oldKey, newKey, unknown := common.HexToAddress("0x1"), common.HexToAddress("0x2"), common.HexToAddress("0x3")
	now := uint64(time.Now().Unix())
	chain := &registryTestChain{header: &types.Header{Number: big.NewInt(100), Time: now}}
	config := minerconfig.BuilderRegistryConfig{Enabled: true, ContractAddress: common.HexToAddress("0x1234")}
	var output []byte
	var callErr error
	caller := registryTestCaller(func(ctx context.Context, block rpc.BlockNumberOrHash, contract common.Address, input []byte, gas uint64) ([]byte, error) {
		if block.BlockHash == nil || *block.BlockHash != chain.header.Hash() || !block.RequireCanonical || block.BlockNumber != nil {
			t.Fatalf("call is not pinned to canonical finalized hash: %+v", block)
		}
		if contract != config.ContractAddress || !bytes.Equal(input, crypto.Keccak256([]byte("getBuilders()"))[:4]) || gas == 0 || gas > 10_000_000 {
			t.Fatal("incorrect contract call")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("registry call has no timeout")
		}
		return output, callErr
	})
	r := newBuilderRegistry(config, []minerconfig.BuilderConfig{{Address: oldKey, URL: "http://old"}}, chain, caller)
	if !r.allowed(oldKey) || r.allowed(newKey) {
		t.Fatal("initial policy must use static builders")
	}
	// An EOA, revert or malformed ABI must not be confused with an empty list.
	for _, invalid := range [][]byte{nil, {0}, make([]byte, 64)} {
		output = invalid
		if _, err := r.refresh(context.Background()); err == nil || r.snapshot.Load() != nil {
			t.Fatal("invalid output replaced fallback")
		}
	}
	output = encodeRegistryBuilders(t, registryTestBuilder{Addr: newKey, Url: "http://new", Name: "builder"})
	if result := r.sync(context.Background()); result != "applied" {
		t.Fatalf("first sync: %s", result)
	}
	if r.allowed(oldKey) || !r.allowed(newKey) || r.allowed(unknown) || r.reportURL(newKey) != "http://new" {
		t.Fatal("registry must replace static membership and URLs")
	}
	if result := r.sync(context.Background()); result != "unchanged" {
		t.Fatalf("unchanged sync: %s", result)
	}
	previous := r.snapshot.Load()
	callErr = errors.New("state pruned")
	if result := r.sync(context.Background()); result != "error" || r.snapshot.Load() != previous {
		t.Fatal("read failure changed policy")
	}
	callErr = nil
	for _, header := range []*types.Header{{Number: big.NewInt(99), Time: now}, {Number: big.NewInt(100), Time: now, Extra: []byte{1}}} {
		chain.header = header
		if _, err := r.refresh(context.Background()); err == nil || r.snapshot.Load() != previous {
			t.Fatal("regressed/conflicting finalized snapshot accepted")
		}
	}
	chain.header = &types.Header{Number: big.NewInt(101), Time: now}
	output = encodeRegistryBuilders(t)
	if result := r.sync(context.Background()); result != "empty" || !r.allowed(unknown) || r.reportURL(newKey) != "http://new" {
		t.Fatal("empty registry must allow all and retain last reporting URLs")
	}
	callErr = errors.New("unavailable")
	if result := r.sync(context.Background()); result != "error" || !r.allowed(unknown) {
		t.Fatal("failure must retain allow-all mode")
	}
	callErr = nil
	chain.header = &types.Header{Number: big.NewInt(102), Time: now}
	output = encodeRegistryBuilders(t, registryTestBuilder{Addr: oldKey, Url: "http://rotated"})
	if result := r.sync(context.Background()); result != "applied" || r.allowed(newKey) || r.allowed(unknown) || !r.allowed(oldKey) {
		t.Fatal("non-empty registry did not restore filtering")
	}
	if r.reportURL(oldKey) != "http://rotated" || r.reportURL(newKey) != "" {
		t.Fatal("stale reporting URLs after rotation")
	}
}

func TestBuilderRegistryEmptyFirstAndInvalidEntries(t *testing.T) {
	key := common.HexToAddress("0x1")
	chain := &registryTestChain{header: &types.Header{Number: big.NewInt(1), Time: uint64(time.Now().Unix())}}
	output := encodeRegistryBuilders(t)
	r := newBuilderRegistry(minerconfig.BuilderRegistryConfig{Enabled: true, ContractAddress: key}, []minerconfig.BuilderConfig{{Address: key, URL: "http://fallback"}}, chain,
		registryTestCaller(func(context.Context, rpc.BlockNumberOrHash, common.Address, []byte, uint64) ([]byte, error) {
			return output, nil
		}))
	if result := r.sync(context.Background()); result != "empty" || !r.allowed(common.Address{}) || r.reportURL(key) != "http://fallback" {
		t.Fatal("initial empty registry policy incorrect")
	}
	previous := r.snapshot.Load()
	for _, entries := range [][]registryTestBuilder{{{}}, {{Addr: key}, {Addr: key}}} {
		output = encodeRegistryBuilders(t, entries...)
		if _, err := r.refresh(context.Background()); err == nil || r.snapshot.Load() != previous {
			t.Fatal("invalid entries changed policy")
		}
	}
	chain.header = nil
	if _, err := r.refresh(context.Background()); err == nil {
		t.Fatal("missing finalized block accepted")
	}
}

func TestBuilderRegistryRejectsStaleFinalizedBlock(t *testing.T) {
	oldKey, newKey, unknown := common.HexToAddress("0x1"), common.HexToAddress("0x2"), common.HexToAddress("0x3")
	chain := &registryTestChain{}
	var output []byte
	var calls int
	r := newBuilderRegistry(minerconfig.BuilderRegistryConfig{Enabled: true, ContractAddress: oldKey},
		[]minerconfig.BuilderConfig{{Address: oldKey}}, chain,
		registryTestCaller(func(context.Context, rpc.BlockNumberOrHash, common.Address, []byte, uint64) ([]byte, error) {
			calls++
			return output, nil
		}))
	for i, tc := range []struct {
		name    string
		stale   bool
		empty   bool
		result  string
		allowed common.Address
		all     bool
	}{
		{name: "startup behind", stale: true, result: "error", allowed: oldKey},
		{name: "caught up", result: "applied", allowed: newKey},
		{name: "old empty cannot open admission", stale: true, empty: true, result: "error", allowed: newKey},
		{name: "fresh empty", empty: true, result: "empty", allowed: newKey, all: true},
		{name: "stale read retains allow-all", stale: true, result: "error", allowed: newKey, all: true},
		{name: "recovered", result: "applied", allowed: newKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			timestamp := time.Now()
			if tc.stale {
				timestamp = timestamp.Add(-2 * builderRegistryMaxBlockAge)
			}
			chain.header = &types.Header{Number: big.NewInt(int64(100 + i)), Time: uint64(timestamp.Unix())}
			output = encodeRegistryBuilders(t, registryTestBuilder{Addr: newKey, Url: "http://new"})
			if tc.empty {
				output = encodeRegistryBuilders(t)
			}
			previous, previousCalls := r.snapshot.Load(), calls
			if result := r.sync(context.Background()); result != tc.result {
				t.Fatalf("sync = %s, want %s", result, tc.result)
			}
			if tc.stale && (calls != previousCalls || r.snapshot.Load() != previous) {
				t.Fatal("stale block caused a contract call or changed the snapshot")
			}
			if !r.allowed(tc.allowed) || r.allowed(unknown) != tc.all {
				t.Fatal("incorrect admission policy")
			}
		})
	}
}

func TestBuilderRegistryPollingAndCancellation(t *testing.T) {
	retry := 2 * time.Second
	for _, seconds := range []int{2, 4, 8, 16, 32, 60, 60} {
		want := time.Duration(seconds) * time.Second
		if got := registryNextDelay("error", 10*time.Minute, &retry); got != want {
			t.Fatalf("retry = %v, want %v", got, want)
		}
	}
	if registryNextDelay("empty", 10*time.Minute, &retry) != 3*time.Second || retry != 2*time.Second {
		t.Fatal("empty polling did not reset backoff")
	}
	if registryNextDelay("applied", 10*time.Minute, &retry) != 10*time.Minute {
		t.Fatal("non-empty polling interval incorrect")
	}
	for _, result := range []string{"error", "empty", "unchanged"} {
		retry = time.Second
		if registryNextDelay(result, time.Second, &retry) != time.Second {
			t.Fatal("delay exceeded PollInterval")
		}
	}
	started := make(chan struct{})
	r := newBuilderRegistry(minerconfig.BuilderRegistryConfig{Enabled: true, ContractAddress: common.HexToAddress("0x1")}, nil,
		&registryTestChain{header: &types.Header{Number: big.NewInt(1), Time: uint64(time.Now().Unix())}},
		registryTestCaller(func(ctx context.Context, _ rpc.BlockNumberOrHash, _ common.Address, _ []byte, _ uint64) ([]byte, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &bidSimulator{exitCh: make(chan struct{}), registryCancel: cancel, registryDone: make(chan struct{})}
	go func() { defer close(b.registryDone); r.run(ctx) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("poller did not read immediately")
	}
	done := make(chan struct{})
	go func() { b.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel in-flight registry call")
	}
	if r.snapshot.Load() != nil {
		t.Fatal("canceled fetch installed a snapshot")
	}
}

func TestBuilderRegistryLifecycle(t *testing.T) {
	w, chainConfig, _, _, _ := laneMinerChain(t, false)
	for _, start := range []bool{false, true} {
		cfg := minerconfig.DefaultMevConfig
		cfg.Registry.Enabled = true
		cfg.Registry.ContractAddress = common.HexToAddress("0x1")
		b := newBidSimulator(&cfg, nil, big.NewInt(0), NewMockBackend(w.chain, nil), chainConfig, w.engine, w)
		// These dependencies may only be read after backend initialization signals
		// StartBuilderRegistry. The race detector verifies this handoff too.
		called := make(chan struct{})
		b.registry.chain = &registryTestChain{header: &types.Header{Number: big.NewInt(1), Time: uint64(time.Now().Unix())}}
		b.registry.caller = registryTestCaller(func(ctx context.Context, _ rpc.BlockNumberOrHash, _ common.Address, _ []byte, _ uint64) ([]byte, error) {
			close(called)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		if start {
			m := &Miner{bidSimulator: b}
			m.StartBuilderRegistry()
			m.StartBuilderRegistry() // Startup is idempotent.
			select {
			case <-called:
			case <-time.After(5 * time.Second):
				t.Fatal("registry did not start with backend")
			}
		}
		done := make(chan struct{})
		go func() { b.close(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("registry did not stop (including before backend startup)")
		}
	}
}

func TestBuilderAdmissionPolicy(t *testing.T) {
	listed, unknown := common.HexToAddress("0x1"), common.HexToAddress("0x2")
	for _, sentry := range []string{"", "http://sentry"} {
		for _, enabled := range []bool{false, true} {
			cfg := &minerconfig.MevConfig{SentryURL: sentry, Registry: minerconfig.BuilderRegistryConfig{Enabled: enabled}}
			b := &bidSimulator{config: cfg, builders: map[common.Address]*builderclient.Client{listed: nil}}
			b.registry = newBuilderRegistry(cfg.Registry, []minerconfig.BuilderConfig{{Address: listed}}, nil, nil)
			m := &Miner{bidSimulator: b}
			if !m.HasBuilder(listed) || m.HasBuilder(unknown) != (!enabled && sentry != "") {
				t.Fatalf("wrong startup policy: registry=%v sentry=%q", enabled, sentry)
			}
			if enabled {
				b.registry.snapshot.Store(&builderRegistrySnapshot{builders: map[common.Address]string{unknown: ""}})
				if m.HasBuilder(listed) || !m.HasBuilder(unknown) {
					t.Fatal("sentry or static list overrode registry")
				}
				if m.AddBuilder(listed, "") == nil || m.RemoveBuilder(unknown) == nil {
					t.Fatal("admin mutation must not silently override registry ownership")
				}
			}
		}
	}
}

func TestSendBidRegistryAdmission(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)
	args := &buildertypes.BidArgs{RawBid: &buildertypes.RawBid{BlockNumber: 100, GasFee: big.NewInt(0), BuilderFee: big.NewInt(0)}}
	args.Signature, err = crypto.Sign(args.RawBid.Hash().Bytes(), key)
	if err != nil {
		t.Fatal(err)
	}
	b := &bidSimulator{
		config:   &minerconfig.MevConfig{Registry: minerconfig.BuilderRegistryConfig{Enabled: true}},
		registry: newBuilderRegistry(minerconfig.BuilderRegistryConfig{}, nil, nil, nil),
		pending:  make(map[uint64]map[common.Address]map[common.Hash]struct{}),
	}
	m := &Miner{bidSimulator: b}
	if _, err := m.SendBid(context.Background(), args); err == nil || !strings.Contains(err.Error(), "not registered") || len(b.pending) != 0 {
		t.Fatalf("unlisted bid must be rejected before quota: %v", err)
	}
	b.registry.snapshot.Store(&builderRegistrySnapshot{builders: map[common.Address]string{addr: ""}})
	// Zero quota deliberately stops at the next gate without a full mining chain.
	if _, err := m.SendBid(context.Background(), args); err == nil || !strings.Contains(err.Error(), "too many bids") {
		t.Fatalf("listed bid must still enforce quota: %v", err)
	}
	args.Signature = nil
	if _, err := m.SendBid(context.Background(), args); err == nil || !strings.Contains(err.Error(), "invalid signature") {
		t.Fatalf("registry must not bypass signature validation: %v", err)
	}
}

func TestSendBidBlockRegistryAdmission(t *testing.T) {
	w, _, _, header, key := laneMinerChain(t, false)
	enabled := true
	w.config = &minerconfig.Config{Mev: minerconfig.MevConfig{Enabled: &enabled, BidBlockEnabled: &enabled, Registry: minerconfig.BuilderRegistryConfig{Enabled: true}}}
	w.permMgr = NewBidBlockPermissionManager("")
	addr := crypto.PubkeyToAddress(key.PublicKey)
	args := &buildertypes.BidBlockArgs{BidBlock: &buildertypes.BidBlock{Header: header}}
	var err error
	args.Signature, err = crypto.Sign(args.BidBlock.Hash().Bytes(), key)
	if err != nil {
		t.Fatal(err)
	}
	b := &bidSimulator{
		config:           &w.config.Mev,
		registry:         newBuilderRegistry(w.config.Mev.Registry, nil, nil, nil),
		pending:          make(map[uint64]map[common.Address]map[common.Hash]struct{}),
		bidBlockBuilders: make(map[common.Address]struct{}),
	}
	m := &Miner{worker: w, bidSimulator: b}
	if _, err := m.SendBidBlock(context.Background(), args); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unlisted BidBlock must be rejected: %v", err)
	}
	b.registry.snapshot.Store(&builderRegistrySnapshot{builders: map[common.Address]string{addr: ""}})
	w.permMgr.SetAllowed(addr, false)
	if _, err := m.SendBidBlock(context.Background(), args); err == nil || !strings.Contains(err.Error(), "permission revoked") {
		t.Fatalf("registry must not bypass BidBlock revocation: %v", err)
	}
	if len(b.pending) != 0 {
		t.Fatal("denied BidBlock consumed quota")
	}
	w.permMgr.SetAllowed(addr, true)
	if _, err := m.SendBidBlock(context.Background(), args); err == nil || !strings.Contains(err.Error(), "empty BidBlock") {
		t.Fatalf("listed key must still pass payload validation: %v", err)
	}
	args.Signature = nil
	if _, err := m.SendBidBlock(context.Background(), args); err == nil || !strings.Contains(err.Error(), "invalid signature") {
		t.Fatalf("registry must not bypass signature validation: %v", err)
	}
}

type registryIssueRecorder struct{ issues chan *buildertypes.BidIssue }

func (r *registryIssueRecorder) ReportIssue(issue *buildertypes.BidIssue) { r.issues <- issue }

func TestRegistryIssueReporting(t *testing.T) {
	recorder := &registryIssueRecorder{issues: make(chan *buildertypes.BidIssue, 4)}
	server := rpc.NewServer()
	if err := server.RegisterName("mev", recorder); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	defer server.Stop()
	key := common.HexToAddress("0x123")
	b := &bidSimulator{config: &minerconfig.MevConfig{SentryURL: httpServer.URL}}
	runtime := &BidRuntime{bid: &buildertypes.Bid{Builder: key}, env: &environment{header: &types.Header{}}}
	counter := metrics.GetOrRegisterCounter(fmt.Sprintf("bid/err/%v", key), nil)
	before := counter.Snapshot().Count()
	// No static key and no pre-dialed client: the sentry still gets the report.
	b.reportIssue(runtime, errors.New("invalid bid"))
	if counter.Snapshot().Count() != before+1 {
		t.Fatal("sentry reporting lost the per-builder error metric")
	}
	select {
	case issue := <-recorder.issues:
		if issue.Builder != key || issue.Message != "invalid bid" {
			t.Fatal("wrong issue")
		}
	default:
		t.Fatal("unknown static key was not reported through sentry")
	}
	b.config.SentryURL = ""
	b.config.Registry.Enabled = true
	b.registry = newBuilderRegistry(b.config.Registry, nil, nil, nil)
	b.registry.snapshot.Store(&builderRegistrySnapshot{builders: map[common.Address]string{key: httpServer.URL}})
	b.reportIssue(runtime, errors.New("registry bid"))
	if counter.Snapshot().Count() != before+2 {
		t.Fatal("registry reporting lost the per-builder error metric")
	}
	select {
	case <-recorder.issues:
	default:
		t.Fatal("registry URL was not used")
	}
	for _, endpoint := range []string{":bad-url", "/tmp/geth.ipc", "builder.example/rpc", "//builder.example/rpc", "ws://127.0.0.1", "wss://127.0.0.1", "file:///tmp/geth.ipc", "stdio:", "http:///rpc", "https://:443/rpc"} {
		b.registry.snapshot.Store(&builderRegistrySnapshot{builders: map[common.Address]string{key: endpoint}})
		cli, release, err := b.issueClient(context.Background(), key)
		if err == nil || !strings.Contains(err.Error(), "must use http or https with a host") || cli != nil || release != nil {
			t.Fatalf("invalid report endpoint %q reached the dialer: %v", endpoint, err)
		}
		if !b.builderAllowed(key) {
			t.Fatal("invalid reporting URL removed admission")
		}
	}
	for _, endpoint := range []string{httpServer.URL, strings.Replace(httpServer.URL, "http://", "https://", 1)} {
		b.registry.snapshot.Store(&builderRegistrySnapshot{builders: map[common.Address]string{key: endpoint}})
		cli, release, err := b.issueClient(context.Background(), key)
		if err != nil || cli == nil || release == nil {
			t.Fatalf("HTTP(S) report endpoint %q rejected: %v", endpoint, err)
		}
		release()
	}
}

func TestBidErrorMetricsBounded(t *testing.T) {
	r := metrics.NewRegistry()
	m := bidErrorMetrics{registry: r}
	for i := 0; i < maxTrackedBidErrorBuilders+100; i++ {
		m.record(common.BigToAddress(big.NewInt(int64(i))))
	}
	if len(m.builders) != maxTrackedBidErrorBuilders || len(r.GetAll()) != maxTrackedBidErrorBuilders+1 {
		t.Fatal("per-builder error metrics are unbounded")
	}
	if count := r.Get("bid/err/overflow").(*metrics.Counter).Snapshot().Count(); count != 100 {
		t.Fatalf("overflow errors = %d, want 100", count)
	}
	// Already tracked builders retain their own counters after the cap is hit.
	key := common.Address{}
	m.record(key)
	if count := r.Get(fmt.Sprintf("bid/err/%v", key)).(*metrics.Counter).Snapshot().Count(); count != 2 {
		t.Fatalf("tracked builder errors = %d, want 2", count)
	}
}

func TestBidErrorMetricsConcurrent(t *testing.T) {
	r := metrics.NewRegistry()
	m := bidErrorMetrics{registry: r}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.record(common.BigToAddress(big.NewInt(int64(j))))
			}
		}()
	}
	wg.Wait()
	if len(m.builders) != 100 || len(r.GetAll()) != 100 {
		t.Fatal("concurrent reports lost per-builder counters")
	}
	r.Each(func(name string, metric interface{}) {
		if count := metric.(*metrics.Counter).Snapshot().Count(); count != 8 {
			t.Errorf("%s = %d, want 8", name, count)
		}
	})
}

func TestBuilderRegistryConcurrentReaders(t *testing.T) {
	key := common.HexToAddress("0x1")
	r := newBuilderRegistry(minerconfig.BuilderRegistryConfig{}, nil, nil, nil)
	b := &bidSimulator{config: &minerconfig.MevConfig{Registry: minerconfig.BuilderRegistryConfig{Enabled: true}}, registry: r}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				b.builderAllowed(key)
				r.reportURL(key)
			}
		}()
	}
	for i := 0; i < 1000; i++ {
		r.snapshot.Store(&builderRegistrySnapshot{allowAll: i%2 == 0, builders: map[common.Address]string{key: "http://builder"}})
	}
	wg.Wait()
}

func TestBidBlockBuilderTrackingBounded(t *testing.T) {
	b := &bidSimulator{bidBlockBuilders: make(map[common.Address]struct{})}
	for i := 0; i < maxTrackedBidBlockBuilders+100; i++ {
		b.recordBidBlockBuilder(common.BigToAddress(big.NewInt(int64(i))))
	}
	if len(b.bidBlockBuilders) != maxTrackedBidBlockBuilders {
		t.Fatalf("builder tracking is unbounded: %d", len(b.bidBlockBuilders))
	}
}
