package vdn

import (
	"context"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// Send a message to a specific peer. Msg will be encoded to RLP to remote peer.
func (s *Server) Send(ctx context.Context, message interface{}, topic string, pid peer.ID) (network.Stream, error) {
	log.Debug("Sending RPC request to peer", "peer", pid, "topic", topic)
	// Apply max dial timeout when opening a new stream.
	ctx, cancel := context.WithTimeout(ctx, RespTimeout)
	defer cancel()

	stream, err := s.host.NewStream(ctx, pid, protocol.ID(topic))
	if err != nil {
		return nil, err
	}
	if err := rlp.Encode(stream, message); err != nil {
		_err := stream.Reset()
		_ = _err
		return nil, err
	}

	// Close stream for writing.
	if err := stream.CloseWrite(); err != nil {
		_err := stream.Reset()
		_ = _err
		return nil, err
	}

	return stream, nil
}

// SetStreamHandler sets the protocol handler on the p2p host multiplexer.
// This method is a pass through to libp2pcore.Host.SetStreamHandler.
func (s *Server) SetStreamHandler(topic string, handler network.StreamHandler) {
	s.host.SetStreamHandler(protocol.ID(topic), handler)
}
