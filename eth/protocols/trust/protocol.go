package trust

import (
	"errors"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

// Constants to match up protocol versions and messages
const (
	Trust1 = 1
)

// ProtocolName is the official short name of the `trust` protocol used during
// devp2p capability negotiation.
const ProtocolName = "trust"

// ProtocolVersions are the supported versions of the `trust` protocol (first
// is primary).
var ProtocolVersions = []uint{Trust1}

// protocolLengths are the number of implemented message corresponding to
// different protocol versions.
var protocolLengths = map[uint]uint64{Trust1: 2}

// maxMessageSize is the maximum cap on the size of a protocol message.
const maxMessageSize = 10 * 1024 * 1024

const (
	RequestRootMsg = 0x00
	RespondRootMsg = 0x01
)

var defaultExtra = []byte{0x00}

var (
	errMsgTooLarge    = errors.New("message too long")
	errDecode         = errors.New("invalid message")
	errInvalidMsgCode = errors.New("invalid message code")
)

// Packet represents a p2p message in the `trust` protocol.
type Packet interface {
	Name() string // Name returns a string corresponding to the message type.
	Kind() byte   // Kind returns the message type.
}

type RootRequestPacket struct {
	RequestId   uint64
	BlockNumber uint64
	BlockHash   common.Hash
	DiffHash    common.Hash
}

type RootResponsePacket struct {
	RequestId   uint64
	Status      types.VerifyStatus
	BlockNumber uint64
	BlockHash   common.Hash
	Root        common.Hash
	Extra       rlp.RawValue // for extension
}

func (*RootRequestPacket) Name() string { return "RequestRoot" }
func (*RootRequestPacket) Kind() byte   { return RequestRootMsg }

func (*RootResponsePacket) Name() string { return "RootResponse" }
func (*RootResponsePacket) Kind() byte   { return RespondRootMsg }
