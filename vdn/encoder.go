package vdn

import (
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
