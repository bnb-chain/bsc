package tracer

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
)

type PoSA interface {
	consensus.Engine

	IsSystemTransaction(tx *types.Transaction, header *types.Header) (bool, error)
	IsSystemContract(to *common.Address) bool
	EnoughDistance(chain consensus.ChainReader, header *types.Header) bool
	IsLocalBlock(header *types.Header) bool
	AllowLightProcess(chain consensus.ChainReader, currentHeader *types.Header) bool
}

var (
	SystemAddress = common.HexToAddress("0xffffFFFfFFffffffffffffffFfFFFfffFFFfFFfE")
)
