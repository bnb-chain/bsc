package types

import (
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	mapset "github.com/deckarep/golang-set/v2"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
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

func (b *BidArgs) ToBid(builder common.Address, signer Signer) (*Bid, error) {
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
		var payBidTx = new(Transaction)
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
		GasUsed:      b.RawBid.GasUsed + b.PayBidTxGasUsed,
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

func (b *RawBid) DecodeTxs(signer Signer) ([]*Transaction, error) {
	if len(b.Txs) == 0 {
		return []*Transaction{}, nil
	}

	txChan := make(chan int, len(b.Txs))
	bidTxs := make([]*Transaction, len(b.Txs))
	decode := func(txBytes hexutil.Bytes) (*Transaction, error) {
		tx := new(Transaction)
		err := tx.UnmarshalBinary(txBytes)
		if err != nil {
			return nil, err
		}

		_, err = Sender(signer, tx)
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
	Txs          Transactions
	UnRevertible mapset.Set[common.Hash]
	GasUsed      uint64
	GasFee       *big.Int
	BuilderFee   *big.Int

	rawBid RawBid
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

type MevParams struct {
	ValidatorCommission   uint64 // 100 means 1%
	BidSimulationLeftOver time.Duration
	GasCeil               uint64
	GasPrice              *big.Int // Minimum avg gas price for bid block
	BuilderFeeCeil        *big.Int
	Version               string
}
