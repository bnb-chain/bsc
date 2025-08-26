package ethapi

import (
	"bytes"
	"errors"

	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

const MaxNumberOfEntries = 1000

func TxOptsCheck(o types.TransactionOpts, blockNumber uint64, timeStamp uint64, statedb *state.StateDB) error {
	if o.BlockNumberMin != nil && blockNumber < uint64(*o.BlockNumberMin) {
		return errors.New("BlockNumberMin condition not met")
	}
	if o.BlockNumberMax != nil && blockNumber > uint64(*o.BlockNumberMax) {
		return errors.New("BlockNumberMax condition not met")
	}
	if o.TimestampMin != nil && timeStamp < uint64(*o.TimestampMin) {
		return errors.New("TimestampMin condition not met")
	}
	if o.TimestampMax != nil && timeStamp > uint64(*o.TimestampMax) {
		return errors.New("TimestampMax condition not met")
	}
	counter := 0
	for _, account := range o.KnownAccounts {
		if account.StorageRoot != nil {
			counter += 1
		} else if account.StorageSlots != nil {
			counter += len(account.StorageSlots)
		}
	}
	if counter > MaxNumberOfEntries {
		return errors.New("knownAccounts too large")
	}
	return TxOptsCheckStorage(o, statedb)
}

func TxOptsCheckStorage(o types.TransactionOpts, statedb *state.StateDB) error {
	for address, accountStorage := range o.KnownAccounts {
		if accountStorage.StorageRoot != nil {
			rootHash := statedb.GetStorageRoot(address)
			if rootHash != *accountStorage.StorageRoot {
				return errors.New("storage root hash condition not met")
			}
		} else if len(accountStorage.StorageSlots) > 0 {
			for slot, value := range accountStorage.StorageSlots {
				stored := statedb.GetState(address, slot)
				if !bytes.Equal(stored.Bytes(), value.Bytes()) {
					return errors.New("storage slot value condition not met")
				}
			}
		}
	}
	return nil
}
