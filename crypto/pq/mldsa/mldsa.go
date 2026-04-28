package mldsa

import (
	"errors"
	"fmt"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/sha3"
)

func GenerateKey() (pubKey []byte, privKey []byte, err error) {
	pub, priv, err := mldsa44.GenerateKey(nil)
	if err != nil {
		return nil, nil, err
	}
	return pub.Bytes(), priv.Bytes(), nil
}

func GenerateKeyFromSeed(seed []byte) (pubKey []byte, privKey []byte, err error) {
	if len(seed) != mldsa44.SeedSize {
		return nil, nil, fmt.Errorf("invalid seed length: have %d want %d", len(seed), mldsa44.SeedSize)
	}
	var fixedSeed [mldsa44.SeedSize]byte
	copy(fixedSeed[:], seed)
	pub, priv := mldsa44.NewKeyFromSeed(&fixedSeed)
	return pub.Bytes(), priv.Bytes(), nil
}

func Sign(privKey []byte, digest []byte) (sig []byte, err error) {
	var key mldsa44.PrivateKey
	if err := key.UnmarshalBinary(privKey); err != nil {
		return nil, err
	}
	return key.Sign(nil, digest, nil)
}

func Verify(pubKey []byte, digest []byte, sig []byte) bool {
	var key mldsa44.PublicKey
	if err := key.UnmarshalBinary(pubKey); err != nil {
		return false
	}
	return mldsa44.Verify(&key, digest, nil, sig)
}

func PublicKeyFromPrivate(privKey []byte) ([]byte, error) {
	var key mldsa44.PrivateKey
	if err := key.UnmarshalBinary(privKey); err != nil {
		return nil, err
	}
	pubKey, ok := key.Public().(*mldsa44.PublicKey)
	if !ok {
		return nil, errors.New("invalid pq public key type")
	}
	return pubKey.Bytes(), nil
}

func PubKeyToAddress(pubKey []byte) common.Address {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write(pubKey)

	sum := make([]byte, 32)
	hasher.Sum(sum[:0])
	return common.BytesToAddress(sum[12:])
}
