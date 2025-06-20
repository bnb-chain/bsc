package ethapi

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// MevAPI implements the interfaces that defined in the BEP-322.
// It offers methods for the interaction between builders and validators.
type MevAPI struct {
	b Backend
}

// NewMevAPI creates a new MevAPI.
func NewMevAPI(b Backend) *MevAPI {
	return &MevAPI{b}
}

// SendBid receives bid from the builders.
// If mev is not running or bid is invalid, return error.
// Otherwise, creates a builder bid for the given argument, submit it to the miner.
func (m *MevAPI) SendBid(ctx context.Context, args types.BidArgs) (common.Hash, error) {
	ctx = context.WithValue(ctx, "receiveTime", time.Now().UnixMilli())
	if !m.b.MevRunning() {
		return common.Hash{}, types.ErrMevNotRunning
	}

	var (
		rawBid        = args.RawBid
		currentHeader = m.b.CurrentHeader() // `currentHeader` might change during use.
	)

	if rawBid == nil {
		return common.Hash{}, types.NewInvalidBidError("rawBid should not be nil")
	}

	// only support bidding for the next block not for the future block
	if latestBlockNumber := currentHeader.Number.Uint64(); rawBid.BlockNumber < latestBlockNumber+1 {
		return common.Hash{}, types.NewInvalidBidError(
			fmt.Sprintf("stale block number: %d, latest block: %d", rawBid.BlockNumber, latestBlockNumber))
	} else if rawBid.BlockNumber > latestBlockNumber+1 {
		// For the first block of a validator's turn, the previous block must be imported first.
		// If a builder sends bids before the import is complete, the following error message will be returned.
		// However, this is not a significant issue because:
		//   a. Each turn consists of 16 blocks, so this situation can only occur at most 1/16 of the time.
		//   b. Each builder is allowed to submit multiple bids for each block.
		return common.Hash{}, types.NewInvalidBidError(
			fmt.Sprintf("block in future: %d, latest block: %d", rawBid.BlockNumber, latestBlockNumber))
	} else if !m.b.MinerInTurn() {
		return common.Hash{}, types.ErrMevNotInTurn
	}

	if rawBid.ParentHash != currentHeader.Hash() {
		return common.Hash{}, types.NewInvalidBidError(
			fmt.Sprintf("non-aligned parent hash: %v", currentHeader.Hash()))
	}

	if rawBid.GasFee == nil || rawBid.GasFee.Cmp(common.Big0) == 0 || rawBid.GasUsed == 0 {
		return common.Hash{}, types.NewInvalidBidError("empty gasFee or empty gasUsed")
	}

	if rawBid.BuilderFee != nil {
		builderFee := rawBid.BuilderFee
		if builderFee.Cmp(common.Big0) < 0 {
			return common.Hash{}, types.NewInvalidBidError("builder fee should not be less than 0")
		}

		if builderFee.Cmp(rawBid.GasFee) >= 0 {
			return common.Hash{}, types.NewInvalidBidError("builder fee must be less than gas fee")
		}
	}

	if len(args.PayBidTx) == 0 || args.PayBidTxGasUsed == 0 {
		return common.Hash{}, types.NewInvalidPayBidTxError("payBidTx and payBidTxGasUsed are must-have")
	}

	if args.PayBidTxGasUsed > params.PayBidTxGasLimit {
		return common.Hash{}, types.NewInvalidBidError(
			fmt.Sprintf("transfer tx gas used must be no more than %v", params.PayBidTxGasLimit))
	}

	return m.b.SendBid(ctx, &args)
}

func (m *MevAPI) Params() *types.MevParams {
	return m.b.MevParams()
}

func (m *MevAPI) HasBuilder(builder common.Address) bool {
	return m.b.HasBuilder(builder)
}

// Running returns true if mev is running
func (m *MevAPI) Running() bool {
	return m.b.MevRunning()
}
