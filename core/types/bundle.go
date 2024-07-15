package types

import (
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// MaxBundleAliveBlock is the max alive block for bundle
	MaxBundleAliveBlock = 100
	// MaxBundleAliveTime is the max alive time for bundle
	MaxBundleAliveTime = 5 * 60 // second
)

// SendBundleArgs represents the arguments for a call.
type SendBundleArgs struct {
	Txs               []hexutil.Bytes `json:"txs"`
	MaxBlockNumber    uint64          `json:"maxBlockNumber"`
	MinTimestamp      *uint64         `json:"minTimestamp"`
	MaxTimestamp      *uint64         `json:"maxTimestamp"`
	RevertingTxHashes []common.Hash   `json:"revertingTxHashes"`
}

type Bundle struct {
	Txs               Transactions
	MaxBlockNumber    uint64
	MinTimestamp      uint64
	MaxTimestamp      uint64
	RevertingTxHashes []common.Hash

	Price *big.Int // for bundle compare and prune

	// caches
	hash atomic.Value
	size atomic.Value
}

type SimulatedBundle struct {
	OriginalBundle *Bundle

	BundleGasFees   *big.Int
	BundleGasPrice  *big.Int
	BundleGasUsed   uint64
	EthSentToSystem *big.Int
}

func (bundle *Bundle) Size() uint64 {
	if size := bundle.size.Load(); size != nil {
		return size.(uint64)
	}
	c := writeCounter(0)
	rlp.Encode(&c, bundle)

	size := uint64(c)
	bundle.size.Store(size)
	return size
}

// Hash returns the bundle hash.
func (bundle *Bundle) Hash() common.Hash {
	if hash := bundle.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}

	h := rlpHash(bundle)
	bundle.hash.Store(h)
	return h
}
