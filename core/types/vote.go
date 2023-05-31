package types

import (
	"bytes"
	"math/big"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v4/crypto/bls"

	"github.com/ethereum/go-ethereum/common"
)

const (
	BLSPublicKeyLength = 48
	BLSSignatureLength = 96

	MaxAttestationExtraLength = 256
)

type BLSPublicKey [BLSPublicKeyLength]byte
type BLSSignature [BLSSignatureLength]byte
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

// VoteAttestation represents the votes of super majority validators.
type VoteAttestation struct {
	VoteAddressSet ValidatorsBitSet // The bitset marks the voted validators.
	AggSignature   BLSSignature     // The aggregated BLS signature of the voted validators' signatures.
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
	if !bytes.Equal(vote1.VoteAddress[:], vote1.VoteAddress[:]) ||
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
