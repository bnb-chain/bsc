package xmss

import "errors"

var errNotImplemented = errors.New("not implemented")

func Sign(privKey []byte, msg []byte) (sig []byte, err error) {
	return nil, errNotImplemented
}

func Aggregate(sigs [][]byte, pubKeys [][]byte, msg []byte) (proof []byte, err error) {
	return nil, errNotImplemented
}

func VerifyProof(proof []byte, pubKeys [][]byte, msg []byte) bool {
	return false
}
