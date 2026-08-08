// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// BidBlock worker helpers for BEP-675.

package miner

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

type bidBlockTaskInfo struct {
	builder       common.Address
	bidHash       common.Hash
	gasFee        *big.Int
	systemTxStart int
}

var errInvalidBidBlockBlobTx = errors.New("BidBlock blob validation failed")

// verifyBidBlockLaneQuota adjudicates a builder-authored BEP-703 commitment as far as it
// can be adjudicated without replaying the block, and must run before this validator
// signs: a BidBlock header is adopted verbatim, and handleBidBlockResult broadcasts
// before InsertChain.
//
// What admission already settled, in verifyCascadingFields: the commitment decodes at all,
// and the block rule holds over the committed values. What is added here:
//
//	laneSize        Exactly. It is a pure function of the parent, the grandparent and
//	                0x2007's parameters, so the builder does not get to choose it - and the
//	                gas limit it derives from is the validator's own, since
//	                preSealVerifyBidBlock pins header.GasLimit to CalcGasLimit.
//	paymentGasUsed  From above only, and only as far as bidBlockPaymentCeiling can bound it.
//	                Over-stating is the profitable direction, which is why an upper bound is
//	                the right shape.
//
// What stays exposed: a payment total that is too low, or too high but inside the ceiling.
// Neither is profitable, but both make a block every importer rejects with ErrUntruthy after
// this validator signed and broadcast it, so the residual is builder BUGS - most likely a
// builder that stamps the commitment but never calls RecordUsedFrom, committing zero. No cheap
// lower bound exists, since any transaction can install code at any address. Revocation on
// the import failure is what prices it.
//
// Nothing here revokes the builder: ErrStateUnavailable and ErrCorruptConfig are local or
// chain-wide, and revoking on "the lane refused it" would revoke every builder in turn on one
// unreadable parent state. The caller falls through to the simBid comparison, so the slot can
// still go to another bid or to the local block.
func (w *worker) verifyBidBlockLaneQuota(decoded *buildertypes.DecodedBidBlock, local *environment) error {
	header := decoded.Header
	// The whole check rests on the local state being open on this bid's parent: the
	// classifier and the parameters have to come from the root the importer will use, and
	// the quota's denominator from the same gas limit. Both hold because the bidblock is
	// selected by the parent hash the local work was built on - so this is the invariant
	// asserted rather than the state re-opened.
	if header.ParentHash != local.header.ParentHash {
		return fmt.Errorf("bidblock parent %x is not the parent the local state is open on (%x)",
			header.ParentHash, local.header.ParentHash)
	}
	parent := w.chain.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	// local.state is the advancing state of our own build, but Reader() is pinned to the
	// root it opened on, so the classifier cannot see our block's writes. Reusing it rather
	// than opening a second state is what keeps this affordable inside DelayLeftOver: the
	// reader is warm, and a fresh one would enumerate the payment-contract list cold.
	lane, err := core.ResolveLaneState(w.chainConfig, w.chain, parent, header, local.state.Reader())
	if err != nil {
		return err
	}
	if !lane.On() {
		return nil
	}
	c, err := paymentlane.Decode(header.UncleHash)
	if err != nil {
		return err
	}
	if err := lane.CheckQuota(c.LaneSize); err != nil {
		return err
	}
	ceiling, err := bidBlockPaymentCeiling(lane, decoded, header.GasUsed)
	if err != nil {
		return err
	}
	if c.PaymentGasUsed > ceiling {
		return fmt.Errorf("%w: committed payment %d exceeds what these transactions can consume as payment (%d)",
			paymentlane.ErrUntruthy, c.PaymentGasUsed, ceiling)
	}
	return nil
}

