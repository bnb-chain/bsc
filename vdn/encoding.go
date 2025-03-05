package vdn

import (
	"bytes"
	"io"

	"github.com/ethereum/go-ethereum/rlp"
)

// EncodeToStream encode any msg by rlp
func EncodeToStream(msg interface{}, writer io.Writer) error {
	return rlp.Encode(writer, msg)
}

// DecodeFromStream msg must be a pointer
func DecodeFromStream(msg interface{}, reader io.Reader) error {
	return rlp.Decode(reader, msg)
}

// EncodeToBytes encode any msg by rlp
func EncodeToBytes(msg interface{}) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := rlp.Encode(buf, msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeFromBytes msg must be a pointer
func DecodeFromBytes(msg interface{}, data []byte) error {
	return rlp.DecodeBytes(data, msg)
}
