package types

import (
	"bytes"
	"math/big"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto/pq/mldsa"
)

const (
	BLSPublicKeyLength = 48
	BLSSignatureLength = 96

	MaxAttestationExtraLength = 256
)

const (
	PQPublicKeyLength = 1312
	PQSignatureLength = 2420
)

type BLSPublicKey [BLSPublicKeyLength]byte
type BLSSignature [BLSSignatureLength]byte
type PQPublicKey [PQPublicKeyLength]byte
type PQSignature [PQSignatureLength]byte
type ValidatorsBitSet uint64

// VoteData represents the vote range that validator voted for fast finality.
type VoteData struct {
	SourceNumber uint64      // The source block number should be the latest justified block number.
	SourceHash   common.Hash // The block hash of the source block.
	TargetNumber uint64      // The target block number which validator wants to vote for.
	TargetHash   common.Hash // The block hash of the target block.
}

// Hash returns the hash of the vote data.
func (d *VoteData) Hash() common.Hash { return rlpHash(d) }

// VoteEnvelope represents the vote of a single validator.
type VoteEnvelope struct {
	VoteAddress BLSPublicKey // The BLS public key of the validator.
	Signature   BLSSignature // Validator's signature for the vote data.
	Data        *VoteData    // The vote data for fast finality.

	// caches
	hash atomic.Value
}

// PQVoteEnvelope represents the vote of a single validator using post-quantum signatures.
type PQVoteEnvelope struct {
	VoteAddress PQPublicKey // The ML-DSA-44 public key of the validator.
	Signature   PQSignature // Validator's ML-DSA-44 signature for the vote data.
	Data        *VoteData   // The vote data for fast finality.

	// caches
	hash atomic.Value
}

// VoteAttestation represents the votes of super majority validators.
type VoteAttestation struct {
	VoteAddressSet ValidatorsBitSet // The bitset marks the voted validators.
	AggSignature   BLSSignature     // The aggregated BLS signature of the voted validators' signatures.
	Data           *VoteData        // The vote data for fast finality.
	Extra          []byte           // Reserved for future usage.
}

// PQVoteAttestation represents the votes of super majority validators using STARK aggregation.
type PQVoteAttestation struct {
	VoteAddressSet ValidatorsBitSet // The bitset marks the voted validators.
	AggProof       []byte           // The STARK aggregate proof replacing BLS aggregate signature.
	Data           *VoteData        // The vote data for fast finality.
	Extra          []byte           // Reserved for future usage.
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
func (b PQPublicKey) Bytes() []byte  { return b[:] }

// Hash returns the PQ vote's hash.
func (v *PQVoteEnvelope) Hash() common.Hash {
	if hash := v.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	h := v.calcVoteHash()
	v.hash.Store(h)
	return h
}

func (v *PQVoteEnvelope) calcVoteHash() common.Hash {
	vote := struct {
		VoteAddress PQPublicKey
		Signature   PQSignature
		Data        *VoteData
	}{v.VoteAddress, v.Signature, v.Data}
	return rlpHash(vote)
}

// Verify vote using BLS.
func (v *VoteEnvelope) Verify() error {
	blsPubKey, err := bls.PublicKeyFromBytes(v.VoteAddress[:])
	if err != nil {
		return errors.Wrap(err, "convert public key from bytes to bls failed")
	}

	sig, err := bls.SignatureFromBytes(v.Signature[:])
	if err != nil {
		return errors.Wrap(err, "invalid signature")
	}

	voteDataHash := v.Data.Hash()
	if !sig.Verify(blsPubKey, voteDataHash[:]) {
		return errors.New("verify bls signature failed.")
	}
	return nil
}

// Verify verifies the PQ vote using ML-DSA-44.
func (v *PQVoteEnvelope) Verify() error {
	voteDataHash := v.Data.Hash()
	if !mldsa.Verify(v.VoteAddress[:], voteDataHash[:], v.Signature[:]) {
		return errors.New("verify ML-DSA-44 signature failed")
	}
	return nil
}

type SlashIndicatorVoteDataWrapper struct {
	SrcNum  *big.Int
	SrcHash string
	TarNum  *big.Int
	TarHash string
	Sig     string
}

type SlashIndicatorFinalityEvidenceWrapper struct {
	VoteA    SlashIndicatorVoteDataWrapper
	VoteB    SlashIndicatorVoteDataWrapper
	VoteAddr string
}

func NewSlashIndicatorFinalityEvidenceWrapper(vote1, vote2 *VoteEnvelope) *SlashIndicatorFinalityEvidenceWrapper {
	if !bytes.Equal(vote1.VoteAddress[:], vote2.VoteAddress[:]) ||
		vote1.Data == nil || vote2.Data == nil {
		return nil
	}
	return &SlashIndicatorFinalityEvidenceWrapper{
		VoteA: SlashIndicatorVoteDataWrapper{
			SrcNum:  big.NewInt(int64(vote1.Data.SourceNumber)),
			SrcHash: common.Bytes2Hex(vote1.Data.SourceHash[:]),
			TarNum:  big.NewInt(int64(vote1.Data.TargetNumber)),
			TarHash: common.Bytes2Hex(vote1.Data.TargetHash[:]),
			Sig:     common.Bytes2Hex(vote1.Signature[:]),
		},
		VoteB: SlashIndicatorVoteDataWrapper{
			SrcNum:  big.NewInt(int64(vote2.Data.SourceNumber)),
			SrcHash: common.Bytes2Hex(vote2.Data.SourceHash[:]),
			TarNum:  big.NewInt(int64(vote2.Data.TargetNumber)),
			TarHash: common.Bytes2Hex(vote2.Data.TargetHash[:]),
			Sig:     common.Bytes2Hex(vote2.Signature[:]),
		},
		VoteAddr: common.Bytes2Hex(vote1.VoteAddress[:]),
	}
}
