package rawdb_test

import (
	"context"
	"crypto/rand"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/rawdb/bloblevel"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/beacon-chain/db/kv"
	"github.com/prysmaticlabs/prysm/v4/encoding/bytesutil"
	"github.com/stretchr/testify/require"
	"testing"
)

// setupDB instantiates and returns a Store instance.
func setupDB(t testing.TB) *kv.Store {
	db, err := kv.NewKVStore(context.Background(), t.TempDir())
	require.NoError(t, err, "Failed to instantiate DB")
	t.Cleanup(func() {
		require.NoError(t, db.Close(), "Failed to close database")
	})
	return db
}

func equalBlobSlices(expect []*types.Sidecar, got []*types.Sidecar) error {
	if len(expect) != len(got) {
		return errors.Errorf("mismatched lengths, expect=%d, got=%d", len(expect), len(got))
	}

	if diff := cmp.Diff(got, expect); diff != "" {
		return errors.Errorf(diff)
	}

	return nil
}

func TestBlobLevelDbStorage(t *testing.T) {
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, 0)
		require.ErrorContains(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs), "nil or empty blob sidecars")
	})
	t.Run("empty by root", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		got, err := rawdb.GetBlobSidecarsByRoot(ctx, storage, [32]byte{})
		require.ErrorContains(t, err, "not found")
		require.Equal(t, 0, len(got))
	})
	t.Run("empty by slot", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		got, err := rawdb.GetBlobSidecarsBySlot(ctx, storage, params.TestChainConfig, 1)
		require.ErrorContains(t, err, "not found")
		require.Equal(t, 0, len(got))
	})
	t.Run("save and retrieve by root (one)", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, 1)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, 1, len(scs))
		got, err := rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(scs[0].BlockRoot))
		require.NoError(t, err)
		require.NoError(t, equalBlobSlices(scs, got))
	})
	t.Run("save and retrieve by root (max)", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, params.MaxBlobsPerBlock, len(scs))
		got, err := rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(scs[0].BlockRoot))
		require.NoError(t, err)
		require.NoError(t, equalBlobSlices(scs, got))
	})
	t.Run("save and retrieve valid subset by root", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, params.MaxBlobsPerBlock, len(scs))

		// we'll request indices 0 and 3, so make a slice with those indices for comparison
		expect := make([]*types.Sidecar, 2)
		expect[0] = scs[0]
		expect[1] = scs[3]

		got, err := rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(scs[0].BlockRoot), 0, 3)
		require.NoError(t, err)
		require.NoError(t, equalBlobSlices(expect, got))
		require.Equal(t, uint64(0), got[0].Index)
		require.Equal(t, uint64(3), got[1].Index)
	})
	t.Run("error for invalid index when retrieving by root", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, params.MaxBlobsPerBlock, len(scs))

		got, err := rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(scs[0].BlockRoot), uint64(len(scs)))
		require.ErrorContains(t, err, "not found")
		require.Equal(t, 0, len(got))
	})
	t.Run("save and retrieve by slot (one)", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, 1)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, 1, len(scs))
		got, err := rawdb.GetBlobSidecarsBySlot(ctx, storage, params.TestChainConfig, scs[0].Slot)
		require.NoError(t, err)
		require.NoError(t, equalBlobSlices(scs, got))
	})
	t.Run("save and retrieve by slot (max)", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, params.MaxBlobsPerBlock, len(scs))
		got, err := rawdb.GetBlobSidecarsBySlot(ctx, storage, params.TestChainConfig, scs[0].Slot)
		require.NoError(t, err)
		require.NoError(t, equalBlobSlices(scs, got))
	})
	t.Run("save and retrieve valid subset by slot", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, params.MaxBlobsPerBlock, len(scs))

		// we'll request indices 0 and 3, so make a slice with those indices for comparison
		expect := make([]*types.Sidecar, 2)
		expect[0] = scs[0]
		expect[1] = scs[3]

		got, err := rawdb.GetBlobSidecarsBySlot(ctx, storage, params.TestChainConfig, scs[0].Slot, 0, 3)
		require.NoError(t, err)
		require.NoError(t, equalBlobSlices(expect, got))

		require.Equal(t, uint64(0), got[0].Index)
		require.Equal(t, uint64(3), got[1].Index)
	})
	t.Run("error for invalid index when retrieving by slot", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, params.MaxBlobsPerBlock, len(scs))

		got, err := rawdb.GetBlobSidecarsBySlot(ctx, storage, params.TestChainConfig, scs[0].Slot, uint64(len(scs)))
		require.ErrorContains(t, err, "not found")
		require.Equal(t, 0, len(got))
	})
	t.Run("delete works", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, params.MaxBlobsPerBlock, len(scs))
		got, err := rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(scs[0].BlockRoot))
		require.NoError(t, err)
		require.NoError(t, equalBlobSlices(scs, got))
		require.NoError(t, rawdb.DeleteBlobSidecar(ctx, storage, bytesutil.ToBytes32(scs[0].BlockRoot)))
		got, err = rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(scs[0].BlockRoot))
		require.ErrorContains(t, err, "not found")
		require.Equal(t, 0, len(got))
	})
	t.Run("saving a blob with older slot", func(t *testing.T) {
		storage := bloblevel.NewStorage(rawdb.NewMemoryDatabase())
		scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
		require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
		require.Equal(t, params.MaxBlobsPerBlock, len(scs))
		got, err := rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(scs[0].BlockRoot))
		require.NoError(t, err)
		require.NoError(t, equalBlobSlices(scs, got))
		require.ErrorContains(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs), "but already have older blob with slot")
	})

	//t.Run("saving a new blob for rotation", func(t *testing.T) {
	//	ctx := context.Background()
	//	db := rawdb.NewMemoryDatabase()
	//
	//	storage := bloblevel.NewStorage(db)
	//	scs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
	//	require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, scs))
	//	require.Equal(t, params.MaxBlobsPerBlock, len(scs))
	//	oldBlockRoot := scs[0].BlockRoot
	//	got, err := rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(oldBlockRoot))
	//	require.NoError(t, err)
	//	require.NoError(t, equalBlobSlices(scs, got))
	//
	//	newScs := generateBlobSidecars(t, params.MaxBlobsPerBlock)
	//	newRetentionSlot := primitives.Slot(params.TestChainConfig.DataBlobs.MinEpochsForBlobsSidecarsRequest.Mul(uint64(params.TestChainConfig.DataBlobs.SlotsPerEpoch)))
	//	for _, sc := range newScs {
	//		sc.Slot = sc.Slot + newRetentionSlot
	//	}
	//	require.NoError(t, rawdb.SaveBlobSidecar(ctx, storage, params.TestChainConfig, newScs))
	//
	//	_, err = rawdb.GetBlobSidecarsBySlot(ctx, storage, params.TestChainConfig, 100)
	//	require.ErrorIs(t, bloblevel.ErrNotFound, err)
	//
	//	got, err = rawdb.GetBlobSidecarsByRoot(ctx, storage, bytesutil.ToBytes32(newScs[0].BlockRoot))
	//	require.NoError(t, err)
	//	require.NoError(t, equalBlobSlices(newScs, got))
	//})
}

