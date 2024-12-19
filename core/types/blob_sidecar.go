package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
)

type BlobSidecars []*BlobSidecar

// Len returns the length of s.
func (s BlobSidecars) Len() int { return len(s) }

// EncodeIndex encodes the i'th BlobTxSidecar to w. Note that this does not check for errors
// because we assume that BlobSidecars will only ever contain valid sidecars
func (s BlobSidecars) EncodeIndex(i int, w *bytes.Buffer) {
	rlp.Encode(w, s[i])
}

func (s BlobSidecars) BlobTxSidecarList() []*BlobTxSidecar {
	var inner []*BlobTxSidecar
	for _, sidecar := range s {
		inner = append(inner, &(sidecar.BlobTxSidecar))
	}
	return inner
}

type BlobSidecar struct {
	BlobTxSidecar
	BlockNumber *big.Int    `json:"blockNumber"`
	BlockHash   common.Hash `json:"blockHash"`
	TxIndex     uint64      `json:"transactionIndex"`
	TxHash      common.Hash `json:"transactionHash"`
}

func NewBlobSidecarFromTx(tx *Transaction) *BlobSidecar {
	if tx.BlobTxSidecar() == nil {
		return nil
	}
	return &BlobSidecar{
		BlobTxSidecar: *tx.BlobTxSidecar(),
		TxHash:        tx.Hash(),
	}
}

func (s *BlobSidecar) SanityCheck(blockNumber *big.Int, blockHash common.Hash) error {
	if s.BlockNumber.Cmp(blockNumber) != 0 {
		return errors.New("BlobSidecar with wrong block number")
	}
	if s.BlockHash != blockHash {
		return errors.New("BlobSidecar with wrong block hash")
	}
	if len(s.Blobs) != len(s.Commitments) {
		return errors.New("BlobSidecar has wrong commitment length")
	}
	if len(s.Blobs) != len(s.Proofs) {
		return errors.New("BlobSidecar has wrong proof length")
	}
	return nil
}

func (s *BlobSidecar) MarshalJSON() ([]byte, error) {
	fields := map[string]interface{}{
		"blockHash":   s.BlockHash,
		"blockNumber": hexutil.EncodeUint64(s.BlockNumber.Uint64()),
		"txHash":      s.TxHash,
		"txIndex":     hexutil.EncodeUint64(s.TxIndex),
	}
	fields["blobSidecar"] = s.BlobTxSidecar
	return json.Marshal(fields)
}

func (s *BlobSidecar) UnmarshalJSON(input []byte) error {
	type blobSidecar struct {
		BlobSidecar BlobTxSidecar `json:"blobSidecar"`
		BlockNumber *hexutil.Big  `json:"blockNumber"`
		BlockHash   common.Hash   `json:"blockHash"`
		TxIndex     *hexutil.Big  `json:"txIndex"`
		TxHash      common.Hash   `json:"txHash"`
	}
	var blob blobSidecar
	if err := json.Unmarshal(input, &blob); err != nil {
		return err
	}
	s.BlobTxSidecar = blob.BlobSidecar
	if blob.BlockNumber == nil {
		return errors.New("missing required field 'blockNumber' for BlobSidecar")
	}
	s.BlockNumber = blob.BlockNumber.ToInt()
	s.BlockHash = blob.BlockHash
	if blob.TxIndex == nil {
		return errors.New("missing required field 'txIndex' for BlobSidecar")
	}
	s.TxIndex = blob.TxIndex.ToInt().Uint64()
	s.TxHash = blob.TxHash
	return nil
}
