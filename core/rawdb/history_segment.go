package rawdb

import (
	"bytes"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/params"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

// PruneTxLookupToTail it will iterator tx look up and delete to tail
func PruneTxLookupToTail(db ethdb.KeyValueStore, tail uint64) error {
	indexTail := ReadTxIndexTail(db)
	if tail == 0 || tail <= *indexTail {
		return nil
	}

	var (
		start     = time.Now()
		logged    = time.Now()
		txlookups stat

		batch = db.NewBatch()
		iter  = db.NewIterator(txLookupPrefix, nil)
	)

	for iter.Next() {
		key := iter.Key()
		val := iter.Value()
		if !bytes.HasPrefix(key, txLookupPrefix) || len(key) != (len(txLookupPrefix)+common.HashLength) {
			continue
		}
		txlookups.Add(common.StorageSize(len(key) + len(val)))
		// check if need delete current tx index
		number := new(big.Int).SetBytes(val).Uint64()
		if number >= tail {
			continue
		}

		batch.Delete(key)
		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
		if time.Since(logged) > 30*time.Second {
			log.Info("PruneTxLookupToTail", "count", txlookups.Count(), "size", txlookups.Size(), "elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
	}
	WriteTxIndexTail(batch, tail)
	if err := batch.Write(); err != nil {
		return err
	}
	log.Info("PruneTxLookupToTail finish", "count", txlookups.Count(), "size", txlookups.Size(), "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

func AvailableHistorySegment(db ethdb.Reader, segments ...params.HisSegment) error {
	for _, s := range segments {
		hash := ReadCanonicalHash(db, s.StartAtBlock.Number)
		if hash != s.StartAtBlock.Hash {
			return fmt.Errorf("cannot find segment StartAtBlock, seg: %v", s)
		}
		hash = ReadCanonicalHash(db, s.FinalityAtBlock.Number)
		if hash != s.FinalityAtBlock.Hash {
			return fmt.Errorf("cannot find segment FinalityAtBlock, seg: %v", s)
		}
	}
	return nil
}
