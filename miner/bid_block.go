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
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
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
