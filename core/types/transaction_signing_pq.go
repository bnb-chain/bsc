package types

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var pqRegistryBackend func(addr common.Address) []byte

func SetPQRegistryBackend(backend func(addr common.Address) []byte) func() {
	prev := pqRegistryBackend
	pqRegistryBackend = backend
	return func() {
		pqRegistryBackend = prev
	}
}

// pqDispatchSigner wraps any existing Signer and additionally handles PQTxType
// transactions by delegating to PQSigner. Used by MakeSigner when the PQ fork
// is active so that the rest of the codebase (state processor, tx pool) does not
// need to be aware of the PQ signer directly.
type pqDispatchSigner struct {
	Signer
	pq PQSigner
}

// NewPQDispatchSigner wraps base with PQ dispatch for the given chainID.
func NewPQDispatchSigner(base Signer, chainID *big.Int) Signer {
	return &pqDispatchSigner{Signer: base, pq: NewPQSigner(chainID)}
}

func (s *pqDispatchSigner) Sender(tx *Transaction) (common.Address, error) {
	if tx.Type() == PQTxType {
		return s.pq.Sender(tx)
	}
	return s.Signer.Sender(tx)
}

func (s *pqDispatchSigner) Hash(tx *Transaction) common.Hash {
	if tx.Type() == PQTxType {
		return s.pq.Hash(tx)
	}
	return s.Signer.Hash(tx)
}

func (s *pqDispatchSigner) SignatureValues(tx *Transaction, sig []byte) (r, ss, v *big.Int, err error) {
	if tx.Type() == PQTxType {
		return s.pq.SignatureValues(tx, sig)
	}
	return s.Signer.SignatureValues(tx, sig)
}

func (s *pqDispatchSigner) Equal(s2 Signer) bool {
	other, ok := s2.(*pqDispatchSigner)
	if !ok {
		return false
	}
	return s.Signer.Equal(other.Signer) && s.pq.Equal(other.pq)
}

func (s *pqDispatchSigner) ChainID() *big.Int { return s.pq.chainID }

type PQSigner struct {
	chainID *big.Int
}

func NewPQSigner(chainID *big.Int) PQSigner {
	if chainID == nil {
		chainID = new(big.Int)
	}
	return PQSigner{chainID: new(big.Int).Set(chainID)}
}

func (s PQSigner) ChainID() *big.Int {
	return s.chainID
}

func (s PQSigner) Equal(s2 Signer) bool {
	switch other := s2.(type) {
	case PQSigner:
		return s.chainID.Cmp(other.chainID) == 0
	case *PQSigner:
		return other != nil && s.chainID.Cmp(other.chainID) == 0
	default:
		return false
	}
}

func (s PQSigner) Hash(tx *Transaction) common.Hash {
	pqtx, ok := tx.inner.(*PQTxData)
	if !ok {
		return common.Hash{}
	}
	return pqtx.sigHash(s.chainID)
}

func (s PQSigner) Sender(tx *Transaction) (common.Address, error) {
	pqtx, ok := tx.inner.(*PQTxData)
	if !ok {
		return common.Address{}, ErrTxTypeNotSupported
	}
	if pqtx.ChainID != nil && pqtx.ChainID.Sign() != 0 && pqtx.ChainID.Cmp(s.chainID) != 0 {
		return common.Address{}, fmt.Errorf("%w: have %d want %d", ErrInvalidChainId, pqtx.ChainID, s.chainID)
	}

	pubKey := pqKeyRegistryLookup(pqtx.From)
	if len(pubKey) != mldsa44.PublicKeySize || isZeroPQPubKey(pubKey) {
		return common.Address{}, errors.New("sender not registered")
	}

	hash := s.Hash(tx)
	if !crypto.VerifyPQ(pubKey, hash[:], pqtx.PQSignature) {
		return common.Address{}, errors.New("invalid pq signature")
	}
	return pqtx.From, nil
}

func (s PQSigner) SignatureValues(tx *Transaction, sig []byte) (r, ss, v *big.Int, err error) {
	pqtx, ok := tx.inner.(*PQTxData)
	if !ok {
		return nil, nil, nil, ErrTxTypeNotSupported
	}
	if pqtx.ChainID != nil && pqtx.ChainID.Sign() != 0 && pqtx.ChainID.Cmp(s.chainID) != 0 {
		return nil, nil, nil, fmt.Errorf("%w: have %d want %d", ErrInvalidChainId, pqtx.ChainID, s.chainID)
	}
	return new(big.Int), new(big.Int), new(big.Int), nil
}

func SignPQTx(tx *Transaction, s PQSigner, privKey []byte) (*Transaction, error) {
	pqtx, ok := tx.inner.(*PQTxData)
	if !ok {
		return nil, ErrTxTypeNotSupported
	}
	if pqtx.ChainID != nil && pqtx.ChainID.Sign() != 0 && pqtx.ChainID.Cmp(s.chainID) != 0 {
		return nil, fmt.Errorf("%w: have %d want %d", ErrInvalidChainId, pqtx.ChainID, s.chainID)
	}

	hash := s.Hash(tx)
	sig, err := crypto.SignPQ(hash[:], privKey)
	if err != nil {
		return nil, err
	}
	if len(sig) != mldsa44.SignatureSize {
		return nil, fmt.Errorf("invalid pq signature size: have %d want %d", len(sig), mldsa44.SignatureSize)
	}

	pubKey, err := pqPublicKeyFromPrivate(privKey)
	if err != nil {
		return nil, err
	}
	if len(pubKey) != mldsa44.PublicKeySize {
		return nil, fmt.Errorf("invalid pq public key size: have %d want %d", len(pubKey), mldsa44.PublicKeySize)
	}

	cpy, ok := tx.inner.copy().(*PQTxData)
	if !ok {
		return nil, ErrTxTypeNotSupported
	}
	cpy.From = crypto.PQPubkeyToAddress(pubKey)
	cpy.PQSignature = sig
	if cpy.ChainID == nil {
		cpy.ChainID = new(big.Int)
	}
	cpy.ChainID.Set(s.chainID)

	return &Transaction{inner: cpy, time: tx.time}, nil
}

func pqPublicKeyFromPrivate(privKey []byte) ([]byte, error) {
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

func pqKeyRegistryLookup(addr common.Address) []byte {
	if pqRegistryBackend == nil {
		return nil
	}
	return common.CopyBytes(pqRegistryBackend(addr))
}

func isZeroPQPubKey(pubKey []byte) bool {
	for _, b := range pubKey {
		if b != 0 {
			return false
		}
	}
	return true
}