func generateBlobSidecars(t *testing.T, n uint64) []*types.Sidecar {
	blobSidecars := make([]*types.Sidecar, n)
	for i := uint64(0); i < n; i++ {
		blobSidecars[i] = generateBlobSidecar(t, i)
	}
	return blobSidecars
}

func generateBlobSidecar(t *testing.T, index uint64) *types.Sidecar {
	blob := make([]byte, 131072)
	_, err := rand.Read(blob)
	require.NoError(t, err)
	kzgCommitment := make([]byte, 48)
	_, err = rand.Read(kzgCommitment)
	require.NoError(t, err)
	kzgProof := make([]byte, 48)
	_, err = rand.Read(kzgProof)
	require.NoError(t, err)

	var arrblob types.Blob
	copy(arrblob[:], blob)

	var arrcommit types.KZGCommitment
	copy(arrcommit[:], kzgCommitment)

	var arrproof types.KZGProof
	copy(arrproof[:], kzgProof)

	return &types.Sidecar{
		BlockRoot:       PadTo([]byte{'a'}, 32),
		Index:           index,
		Slot:            100,
		BlockParentRoot: PadTo([]byte{'b'}, 32),
		ProposerIndex:   101,
		Blob:            arrblob,
		KZGCommitment:   arrcommit,
		KZGProof:        arrproof,
	}
}

// PadTo pads a byte slice to the given size. If the byte slice is larger than the given size, the
// original slice is returned.
func PadTo(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	return append(b, make([]byte, size-len(b))...)
}
