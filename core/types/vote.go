package types

import (
	"github.com/ethereum/go-ethereum/common"
)

const (
	BLSPublicKeyLength = 48
	BLSSignatureLength = 96

	MaxDiffForkDist = 11 // Maximum allowed backward distance from the chain head
)

type BLSPublicKey [BLSPublicKeyLength]byte
type BLSSignature [BLSSignatureLength]byte
type ValidatorsBitSet uint64

// Bytes gets the string representation of the underlying BLS public key.
func (p BLSPublicKey) Bytes() []byte { return p[:] }

type VoteData struct {
	BlockNumber uint64
	BlockHash   common.Hash
}

type VoteRecord struct {
	VoteAddress BLSPublicKey
	Signature   BLSSignature
	Data        VoteData
}

type AggVoteRecord struct {
	VoteAddressSet ValidatorsBitSet
	AggSignature   BLSSignature
	Data           VoteData
	Extra          []byte
}
