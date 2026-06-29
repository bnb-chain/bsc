package builder

import (
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	mapset "github.com/deckarep/golang-set/v2"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

const TxDecodeConcurrencyForPerBid = 5

// BidArgs represents the arguments to submit a bid.
type BidArgs struct {
	// RawBid from builder directly
	RawBid *RawBid
	// Signature of the bid from builder
	Signature hexutil.Bytes `json:"signature"`

	// PayBidTx is a payment tx to builder from sentry, which is optional
	PayBidTx        hexutil.Bytes `json:"payBidTx"`
	PayBidTxGasUsed uint64        `json:"payBidTxGasUsed"`
}

func (b *BidArgs) EcrecoverSender() (common.Address, error) {
	pk, err := crypto.SigToPub(b.RawBid.Hash().Bytes(), b.Signature)
	if err != nil {
		return common.Address{}, err
	}

	return crypto.PubkeyToAddress(*pk), nil
}

func (b *BidArgs) ToBid(builder common.Address, signer types.Signer) (*Bid, error) {
	txs, err := b.RawBid.DecodeTxs(signer)
	if err != nil {
		return nil, err
	}

	if len(b.RawBid.UnRevertible) > len(txs) {
		return nil, fmt.Errorf("expect NonRevertible no more than %d", len(txs))
	}
	unRevertibleHashes := mapset.NewThreadUnsafeSetWithSize[common.Hash](len(b.RawBid.UnRevertible))
	unRevertibleHashes.Append(b.RawBid.UnRevertible...)

	if len(b.PayBidTx) != 0 {
		var payBidTx = new(types.Transaction)
		err = payBidTx.UnmarshalBinary(b.PayBidTx)
		if err != nil {
			return nil, err
		}

		txs = append(txs, payBidTx)
	}

	bid := &Bid{
		Builder:      builder,
		BlockNumber:  b.RawBid.BlockNumber,
		ParentHash:   b.RawBid.ParentHash,
		Txs:          txs,
		UnRevertible: unRevertibleHashes,
		GasUsed:      b.RawBid.GasUsed,
		GasFee:       b.RawBid.GasFee,
		BuilderFee:   b.RawBid.BuilderFee,
		rawBid:       *b.RawBid,
	}

	if bid.BuilderFee == nil {
		bid.BuilderFee = big.NewInt(0)
	}

	return bid, nil
}

// RawBid represents a raw bid from builder directly.
type RawBid struct {
	BlockNumber  uint64          `json:"blockNumber"`
	ParentHash   common.Hash     `json:"parentHash"`
	Txs          []hexutil.Bytes `json:"txs"`
	UnRevertible []common.Hash   `json:"unRevertible"`
	GasUsed      uint64          `json:"gasUsed"`
	GasFee       *big.Int        `json:"gasFee"`
	BuilderFee   *big.Int        `json:"builderFee"`

	hash atomic.Value
}

func (b *RawBid) DecodeTxs(signer types.Signer) ([]*types.Transaction, error) {
	if len(b.Txs) == 0 {
		return []*types.Transaction{}, nil
	}

	txChan := make(chan int, len(b.Txs))
	bidTxs := make([]*types.Transaction, len(b.Txs))
	decode := func(txBytes hexutil.Bytes) (*types.Transaction, error) {
		tx := new(types.Transaction)
		err := tx.UnmarshalBinary(txBytes)
		if err != nil {
			return nil, err
		}

		_, err = types.Sender(signer, tx)
		if err != nil {
			return nil, err
		}

		return tx, nil
	}

	errChan := make(chan error, TxDecodeConcurrencyForPerBid)
	for i := 0; i < TxDecodeConcurrencyForPerBid; i++ {
		go func() {
			for {
				txIndex, ok := <-txChan
				if !ok {
					errChan <- nil
					return
				}

				txBytes := b.Txs[txIndex]
				tx, err := decode(txBytes)
				if err != nil {
					errChan <- err
					return
				}

				bidTxs[txIndex] = tx
			}
		}()
	}

	for i := 0; i < len(b.Txs); i++ {
		txChan <- i
	}

	close(txChan)

	for i := 0; i < TxDecodeConcurrencyForPerBid; i++ {
		err := <-errChan
		if err != nil {
			return nil, fmt.Errorf("failed to decode tx, %v", err)
		}
	}

	return bidTxs, nil
}

// Hash returns the hash of the bid.
func (b *RawBid) Hash() common.Hash {
	if hash := b.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}

	h := rlpHash(b)
	b.hash.Store(h)

	return h
}

