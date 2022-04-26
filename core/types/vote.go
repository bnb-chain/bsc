package types

import (
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/crypto/bls"

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
	SourceNumber uint64
	SourceHash   common.Hash
	TargetNumber uint64
	TargetHash   common.Hash
}

func (d *VoteData) Hash() common.Hash { return rlpHash(d) }

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

// Hash returns the vote's hash.
func (v *VoteEnvelope) Hash() common.Hash {
	if hash := v.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}

	h := v.calcVoteHash()
	v.hash.Store(h)
	return h
}

func (v *VoteEnvelope) calcVoteHash() common.Hash {
	vote := struct {
		VoteAddress BLSPublicKey
		Signature   BLSSignature
		Data        *VoteData
	}{v.VoteAddress, v.Signature, v.Data}
	return rlpHash(vote)
}

func (b BLSPublicKey) Bytes() []byte { return b[:] }

// Verify vote using BLS.
func (vote *VoteEnvelope) Verify() error {
	blsPubKey, err := bls.PublicKeyFromBytes(vote.VoteAddress[:])
	if err != nil {
		return errors.Wrap(err, "convert public key from bytes to bls failed")
	}

	sig, err := bls.SignatureFromBytes(vote.Signature[:])
	if err != nil {
		return errors.Wrap(err, "invalid signature")
	}

	voteDataHash := vote.Data.Hash()
	if !sig.Verify(blsPubKey, voteDataHash[:]) {
		return errors.New("verify bls signature failed.")
	}
	return nil
}
