package types

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type SimulateGaslessBundleArgs struct {
	Txs []hexutil.Bytes `json:"txs"`
}

type GaslessTxSimResult struct {
	Hash    common.Hash
	GasUsed uint64
}

type SimulateGaslessBundleResp struct {
	ValidResults     []GaslessTxSimResult
	BasedBlockNumber int64
}
