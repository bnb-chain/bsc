package blobbolt

import (
	"bytes"
	"fmt"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/consensus-types/primitives"
	"github.com/prysmaticlabs/prysm/v4/encoding/bytesutil"
	bolt "go.etcd.io/bbolt"
)

type Storage struct {
	db *bolt.DB
}

func NewStorage(db *bolt.DB) *Storage {
	return &Storage{db: db}
}

var blobsBucket = []byte("blobs")

// SaveBlobSidecar saves the blobs for a given epoch in the sidecar bucket. When we receive a blob:
//
//  1. Convert slot using a modulo operator to [0, maxSlots] where maxSlots = MAX_BLOB_EPOCHS*SLOTS_PER_EPOCH
//
//  2. Compute key for blob as bytes(slot_to_rotating_buffer(blob.slot)) ++ bytes(blob.slot) ++ blob.block_root
//
//  3. Begin the save algorithm:  If the incoming blob has a slot bigger than the saved slot at the spot
//     in the rotating keys buffer, we overwrite all elements for that slot.
func (blob *Storage) SaveBlobSidecar(config *params.ChainConfig, scs []*types.Sidecar) error {
	slot := scs[0].Slot
	return blob.db.Update(func(tx *bolt.Tx) error {
		// TODO: should we use our default encoding? or do we need to use the same encoding
		// as prysm uses (protobuf based) which is defined in the `encode` below
		encodedBlobSidecar, err := rlp.EncodeToBytes(scs)
		if err != nil {
			return errors.Wrap(err, "encoding to bytes")
		}
		bkt := tx.Bucket(blobsBucket)
		c := bkt.Cursor()
		newKey := blobSidecarKey(scs[0], config)
		rotatingBufferPrefix := newKey[0:8]
		var replacingKey []byte
		for k, _ := c.Seek(rotatingBufferPrefix); bytes.HasPrefix(k, rotatingBufferPrefix); k, _ = c.Next() {
			if len(k) != 0 {
				replacingKey = k
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
			if err := bkt.Delete(replacingKey); err != nil {
				fmt.Printf("Could not delete blob with key %#x\n", replacingKey)
			}
		}
		return bkt.Put(newKey, encodedBlobSidecar)
	})
}

func (blob *Storage) GetBlobSidecarsByRoot(root [32]byte) ([]*types.Sidecar, error) {
	var enc []byte
	if err := blob.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(blobsBucket).Cursor()
		// Bucket size is bounded and bolt cursors are fast. Moreover, a thin caching layer can be added.
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if bytes.HasSuffix(k, root[:]) {
				enc = v
				break
			}
		}
		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "calling db view")
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
	sk := slotKey(slot, config)
	if err := blob.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(blobsBucket).Cursor()
		// Bucket size is bounded and bolt cursors are fast. Moreover, a thin caching layer can be added.
		for k, v := c.Seek(sk); bytes.HasPrefix(k, sk); k, _ = c.Next() {
			slotInKey := bytesutil.BytesToSlotBigEndian(k[8:16])
			if slotInKey == slot {
				enc = v
				break
			}
		}
		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "calling db view")
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

func (blob *Storage) DeleteBlobSidecar(beaconBlockRoot [32]byte) error {
	return blob.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(blobsBucket)
		c := bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if bytes.HasSuffix(k, beaconBlockRoot[:]) {
				if err := bkt.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// We define a blob sidecar key as: bytes(slot_to_rotating_buffer(blob.slot)) ++ bytes(blob.slot) ++ blob.block_root
// where slot_to_rotating_buffer(slot) = slot % MAX_SLOTS_TO_PERSIST_BLOBS.
func blobSidecarKey(blob *types.Sidecar, config *params.ChainConfig) []byte {
	key := slotKey(blob.Slot, config)
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
