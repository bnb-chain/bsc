package rlpx

import (
	"crypto/aes"
	"crypto/cipher"
	"testing"

	mopenssl "github.com/microsoft/go-crypto-openssl/openssl"
)

func BenchmarkOpenssl(t *testing.B) {
	mopenssl.Init()
	msg := make([]byte, 10240)
	for i := 0; i < 10240; i++ {
		msg[i] = 1
	}

	key := []byte("1234567891234567")
	enc := make([]byte, len(msg))
	dec := make([]byte, len(msg))

	c, _ := mopenssl.NewAESCipher(key)
	streamEnc := c.(extraModes).NewCTR(key)
	streamDec := c.(extraModes).NewCTR(key)
	for i := 0; i < t.N; i++ {
		streamEnc.XORKeyStream(enc[:], msg)
		streamDec.XORKeyStream(dec[:], enc)
	}
}

func BenchmarkAes(t *testing.B) {
	mopenssl.Init()
	msg := make([]byte, 10240)
	for i := 0; i < 10240; i++ {
		msg[i] = 1
	}

	key := []byte("1234567891234567")
	enc := make([]byte, len(msg))
	dec := make([]byte, len(msg))

	c, _ := aes.NewCipher(key)
	streamEnc := cipher.NewCTR(c, key)
	streamDec := cipher.NewCTR(c, key)
	for i := 0; i < t.N; i++ {
		streamEnc.XORKeyStream(enc[:], msg)
		streamDec.XORKeyStream(dec[:], enc)
	}
}