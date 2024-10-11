package types

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
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