// bidBlockPaymentCeiling is the largest payment total the BidBlock's user transactions could
// produce: each payment-class transaction's declared gas limit, which needs no execution
// state. Exact on honest traffic - a bare transfer's intrinsic gas IS params.TxGas and wallets
// declare that - so it catches a builder whose accounting is wrong; weak against one that
// wants past it, since declaring gas is free and params.MaxTxGas is 16.7M against a lane of
// 2-4.4M. Bounding by intrinsic gas would be tighter and is NOT sound: a payment-class
// transfer whose destination gains code mid-block really does consume its limit, per the known
// leak on Classify's gate 7, so it would refuse honest blocks.
//
// System transactions are skipped because the importer never classifies them either -
// IsSystemTransaction splits them out first. Their addresses are also inside the reserved
// range, but that is the weaker reason: it would stop being sufficient if the range narrowed.
// The clamp at headerGasUsed is an overflow guard that admits nothing CheckHeaderBounds has
// not already allowed, and admission caps every transaction here at params.MaxTxGas, so the
// clamp is reached long before uint64 is.
func bidBlockPaymentCeiling(lane *core.LaneState, decoded *buildertypes.DecodedBidBlock, headerGasUsed uint64) (uint64, error) {
	var ceiling uint64
	for _, tx := range decoded.Txs[:decoded.SystemTxStart] {
		class, err := lane.Classify(tx)
		if err != nil {
			return 0, err
		}
		if class != paymentlane.ClassPayment {
			continue
		}
		if ceiling += tx.Gas(); ceiling >= headerGasUsed {
			return headerGasUsed, nil
		}
	}
	return ceiling, nil
}

// setBidMevInfo tags header.RequestsHash with the BEP-675 block-source info
func setBidMevInfo(header *types.Header, builder common.Address, isBidBlock bool) {
	// Legacy BID: a nil RequestsHash denotes a pre-Prague block that must stay
	// untagged. BIDBLOCK is post-Prague and validator-owned, so always stamped.
	if !isBidBlock && header.RequestsHash == nil {
		return
	}
	version := buildertypes.BlockMevInfoVersionBid
	if isBidBlock {
		version = buildertypes.BlockMevInfoVersionBidBlock
	}
	tag := buildertypes.EncodeBlockMevInfo(version, builder)
	header.RequestsHash = &tag
}

func (w *worker) selectBidBlock(bidBlock *buildertypes.DecodedBidBlock, simBidBlockReward, simBidValidatorReward, bestReward *uint256.Int) bool {
	if bidBlock == nil {
		return false
	}

	bidBlockFee := bidBlock.GasFee
	bidBlockValidatorReward := new(big.Int).Mul(bidBlockFee, new(big.Int).SetUint64(*w.config.Mev.ValidatorCommission))
	bidBlockValidatorReward.Div(bidBlockValidatorReward, big.NewInt(10000))

	if simBidValidatorReward != nil && bidBlockValidatorReward.Cmp(simBidValidatorReward.ToBig()) <= 0 {
		return false
	}
	if simBidBlockReward != nil && bidBlockFee.Cmp(simBidBlockReward.ToBig()) <= 0 {
		return false
	}

	simBidBR := "<none>"
	if simBidBlockReward != nil {
		simBidBR = simBidBlockReward.String()
	}
	simBidVR := "<none>"
	if simBidValidatorReward != nil {
		simBidVR = simBidValidatorReward.String()
	}
	blockNum := bidBlock.Header.Number.Uint64()
	// TODO: switch back to Debug after BidBlock rollout stabilizes.
	log.Info("BidSimulator: BidBlock win bid, compare with local",
		"block", blockNum,
		"bidHash", bidBlock.Hash(),
		"localBlockReward", bestReward.String(),
		"bidReward", bidBlockFee.String(),
		"bidValidatorReward", bidBlockValidatorReward.String(),
		"simBidBlockReward", simBidBR,
		"simBidValidatorReward", simBidVR)

	if bidBlockFee.Cmp(bestReward.ToBig()) > 0 {
		log.Info("[BID BLOCK selected]",
			"block", blockNum,
			"bidHash", bidBlock.Hash(),
			"builder", bidBlock.Builder,
			"gasFee", weiToEtherStringF6(bidBlock.GasFee),
			"txs", len(bidBlock.Txs))
		return true
	}
	return false
}

// bindSignBidBlockSystemTxs signs the verified unsigned system txs from a BidBlock in place.
func bindSignBidBlockSystemTxs(
	systemTxs []*types.Transaction,
	chainID *big.Int,
	p *parlia.Parlia,
) error {
	for i, tx := range systemTxs {
		signed, err := p.SignSystemTx(tx, chainID)
		if err != nil {
			return fmt.Errorf("failed to sign system tx %d: %v", i, err)
		}
		systemTxs[i] = signed
	}
	return nil
}

