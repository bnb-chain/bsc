package miner

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/version"
	"github.com/ethereum/go-ethereum/log"
)

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

// HasBuilder returns true if the builder is in the builder list.
func (miner *Miner) HasBuilder(builder common.Address) bool {
	return miner.bidSimulator.ExistBuilder(builder)
}

func (miner *Miner) SendBid(ctx context.Context, bidArgs *types.BidArgs) (common.Hash, error) {
	builder, err := bidArgs.EcrecoverSender()
	if err != nil {
		return common.Hash{}, types.NewInvalidBidError(fmt.Sprintf("invalid signature:%v", err))
	}

	if !miner.bidSimulator.ExistBuilder(builder) {
		return common.Hash{}, types.NewInvalidBidError("builder is not registered")
	}

	err = miner.bidSimulator.CheckPending(bidArgs.RawBid.BlockNumber, builder, bidArgs.RawBid.Hash())
	if err != nil {
		return common.Hash{}, err
	}

	signer := types.MakeSigner(miner.worker.chainConfig, big.NewInt(int64(bidArgs.RawBid.BlockNumber)), uint64(time.Now().Unix()))
	bid, err := bidArgs.ToBid(builder, signer)
	if err != nil {
		return common.Hash{}, types.NewInvalidBidError(fmt.Sprintf("fail to convert bidArgs to bid, %v", err))
	}

	bidBetterBefore := miner.bidSimulator.bidBetterBefore(bidArgs.RawBid.ParentHash)
	timeout := time.Until(bidBetterBefore)

	if timeout <= 0 {
		return common.Hash{}, fmt.Errorf("too late, expected befor %s, appeared %s later", bidBetterBefore,
			common.PrettyDuration(timeout))
	}

	err = miner.bidSimulator.sendBid(ctx, bid)

	if err != nil {
		return common.Hash{}, err
	}

	return bid.Hash(), nil
}

func (miner *Miner) BestPackedBlockReward(parentHash common.Hash) *big.Int {
	bidRuntime := miner.bidSimulator.GetBestBid(parentHash)
	if bidRuntime == nil {
		return big.NewInt(0)
	}

	return bidRuntime.packedBlockReward
}

func (miner *Miner) MevParams() *types.MevParams {
	builderFeeCeil, ok := big.NewInt(0).SetString(miner.worker.config.Mev.BuilderFeeCeil, 10)
	if !ok {
		log.Error("failed to parse builder fee ceil", "BuilderFeeCeil", miner.worker.config.Mev.BuilderFeeCeil)
		return nil
	}

	return &types.MevParams{
		ValidatorCommission:   miner.worker.config.Mev.ValidatorCommission,
		BidSimulationLeftOver: miner.worker.config.Mev.BidSimulationLeftOver,
		GasCeil:               miner.worker.config.GasCeil,
		GasPrice:              miner.worker.config.GasPrice,
		BuilderFeeCeil:        builderFeeCeil,
		Version:               version.Semantic,
	}
}
