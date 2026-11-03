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

package rawdb

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestChainFreezerPruneBeforeCancun(t *testing.T) {
	// Other freezer tests replace this package-level table list.
	previous := additionTables
	additionTables = []string{ChainFreezerBlobSidecarTable}
	t.Cleanup(func() { additionTables = previous })

	for _, restart := range []bool{false, true} {
		name := "continue"
		if restart {
			name = "reopen"
		}
		t.Run(name, func(t *testing.T) {
			var cancun uint64 = 110
			env := &ethdb.FreezerEnv{
				ChainCfg: &params.ChainConfig{
					LondonBlock: big.NewInt(0),
					CancunTime:  &cancun,
				},
				BlobExtraReserve: params.DefaultExtraReserveForBlobRequests,
			}
			db := NewMemoryDatabase()
			t.Cleanup(func() { db.Close() })
			for n := uint64(0); n < 125; n++ {
				header := &types.Header{Number: new(big.Int).SetUint64(n), Time: n}
				hash := header.Hash()
				WriteHeader(db, header)
				WriteCanonicalHash(db, hash, n)
				WriteBody(db, hash, n, &types.Body{})
				WriteReceipts(db, hash, n, nil)
				WriteTd(db, hash, n, big.NewInt(1))
				if n >= cancun {
					WriteBlobSidecars(db, hash, n, nil)
				}
			}
			dir := t.TempDir()
			f, err := NewFreezer(dir, "", false, 2048, chainFreezerTableConfigs)
			require.NoError(t, err)
			t.Cleanup(func() {
				if f != nil {
					f.Close()
				}
			})
			cf := &chainFreezer{ancients: f}
			require.NoError(t, cf.SetupFreezerEnv(env, 0))
			freeze := func(first, last uint64) {
				_, err := cf.freezeRangeWithBlobs(&nofreezedb{KeyValueStore: db}, first, last)
				require.NoError(t, err)
			}
			reopen := func() {
				require.NoError(t, f.Close())
				var err error
				f, err = NewFreezer(dir, "", false, 2048, chainFreezerTableConfigs)
				require.NoError(t, err)
				cf.ancients = f
			}
			freeze(0, 99)

			// The live head has passed Cancun, but frozen history has not.
			// Pruning to 70 must not initialize the empty blob table at 70.
			reserve := params.MinBlocksForBlobRequests + env.BlobExtraReserve
			require.True(t, isCancun(env, new(big.Int).SetUint64(reserve+70), reserve+70))
			cf.tryPruneBlobAncientTable(env, reserve+70)
			if restart {
				reopen()
				require.Equal(t, uint64(100), f.head.Load())
			}
			freeze(100, 104)
			require.Zero(t, f.tables[ChainFreezerBlobSidecarTable].items.Load())
			require.Zero(t, f.tables[ChainFreezerBlobSidecarTable].itemHidden.Load())

			// Crossing Cancun must still initialize and append the blob table.
			freeze(105, 119)
			cf.tryPruneBlobAncientTable(env, reserve+115)
			require.Equal(t, uint64(120), f.tables[ChainFreezerBlobSidecarTable].items.Load())
			require.Equal(t, uint64(115), f.tables[ChainFreezerBlobSidecarTable].itemHidden.Load())
			_, err = f.Ancient(ChainFreezerBlobSidecarTable, 114)
			require.Error(t, err)
			blob, err := f.Ancient(ChainFreezerBlobSidecarTable, 115)
			require.NoError(t, err)
			require.Equal(t, []byte{0xc0}, blob)

			// An initialized table with all records pruned must remain appendable.
			_, err = f.TruncateTableTail(ChainFreezerBlobSidecarTable, 120)
			require.NoError(t, err)
			freeze(120, 124)
			reopen()
			require.Equal(t, uint64(125), f.head.Load())
			for n := uint64(0); n < 125; n++ {
				header, err := f.Ancient(ChainFreezerHeaderTable, n)
				require.NoError(t, err)
				require.Equal(t, []byte(ReadHeaderRLP(db, ReadCanonicalHash(db, n), n)), header)
			}
			blob, err = f.Ancient(ChainFreezerBlobSidecarTable, 124)
			require.NoError(t, err)
			require.Equal(t, []byte{0xc0}, blob)
		})
	}
}
