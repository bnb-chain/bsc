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

type VoteEnvelopes []*VoteEnvelope

// Hash returns the vote hash.
func (v *VoteEnvelope) Hash() common.Hash {
	if hash := v.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}

	h := v.calcVoteHash()
	v.hash.Store(h)
	return h
}

func (v *VoteEnvelope) calcVoteHash() common.Hash {
	voteData := struct {
		VoteAddress BLSPublicKey
		Signature   BLSSignature
		Data        VoteData
	}{v.VoteAddress, v.Signature, v.Data}
	return rlpHash(voteData)
}

type VoteAttestation struct {
	VoteAddressSet ValidatorsBitSet
	AggSignature   BLSSignature
	Data           VoteData
	Extra          []byte
}
