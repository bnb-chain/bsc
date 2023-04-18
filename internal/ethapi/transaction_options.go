package ethapi

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
)

type AccountStorage struct {
	StorageRoot  *common.Hash
	StorageSlots map[common.Hash]common.Hash
}

func (a *AccountStorage) UnmarshalJSON(data []byte) error {
	var hash common.Hash
	if err := json.Unmarshal(data, &hash); err == nil {
		a.StorageRoot = &hash
		return nil
	}
	return json.Unmarshal(data, &a.StorageSlots)
}

func (a AccountStorage) MarshalJSON() ([]byte, error) {
	if a.StorageRoot != nil {
		return json.Marshal(*a.StorageRoot)
	}
	return json.Marshal(a.StorageSlots)
}

type KnownAccounts map[common.Address]AccountStorage

// It is known that marshaling is broken
// https://github.com/golang/go/issues/55890

//go:generate go run github.com/fjl/gencodec -type TransactionOpts -out gen_tx_opts_json.go
type TransactionOpts struct {
	KnownAccounts  KnownAccounts   `json:"knownAccounts"`
	BlockNumberMin *hexutil.Uint64 `json:"blockNumberMin,omitempty"`
	BlockNumberMax *hexutil.Uint64 `json:"blockNumberMax,omitempty"`
	TimestampMin   *hexutil.Uint64 `json:"timestampMin,omitempty"`
	TimestampMax   *hexutil.Uint64 `json:"timestampMax,omitempty"`
}

const MaxNumberOfEntries = 1000

func (o *TransactionOpts) Check(blockNumber uint64, timeStamp uint64, statedb *state.StateDB) error {
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
	return o.CheckStorage(statedb)
}

func (o *TransactionOpts) CheckStorage(statedb *state.StateDB) error {
	for address, accountStorage := range o.KnownAccounts {
		if accountStorage.StorageRoot != nil {
			rootHash := statedb.GetRoot(address)
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
