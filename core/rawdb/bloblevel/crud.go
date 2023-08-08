package bloblevel

import (
	"bytes"
	"fmt"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v4/encoding/bytesutil"
)

// TODO: move this to schema and investigate if the key format is correct
// and does not conflict with others
var blobSidecarPrefix = []byte("sidecarBlob")

type Storage struct {
	db ethdb.Database
}

func NewStorage(db ethdb.Database) *Storage {
	return &Storage{db: db}
}

func (blob *Storage) SaveBlobSidecar(config *params.ChainConfig, scs []*types.Sidecar) error {
	slot := scs[0].Slot
	encodedBlobSidecar, err := rlp.EncodeToBytes(scs)
	if err != nil {
		return errors.Wrap(err, "encoding to bytes")
	}

	newKey := blobSidecarKey(scs[0], config)
	rotatingBufferPrefix := newKey[0:8]

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
			oldSlotBytes := replacingKey[8:16]
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

func (blob *Storage) GetBlobSidecarsByRoot(root [32]byte) ([]*types.Sidecar, error) {
	var enc []byte

	start, end := blobSidecarPrefix, blobSidecarPrefix
	it := blob.db.NewIterator(nil, start)
	defer it.Release()

	for it.Next() {
		if bytes.Compare(it.Key(), end) >= 0 {
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
		return nil, errors.New("not found")
	}

	scs := make([]*types.Sidecar, 0)
	if err := rlp.Decode(bytes.NewReader(enc), scs); err != nil {
		return nil, errors.Wrap(err, "Invalid block body RLP")
	}

	return scs, nil
}

func (blob *Storage) GetBlobSidecarsBySlot(config *params.ChainConfig, slot primitives.Slot) ([]*types.Sidecar, error) {
	var enc []byte

	key := append(blobSidecarPrefix, slotKey(slot, config)...)
	start, end := key, key

	it := blob.db.NewIterator(nil, start)
	defer it.Release()

	for it.Next() {
		if bytes.Compare(it.Key(), end) >= 0 {
			break
		}

		slotInKey := bytesutil.BytesToSlotBigEndian(it.Key()[8:16])
		if slotInKey == slot {
			enc = it.Value()
			break
		}
	}

	if enc == nil {
		return nil, errors.New("not found")
	}

	scs := make([]*types.Sidecar, 0)
	if err := rlp.Decode(bytes.NewReader(enc), scs); err != nil {
		return nil, errors.Wrap(err, "Invalid block body RLP")
	}

	return scs, nil
}

func (blob *Storage) DeleteBlobSidecar(root [32]byte) error {
	start, end := blobSidecarPrefix, blobSidecarPrefix
	it := blob.db.NewIterator(nil, start)
	defer it.Release()

	for it.Next() {
		key := it.Key()
		if bytes.Compare(key, end) >= 0 {
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
