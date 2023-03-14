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
	RootHash  *common.Hash
	SlotValue map[common.Hash]common.Hash
}

func (r *AccountStorage) UnmarshalJSON(data []byte) error {
	var hash common.Hash
	if err := json.Unmarshal(data, &hash); err == nil {
		r.RootHash = &hash
		return nil
	}
	return json.Unmarshal(data, &r.SlotValue)
}

func (r AccountStorage) MarshalJSON() ([]byte, error) {
	if r.RootHash != nil {
		return json.Marshal(*r.RootHash)
	}
	return json.Marshal(r.SlotValue)
}

type TransactionOpts struct {
	KnownAccounts  map[common.Address]AccountStorage `json:"knownAccounts"`
	BlockNumberMin *hexutil.Uint64                   `json:"blockNumberMin,omitempty"`
	BlockNumberMax *hexutil.Uint64                   `json:"blockNumberMax,omitempty"`
	TimestampMin   *hexutil.Uint64                   `json:"timestampMin,omitempty"`
	TimestampMax   *hexutil.Uint64                   `json:"timestampMax,omitempty"`
}

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
	if len(o.KnownAccounts) > 1000 {
		return errors.New("knownAccounts too large")
	}
	return o.CheckOnlyStorage(statedb)
}

func (o *TransactionOpts) CheckOnlyStorage(statedb *state.StateDB) error {
	for address, accountStorage := range o.KnownAccounts {
		if accountStorage.RootHash != nil {
			trie := statedb.StorageTrie(address)
			if trie == nil {
				return errors.New("storage trie not found for address key in knownAccounts option")
			}
			if trie.Hash() != *accountStorage.RootHash {
				return errors.New("storage root hash condition not met")
			}
		} else if len(accountStorage.SlotValue) > 0 {
			for slot, value := range accountStorage.SlotValue {
				stored := statedb.GetState(address, slot)
				if !bytes.Equal(stored.Bytes(), value.Bytes()) {
					return errors.New("storage slot value condition not met")
				}
			}
		}
	}
	return nil
}
