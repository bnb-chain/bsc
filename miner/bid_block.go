// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// BidBlock worker helpers for BEP-675.

package miner

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/parlia"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

type bidBlockTaskInfo struct {
	builder common.Address
	gasFee  *big.Int
}

type verifiedBidBlockTxs struct {
	allTxs    []*types.Transaction
	systemTxs []*types.Transaction
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
		"builder", task.bidBlockInfo.builder,
		"elapsed", common.PrettyDuration(time.Since(task.createdAt)))

	// Use NewSealedBlockEvent (full-block push) instead of NewMinedBlockEvent
	// (announce-only) so peers receive the BidBlock immediately and can validate
	// it in parallel with our async InsertChain. The duplicate NewSealedBlockEvent
	// posted by WriteBlockAndSetHead after InsertChain is deduplicated by the
	// handler's known-peers cache.
	w.mux.Post(core.NewSealedBlockEvent{Block: block})

	// InsertChain: re-execute all transactions and verify stateRoot/receiptHash
	if _, err := w.chain.InsertChain(types.Blocks{block}); err != nil {
		log.Error("[BID BLOCK VERIFY FAILED]",
			"number", block.Number(),
			"hash", hash,
			"builder", task.bidBlockInfo.builder,
			"err", err,
			"revokeReason", RevokeReasonInsertChainFailed)
		w.permMgr.Revoke(task.bidBlockInfo.builder, RevokeReasonInsertChainFailed, hash, block.NumberU64())
		return
	}

	p := w.engine.(*parlia.Parlia)
	actualGasFee := p.ExtractDistributedGasFee(block)
	if task.bidBlockInfo.gasFee != nil && task.bidBlockInfo.gasFee.Cmp(actualGasFee) > 0 {
		log.Error("[BID BLOCK GAS FEE OVER-CLAIM]",
			"number", block.Number(),
			"hash", hash,
			"builder", task.bidBlockInfo.builder,
			"claimed", task.bidBlockInfo.gasFee,
			"actual", actualGasFee,
			"revokeReason", RevokeReasonGasFeeOverClaim)
		w.permMgr.Revoke(task.bidBlockInfo.builder, RevokeReasonGasFeeOverClaim, hash, block.NumberU64())
		return
	}

	log.Info("[BID BLOCK VERIFIED]",
		"number", block.Number(),
		"hash", hash,
		"builder", task.bidBlockInfo.builder,
		"gasFee", task.bidBlockInfo.gasFee)
}

func (w *worker) getBestBidBlock(header *types.Header) *types.DecodedBidBlock {
	parentHash := header.ParentHash
	return w.bidFetcher.GetBestBidBlock(parentHash)
}

func (w *worker) selectBidBlock(header *types.Header, bidBlock *types.DecodedBidBlock, simBidValidatorReward, bestReward *uint256.Int) bool {
	if bidBlock == nil {
		return false
	}

	bidBlockFee := uint256.MustFromBig(bidBlock.GasFee)
	bidBlockValidatorReward := new(uint256.Int).Mul(bidBlockFee, uint256.NewInt(*w.config.Mev.ValidatorCommission))
	bidBlockValidatorReward.Div(bidBlockValidatorReward, uint256.NewInt(10000))

	if simBidValidatorReward != nil && bidBlockValidatorReward.Cmp(simBidValidatorReward) <= 0 {
		return false
	}

	simBidVR := "<none>"
	if simBidValidatorReward != nil {
		simBidVR = simBidValidatorReward.String()
	}
	// TODO: switch back to Debug after BidBlock rollout stabilizes.
	log.Info("BidSimulator: BidBlock win bid, compare with local",
		"block", header.Number.Uint64(),
		"localBlockReward", bestReward.String(),
		"bidReward", bidBlockFee.String(),
		"bidValidatorReward", bidBlockValidatorReward.String(),
		"simBidValidatorReward", simBidVR)

	if bestReward.Cmp(bidBlockFee) < 0 {
		log.Info("[BID BLOCK selected]",
			"block", header.Number.Uint64(),
			"builder", bidBlock.Builder,
			"gasFee", bidBlock.GasFee,
			"txs", len(bidBlock.Txs))
		return true
	}
	return false
}

func (w *worker) verifyAndSelectBidBlock(header *types.Header, bidBlock *types.DecodedBidBlock, simBidValidatorReward, bestReward *uint256.Int) (*verifiedBidBlockTxs, bool, error) {
	if bidBlock == nil {
		return nil, false, nil
	}
	// preSealVerifyBidBlock already enforced engine == parlia.
	p := w.engine.(*parlia.Parlia)
	verifiedTxs, err := verifyBidBlockSystemTxs(bidBlock, header, w.chain, p)
	if err != nil {
		return nil, false, err
	}
	selected := w.selectBidBlock(header, bidBlock, simBidValidatorReward, bestReward)
	return verifiedTxs, selected, nil
}

