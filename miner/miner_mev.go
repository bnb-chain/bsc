package miner

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

type BuilderConfig struct {
	Address common.Address
	URL     string
}

type MevConfig struct {
	Enabled               bool            // Whether to enable Mev or not
	GreedyMergeTx         bool            // Whether to merge local transactions to the bid
	BuilderFeeCeil        string          // The maximum builder fee of a bid
	SentryURL             string          // The url of Mev sentry
	Builders              []BuilderConfig // The list of builders
	ValidatorCommission   uint64          // 100 means the validator claims 1% from block reward
	BidSimulationLeftOver time.Duration
}

var DefaultMevConfig = MevConfig{
	Enabled:               false,
	SentryURL:             "",
	Builders:              nil,
	ValidatorCommission:   100,
	BidSimulationLeftOver: 50 * time.Millisecond,
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
		BuilderFeeCeil:        builderFeeCeil,
	}
}
