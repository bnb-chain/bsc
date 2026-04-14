package mlkem

import "github.com/cloudflare/circl/kem/mlkem/mlkem768"

func GenerateKey() (encapKey []byte, decapKey []byte, err error) {
	pub, priv, err := mlkem768.GenerateKeyPair(nil)
	if err != nil {
		return nil, nil, err
	}
	encapKey, err = pub.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	decapKey, err = priv.MarshalBinary()
	if err != nil {
		return nil, nil, err
	}
	return encapKey, decapKey, nil
}

func Encapsulate(encapKey []byte) (ciphertext []byte, sharedSecret []byte, err error) {
	scheme := mlkem768.Scheme()
	pub, err := scheme.UnmarshalBinaryPublicKey(encapKey)
	if err != nil {
		return nil, nil, err
	}
	return scheme.Encapsulate(pub)
}

func Decapsulate(decapKey []byte, ciphertext []byte) (sharedSecret []byte, err error) {
	scheme := mlkem768.Scheme()
	priv, err := scheme.UnmarshalBinaryPrivateKey(decapKey)
	if err != nil {
		return nil, err
	}
	return scheme.Decapsulate(priv, ciphertext)
}
