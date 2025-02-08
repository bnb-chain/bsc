package core

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/metrics"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
)

var (
	daCheckTimer = metrics.NewRegisteredTimer("chain/dacheck", nil)
)

// validateBlobSidecar it is same as validateBlobSidecar in core/txpool/validation.go
func validateBlobSidecar(hashes []common.Hash, sidecar *types.BlobSidecar) error {
	if len(sidecar.Blobs) != len(hashes) {
		return fmt.Errorf("invalid number of %d blobs compared to %d blob hashes", len(sidecar.Blobs), len(hashes))
	}
	if len(sidecar.Commitments) != len(hashes) {
		return fmt.Errorf("invalid number of %d blob commitments compared to %d blob hashes", len(sidecar.Commitments), len(hashes))
	}
	if len(sidecar.Proofs) != len(hashes) {
		return fmt.Errorf("invalid number of %d blob proofs compared to %d blob hashes", len(sidecar.Proofs), len(hashes))
	}
	// Blob quantities match up, validate that the provers match with the
	// transaction hash before getting to the cryptography
	hasher := sha256.New()
	for i, vhash := range hashes {
		computed := kzg4844.CalcBlobHashV1(hasher, &sidecar.Commitments[i])
		if vhash != computed {
			return fmt.Errorf("blob %d: computed hash %#x mismatches transaction one %#x", i, computed, vhash)
		}
	}
	// Blob commitments match with the hashes in the transaction, verify the
	// blobs themselves via KZG
	for i := range sidecar.Blobs {
		if err := kzg4844.VerifyBlobProof(&sidecar.Blobs[i], sidecar.Commitments[i], sidecar.Proofs[i]); err != nil {
			return fmt.Errorf("invalid blob %d: %v", i, err)
		}
	}
	return nil
}

// IsDataAvailable it checks that the blobTx block has available blob data
func IsDataAvailable(chain consensus.ChainHeaderReader, block *types.Block) (err error) {
	defer func(start time.Time) {
		daCheckTimer.Update(time.Since(start))
	}(time.Now())

	// refer logic in ValidateBody
	if !chain.Config().IsCancun(block.Number(), block.Time()) {
		if block.Sidecars() != nil {
			return errors.New("sidecars present in block body before cancun")
		}
		return nil
	}

	// only required to check within MinBlocksForBlobRequests block's DA
	highest := chain.ChasingHead()
	current := chain.CurrentHeader()
	if highest == nil || highest.Number.Cmp(current.Number) < 0 {
		highest = current
	}
	if block.NumberU64()+params.MinBlocksForBlobRequests < highest.Number.Uint64() {
		// if we needn't check DA of this block, just clean it
		block.CleanSidecars()
		return nil
	}

	// if sidecar is nil, just clean it. And it will be used for saving in ancient.
	if block.Sidecars() == nil {
		block.CleanSidecars()
	}
	sidecars := block.Sidecars()
	for _, s := range sidecars {
		if err := s.SanityCheck(block.Number(), block.Hash()); err != nil {
			return err
		}
	}
	// alloc block's blobTx
	blobTxs := make([]*types.Transaction, 0, len(sidecars))
	blobTxIndexes := make([]uint64, 0, len(sidecars))
	for i, tx := range block.Transactions() {
		if tx.Type() != types.BlobTxType {
			continue
		}
		blobTxs = append(blobTxs, tx)
		blobTxIndexes = append(blobTxIndexes, uint64(i))
	}
	if len(blobTxs) != len(sidecars) {
		return fmt.Errorf("blob info mismatch: sidecars %d, versionedHashes:%d", len(sidecars), len(blobTxs))
	}

	// check blob amount
	blobCnt := 0
	for _, s := range sidecars {
		blobCnt += len(s.Blobs)
	}
	maxBlobPerBlock := eip4844.MaxBlobsPerBlock(chain.Config(), block.Time())
	if blobCnt > maxBlobPerBlock {
		return fmt.Errorf("too many blobs in block: have %d, permitted %d", blobCnt, maxBlobPerBlock)
	}

	// check blob and versioned hash
	for i, tx := range blobTxs {
		// check sidecar tx related
		if sidecars[i].TxHash != tx.Hash() {
			return fmt.Errorf("sidecar's TxHash mismatch with expected transaction, want: %v, have: %v", sidecars[i].TxHash, tx.Hash())
		}
		if sidecars[i].TxIndex != blobTxIndexes[i] {
			return fmt.Errorf("sidecar's TxIndex mismatch with expected transaction, want: %v, have: %v", sidecars[i].TxIndex, blobTxIndexes[i])
		}
		if err := validateBlobSidecar(tx.BlobHashes(), sidecars[i]); err != nil {
			return err
		}
	}

	return nil
}

func CheckDataAvailableInBatch(chainReader consensus.ChainHeaderReader, chain types.Blocks) (int, error) {
	if len(chain) == 1 {
		return 0, IsDataAvailable(chainReader, chain[0])
	}

	var (
		wg   sync.WaitGroup
		errs sync.Map
	)

	for i := range chain {
		wg.Add(1)
		func(index int, block *types.Block) {
			gopool.Submit(func() {
				defer wg.Done()
				errs.Store(index, IsDataAvailable(chainReader, block))
			})
		}(i, chain[i])
	}

	wg.Wait()
	for i := range chain {
		val, exist := errs.Load(i)
		if !exist || val == nil {
			continue
		}
		err := val.(error)
		if err != nil {
			return i, err
		}
	}
	return 0, nil
}
