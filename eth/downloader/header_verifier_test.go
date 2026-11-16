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

package downloader

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

// Build signed headers across a validator-set change, without importing bodies.
func parliaHeaderChain(t *testing.T) (*core.BlockChain, ethdb.Database, []*types.Header) {
	t.Helper()
	var keys [2][]*ecdsa.PrivateKey
	var validators [2][]byte
	for set := range keys {
		for range 3 {
			key, err := crypto.GenerateKey()
			if err != nil {
				t.Fatal(err)
			}
			keys[set] = append(keys[set], key)
		}
		sort.Slice(keys[set], func(i, j int) bool {
			a := crypto.PubkeyToAddress(keys[set][i].PublicKey)
			b := crypto.PubkeyToAddress(keys[set][j].PublicKey)
			return bytes.Compare(a[:], b[:]) < 0
		})
		for _, key := range keys[set] {
			validators[set] = append(validators[set], crypto.PubkeyToAddress(key.PublicKey).Bytes()...)
		}
	}
	config := &params.ChainConfig{ChainID: big.NewInt(56), Parlia: &params.ParliaConfig{}}
	genesis := &core.Genesis{
		Config: config, Timestamp: 1, GasLimit: 30_000_000, Difficulty: big.NewInt(1),
		ExtraData: append(append(make([]byte, 32), validators[0]...), make([]byte, 65)...),
	}
	db := rawdb.NewMemoryDatabase()
	engine := parlia.New(config, db, nil, genesis.ToBlock().Hash())
	chain, err := core.NewBlockChain(db, genesis, engine, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		chain.Stop()
		db.Close()
	})
	parent := chain.Genesis().Header()
	var headers []*types.Header
	for number := uint64(1); number <= 405; number++ {
		set := 0
		if number > 201 { // The epoch header's validators activate after the delay.
			set = 1
		}
		key := keys[set][number%3]
		extra := make([]byte, 32)
		if number%200 == 0 {
			extra = append(extra, validators[1]...)
		}
		header := &types.Header{
			ParentHash: parent.Hash(), Number: new(big.Int).SetUint64(number),
			Coinbase: crypto.PubkeyToAddress(key.PublicKey), Difficulty: big.NewInt(2),
			GasLimit: parent.GasLimit, Time: parent.Time + 3,
			UncleHash: types.EmptyUncleHash, TxHash: common.Hash{1}, ReceiptHash: types.EmptyReceiptsHash,
			Extra: append(extra, make([]byte, 65)...),
		}
		hash := types.SealHash(header, config.ChainID)
		sig, err := crypto.Sign(hash[:], key)
		if err != nil {
			t.Fatal(err)
		}
		copy(header.Extra[len(header.Extra)-65:], sig)
		headers = append(headers, header)
		parent = header
	}
	return chain, db, headers
}

func TestHeaderVerifierParlia(t *testing.T) {
	chain, db, headers := parliaHeaderChain(t)
	verifier := newHeaderVerifier(chain)
	// Boundaries straddle the epoch header and the delayed validator-set change.
	for _, bounds := range [][2]int{{0, 199}, {199, 200}, {200, 201}, {201, 300}} {
		if err := verifier.verify(headers[bounds[0]:bounds[1]], nil); err != nil {
			t.Fatal(err)
		}
	}
	if chain.CurrentHeader().Number.Sign() != 0 || chain.HasHeader(headers[299].Hash(), 300) {
		t.Fatal("header verification changed the local chain")
	}
	if pending := verifier.prune(); pending != 300 {
		t.Fatalf("unpersisted headers lost: have %d, want 300", pending)
	}
	// Once the importer stores headers, the verifier may release them.
	for _, header := range headers[:250] {
		rawdb.WriteHeader(db, header)
	}
	if pending := verifier.prune(); pending != 50 || len(verifier.headers) != 50 {
		t.Fatalf("persisted headers retained: pending %d, cached %d", pending, len(verifier.headers))
	}
	if err := verifier.verify(headers[300:], nil); err != nil {
		t.Fatal(err)
	}
	cancel := make(chan struct{})
	close(cancel)
	if err := verifier.verify(headers[:1], cancel); !errors.Is(err, errCanceled) {
		t.Fatalf("cancel error: %v", err)
	}
}

func TestProcessHeadersRejectsInvalidParliaSeal(t *testing.T) {
	chain, _, headers := parliaHeaderChain(t)
	for _, mode := range []ethconfig.SyncMode{ethconfig.FullSync, ethconfig.SnapSync} {
		for _, tc := range []struct{ invalid, prefix int }{{0, 0}, {100, 0}, {199, 0}, {202, 0}, {202, 201}} {
			invalid := tc.invalid
			t.Run(fmt.Sprintf("%s/invalid=%d/prefix=%d", mode, invalid+1, tc.prefix), func(t *testing.T) {
				batch := append([]*types.Header(nil), headers[:invalid+1]...)
				batch[invalid] = types.CopyHeader(batch[invalid])
				batch[invalid].TxHash = common.Hash{2} // No matching validator signature.
				hashes := make([]common.Hash, len(batch))
				for i, header := range batch {
					hashes[i] = header.Hash()
				}
				d := &Downloader{
					blockchain: chain, queue: newQueue(blockCacheMaxItems, blockCacheInitialItems),
					headerProcCh: make(chan *headerTask, 2), cancelCh: make(chan struct{}),
				}
				d.mode.Store(uint32(mode))
				d.queue.Prepare(1, mode)
				if tc.prefix > 0 {
					d.headerProcCh <- &headerTask{headers: batch[:tc.prefix], hashes: hashes[:tc.prefix]}
				}
				d.headerProcCh <- &headerTask{headers: batch[tc.prefix:], hashes: hashes[tc.prefix:]}
				if err := d.processHeaders(1, nil, nil, false); !errors.Is(err, errInvalidChain) {
					t.Fatalf("invalid header error: %v", err)
				}
				if d.queue.PendingBodies() != tc.prefix || d.queue.PendingReceipts() != 0 {
					t.Fatal("unverified batch scheduled content downloads")
				}
			})
		}
	}
}
