package miner

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/internal/version"
	"github.com/ethereum/go-ethereum/log"
)

const (
	maxBlobValConcurrency = 3
	maxBlobTxPerBlock     = 6
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

func (miner *Miner) GetBidBlockPermission(builder common.Address) buildertypes.BidBlockPermissionStatus {
	return miner.worker.permMgr.GetStatus(builder)
}

func (miner *Miner) SetBidBlockPermission(builder common.Address, allowed bool) {
	miner.worker.permMgr.SetAllowed(builder, allowed)
}

// bidBlockEnabled reports whether SendBidBlock is accepted.
func (miner *Miner) bidBlockEnabled() bool {
	if !*miner.worker.config.Mev.Enabled || !*miner.worker.config.Mev.BidBlockEnabled {
		return false
	}
	return miner.bidBlockPasteurActive()
}

func (miner *Miner) bidBlockPasteurActive() bool {
	head := miner.worker.chain.CurrentBlock()
	return head != nil && miner.worker.chainConfig.IsPasteur(head.Number, head.Time)
}

func (miner *Miner) SendBidBlock(ctx context.Context, args *buildertypes.BidBlockArgs) (common.Hash, error) {
	if !miner.bidBlockEnabled() {
		return common.Hash{}, buildertypes.NewInvalidBidError("BidBlock disabled, fallback to SendBid")
	}

	bb := args.BidBlock
	bidHash := bb.Hash()

	builder, err := args.EcrecoverSender()
	if err != nil {
		return common.Hash{}, buildertypes.NewInvalidBidError(fmt.Sprintf("invalid signature: bidHash=%s, err=%v", bidHash, err))
	}

	// Receive marker for the mev-sentry -> validator hop: correlate this bidHash with
	// the send-side log for latency. Logged before the gates so rejected arrivals count too.
	log.Debug("[BID BLOCK RECEIVED]",
		"bidHash", bidHash,
		"builder", builder,
		"number", bb.Header.Number,
		"parentHash", bb.Header.ParentHash,
		"txs", len(bb.Transactions))

	if !miner.bidSimulator.ExistBuilder(builder) {
		return common.Hash{}, buildertypes.NewInvalidBidError(fmt.Sprintf("builder is not registered: builder=%s, bidHash=%s", builder, bidHash))
	}
	miner.bidSimulator.recordBidBlockBuilder(builder)

	// Check permission before ReservePending so rejected BidBlocks do not use quota.
	if !miner.worker.permMgr.IsAllowed(builder) {
		return common.Hash{}, buildertypes.NewBidBlockPermissionRevokedError("builder BidBlock permission revoked, fallback to SendBid")
	}

	if len(bb.Transactions) == 0 {
		return common.Hash{}, buildertypes.NewInvalidBidError("empty BidBlock txs")
	}
	blockNumber := bb.Header.Number.Uint64()
	parentHash := bb.Header.ParentHash
	parent := miner.bidSimulator.chain.GetHeaderByHash(parentHash)
	if parent == nil {
		return common.Hash{}, buildertypes.NewInvalidBidError(fmt.Sprintf("parent not found: %s, bidHash=%s", parentHash.Hex(), bidHash))
	}

	// Security: validators must self-produce hard-fork activation blocks, whose state
	// transition does a SetCode the builder never simulated. Add every new system-contract
	// fork here. The check runs against the bid's own parent, not the head, so it also fires
	// on a stale bid naming a pre-fork parent while the head is already past the fork.
	// BEP-703 additionally disables this channel for as long as the payment lane is live,
	// not merely on its activation block, hence IsGauss rather than IsOnGauss - which also
	// keeps the activation block itself covered. A bidblock header is taken verbatim, so
	// the builder authors the lane commitment, and unlike every other builder-supplied
	// field the buckets cannot be adjudicated before signing: a cheap bound (committed
	// payment gas <= the sum of payment-class gas limits) is free to defeat, since a bare
	// transfer may declare a limit far above the 21,000 it uses, and full replay costs an
	// order of magnitude more than the DelayLeftOver left after admission. Meanwhile
	// handleBidBlockResult signs and BROADCASTS before InsertChain verifies. So an
	// un-upgraded builder would put an invalid block on the wire under this validator's
	// key on every slot it wins, diagnosed as a BAD_BLOCK that never mentions the lane.
	// Reopening it is docs/bep703-wiring-plan.md section 4.3.
	if miner.worker.chainConfig.IsOnPasteur(bb.Header.Number, parent.Time, bb.Header.Time) ||
		miner.worker.chainConfig.IsGauss(bb.Header.Number, bb.Header.Time) {
		return common.Hash{}, buildertypes.NewInvalidBidError(fmt.Sprintf(
			"BidBlock disabled at block %d (hard-fork activation, or the payment lane is active), fallback to SendBid", blockNumber))
	}
	// Reserve the quota slot atomically (check + insert under one lock).
	if err := miner.bidSimulator.ReservePending(blockNumber, builder, bidHash); err != nil {
		return common.Hash{}, err
	}

	bidMustBefore := miner.bidSimulator.bidMustBefore(parentHash)
	if timeout := time.Until(bidMustBefore); timeout <= 0 {
		return common.Hash{}, buildertypes.NewBidBlockTooLateError(fmt.Sprintf("too late, expected before %s, appeared %s later, bidHash=%s",
			bidMustBefore, common.PrettyDuration(timeout), bidHash))
	}

	decoded, err := args.ToDecodedBidBlock(builder)
	if err != nil {
		return common.Hash{}, buildertypes.NewInvalidBidError(fmt.Sprintf("failed to decode bid block: bidHash=%s, err=%v", bidHash, err))
	}

	// Validator owns the entire Extra: overwrite builder's bytes with the operator-configured
	// vanity and let SetExtraData rebuild forkhash + validators + turnLength + reserved seal
	// space. This runs before preSealVerifyBidBlock so VerifyUnsealedHeader validates the
	// validator-constructed Extra rather than the builder's.
	parliaEngine, ok := miner.worker.engine.(*parlia.Parlia)
	if !ok {
		return common.Hash{}, errors.New("consensus engine is not parlia")
	}
	miner.worker.confMu.RLock()
	decoded.Header.Extra = common.CopyBytes(miner.worker.extra)
	miner.worker.confMu.RUnlock()
	if err := parliaEngine.SetExtraData(miner.worker.chain, decoded.Header); err != nil {
		return common.Hash{}, buildertypes.NewInvalidBidError(fmt.Sprintf("set extra data: %v", err))
	}
	// Record MEV v2 (bidblock path) source and builder address.
	setBidMevInfo(decoded.Header, builder, true)

	if err := miner.bidSimulator.preSealVerifyBidBlock(decoded); err != nil {
		log.Warn("BidBlock pre-seal verification failed",
			"block", blockNumber,
			"builder", builder,
			"bidHash", decoded.Hash(),
			"err", err)
		return common.Hash{}, buildertypes.NewBidBlockPreSealVerifyError(fmt.Sprintf("pre-seal verify failed: bidHash=%s, err=%v", bidHash, err))
	}
	if receiveTime, ok := ctx.Value("receiveTime").(int64); ok {
		bidBlockPreCheckTimer.UpdateSince(time.UnixMilli(receiveTime))
	}

	if err := miner.bidSimulator.sendBidBlock(ctx, decoded); err != nil {
		return common.Hash{}, err
	}

	return bidHash, nil
}

func (miner *Miner) SendBid(ctx context.Context, bidArgs *buildertypes.BidArgs) (common.Hash, error) {
	builder, err := bidArgs.EcrecoverSender()
	if err != nil {
		return common.Hash{}, buildertypes.NewInvalidBidError(fmt.Sprintf("invalid signature:%v", err))
	}

	if !miner.bidSimulator.ExistBuilder(builder) {
		return common.Hash{}, buildertypes.NewInvalidBidError("builder is not registered")
	}

	// Reserve the quota slot atomically (check + insert under one lock).
	if err = miner.bidSimulator.ReservePending(bidArgs.RawBid.BlockNumber, builder, bidArgs.RawBid.Hash()); err != nil {
		return common.Hash{}, err
	}

	signer := types.MakeSigner(miner.worker.chainConfig, big.NewInt(int64(bidArgs.RawBid.BlockNumber)), uint64(time.Now().Unix()))
	bid, err := bidArgs.ToBid(builder, signer)
	if err != nil {
		return common.Hash{}, buildertypes.NewInvalidBidError(fmt.Sprintf("fail to convert bidArgs to bid, %v", err))
	}

	bidBetterBefore := miner.bidSimulator.bidBetterBefore(bidArgs.RawBid.ParentHash)
	timeout := time.Until(bidBetterBefore)

	if timeout <= 0 {
		return common.Hash{}, fmt.Errorf("too late, expected before %s, appeared %s later", bidBetterBefore,
			common.PrettyDuration(timeout))
	}

	err = miner.bidSimulator.sendBid(ctx, bid)
	if err != nil {
		return common.Hash{}, err
	}

	return bid.Hash(), nil
}

// startAsyncBlobValidation uses a fixed-size worker pool to validate blob
// transactions in the background (field checks + KZG proof verification).
// Results are stored per-tx in bid.BlobValResults keyed by tx hash.
func startAsyncBlobValidation(bid *buildertypes.Bid) {
	type blobJob struct {
		tx *types.Transaction
		ch chan error
	}

	bid.BlobValResults = make(map[common.Hash]chan error)
	jobs := make([]blobJob, 0, maxBlobTxPerBlock)

	for _, tx := range bid.Txs {
		if tx.Type() == types.BlobTxType {
			if _, dup := bid.BlobValResults[tx.Hash()]; dup {
				continue
			}
			ch := make(chan error, 1)
			bid.BlobValResults[tx.Hash()] = ch
			jobs = append(jobs, blobJob{tx: tx, ch: ch})
			if len(jobs) >= maxBlobTxPerBlock {
				break
			}
		}
	}

	jobCh := make(chan blobJob, len(jobs))
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	workers := len(jobs)
	if workers > maxBlobValConcurrency {
		workers = maxBlobValConcurrency
	}
	for i := 0; i < workers; i++ {
		go func() {
			for j := range jobCh {
				j.ch <- txpool.ValidateBlobTx(j.tx, nil, nil)
			}
		}()
	}
}

func (miner *Miner) MevParams() *buildertypes.MevParams {
	builderFeeCeil, ok := big.NewInt(0).SetString(*miner.worker.config.Mev.BuilderFeeCeil, 10)
	if !ok {
		log.Error("failed to parse builder fee ceil", "BuilderFeeCeil", *miner.worker.config.Mev.BuilderFeeCeil)
		return nil
	}

	return &buildertypes.MevParams{
		ValidatorCommission:   *miner.worker.config.Mev.ValidatorCommission,
		BidSimulationLeftOver: *miner.worker.config.Mev.BidSimulationLeftOver,
		NoInterruptLeftOver:   *miner.worker.config.Mev.NoInterruptLeftOver,
		MaxBidsPerBuilder:     *miner.worker.config.Mev.MaxBidsPerBuilder,
		GasCeil:               miner.worker.config.GasCeil,
		GasPrice:              miner.worker.config.GasPrice,
		BuilderFeeCeil:        builderFeeCeil,
		BidBlockEnabled:       miner.bidBlockEnabled(),
		Version:               version.Semantic,
	}
}
