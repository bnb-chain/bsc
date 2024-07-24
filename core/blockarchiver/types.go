package blockarchiver

import (
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/core/types"
)

// JsonError represents an error in JSON format
type JsonError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Block represents a block in the Ethereum blockchain
type Block struct {
	WithdrawalsRoot  string        `json:"withdrawalsRoot"`
	Withdrawals      []string      `json:"withdrawals"`
	Hash             string        `json:"hash"`
	ParentHash       string        `json:"parentHash"`
	Sha3Uncles       string        `json:"sha3Uncles"`
	Miner            string        `json:"miner"`
	StateRoot        string        `json:"stateRoot"`
	TransactionsRoot string        `json:"transactionsRoot"`
	ReceiptsRoot     string        `json:"receiptsRoot"`
	LogsBloom        string        `json:"logsBloom"`
	Difficulty       string        `json:"difficulty"`
	Number           string        `json:"number"`
	GasLimit         string        `json:"gasLimit"`
	GasUsed          string        `json:"gasUsed"`
	Timestamp        string        `json:"timestamp"`
	ExtraData        string        `json:"extraData"`
	MixHash          string        `json:"mixHash"`
	Nonce            string        `json:"nonce"`
	Size             string        `json:"size"`
	TotalDifficulty  string        `json:"totalDifficulty"`
	BaseFeePerGas    string        `json:"baseFeePerGas"`
	Transactions     []Transaction `json:"transactions"`
	Uncles           []string      `json:"uncles"`
	BlobGasUsed      string        `json:"blobGasUsed"`
	ExcessBlobGas    string        `json:"excessBlobGas"`
	ParentBeaconRoot string        `json:"parentBeaconBlockRoot"`
}

// GetBlockResponse represents a response from the getBlock RPC call
type GetBlockResponse struct {
	ID      int64      `json:"id,omitempty"`
	Error   *JsonError `json:"error,omitempty"`
	Jsonrpc string     `json:"jsonrpc,omitempty"`
	Result  *Block     `json:"result,omitempty"`
}

// GetBlocksResponse represents a response from the getBlocks RPC call
type GetBlocksResponse struct {
	ID      int64      `json:"id,omitempty"`
	Error   *JsonError `json:"error,omitempty"`
	Jsonrpc string     `json:"jsonrpc,omitempty"`
	Result  []*Block   `json:"result,omitempty"`
}

// GetBundleNameResponse represents a response from the getBundleName RPC call
type GetBundleNameResponse struct {
	Data string `json:"data"`
}

// Transaction represents a transaction in the Ethereum blockchain
type Transaction struct {
	BlockHash            string        `json:"blockHash"`
	BlockNumber          string        `json:"blockNumber"`
	From                 string        `json:"from"`
	Gas                  string        `json:"gas"`
	GasPrice             string        `json:"gasPrice"`
	Hash                 string        `json:"hash"`
	Input                string        `json:"input"`
	Nonce                string        `json:"nonce"`
	To                   string        `json:"to"`
	TransactionIndex     string        `json:"transactionIndex"`
	Value                string        `json:"value"`
	Type                 string        `json:"type"`
	AccessList           []AccessTuple `json:"accessList"`
	ChainId              string        `json:"chainId"`
	V                    string        `json:"v"`
	R                    string        `json:"r"`
	S                    string        `json:"s"`
	YParity              string        `json:"yParity"`
	MaxPriorityFeePerGas string        `json:"maxPriorityFeePerGas"`
	MaxFeePerGas         string        `json:"maxFeePerGas"`
	MaxFeePerDataGas     string        `json:"maxFeePerDataGas"`
	MaxFeePerBlobGas     string        `json:"maxFeePerBlobGas"`
	BlobVersionedHashes  []string      `json:"blobVersionedHashes"`
}

// AccessTuple represents a tuple of an address and a list of storage keys
type AccessTuple struct {
	Address     string
	StorageKeys []string
}

// GeneralBlock represents a block in the Ethereum blockchain
type GeneralBlock struct {
	*types.Block
	TotalDifficulty *big.Int `json:"totalDifficulty"` // Total difficulty in the canonical chain up to and including this block.
}

// Range represents a range of Block numbers
type Range struct {
	from uint64
	to   uint64
	// done is a channel closed when the range is removed
	done chan struct{}
}

// RequestLock is a lock for making sure we don't fetch the same bundle concurrently
type RequestLock struct {
	// TODO
	// there is tradeoff between using a Map or List of ranges, in this case, the lookup needs to be populated every
	// time a new range is added, but the lookup is faster. If we use a list, we need to iterate over the list to check
	// if the number is within any of the ranges, but we don't need to populate the lookup every time a new range is added.
	rangeMap  map[uint64]*Range
	lookupMap map[uint64]*Range
	mu        sync.RWMutex
}

// NewRequestLock creates a new RequestLock
func NewRequestLock() *RequestLock {
	return &RequestLock{
		rangeMap:  make(map[uint64]*Range),
		lookupMap: make(map[uint64]*Range),
	}
}

// IsWithinAnyRange checks if the number is within any of the cached ranges
func (rl *RequestLock) IsWithinAnyRange(num uint64) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	_, exists := rl.lookupMap[num]
	return exists
}

// AddRange adds a new range to the cache
func (rl *RequestLock) AddRange(from, to uint64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	newRange := &Range{
		from: from,
		to:   to,
		done: make(chan struct{}),
	}
	rl.rangeMap[from] = newRange
	// provide fast lookup
	for i := from; i <= to; i++ {
		rl.lookupMap[i] = newRange
	}
}

// RemoveRange removes a range from the cache
func (rl *RequestLock) RemoveRange(from, to uint64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	r, exists := rl.rangeMap[from]
	if !exists {
		return
	}
	delete(rl.rangeMap, from)
	for i := from; i <= to; i++ {
		delete(rl.lookupMap, i)
	}
	close(r.done)
}

func (rl *RequestLock) GetRangeForNumber(number uint64) *Range {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.lookupMap[number]
}

func ParseBundleName(bundleName string) (uint64, uint64, error) {
	parts := strings.Split(bundleName, "_")
	startHeight, err := strconv.ParseUint(parts[1][1:], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	endHeight, err := strconv.ParseUint(parts[2][1:], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return startHeight, endHeight, nil
}
