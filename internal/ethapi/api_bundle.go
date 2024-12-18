package ethapi

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

const InvalidBundleParamError = -38000

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

// SimulateGaslessBundle simulates the execution of a list of transactions with order
func (s *PrivateTxBundleAPI) SimulateGaslessBundle(_ context.Context, args types.SimulateGaslessBundleArgs) (*types.SimulateGaslessBundleResp, error) {
	if len(args.Txs) == 0 {
		return nil, newBundleError(errors.New("bundle missing txs"))
	}

	var txs types.Transactions

	for _, encodedTx := range args.Txs {
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(encodedTx); err != nil {
			log.Error("failed to unmarshal gasless tx", "err", err)
			continue
		}
		txs = append(txs, tx)
	}

	bundle := &types.Bundle{
		Txs: txs,
	}

	return s.b.SimulateGaslessBundle(bundle)
}

// SendBundle will add the signed transaction to the transaction pool.
// The sender is responsible for signing the transaction and using the correct nonce and ensuring validity
func (s *PrivateTxBundleAPI) SendBundle(ctx context.Context, args types.SendBundleArgs) (common.Hash, error) {
	if len(args.Txs) == 0 {
		return common.Hash{}, newBundleError(errors.New("bundle missing txs"))
	}

	currentHeader := s.b.CurrentHeader()

	if args.MaxBlockNumber == 0 && (args.MaxTimestamp == nil || *args.MaxTimestamp == 0) {
		maxTimeStamp := currentHeader.Time + types.MaxBundleAliveTime
		args.MaxTimestamp = &maxTimeStamp
	}

	if args.MaxBlockNumber != 0 && args.MaxBlockNumber > currentHeader.Number.Uint64()+types.MaxBundleAliveBlock {
		return common.Hash{}, newBundleError(errors.New("the maxBlockNumber should not be lager than currentBlockNum + 100"))
	}

	if args.MaxTimestamp != nil && args.MinTimestamp != nil && *args.MaxTimestamp != 0 && *args.MinTimestamp != 0 {
		if *args.MaxTimestamp <= *args.MinTimestamp {
			return common.Hash{}, newBundleError(errors.New("the maxTimestamp should not be less than minTimestamp"))
		}
	}

	if args.MaxTimestamp != nil && *args.MaxTimestamp != 0 && *args.MaxTimestamp < currentHeader.Time {
		return common.Hash{}, newBundleError(errors.New("the maxTimestamp should not be less than currentBlockTimestamp"))
	}

	if (args.MaxTimestamp != nil && *args.MaxTimestamp > currentHeader.Time+types.MaxBundleAliveTime) ||
		(args.MinTimestamp != nil && *args.MinTimestamp > currentHeader.Time+types.MaxBundleAliveTime) {
		return common.Hash{}, newBundleError(errors.New("the minTimestamp/maxTimestamp should not be later than currentBlockTimestamp + 5 minutes"))
	}

	var txs types.Transactions

	for _, encodedTx := range args.Txs {
		tx := new(types.Transaction)
		if err := tx.UnmarshalBinary(encodedTx); err != nil {
			return common.Hash{}, err
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
		DroppingTxHashes:  args.DroppingTxHashes,
	}

	// If the maxBlockNumber and maxTimestamp are not set, set max ddl of bundle as types.MaxBundleAliveBlock
	if bundle.MaxBlockNumber == 0 && bundle.MaxTimestamp == 0 {
		bundle.MaxBlockNumber = currentHeader.Number.Uint64() + types.MaxBundleAliveBlock
	}

	err := s.b.SendBundle(ctx, bundle)
	if err != nil {
		return common.Hash{}, err
	}

	return bundle.Hash(), nil
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

// Bundles returns the bundles in the given block range.
// fromBlock is beginning of the queried range, must be the hex value of a block number,
// toBlock   is end of the range, nil means latest block.
func (s *PrivateTxBundleAPI) Bundles(ctx context.Context, fromBlock, toBlock *rpc.BlockNumber) ([]*types.BundlesItem, error) {
	if fromBlock == nil {
		return nil, newBundleError(errors.New("the fromBlock is required"))
	}

	if fromBlock.Int64() <= 0 {
		return nil, newBundleError(errors.New("the fromBlock must be hex value number and greater than 0"))
	}

	from := fromBlock.Int64()

	var to int64
	if toBlock == nil || *toBlock == rpc.LatestBlockNumber || *toBlock == rpc.PendingBlockNumber {
		to = s.b.CurrentHeader().Number.Int64()
	} else {
		to = toBlock.Int64()
	}

	if to > s.b.CurrentHeader().Number.Int64() {
		return nil, newBundleError(errors.New("the toBlock must be no more than the latest block number"))
	}

	if to < from || to >= from+types.MaxBundleAliveBlock {
		return nil, newBundleError(errors.New("the toBlock must be greater than fromBlock and less than fromBlock + 100"))
	}

	return s.b.Bundles(ctx, from, to), nil
}
