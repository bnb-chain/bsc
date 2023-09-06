// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package diff

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/sha3"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// Constants to match up protocol versions and messages
const (
	Diff1 = 1
)

// ProtocolName is the official short name of the `diff` protocol used during
// devp2p capability negotiation.
const ProtocolName = "diff"

// ProtocolVersions are the supported versions of the `diff` protocol (first
// is primary).
var ProtocolVersions = []uint{Diff1}

// protocolLengths are the number of implemented message corresponding to
// different protocol versions.
var protocolLengths = map[uint]uint64{Diff1: 4}

// maxMessageSize is the maximum cap on the size of a protocol message.
const maxMessageSize = 10 * 1024 * 1024

const (
	DiffCapMsg       = 0x00
	GetDiffLayerMsg  = 0x01
	DiffLayerMsg     = 0x02
	FullDiffLayerMsg = 0x03
)

var defaultExtra = []byte{0x00}

var (
	errMsgTooLarge    = errors.New("message too long")
	errDecode         = errors.New("invalid message")
	errInvalidMsgCode = errors.New("invalid message code")
	errUnexpectedMsg  = errors.New("unexpected message code")
	errNoCapMsg       = errors.New("miss cap message during handshake")
)

// Packet represents a p2p message in the `diff` protocol.
type Packet interface {
	Name() string // Name returns a string corresponding to the message type.
	Kind() byte   // Kind returns the message type.
}

type GetDiffLayersPacket struct {
	RequestId   uint64
	BlockHashes []common.Hash
}

func (p *DiffLayersPacket) Unpack() ([]*types.DiffLayer, error) {
	diffLayers := make([]*types.DiffLayer, 0, len(*p))
	hasher := sha3.NewLegacyKeccak256()
	for _, rawData := range *p {
		var diff types.DiffLayer
		err := rlp.DecodeBytes(rawData, &diff)
		if err != nil {
			return nil, fmt.Errorf("%w: diff layer %v", errDecode, err)
		}
		diffLayers = append(diffLayers, &diff)
		_, err = hasher.Write(rawData)
		if err != nil {
			return nil, err
		}
		var diffHash common.Hash
		hasher.Sum(diffHash[:0])
		hasher.Reset()
		diff.DiffHash.Store(diffHash)
	}
	return diffLayers, nil
}

type DiffCapPacket struct {
	DiffSync bool
	Extra    rlp.RawValue // for extension
}

type DiffLayersPacket []rlp.RawValue

type FullDiffLayersPacket struct {
	RequestId uint64
	DiffLayersPacket
}

func (*GetDiffLayersPacket) Name() string { return "GetDiffLayers" }
func (*GetDiffLayersPacket) Kind() byte   { return GetDiffLayerMsg }

func (*DiffLayersPacket) Name() string { return "DiffLayers" }
func (*DiffLayersPacket) Kind() byte   { return DiffLayerMsg }

func (*FullDiffLayersPacket) Name() string { return "FullDiffLayers" }
func (*FullDiffLayersPacket) Kind() byte   { return FullDiffLayerMsg }

func (*DiffCapPacket) Name() string { return "DiffCap" }
func (*DiffCapPacket) Kind() byte   { return DiffCapMsg }
