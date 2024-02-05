package ethapi

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	TransferTxGasLimit = 25000
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
	if !m.b.MevRunning() {
		return common.Hash{}, types.ErrMevNotRunning
	}

	if !m.b.MinerInTurn() {
		return common.Hash{}, types.ErrMevNotInTurn
	}

	var (
		bid           = args.Bid
		currentHeader = m.b.CurrentHeader()
	)

	// only support bidding for the next block not for the future block
	if bid.BlockNumber != currentHeader.Number.Uint64()+1 {
		return common.Hash{}, types.NewInvalidBidError("stale block number or block in future")
	}

	if bid.ParentHash != currentHeader.Hash() {
		return common.Hash{}, types.NewInvalidBidError(
			fmt.Sprintf("non-aligned parent hash: %v", currentHeader.Hash()))
	}

	if bid.BuilderFee != nil {
		builderFee := bid.BuilderFee
		if builderFee.Cmp(common.Big0) < 0 {
			return common.Hash{}, types.NewInvalidBidError("builder fee should not be less than 0")
		}

		if builderFee.Cmp(common.Big0) == 0 {
			if len(args.PayBidTx) != 0 || args.PayBidTxGasUsed != 0 {
				return common.Hash{}, types.NewInvalidPayBidTxError("payBidTx should be nil when builder fee is 0")
			}
		}

		if builderFee.Cmp(bid.GasFee) >= 0 {
			return common.Hash{}, types.NewInvalidBidError("builder fee must be less than gas fee")
		}

		if builderFee.Cmp(common.Big0) > 0 {
			if args.PayBidTxGasUsed >= TransferTxGasLimit {
				return common.Hash{}, types.NewInvalidBidError(
					fmt.Sprintf("transfer tx gas used must be less than %v", TransferTxGasLimit))
			}
		}
	} else {
		if len(args.PayBidTx) != 0 || args.PayBidTxGasUsed != 0 {
			return common.Hash{}, types.NewInvalidPayBidTxError("payBidTx should be nil when builder fee is nil")
		}
	}

	return m.b.SendBid(ctx, &args)
}

// Running returns true if mev is running
func (m *MevAPI) Running() bool {
	return m.b.MevRunning()
}