// Bid represents a bid.
type Bid struct {
	Builder      common.Address
	BlockNumber  uint64
	ParentHash   common.Hash
	Txs          types.Transactions
	UnRevertible mapset.Set[common.Hash]
	GasUsed      uint64
	GasFee       *big.Int
	BuilderFee   *big.Int
	committed    bool // whether the bid has been committed to simulate or not

	rawBid RawBid

	// BlobValResults carries per-tx results of async blob validation (field
	// checks + KZG proof verification), keyed by transaction hash.
	BlobValResults map[common.Hash]chan error
}

func (b *Bid) Commit() {
	b.committed = true
}

func (b *Bid) IsCommitted() bool {
	return b.committed
}

// Hash returns the bid hash.
func (b *Bid) Hash() common.Hash {
	return b.rawBid.Hash()
}

// BidIssue represents a bid issue.
type BidIssue struct {
	Validator common.Address
	Builder   common.Address
	BidHash   common.Hash
	Message   string
}

// BidBlockArgs is the input for the SendBidBlock RPC.
type BidBlockArgs struct {
	BidBlock  *BidBlock
	Signature hexutil.Bytes `json:"signature"`
}

// EcrecoverSender recovers the builder address from the signature over BidBlock.Hash().
func (b *BidBlockArgs) EcrecoverSender() (common.Address, error) {
	pk, err := crypto.SigToPub(b.BidBlock.Hash().Bytes(), b.Signature)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pk), nil
}

// ToDecodedBidBlock converts BidBlockArgs to a decoded internal representation.
// Note: transaction sender recovery is deferred to InsertChain.
func (b *BidBlockArgs) ToDecodedBidBlock(builder common.Address) (*DecodedBidBlock, error) {
	txs, err := b.DecodeTxs()
	if err != nil {
		return nil, err
	}

	sidecars := b.BidBlock.Sidecars
	if sidecars == nil {
		sidecars = types.BlobSidecars{}
	}

	return &DecodedBidBlock{
		Builder:  builder,
		Header:   types.CopyHeader(b.BidBlock.Header),
		Txs:      txs,
		Sidecars: sidecars,
		bidHash:  b.BidBlock.Hash(),
	}, nil
}

// DecodeTxs decodes user txs followed by unsigned system txs.
func (b *BidBlockArgs) DecodeTxs() ([]*types.Transaction, error) {
	txs := make([]*types.Transaction, len(b.BidBlock.Transactions))
	for i, txBytes := range b.BidBlock.Transactions {
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(txBytes); err != nil {
			return nil, fmt.Errorf("failed to decode tx %d: %v", i, err)
		}
		txs[i] = tx
	}
	return txs, nil
}

// BidBlock is the builder-proposed block carried by BidBlockArgs.
type BidBlock struct {
	Header       *types.Header      `json:"header"`
	Transactions []hexutil.Bytes    `json:"transactions"` // user txs first, unsigned system txs last
	Sidecars     types.BlobSidecars `json:"sidecars,omitempty"`

	hash atomic.Value
}

// Hash returns rlpHash over all BidBlock fields. This is what the builder signs.
func (b *BidBlock) Hash() common.Hash {
	if hash := b.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	h := rlpHash(b)
	b.hash.Store(h)
	return h
}

// DecodedBidBlock is the validator-side decoded representation of a BidBlock.
type DecodedBidBlock struct {
	Builder       common.Address // recovered from BidBlockArgs.Signature
	Header        *types.Header
	Txs           types.Transactions
	Sidecars      types.BlobSidecars
	GasFee        *big.Int
	SystemTxStart int // index in Txs where the unsigned trailing system-tx region begins; set during admission.

	bidHash common.Hash
}

// Hash returns the hash of the original BidBlock payload.
func (d *DecodedBidBlock) Hash() common.Hash {
	return d.bidHash
}

// BlockNumber returns the block number from the header.
func (d *DecodedBidBlock) BlockNumber() uint64 {
	return d.Header.Number.Uint64()
}

// ParentHash returns the parent hash from the header.
func (d *DecodedBidBlock) ParentHash() common.Hash {
	return d.Header.ParentHash
}

type MevParams struct {
	ValidatorCommission   uint64 // 100 means 1%
	BidSimulationLeftOver time.Duration
	NoInterruptLeftOver   time.Duration
	MaxBidsPerBuilder     uint32 // Maximum number of bids allowed per builder per block
	GasCeil               uint64
	GasPrice              *big.Int // Minimum avg gas price for bid block
	BuilderFeeCeil        *big.Int
	BidBlockEnabled       bool // whether mev_sendBidBlock is accepted
	Version               string
}

// rlpHash encodes x and returns the keccak256 hash of the encoding. It mirrors
// the unexported helper in package types, replicated here so the bid/bidblock
// hashing stays byte-for-byte identical after moving out of core/types.
func rlpHash(x interface{}) (h common.Hash) {
	sha := crypto.NewKeccakState()
	rlp.Encode(sha, x)
	sha.Read(h[:])
	return h
}
