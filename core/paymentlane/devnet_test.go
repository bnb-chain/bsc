package paymentlane

import (
	"context"
	"encoding/hex"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
)

// rpcReader backs StorageReader with eth_getStorageAt at a fixed block.
type rpcReader struct {
	t     *testing.T
	c     *ethclient.Client
	block *big.Int
	reads int
}

func (r *rpcReader) Storage(addr common.Address, slot common.Hash) (common.Hash, error) {
	r.reads++
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := r.c.StorageAt(ctx, addr, slot, r.block)
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(b), nil
}

func (r *rpcReader) call(selector string, args ...byte) []*big.Int {
	r.t.Helper()
	input, err := hex.DecodeString(selector)
	require.NoError(r.t, err)
	input = append(input, args...)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ret, err := r.c.CallContract(ctx, ethereum.CallMsg{To: &ContractAddress, Data: input}, r.block)
	require.NoError(r.t, err, "eth_call %s at block %v", selector, r.block)

	out := make([]*big.Int, 0, len(ret)/32)
	for i := 0; i+32 <= len(ret); i += 32 {
		out = append(out, new(big.Int).SetBytes(ret[i:i+32]))
	}
	return out
}

// Cross-check the production read path against a live post-Gauss devnet.
//
// Run with:
//
//	PAYMENTLANE_DEVNET_RPC=http://127.0.0.1:8545 go test ./core/paymentlane/ -run TestDevnet -v
func TestDevnetReadPath(t *testing.T) {
	endpoints := strings.Split(os.Getenv("PAYMENTLANE_DEVNET_RPC"), ",")
	if endpoints[0] == "" {
		t.Skip("set PAYMENTLANE_DEVNET_RPC to one or more comma-separated RPC endpoints of a chain past Gauss")
	}
	endpoint := endpoints[0]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := ethclient.DialContext(ctx, endpoint)
	require.NoError(t, err)
	defer client.Close()

	head, err := client.HeaderByNumber(ctx, nil)
	require.NoError(t, err)
	t.Logf("head #%d gasLimit %d", head.Number, head.GasLimit)

	// The contract must already be installed at head.
	code, err := client.CodeAt(ctx, ContractAddress, head.Number)
	require.NoError(t, err)
	require.NotEmpty(t, code, "no code at %s: the chain has not passed Gauss", ContractAddress)
	t.Logf("PaymentLane code at %s: %d bytes", ContractAddress, len(code))

	r := &rpcReader{t: t, c: client, block: head.Number}

	t.Run("LoadParams agrees with getPaymentLaneParams", func(t *testing.T) {
		got, err := LoadParams(r)
		require.NoError(t, err)
		t.Logf("LoadParams via storage slots: %s", got)

		want := r.call(selGetPaymentLaneParams)
		require.Len(t, want, numParams)
		require.Equal(t, want[0].Uint64(), got.MinRatio)
		require.Equal(t, want[1].Uint64(), got.MaxRatio)
		require.Equal(t, want[2].Uint64(), got.ExpandTrigger)
		require.Equal(t, want[3].Uint64(), got.ShrinkTrigger)
		require.Equal(t, want[4].Uint64(), got.ExpandStep)
		require.Equal(t, want[5].Uint64(), got.ShrinkStep)
		require.Equal(t, want[6].Uint64(), got.MinGas)
		require.Equal(t, want[7].Uint64(), got.MaxGas)

		// LoadParams should read exactly the eight parameter slots.
		require.Equal(t, numParams, r.reads, "LoadParams must cost exactly one read per parameter")
	})

	t.Run("LoadPaymentContracts agrees with getPaymentContracts", func(t *testing.T) {
		got, err := LoadPaymentContracts(r)
		require.NoError(t, err)

		// getPaymentContracts(0, 0) returns the full list.
		ret := r.call(selGetPaymentContracts, make([]byte, 64)...)
		require.GreaterOrEqual(t, len(ret), 3)
		n := ret[1].Uint64()
		require.Equal(t, n, ret[2].Uint64(), "limit 0 must return the whole list in one page")
		require.Len(t, got, int(n), "the enumerated set size must match the contract's own length")
		for i := uint64(0); i < n; i++ {
			addr := common.BigToAddress(ret[3+i])
			require.Contains(t, got, addr)
		}
		t.Logf("payment-contract whitelist: %d entries", n)
	})

	t.Run("the quota is computable from the live header", func(t *testing.T) {
		p, err := LoadParams(r)
		require.NoError(t, err)

		// This devnet does not commit quotas yet, so only the bootstrap case is observable here.
		size := newSignal(nil, head.GasUsed, head.GasLimit).NextLaneSize(p, head.GasLimit)
		floor, ceiling := laneFloor(p, head.GasLimit), laneCeiling(p, head.GasLimit)
		t.Logf("gasLimit %d -> floor %d ceiling %d laneSize %d", head.GasLimit, floor, ceiling, size)

		require.Equal(t, floor, size)
		require.LessOrEqual(t, size, ceiling)
		// Below the safety-clamp boundary this harness would exercise an inert lane.
		require.Greater(t, head.GasLimit, uint64(25_000_000),
			"devnet gasLimit %d is below the safety-clamp boundary: raise it or the lane is inert here", head.GasLimit)
	})

	t.Run("the activation boundary is exact", func(t *testing.T) {
		// Needs archive mode; skip if historical state is gone.
		if _, err := client.CodeAt(ctx, ContractAddress, common.Big1); err != nil {
			t.Skipf("historical state unavailable (%v); run the devnet with GCMODE=archive", err)
		}
		// Search for the first block with code instead of hardcoding a height.
		lo, hi := uint64(0), head.Number.Uint64()
		for lo+1 < hi {
			mid := lo + (hi-lo)/2
			code, err := client.CodeAt(ctx, ContractAddress, new(big.Int).SetUint64(mid))
			require.NoError(t, err)
			if len(code) == 0 {
				lo = mid
			} else {
				hi = mid
			}
		}
		before, err := client.CodeAt(ctx, ContractAddress, new(big.Int).SetUint64(lo))
		require.NoError(t, err)
		after, err := client.CodeAt(ctx, ContractAddress, new(big.Int).SetUint64(hi))
		require.NoError(t, err)
		require.Empty(t, before, "block #%d must have no PaymentLane code", lo)
		require.NotEmpty(t, after, "block #%d must have the PaymentLane code", hi)
		t.Logf("activation boundary: no code at #%d, %d bytes at #%d", lo, len(after), hi)

		// The activation block installs code into post-state only, so its parent root still reads defaults.
		r := &rpcReader{t: t, c: client, block: new(big.Int).SetUint64(lo)}
		p, err := LoadParams(r)
		require.NoError(t, err, "reading absent storage must not fail")
		require.Equal(t, defaultParams(), p,
			"before activation every slot is zero, so the read yields the defaults - which is exactly why the rules exclude the activation block instead of relying on this")
	})

	t.Run("every node reads the same configuration", func(t *testing.T) {
		if len(endpoints) < 2 {
			t.Skip("pass several comma-separated endpoints to compare nodes")
		}
		// Multi-node agreement is the only property a single endpoint cannot show.
		want, err := LoadParams(&rpcReader{t: t, c: client, block: head.Number})
		require.NoError(t, err)
		for _, ep := range endpoints[1:] {
			peer, err := ethclient.DialContext(ctx, ep)
			require.NoError(t, err)
			defer peer.Close()

			hdr, err := peer.HeaderByNumber(ctx, head.Number)
			require.NoError(t, err)
			require.Equal(t, head.Hash(), hdr.Hash(), "%s disagrees on the block hash at #%d", ep, head.Number)
			require.Equal(t, head.GasLimit, hdr.GasLimit)

			got, err := LoadParams(&rpcReader{t: t, c: peer, block: head.Number})
			require.NoError(t, err)
			require.Equal(t, want, got, "%s reads different parameters", ep)

			set, err := LoadPaymentContracts(&rpcReader{t: t, c: peer, block: head.Number})
			require.NoError(t, err)
			require.Empty(t, set)
			t.Logf("%s agrees: %s", ep, got)
		}
	})

	t.Run("the rounding rule is NOT covered here", func(t *testing.T) {
		// Record, don't assert: a round gas limit hides the rounding difference.
		if head.GasLimit%RatioDenom == 0 {
			t.Logf("gasLimit %d is a multiple of %d: the rounding divergence is invisible on this chain",
				head.GasLimit, RatioDenom)
		}
	})
}
