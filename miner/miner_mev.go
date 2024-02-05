package miner

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const MevRoutineLimit = 10000

type BuilderConfig struct {
	Address common.Address
	URL     string
}

type MevConfig struct {
	Enabled   bool            // Whether to enable Mev or not
	SentryURL string          // The url of Mev sentry
	Builders  []BuilderConfig // The list of builders

	ValidatorCommission int64 // 100 means 1%
}

// MevRunning return true if mev is running.
func (miner *Miner) MevRunning() bool {
	return miner.bidSimulator.isRunning() && miner.bidSimulator.receivingBid()
}

// StartMev starts mev.
func (miner *Miner) StartMev() {
	miner.bidSimulator.startReceivingBid()
}

// StopMev stops mev.
func (miner *Miner) StopMev() {
	miner.bidSimulator.stopReceivingBid()
}

// AddBuilder adds a builder to the bid simulator.
func (miner *Miner) AddBuilder(builder common.Address, url string) error {
	return miner.bidSimulator.AddBuilder(builder, url)
}

// RemoveBuilder removes a builder from the bid simulator.
func (miner *Miner) RemoveBuilder(builderAddr common.Address) error {
	return miner.bidSimulator.RemoveBuilder(builderAddr)
}

func (miner *Miner) SendBid(ctx context.Context, bid *types.BidArgs) (common.Hash, error) {
	builder, err := types.EcrecoverBuilder(bid)
	if err != nil {
		return common.Hash{}, types.NewInvalidBidError(fmt.Sprintf("invalid signature:%v", err))
	}

	bidTxs := make([]*types.Transaction, len(bid.Bid.Txs))
	signer := types.MakeSigner(miner.worker.chainConfig, big.NewInt(int64(bid.Bid.BlockNumber)), uint64(time.Now().Unix()))

	var wg sync.WaitGroup
	for i, encodedTx := range bid.Bid.Txs {
		i := i
		encodedTx := encodedTx
		wg.Add(1)

		err = miner.antsPool.Submit(func() {
			defer wg.Done()

			var er error
			tx := new(types.Transaction)
			er = tx.UnmarshalBinary(encodedTx)
			if er != nil {
				return
			}

			_, er = types.Sender(signer, tx)
			if er != nil {
				return
			}

			bidTxs[i] = tx
		})

		if err != nil {
			return common.Hash{}, types.ErrMevBusy
		}
	}

	wg.Wait()

	for _, v := range bidTxs {
		if v == nil {
			return common.Hash{}, types.NewInvalidBidError("invalid tx in bid")
		}
	}

	txs := make([]*types.Transaction, 0)
	txs = append(txs, bidTxs...)
	if len(bid.PayBidTx) != 0 {
		var payBidTx = new(types.Transaction)
		if err = payBidTx.UnmarshalBinary(bid.PayBidTx); err != nil {
			return common.Hash{}, types.NewInvalidBidError(fmt.Sprintf("unmarshal transfer tx err:%v", err))
		}
		txs = append(txs, payBidTx)
	}

	innerBid := &types.Bid{
		Builder:     builder,
		BlockNumber: bid.Bid.BlockNumber,
		ParentHash:  bid.Bid.ParentHash,
		Txs:         txs,
		GasUsed:     bid.Bid.GasUsed + bid.PayBidTxGasUsed,
		GasFee:      bid.Bid.GasFee,
		BuilderFee:  big.NewInt(0),
	}

	if bid.Bid.BuilderFee != nil {
		innerBid.BuilderFee = bid.Bid.BuilderFee
	}

	bidMustBefore := miner.bidSimulator.bidMustBefore(bid.Bid.ParentHash)
	timeout := time.Until(bidMustBefore)

	if timeout <= 0 {
		return common.Hash{}, fmt.Errorf("too late, expected befor %s, appeared %s later", bidMustBefore,
			common.PrettyDuration(timeout))
	}

	err = miner.bidSimulator.sendBid(ctx, innerBid)

	if err != nil {
		return common.Hash{}, err
	}

	return innerBid.Hash(), nil
}
