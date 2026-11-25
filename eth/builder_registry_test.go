package eth

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
	active := map[common.Address]string{oldKey: "http://old"}
	applied := 0
	r := &builderRegistry{config: config, chain: chain, readRegistry: caller, apply: func(next map[common.Address]string) {
		active = next
		applied++
	}}
	allowed := func(addr common.Address) bool {
		_, ok := active[addr]
		return ok
	}
	for _, invalid := range [][]byte{
		nil, {0}, make([]byte, 64),
		encodeRegistryBuilders(t, registryTestBuilder{}),
		encodeRegistryBuilders(t, registryTestBuilder{Addr: oldKey}, registryTestBuilder{Addr: oldKey}),
	} {
		output = invalid
		if _, err := r.refresh(context.Background()); err == nil || r.last != nil || applied != 0 {
			t.Fatal("invalid output replaced fallback")
		}
	}
	output = encodeRegistryBuilders(t)
	calls = 0
	if result := r.sync(context.Background()); result != "applied" || calls != 1 {
		t.Fatalf("first empty registry sync: result=%s calls=%d", result, calls)
	}
	if allowed(oldKey) || allowed(unknown) || active[oldKey] != "" {
		t.Fatal("first successful empty registry must replace the static builders")
	}
	chain.header = &types.Header{Number: big.NewInt(100), Time: now}
	output = encodeRegistryBuilders(t, registryTestBuilder{Addr: newKey, Url: "http://new", Name: "builder"})
	if result := r.sync(context.Background()); result != "applied" {
		t.Fatalf("non-empty sync: %s", result)
	}
	if allowed(oldKey) || !allowed(newKey) || allowed(unknown) || active[newKey] != "http://new" {
		t.Fatal("registry must replace static membership and URLs")
	}
	if result := r.sync(context.Background()); result != "unchanged" {
		t.Fatalf("unchanged sync: %s", result)
	}
	previous := r.last
	callErr = errors.New("state pruned")
	if result := r.sync(context.Background()); result != "error" || r.last != previous || applied != 2 {
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
		if _, err := r.refresh(context.Background()); err == nil || r.last != previous || applied != 2 {
			t.Fatal("missing, regressed, conflicting or stale finalized snapshot accepted")
		}
	}
	chain.header = &types.Header{Number: big.NewInt(101), Time: now}
	output = encodeRegistryBuilders(t)
	if result := r.sync(context.Background()); result != "applied" || allowed(oldKey) || allowed(newKey) || allowed(unknown) || active[newKey] != "" {
		t.Fatal("empty registry must clear all builder addresses and reporting URLs")
	}
	if result := r.sync(context.Background()); result != "unchanged" || applied != 3 {
		t.Fatalf("repeated empty sync: %s", result)
	}
	previous = r.last
	callErr = errors.New("unavailable")
	if result := r.sync(context.Background()); result != "error" || r.last != previous {
		t.Fatal("read failure replaced the empty snapshot")
	}
	callErr = nil
	chain.header = &types.Header{Number: big.NewInt(102), Time: now}
	output = encodeRegistryBuilders(t, registryTestBuilder{Addr: oldKey, Url: "http://rotated"})
	if result := r.sync(context.Background()); result != "applied" || allowed(newKey) || allowed(unknown) || !allowed(oldKey) {
		t.Fatal("non-empty registry did not restore filtering")
	}
	if active[oldKey] != "http://rotated" || active[newKey] != "" {
		t.Fatal("stale reporting URLs after rotation")
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

func TestBuilderRegistryStopWaitsForRead(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	r := &builderRegistry{
		config: minerconfig.BuilderRegistryConfig{Enabled: true, ContractAddress: common.HexToAddress("0x1234")},
		chain:  &registryTestChain{header: &types.Header{Number: big.NewInt(1), Time: uint64(time.Now().Unix())}},
		readRegistry: func(ctx context.Context, _ rpc.BlockNumberOrHash, _ common.Address, _ []byte, _ uint64) ([]byte, error) {
			close(started)
			<-ctx.Done()
			close(finished)
			return nil, ctx.Err()
		},
		apply: func(map[common.Address]string) { t.Error("cancelled read published a list") },
	}
	r.start()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		r.stop()
		t.Fatal("registry did not start its first read")
	}
	r.stop()
	select {
	case <-finished:
	default:
		t.Fatal("stop returned before the registry read finished")
	}
}
