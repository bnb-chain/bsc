package eth

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	require.NoError(t, err)
	return output
}

func TestBuilderRegistryTransitions(t *testing.T) {
	oldKey, newKey := common.HexToAddress("0x1"), common.HexToAddress("0x2")
	chain := &registryTestChain{header: &types.Header{Number: big.NewInt(100), Time: uint64(time.Now().Unix())}}
	cfg := minerconfig.BuilderRegistryConfig{Enabled: true, ContractAddress: common.HexToAddress("0x1234")}
	active := map[common.Address]string{oldKey: "http://old"}
	var output []byte
	var callErr error
	r := &builderRegistry{
		config: cfg,
		chain:  chain,
		readRegistry: func(ctx context.Context, block rpc.BlockNumberOrHash, to common.Address, data []byte, gas uint64) ([]byte, error) {
			require.Equal(t, rpc.BlockNumberOrHashWithHash(chain.header.Hash(), true), block)
			require.Equal(t, cfg.ContractAddress, to)
			require.Equal(t, crypto.Keccak256([]byte("getBuilders()"))[:4], data)
			return output, callErr
		},
		apply: func(next map[common.Address]string) { active = next },
	}
	check := func(result string, want map[common.Address]string) {
		t.Helper()
		require.Equal(t, result, r.sync(context.Background()))
		require.Equal(t, want, active)
	}

	// A missing reply preserves the static fallback. An encoded empty list clears it.
	fallback := map[common.Address]string{oldKey: "http://old"}
	check("error", fallback)
	output = make([]byte, 64) // Noncanonical ABI encoding of an empty array.
	check("error", fallback)
	output = encodeRegistryBuilders(t)
	check("applied", map[common.Address]string{})

	chain.header.Number = big.NewInt(101)
	output = encodeRegistryBuilders(t, registryTestBuilder{Addr: newKey, Url: "http://new"})
	rotated := map[common.Address]string{newKey: "http://new"}
	check("applied", rotated)
	callErr = errors.New("state unavailable")
	check("error", rotated)
	callErr = nil

	output = encodeRegistryBuilders(t)
	chain.header.Number = big.NewInt(100)
	check("error", rotated)
	chain.header.Number = big.NewInt(102)
	check("applied", map[common.Address]string{})
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
