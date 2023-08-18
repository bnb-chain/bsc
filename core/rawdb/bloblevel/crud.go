package bloblevel

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v4/encoding/bytesutil"
)

var ErrNotFound = errors.New("not found")

// TODO: move this to schema and investigate if the key format is correct
// and does not conflict with others
var blobSidecarPrefix = []byte("sidecarBlob")

type Storage struct {
	db ethdb.Database
}

func NewStorage(db ethdb.Database) *Storage {
	return &Storage{db: db}
}

// SaveBlobSidecar saves the blobs for a given epoch in the sidecar bucket. When we receive a blob:
//
//  1. Convert slot using a modulo operator to [0, maxSlots] where maxSlots = MAX_BLOB_EPOCHS*SLOTS_PER_EPOCH
//
//  2. Compute key for blob as bytes(slot_to_rotating_buffer(blob.slot)) ++ bytes(blob.slot) ++ blob.block_root
//
//  3. Begin the save algorithm:  If the incoming blob has a slot bigger than the saved slot at the spot
//     in the rotating keys buffer, we overwrite all elements for that slot.
func (blob *Storage) SaveBlobSidecar(ctx context.Context, config *params.ChainConfig, scs []*types.Sidecar) error {
	slot := scs[0].Slot
	encodedBlobSidecar, err := rlp.EncodeToBytes(scs)
	if err != nil {
		return errors.Wrap(err, "encoding to bytes")
	}

	newKey := blobSidecarKey(scs[0], config)
	rotatingBufferPrefix := newKey[len(blobSidecarPrefix) : len(blobSidecarPrefix)+8]

	var replacingKey []byte
	it := blob.db.NewIterator(nil, rotatingBufferPrefix)
	defer it.Release()

	for it.Next() {
		key := it.Key()

		if key == nil {
			break
		}

		if len(key) != 0 {
			replacingKey = key
			oldSlotBytes := replacingKey[len(blobSidecarPrefix)+8 : len(blobSidecarPrefix)+16]
			oldSlot := bytesutil.BytesToSlotBigEndian(oldSlotBytes)
			if oldSlot >= slot {
				return errors.Errorf("attempted to save blob with slot %d but already have older blob with slot %d", slot, oldSlot)
			}
			break
		}
	}

	// If there is no element stored at blob.slot % MAX_SLOTS_TO_PERSIST_BLOBS, then we simply
	// store the blob by key and exit early.
	if len(replacingKey) != 0 {
		if err := blob.db.Delete(replacingKey); err != nil {
			fmt.Printf("Could not delete blob with key %#x\n", replacingKey)
		}
	}

	err = blob.db.Put(newKey, encodedBlobSidecar)
	if err != nil {
		return errors.Wrap(err, "saving to db")
	}

	return nil
}

func (blob *Storage) GetBlobSidecarsByRoot(ctx context.Context, root [32]byte) ([]*types.Sidecar, error) {
	var enc []byte

	it := blob.db.NewIterator(nil, blobSidecarPrefix)
	defer it.Release()

	for it.Next() {
		if !bytes.HasPrefix(it.Key(), blobSidecarPrefix) {
			break
		}

		// TODO: searching by suffix can be slow so we may need to use a different method to get blobs by root without
		// iterating over all keys
		if bytes.HasSuffix(it.Key(), root[:]) {
			enc = it.Value()
			break
		}
	}

	if enc == nil {
		return nil, ErrNotFound
	}

	scs := make([]*types.Sidecar, 0)
	if err := rlp.Decode(bytes.NewReader(enc), &scs); err != nil {
		return nil, errors.Wrap(err, "Invalid block body RLP")
	}

	return scs, nil
}

// GetBlobSidecarsBySlot retrieves BlobSidecars for the given slot.
// If the `indices` argument is omitted, all blobs for the root will be returned.
// Otherwise, the result will be filtered to only include the specified indices.
// An error will result if an invalid index is specified.
func (blob *Storage) GetBlobSidecarsBySlot(ctx context.Context, config *params.ChainConfig, slot primitives.Slot) ([]*types.Sidecar, error) {
	var enc []byte

	sk := slotKey(slot, config)
	key := append(blobSidecarPrefix, sk...)

	it := blob.db.NewIterator(nil, key)
	defer it.Release()

	for it.Next() {
		if !bytes.HasPrefix(it.Key(), key) {
			break
		}

		slotInKey := bytesutil.BytesToSlotBigEndian(it.Key()[len(blobSidecarPrefix):len(key)])
		if slotInKey == slot {
			enc = it.Value()
			break
		}
	}

	if enc == nil {
		return nil, ErrNotFound
	}

	scs := make([]*types.Sidecar, 0)
	if err := rlp.Decode(bytes.NewReader(enc), &scs); err != nil {
		return nil, errors.Wrap(err, "Invalid block body RLP")
	}

	return scs, nil
}

func (blob *Storage) DeleteBlobSidecar(ctx context.Context, root [32]byte) error {
	it := blob.db.NewIterator(nil, blobSidecarPrefix)
	defer it.Release()

	for it.Next() {
		key := it.Key()
		if !bytes.HasPrefix(key, blobSidecarPrefix) {
			break
		}

		// TODO: searching by suffix can be slow so we may need to use a different method to get blobs by root without
		// iterating over all keys
		if bytes.HasSuffix(key, root[:]) {
			err := blob.db.Delete(key)
			if err != nil {
				return errors.Wrapf(err, "deleting key: %s from db", key)
			}

			break
		}
	}

	return nil
}

func blobSidecarKey(blob *types.Sidecar, config *params.ChainConfig) []byte {
	key := append(blobSidecarPrefix, slotKey(blob.Slot, config)...)
	key = append(key, bytesutil.Uint64ToBytesBigEndian(uint64(blob.Slot))...)
	key = append(key, blob.BlockRoot[:]...)
	return key
}

func slotKey(slot primitives.Slot, config *params.ChainConfig) []byte {
	slotsPerEpoch := config.DataBlobs.SlotsPerEpoch
	maxEpochsToPersistBlobs := config.DataBlobs.MinEpochsForBlobsSidecarsRequest
	maxSlotsToPersistBlobs := primitives.Slot(maxEpochsToPersistBlobs.Mul(uint64(slotsPerEpoch)))
	return bytesutil.Uint64ToBytesBigEndian(uint64(slot.ModSlot(maxSlotsToPersistBlobs)))
}
