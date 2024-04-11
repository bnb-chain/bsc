package ethapi

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
)

const (
	// MaxBundleAliveBlock is the max alive block for bundle
	MaxBundleAliveBlock = 100
	// MaxBundleAliveTime is the max alive time for bundle
	MaxBundleAliveTime = 5 * 60 // second
	MaxOracleBlocks    = 21
	DropBlocks         = 3

	InvalidBundleParamError = -38000
)

// PrivateTxBundleAPI offers an API for accepting bundled transactions
type PrivateTxBundleAPI struct {
	b Backend
}

// NewPrivateTxBundleAPI creates a new Tx Bundle API instance.
func NewPrivateTxBundleAPI(b Backend) *PrivateTxBundleAPI {
	return &PrivateTxBundleAPI{b}
}

func (s *PrivateTxBundleAPI) BundlePrice(ctx context.Context) *big.Int {
	return s.b.BundlePrice()
}

// SendBundle will add the signed transaction to the transaction pool.
// The sender is responsible for signing the transaction and using the correct nonce and ensuring validity
func (s *PrivateTxBundleAPI) SendBundle(ctx context.Context, args types.SendBundleArgs) error {
	if len(args.Txs) == 0 {
		return newBundleError(errors.New("bundle missing txs"))
	}

	currentHeader := s.b.CurrentHeader()

	if args.MaxBlockNumber == 0 && (args.MaxTimestamp == nil || *args.MaxTimestamp == 0) {
		maxTimeStamp := currentHeader.Time + MaxBundleAliveTime
		args.MaxTimestamp = &maxTimeStamp
	}

	if args.MaxBlockNumber != 0 && args.MaxBlockNumber > currentHeader.Number.Uint64()+MaxBundleAliveBlock {
		return newBundleError(errors.New("the maxBlockNumber should not be lager than currentBlockNum + 100"))
	}

	if args.MaxTimestamp != nil && args.MinTimestamp != nil && *args.MaxTimestamp != 0 && *args.MinTimestamp != 0 {
		if *args.MaxTimestamp <= *args.MinTimestamp {
			return newBundleError(errors.New("the maxTimestamp should not be less than minTimestamp"))
		}
	}

	if args.MaxTimestamp != nil && *args.MaxTimestamp != 0 && *args.MaxTimestamp < currentHeader.Time {
		return newBundleError(errors.New("the maxTimestamp should not be less than currentBlockTimestamp"))
	}

	if (args.MaxTimestamp != nil && *args.MaxTimestamp > currentHeader.Time+MaxBundleAliveTime) ||
		(args.MinTimestamp != nil && *args.MinTimestamp > currentHeader.Time+MaxBundleAliveTime) {
		return newBundleError(errors.New("the minTimestamp/maxTimestamp should not be later than currentBlockTimestamp + 5 minutes"))
	}

	var txs types.Transactions

	for _, encodedTx := range args.Txs {
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(encodedTx); err != nil {
			return err
		}
		txs = append(txs, tx)
	}

	var minTimestamp, maxTimestamp uint64

	if args.MinTimestamp != nil {
		minTimestamp = *args.MinTimestamp
	}

	if args.MaxTimestamp != nil {
		maxTimestamp = *args.MaxTimestamp
	}

	bundle := &types.Bundle{
		Txs:               txs,
		MaxBlockNumber:    args.MaxBlockNumber,
		MinTimestamp:      minTimestamp,
		MaxTimestamp:      maxTimestamp,
		RevertingTxHashes: args.RevertingTxHashes,
	}

	return s.b.SendBundle(ctx, bundle)
}

func newBundleError(err error) *bundleError {
	return &bundleError{
		error: err,
	}
}

// bundleError is an API error that encompasses an invalid bundle with JSON error
// code and a binary data blob.
type bundleError struct {
	error
}

// ErrorCode returns the JSON error code for an invalid bundle.
// See: https://github.com/ethereum/wiki/wiki/JSON-RPC-Error-Codes-Improvement-Proposal
func (e *bundleError) ErrorCode() int {
	return InvalidBundleParamError
}
