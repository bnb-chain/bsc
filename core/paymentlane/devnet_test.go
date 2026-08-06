// Copyright 2026 The go-ethereum Authors
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

// rpcReader is a StorageReader backed by eth_getStorageAt at a fixed block.
//
// It exists so the end-to-end check drives the REAL slot arithmetic - paramSlot,
// paymentContractSlot and the length slot - against a live chain, rather than
// re-deriving the layout in the test and comparing two copies of the same guess.
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

// TestDevnetReadPath drives the production configuration read path against a live
// chain that is past the Gauss activation, and cross-checks every value against the
// contract's own getters over eth_call.
//
// The unit tests pin the layout against the deployed bytecode in a synthetic
// StateDB. This is the other half: it proves the same arithmetic still lands on the
// right slots when the contract was installed by the real fork mechanism, through a
// real trie, on a chain built by three independent validators. A slot-arithmetic
// mistake that a synthetic StateDB happens to tolerate shows up here.
//
// Run it against the node-deploy devnet:
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

	// The lane rules only bind from Gauss+1, so the contract must already have code.
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

		// Eight slot reads and not one more: a per-transaction read pattern would
		// show up here as a much larger number.
		require.Equal(t, numParams, r.reads, "LoadParams must cost exactly one read per parameter")
	})

	t.Run("LoadPaymentContracts agrees with getPaymentContracts", func(t *testing.T) {
		got, err := LoadPaymentContracts(r)
		require.NoError(t, err)

		ret := r.call(selGetPaymentContracts)
		require.GreaterOrEqual(t, len(ret), 2, "getPaymentContracts returns (offset, length, elems...)")
		n := ret[1].Uint64()
		require.Len(t, got, int(n), "the enumerated set size must match the contract's own length")
		for i := uint64(0); i < n; i++ {
			addr := common.BigToAddress(ret[2+i])
			require.Contains(t, got, addr)
		}
		t.Logf("payment-contract whitelist: %d entries", n)
	})

	t.Run("the quota is computable from the live header", func(t *testing.T) {
		p, err := LoadParams(r)
		require.NoError(t, err)

		// Nothing on this chain commits a quota yet - the scaffolding is not wired
		// into block production - so the meaningful assertion is the bootstrap one:
		// a parent carrying no commitment opens the lane at its floor.
		size := LaneSize(p, newSignal(nil, head.GasUsed, head.GasLimit), head.GasLimit)
		floor, ceiling := laneFloor(p, head.GasLimit), laneCeiling(p, head.GasLimit)
		t.Logf("gasLimit %d -> floor %d ceiling %d laneSize %d", head.GasLimit, floor, ceiling, size)

		require.Equal(t, floor, size)
		require.LessOrEqual(t, size, ceiling)
		// The devnet must sit above the safety-clamp boundary, or the lane would be
		// silently switched off on the only multi-node harness available and the
		// end-to-end run would prove nothing about it.
		require.Greater(t, head.GasLimit, uint64(25_000_000),
			"devnet gasLimit %d is below the safety-clamp boundary: raise it or the lane is inert here", head.GasLimit)
	})

	t.Run("the activation boundary is exact", func(t *testing.T) {
		// Needs archive mode; skip rather than fail when history has been pruned.
		if _, err := client.CodeAt(ctx, ContractAddress, common.Big1); err != nil {
			t.Skipf("historical state unavailable (%v); run the devnet with GCMODE=archive", err)
		}
		// Binary search for the first height with code. Searching rather than
		// hardcoding matters: the activation height depends on the fork timestamp and
		// the block rate, and an earlier version of this test clamped its lower bound
		// to genesis and so only proved that block 0 has no code - which is vacuous.
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

		// The parameters are unreadable at the activation block, which is why the
		// rules only bind from the block after it: the code lands in that block's
		// POST-state, so a reader at its parent root sees nothing.
		r := &rpcReader{t: t, c: client, block: new(big.Int).SetUint64(lo)}
		p, err := LoadParams(r)
		require.NoError(t, err, "reading absent storage must not fail")
		require.Equal(t, defaultParams(), p,
			"before activation every slot is zero, so the read yields the defaults - which is exactly why Applies excludes the activation block instead of relying on this")
	})

	t.Run("every node reads the same configuration", func(t *testing.T) {
		if len(endpoints) < 2 {
			t.Skip("pass several comma-separated endpoints to compare nodes")
		}
		// The only property here that a single node cannot establish: three
		// independently built chains must agree on the configuration at the same
		// block, which is what "no node-local input reaches the read" means in
		// practice.
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
		// Recorded, not asserted. The multiply-first rule only diverges from
		// divide-first when GasLimit is not a multiple of RatioDenom, and a devnet
		// settles on its GasCeil, which is round. So this harness structurally cannot
		// catch that bug and TestRoundingIsMultiplyFirst is the only thing that does.
		if head.GasLimit%RatioDenom == 0 {
			t.Logf("gasLimit %d is a multiple of %d: the rounding divergence is invisible on this chain",
				head.GasLimit, RatioDenom)
		}
	})
}