// prepareBidBlockTask signs system txs and assembles a BidBlock task.
// Extra was finalized by the validator during admission (SendBidBlock calls
// SetExtraData before preSealVerifyBidBlock); engine.Seal will later fill the
// reserved vote-attestation/seal-signature bytes. Here we only recompute TxHash
// after bind-signing the trailing system txs. Do not touch fields that enter
// the EVM BlockContext (GasLimit, Coinbase, Time, Difficulty, BaseFee, ...) —
// changing them after the builder's pre-execution would diverge the re-executed
// stateRoot and fail InsertChain.
func (w *worker) prepareBidBlockTask(
	decoded *buildertypes.DecodedBidBlock,
	start time.Time,
) (*task, error) {
	prepareStart := time.Now()
	defer bidBlockPrepareTimer.UpdateSince(prepareStart)

	if !w.isRunning() {
		return nil, errors.New("worker is not running")
	}

	p := w.engine.(*parlia.Parlia)

	// Copy the tx slice so bind-signing does not mutate the cached BidBlock.
	allTxs := make([]*types.Transaction, len(decoded.Txs))
	copy(allTxs, decoded.Txs)

	header := types.CopyHeader(decoded.Header)
	if err := validateBidBlockBlobTxs(header, allTxs, decoded.Sidecars, decoded.SystemTxStart); err != nil {
		if errors.Is(err, errInvalidBidBlockBlobTx) {
			w.revokeBidBlockBuilder(decoded.Builder, err.Error(), decoded.Hash(), decoded.BlockNumber())
		}
		return nil, err
	}
	if err := bindSignBidBlockSystemTxs(allTxs[decoded.SystemTxStart:], w.chainConfig.ChainID, p); err != nil {
		return nil, err
	}
	header.TxHash = types.DeriveSha(types.Transactions(allTxs), trie.NewStackTrie(nil))

	body := &types.Body{
		Transactions: allTxs,
		Withdrawals:  make([]*types.Withdrawal, 0),
	}
	block := types.NewBlockWithHeader(header).WithBody(*body).WithSidecars(decoded.Sidecars)

	return &task{
		block: block,
		bidBlockInfo: &bidBlockTaskInfo{
			builder:       decoded.Builder,
			bidHash:       decoded.Hash(),
			gasFee:        decoded.GasFee,
			systemTxStart: decoded.SystemTxStart,
		},
		createdAt:     time.Now(),
		miningStartAt: start,
	}, nil
}

type bidBlockBlobValidationJob struct {
	txIndex int
	tx      *types.Transaction
}

// validateBidBlockBlobTxs runs expensive blob proof checks for the selected BidBlock.
func validateBidBlockBlobTxs(header *types.Header, txs []*types.Transaction, sidecars types.BlobSidecars, systemTxStart int) error {
	jobs := make([]bidBlockBlobValidationJob, 0, len(sidecars))
	sidecarIndex := 0
	for txIndex, tx := range txs[:systemTxStart] {
		if tx.Type() != types.BlobTxType {
			continue
		}
		sidecar := sidecars[sidecarIndex]
		jobs = append(jobs, bidBlockBlobValidationJob{
			txIndex: txIndex,
			tx:      tx.WithBlobTxSidecar(&sidecar.BlobTxSidecar),
		})
		sidecarIndex++
	}

	workers := len(jobs)
	if workers > maxBlobValConcurrency {
		workers = maxBlobValConcurrency
	}
	jobCh := make(chan bidBlockBlobValidationJob, len(jobs))
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)

	errCh := make(chan error, len(jobs))
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for job := range jobCh {
				if err := txpool.ValidateBlobTx(job.tx, header, nil); err != nil {
					errCh <- fmt.Errorf("%w: %v", errInvalidBidBlockBlobTx, err)
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		return err
	}
	return nil
}

func (w *worker) enqueueBidBlockTask(task *task, systemTxs int) {
	// assembleVoteAttestation + sign header happen inside Seal.
	select {
	case w.taskCh <- task:
		log.Info("[BID BLOCK COMMIT]",
			"number", task.block.Number(),
			"bidHash", task.bidBlockInfo.bidHash,
			"builder", task.bidBlockInfo.builder,
			"txs", len(task.block.Transactions()),
			"systemTxs", systemTxs,
			"gas", task.block.GasUsed(),
			"gasFee", weiToEtherStringF6(task.bidBlockInfo.gasFee))
	case <-w.exitCh:
		log.Info("Worker has exited")
	}
}

func (w *worker) revokeBidBlockBuilder(builder common.Address, reason string, hash common.Hash, blockNum uint64) {
	w.revokeBidBlockBuilderFor(builder, reason, hash, blockNum, bidBlockRevokeDuration)
}

