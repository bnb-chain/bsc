package miner

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/miner/builderclient"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
	"github.com/ethereum/go-ethereum/rpc"
)

type registryTestChain struct{ header *types.Header }

func (c *registryTestChain) CurrentFinalBlock() *types.Header { return c.header }

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
	chain := &registryTestChain{header: &types.Header{Number: big.NewInt(99), Time: now}}
	config := minerconfig.BuilderRegistryConfig{Enabled: true, ContractAddress: common.HexToAddress("0x1234")}
	var output []byte
	var callErr error
	var calls int
	caller := func(ctx context.Context, block rpc.BlockNumberOrHash, contract common.Address, input []byte, gas uint64) ([]byte, error) {
		if block.BlockHash == nil || *block.BlockHash != chain.header.Hash() || !block.RequireCanonical || block.BlockNumber != nil {
			t.Fatalf("call is not pinned to canonical finalized hash: %+v", block)
		}
		if contract != config.ContractAddress || gas == 0 || gas > 10_000_000 {
			t.Fatal("incorrect contract call")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("registry call has no timeout")
		}
		if !bytes.Equal(input, crypto.Keccak256([]byte("getBuilders()"))[:4]) {
			t.Fatalf("unexpected registry call: %x", input)
		}
		calls++
		return output, callErr
	}
	r := newBuilderRegistry(config, []minerconfig.BuilderConfig{{Address: oldKey, URL: "http://old"}}, chain, caller)
	if !r.allowed(oldKey) || r.allowed(newKey) {
		t.Fatal("initial policy must use static builders")
	}
	for _, invalid := range [][]byte{
		nil, {0}, make([]byte, 64),
		encodeRegistryBuilders(t, registryTestBuilder{}),
		encodeRegistryBuilders(t, registryTestBuilder{Addr: oldKey}, registryTestBuilder{Addr: oldKey}),
	} {
		output = invalid
		if _, err := r.refresh(context.Background()); err == nil || r.snapshot.Load() != nil {
			t.Fatal("invalid output replaced fallback")
		}
	}
	output = encodeRegistryBuilders(t)
	calls = 0
	if result := r.sync(context.Background()); result != "applied" || calls != 1 {
		t.Fatalf("first empty registry sync: result=%s calls=%d", result, calls)
	}
	if r.allowed(oldKey) || r.allowed(unknown) || r.reportURL(oldKey) != "" {
		t.Fatal("first successful empty registry must replace the static builders")
	}
	chain.header = &types.Header{Number: big.NewInt(100), Time: now}
	output = encodeRegistryBuilders(t, registryTestBuilder{Addr: newKey, Url: "http://new", Name: "builder"})
	if result := r.sync(context.Background()); result != "applied" {
		t.Fatalf("non-empty sync: %s", result)
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
	for _, header := range []*types.Header{
		nil,
		{Number: big.NewInt(99), Time: now},
		{Number: big.NewInt(100), Time: now, Extra: []byte{1}},
		{Number: big.NewInt(101), Time: uint64(time.Now().Add(-2 * builderRegistryMaxBlockAge).Unix())},
	} {
		chain.header = header
		if _, err := r.refresh(context.Background()); err == nil || r.snapshot.Load() != previous {
			t.Fatal("missing, regressed, conflicting or stale finalized snapshot accepted")
		}
	}
	chain.header = &types.Header{Number: big.NewInt(101), Time: now}
	output = encodeRegistryBuilders(t)
	if result := r.sync(context.Background()); result != "applied" || r.allowed(oldKey) || r.allowed(newKey) || r.allowed(unknown) || r.reportURL(newKey) != "" {
		t.Fatal("empty registry must clear all builder addresses and reporting URLs")
	}
	if result := r.sync(context.Background()); result != "unchanged" {
		t.Fatalf("repeated empty sync: %s", result)
	}
	previous = r.snapshot.Load()
	callErr = errors.New("unavailable")
	if result := r.sync(context.Background()); result != "error" || r.snapshot.Load() != previous {
		t.Fatal("read failure replaced the empty snapshot")
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

func TestBuilderAdmissionPolicy(t *testing.T) {
	listed, unknown := common.HexToAddress("0x1"), common.HexToAddress("0x2")
	for _, sentry := range []string{"", "http://sentry"} {
		for _, enabled := range []bool{false, true} {
			cfg := &minerconfig.MevConfig{SentryURL: sentry, BuilderRegistry: minerconfig.BuilderRegistryConfig{Enabled: enabled}}
			b := &bidSimulator{config: cfg, builders: map[common.Address]*builderclient.Client{listed: nil}}
			b.registry = newBuilderRegistry(cfg.BuilderRegistry, []minerconfig.BuilderConfig{{Address: listed}}, nil, nil)
			m := &Miner{bidSimulator: b}
			if !m.HasBuilder(listed) || m.HasBuilder(unknown) != (sentry != "") {
				t.Fatalf("wrong startup policy: registry=%v sentry=%q", enabled, sentry)
			}
			if enabled {
				b.registry.snapshot.Store(&builderRegistrySnapshot{builders: map[common.Address]string{unknown: ""}})
				if m.HasBuilder(listed) != (sentry != "") || !m.HasBuilder(unknown) {
					t.Fatalf("wrong policy after registry sync: sentry=%q", sentry)
				}
				b.registry.snapshot.Store(&builderRegistrySnapshot{})
				if m.HasBuilder(listed) != (sentry != "") || m.HasBuilder(unknown) != (sentry != "") {
					t.Fatalf("wrong policy after empty registry sync: sentry=%q", sentry)
				}
				if m.AddBuilder(listed, "") == nil || m.RemoveBuilder(unknown) == nil {
					t.Fatal("admin mutation must not silently override registry ownership")
				}
			} else {
				if sentry == "" {
					if err := m.AddBuilder(unknown, ""); err != nil {
						t.Fatal(err)
					}
					if !m.HasBuilder(unknown) {
						t.Fatal("manually added builder must be allowed")
					}
					if err := m.RemoveBuilder(listed); err != nil {
						t.Fatal(err)
					}
					if m.HasBuilder(listed) {
						t.Fatal("manually removed builder must be rejected")
					}
				}
				b.builders = nil
				if m.HasBuilder(listed) != (sentry != "") || m.HasBuilder(unknown) != (sentry != "") {
					t.Fatalf("empty static list must only allow builders when sentry is configured: sentry=%q", sentry)
				}
			}
		}
	}
}

func TestBuilderRegistryPolling(t *testing.T) {
	retry := 2 * time.Second
	for _, seconds := range []int{2, 4, 8, 16, 32, 60, 60} {
		want := time.Duration(seconds) * time.Second
		if got := registryNextDelay("error", 10*time.Minute, &retry); got != want {
			t.Fatalf("retry = %v, want %v", got, want)
		}
	}
	for _, result := range []string{"applied", "unchanged"} {
		if registryNextDelay(result, 10*time.Minute, &retry) != 10*time.Minute || retry != 2*time.Second {
			t.Fatalf("%s did not resume normal polling and reset backoff", result)
		}
	}
	for _, result := range []string{"error", "applied", "unchanged"} {
		retry = time.Second
		if registryNextDelay(result, time.Second, &retry) != time.Second {
			t.Fatal("delay exceeded PollInterval")
		}
	}
}
