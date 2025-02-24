package vdn

import (
	"crypto/ecdsa"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
	pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/pkg/errors"
)

func LoadPrivateKey(keyPath string) (*ecdsa.PrivateKey, error) {
	// try load key from datadir first, create a new key when there is no key found
	// save the new key in the last
	_, err := os.Stat(keyPath)
	if !os.IsNotExist(err) {
		return crypto.LoadECDSA(keyPath)
	}
	priv, err := crypto.GenerateKey()
	if err != nil {
		return nil, errors.Wrapf(err, "GenerateKey err")
	}
	if err := crypto.SaveECDSA(keyPath, priv); err != nil {
		return nil, errors.Wrapf(err, "SaveKey err")
	}
	return priv, nil
}

func ConvertToInterfacePrivkey(privkey *ecdsa.PrivateKey) (pcrypto.PrivKey, error) {
	privBytes := privkey.D.Bytes()
	// In the event the number of bytes outputted by the big-int are less than 32,
	// we append bytes to the start of the sequence for the missing most significant
	// bytes.
	if len(privBytes) < 32 {
		privBytes = append(make([]byte, 32-len(privBytes)), privBytes...)
	}
	return pcrypto.UnmarshalSecp256k1PrivateKey(privBytes)
}
