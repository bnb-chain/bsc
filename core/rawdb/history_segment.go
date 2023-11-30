package rawdb

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/params"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

var (
	rangeCompactionThreshold = 100000
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
		count     = 0

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
		count++
	}
	WriteTxIndexTail(batch, tail)
	if err := batch.Write(); err != nil {
		return err
	}
	log.Info("PruneTxLookupToTail finish", "count", txlookups.Count(), "size", txlookups.Size(), "elapsed", common.PrettyDuration(time.Since(start)))

	// Start compactions, will remove the deleted data from the disk immediately.
	// Note for small pruning, the compaction is skipped.
	if count >= rangeCompactionThreshold {
		cstart := time.Now()
		for b := 0x00; b <= 0xf0; b += 0x10 {
			var (
				start = []byte{byte(b)}
				end   = []byte{byte(b + 0x10)}
			)
			if b == 0xf0 {
				end = nil
			}
			log.Info("Compacting database", "range", fmt.Sprintf("%#x-%#x", start, end), "elapsed", common.PrettyDuration(time.Since(cstart)))
			if err := db.Compact(start, end); err != nil {
				log.Error("Database compaction failed", "error", err)
				return err
			}
		}
		log.Info("Database compaction finished", "elapsed", common.PrettyDuration(time.Since(cstart)))
	}
	return nil
}

func AvailableHistorySegment(db ethdb.Reader, segments ...*params.HistorySegment) error {
	for _, s := range segments {
		if s == nil {
			return errors.New("found nil segment")
		}
		hash := ReadCanonicalHash(db, s.ReGenesisNumber)
		if hash != s.ReGenesisHash {
			return fmt.Errorf("cannot find segment StartAtBlock, seg: %v", s)
		}
	}
	return nil
}
