package types

import "github.com/ethereum/go-ethereum/common"

const (
	BLSPublicKeyLength = 48
	BLSSignatureLength = 96
)

type BLSPublicKey [BLSPublicKeyLength]byte
type BLSSignature [BLSSignatureLength]byte

type VoteData struct {
	BlockNumber uint64
	BlockHash   common.Hash
}

type VoteRecord struct {
	VoteAddress BLSPublicKey
	Signature   BLSSignature
	Data        VoteData
}

type VoteRecords []VoteRecord

func (v *VoteRecord) Hash() common.Hash {
	return rlpHash(v)
}
