package rawdb

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/consensus-types/primitives"
	"sort"
)

type BlobSaver interface {
	SaveBlobSidecar(context.Context, *params.ChainConfig, []*types.Sidecar) error
}

type BlobGetter interface {
	GetBlobSidecarsByRoot(context.Context, [32]byte) ([]*types.Sidecar, error)
	GetBlobSidecarsBySlot(context.Context, *params.ChainConfig, primitives.Slot) ([]*types.Sidecar, error)
}

type BlobDeleter interface {
	DeleteBlobSidecar(context.Context, [32]byte) error
}

func SaveBlobSidecar(ctx context.Context, saver BlobSaver, config *params.ChainConfig, scs []*types.Sidecar) error {
	sortSideCars(scs)
	if err := verifySideCars(scs); err != nil {
		return errors.Wrap(err, "verifying sidecars")
	}

	if err := saver.SaveBlobSidecar(ctx, config, scs); err != nil {
		return errors.Wrap(err, "saving blob in db")
	}

	return nil
}

func GetBlobSidecarsByRoot(ctx context.Context, getter BlobGetter, root [32]byte, indices ...uint64) ([]*types.Sidecar, error) {
	scs, err := getter.GetBlobSidecarsByRoot(ctx, root)
	if err != nil {
		return nil, errors.Wrap(err, "getting blob sidecar by root from db")
	}

	scs, err = filterForIndices(scs, indices...)
	if err != nil {
		return nil, errors.Wrap(err, "filtering for indices")
	}

	return scs, nil
}

func GetBlobSidecarsBySlot(ctx context.Context, getter BlobGetter, config *params.ChainConfig, slot primitives.Slot, indices ...uint64) ([]*types.Sidecar, error) {
	scs, err := getter.GetBlobSidecarsBySlot(ctx, config, slot)
	if err != nil {
		return nil, errors.Wrap(err, "getting blob sidecar by slot from db")
	}

	scs, err = filterForIndices(scs, indices...)
	if err != nil {
		return nil, errors.Wrap(err, "filtering for indices")
	}

	return scs, nil
}

func DeleteBlobSidecar(ctx context.Context, deleter BlobDeleter, root [32]byte) error {
	if err := deleter.DeleteBlobSidecar(ctx, root); err != nil {
		return errors.Wrap(err, "deleting blob sidecar from db")
	}

	return nil
}

func filterForIndices(scs []*types.Sidecar, indices ...uint64) ([]*types.Sidecar, error) {
	if len(indices) == 0 {
		return scs, nil
	}
	// This loop assumes that the BlobSidecars value stores the complete set of blobs for a block
	// in ascending order from eg 0..3, without gaps. This allows us to assume the indices argument
	// maps 1:1 with indices in the BlobSidecars storage object.
	maxIdx := uint64(len(scs)) - 1
	sidecars := make([]*types.Sidecar, len(indices))
	for i, idx := range indices {
		if idx > maxIdx {
			return nil, errors.Errorf("BlobSidecars with index: %d not found", idx)
		}
		sidecars[i] = scs[idx]
	}
	return sidecars, nil
}

// verifySideCars ensures that all sidecars have the same slot, parent root, block root, and proposer index.
// It also ensures that indices are sequential and start at 0 and no more than MAX_BLOB_EPOCHS.
func verifySideCars(scs []*types.Sidecar) error {
	if len(scs) == 0 {
		return errors.New("nil or empty blob sidecars")
	}
	if uint64(len(scs)) > params.MaxBlobsPerBlock {
		return fmt.Errorf("too many sidecars: %d > %d", len(scs), params.MaxBlobsPerBlock)
	}

	sl := scs[0].Slot
	pr := scs[0].BlockParentRoot
	r := scs[0].BlockRoot
	p := scs[0].ProposerIndex

	for i, sc := range scs {
		if sc.Slot != sl {
			return fmt.Errorf("sidecar slot mismatch: %d != %d", sc.Slot, sl)
		}
		// TODO: converting fixed size byte array to slice, may need to revert it
		if !bytes.Equal(sc.BlockParentRoot[:], pr[:]) {
			return fmt.Errorf("sidecar parent root mismatch: %x != %x", sc.BlockParentRoot, pr)
		}
		if !bytes.Equal(sc.BlockRoot[:], r[:]) {
			return fmt.Errorf("sidecar root mismatch: %x != %x", sc.BlockRoot, r)
		}
		if sc.ProposerIndex != p {
			return fmt.Errorf("sidecar proposer index mismatch: %d != %d", sc.ProposerIndex, p)
		}
		if sc.Index != uint64(i) {
			return fmt.Errorf("sidecar index mismatch: %d != %d", sc.Index, i)
		}
	}
	return nil
}

// sortSideCars sorts the sidecars by their index.
func sortSideCars(scs []*types.Sidecar) {
	sort.Slice(scs, func(i, j int) bool {
		return scs[i].Index < scs[j].Index
	})
}
