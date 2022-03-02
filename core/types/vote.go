package types

import (
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
)

const (
	BLSPublicKeyLength = 48
	BLSSignatureLength = 96
)

type BLSPublicKey [BLSPublicKeyLength]byte
type BLSSignature [BLSSignatureLength]byte
type ValidatorsBitSet uint64

type VoteData struct {
	BlockNumber uint64
	BlockHash   common.Hash
}

type VoteEnvelope struct {
	VoteAddress BLSPublicKey
	Signature   BLSSignature
	Data        VoteData

	// caches
	hash atomic.Value
}

type AggVoteEnvelope struct {
	VoteAddressSet ValidatorsBitSet
	AggSignature   BLSSignature
	Data           VoteData
	Extra          []byte
}
