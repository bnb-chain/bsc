package types

import (
	"sync/atomic"

	"github.com/prysmaticlabs/prysm/crypto/bls"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	BLSPublicKeyLength = 48
	BLSSignatureLength = 96
)

type BLSPublicKey [BLSPublicKeyLength]byte
type BLSSignature bls.Signature
type ValidatorsBitSet uint64

type VoteData struct {
	BlockNumber uint64
	BlockHash   common.Hash
}

type VoteEnvelope struct {
	VoteAddress BLSPublicKey
	Signature   BLSSignature
	Data        *VoteData

	// caches
	hash atomic.Value
}

type VoteAttestation struct {
	VoteAddressSet ValidatorsBitSet
	AggSignature   BLSSignature
	Data           *VoteData
	Extra          []byte
}

type VoteEnvelopes []*VoteEnvelope

// Hash returns the vote hash.
func (v *VoteEnvelope) Hash() common.Hash {
	if hash := v.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}

	h := v.CalcVoteHash()
	v.hash.Store(h)
	return h
}

func (v *VoteEnvelope) CalcVoteHash() common.Hash {
	voteData := struct {
		VoteAddress BLSPublicKey
		Signature   BLSSignature
		Data        *VoteData
	}{v.VoteAddress, v.Signature, v.Data}
	return rlpHash(voteData)
}

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
		Data        *VoteData
	}{v.VoteAddress, v.Signature, v.Data}
	return rlpHash(voteData)
}

func (b BLSPublicKey) Bytes() []byte { return b[:] }

func (b BLSSignature) Bytes() []byte { return b[:] }