func (w *worker) revokeBidBlockBuilderFor(builder common.Address, reason string, hash common.Hash, blockNum uint64, duration time.Duration) {
	w.permMgr.RevokeFor(builder, reason, hash, blockNum, duration)
	bidBlockRevokeGauge.Inc(1)
	bidBlockRevokedBuildersGauge.Update(int64(w.permMgr.ActiveRevokeCount()))
}

// handleBidBlockResult handles a sealed BidBlock: broadcast, then InsertChain for verification.
func (w *worker) handleBidBlockResult(block *types.Block, task *task) {
	hash := block.Hash()

	// Broadcast the block first (before verification)
	stats := w.chain.GetBlockStats(hash)
	stats.SendBlockTime.Store(time.Now().UnixMilli())
	stats.StartMiningTime.Store(task.miningStartAt.UnixMilli())

	log.Info("[BID BLOCK SEALED]",
		"number", block.Number(),
		"hash", hash,
		"bidHash", task.bidBlockInfo.bidHash,
		"builder", task.bidBlockInfo.builder,
		"elapsed", common.PrettyDuration(time.Since(task.createdAt)))

	w.mux.Post(core.NewSealedBlockEvent{Block: block})

	// InsertChain re-executes all txs and validates fields the validator could
	// not check at admission. Any mismatch is treated as builder dishonesty and
	// revokes the builder for the default lockout window. Categories caught here:
	//   - Root          (post-execution state root)
	//   - ReceiptHash   (post-execution receipts trie root)
	//   - Bloom         (post-execution logs bloom)
	//   - GasUsed       (cumulative gas consumed)
	//   - Tx precheck failures (nonce, balance, signature, intrinsic gas, ...)
	//   - System tx value / params (e.g. deposit value vs. SystemAddress balance)
	//   - Blob sidecar checks (KZG proofs, blob hashes)
	verifyStart := time.Now()
	_, insertErr := w.chain.InsertChain(types.Blocks{block})
	bidBlockVerifyTimer.UpdateSince(verifyStart)
	if insertErr != nil {
		log.Error("[BID BLOCK VERIFY FAILED]",
			"number", block.Number(),
			"hash", hash,
			"bidHash", task.bidBlockInfo.bidHash,
			"parentHash", block.ParentHash(),
			"txs", len(block.Transactions()),
			"gasUsed", block.GasUsed(),
			"stateRoot", block.Root(),
			"receiptHash", block.ReceiptHash(),
			"builder", task.bidBlockInfo.builder,
			"err", insertErr)
		bidBlockVerifyFailedGauge.Inc(1)
		w.revokeBidBlockBuilder(task.bidBlockInfo.builder, fmt.Sprintf("InsertChain err: %v", insertErr), hash, block.NumberU64())
		return
	}
	// Check the post-import average gas price excluding system transactions; only future BidBlock permission is revoked.
	if receipts := w.chain.GetReceiptsByHash(block.Hash()); receipts != nil {
		avgGasPrice, nonSystemGasUsed, err := validateBidBlockAverageGasPrice(
			task.bidBlockInfo.gasFee,
			receipts,
			task.bidBlockInfo.systemTxStart,
			w.config.GasPrice,
		)
		if err != nil {
			log.Error("[BID BLOCK GASPRICE LOW]",
				"number", block.Number(),
				"hash", block.Hash(),
				"bidHash", task.bidBlockInfo.bidHash,
				"builder", task.bidBlockInfo.builder,
				"avgGasPrice", avgGasPrice,
				"minGasPrice", w.config.GasPrice,
				"nonSystemGasUsed", nonSystemGasUsed,
				"nonSystemTxs", task.bidBlockInfo.systemTxStart,
				"revokeDuration", bidBlockGasPriceLowRevokeDuration,
				"err", err)
			w.revokeBidBlockBuilderFor(task.bidBlockInfo.builder, err.Error(), block.Hash(), block.NumberU64(), bidBlockGasPriceLowRevokeDuration)
			return
		}
	}

	log.Info("[BID BLOCK VERIFIED]",
		"number", block.Number(),
		"hash", hash,
		"bidHash", task.bidBlockInfo.bidHash,
		"builder", task.bidBlockInfo.builder,
		"gasFee", weiToEtherStringF6(task.bidBlockInfo.gasFee))
}