// verifyBidBlockSystemTxs validates the trailing unsigned system-tx region.
// It returns a copied tx list so the caller can bind-sign system txs in place.
func verifyBidBlockSystemTxs(
	decoded *types.DecodedBidBlock,
	localHeader *types.Header,
	chain *core.BlockChain,
	p *parlia.Parlia,
) (*verifiedBidBlockTxs, error) {
	allTxs := make([]*types.Transaction, len(decoded.Txs))
	copy(allTxs, decoded.Txs)

	systemStart := len(allTxs)
	for i := len(allTxs) - 1; i >= 0; i-- {
		if !p.IsUnsignedSystemTxCandidate(allTxs[i]) {
			break
		}
		systemStart = i
	}

	// Stage 1 — whitelist.
	for i := systemStart; i < len(allTxs); i++ {
		if !p.IsSignableSystemTx(allTxs[i]) {
			toAddr := "<nil>"
			if allTxs[i].To() != nil {
				toAddr = allTxs[i].To().Hex()
			}
			return nil, fmt.Errorf(
				"BidBlock rejected: unsigned system tx at position %d (to=%s) "+
					"is not on the BEP-675 signable whitelist", i, toAddr,
			)
		}
	}

	// Stage 2 — shape.
	parent := chain.GetHeaderByHash(localHeader.ParentHash)
	if parent == nil {
		return nil, fmt.Errorf("BidBlock rejected: parent header not found for %s", localHeader.ParentHash.Hex())
	}
	shape := p.ExpectedSystemTxShape(localHeader, parent, decoded.GasFee)
	if err := p.VerifySystemTxShape(allTxs[systemStart:], shape); err != nil {
		return nil, fmt.Errorf("BidBlock rejected: %w", err)
	}

	return &verifiedBidBlockTxs{
		allTxs:    allTxs,
		systemTxs: allTxs[systemStart:],
	}, nil
}

// bindSignBidBlockSystemTxs signs the verified unsigned system txs from a BidBlock.
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

// prepareBidBlockTask signs system txs and assembles a verified BidBlock task.
// Builder execution-result fields are preserved without re-executing transactions.
func (w *worker) prepareBidBlockTask(
	decoded *types.DecodedBidBlock,
	verifiedTxs *verifiedBidBlockTxs,
	localHeader *types.Header,
	start time.Time,
) (*task, error) {
	if !w.isRunning() {
		return nil, errors.New("worker is not running")
	}
	if verifiedTxs == nil {
		return nil, errors.New("missing verified BidBlock txs")
	}

	// preSealVerifyBidBlock already enforced engine == parlia.
	p := w.engine.(*parlia.Parlia)
	if err := bindSignBidBlockSystemTxs(verifiedTxs.systemTxs, w.chainConfig.ChainID, p); err != nil {
		return nil, err
	}

	header := types.CopyHeader(decoded.Header)
	header.Extra = common.CopyBytes(localHeader.Extra)
	header.UncleHash = types.EmptyUncleHash
	if len(verifiedTxs.allTxs) == 0 {
		header.TxHash = types.EmptyTxsHash
	} else {
		header.TxHash = types.DeriveSha(types.Transactions(verifiedTxs.allTxs), trie.NewStackTrie(nil))
	}

	body := &types.Body{Transactions: verifiedTxs.allTxs}
	if header.EmptyWithdrawalsHash() {
		body.Withdrawals = make([]*types.Withdrawal, 0)
	}
	block := types.NewBlockWithHeader(header).WithBody(*body)

	// Attach sidecars if present.
	if decoded.Sidecars != nil {
		block = block.WithSidecars(decoded.Sidecars)
	} else {
		block = block.WithSidecars(make(types.BlobSidecars, 0))
	}

	return &task{
		block:         block,
		bidBlockInfo:  &bidBlockTaskInfo{builder: decoded.Builder, gasFee: decoded.GasFee},
		createdAt:     time.Now(),
		miningStartAt: start,
	}, nil
}

func (w *worker) enqueueBidBlockTask(task *task, systemTxs int) {
	// assembleVoteAttestation + sign header happen inside Seal.
	select {
	case w.taskCh <- task:
		log.Info("[BID BLOCK COMMIT]",
			"number", task.block.Number(),
			"builder", task.bidBlockInfo.builder,
			"txs", len(task.block.Transactions()),
			"systemTxs", systemTxs,
			"gas", task.block.GasUsed(),
			"gasFee", task.bidBlockInfo.gasFee)
	case <-w.exitCh:
		log.Info("Worker has exited")
	}
}
