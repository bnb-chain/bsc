package vdn

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/network"

	"github.com/ethereum/go-ethereum/log"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// Send a message to a specific peer. Msg will be encoded to RLP to remote peer.
func (s *Server) Send(msg interface{}, topic string, pid peer.ID, callback HandleRespFn) (err error) {
	log.Debug("send msg to peer", "peer", pid, "topic", topic)
	// Apply max dial timeout when opening a new stream.
	ctx, cancel := context.WithTimeout(context.Background(), RespTimeout)
	defer cancel()

	var (
		stream network.Stream
	)
	stream, err = s.host.NewStream(ctx, pid, protocol.ID(topic))
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			log.Debug("send msg err", "peer", pid, "topic", topic, "err", err)
			stream.Reset()
			return
		}
		stream.Close()
	}()

	if err = EncodeToStream(msg, stream); err != nil {
		return
	}
	// Close stream for writing.
	if err = stream.CloseWrite(); err != nil {
		return
	}

	if err = callback(stream); err != nil {
		//TODO(galaio): biz logic err, using it for peer scoring
		return
	}
	return
}

// SetMsgHandler sets the protocol handler on the p2p host multiplexer.
// This method is a pass through to libp2pcore.Host.SetStreamHandler.
func (s *Server) SetMsgHandler(topic string, callback HandleMsgFn) {
	s.host.SetStreamHandler(protocol.ID(topic), func(stream network.Stream) {
		var (
			err  error
			conn = stream.Conn()
			pid  = conn.RemotePeer()
		)
		log.Debug("handle msg from peer", "peer", pid, "topic", topic)
		defer func() {
			if err != nil {
				log.Debug("handle msg err", "peer", pid, "topic", topic, "err", err)
				stream.Reset()
				return
			}
			stream.Close()
		}()

		if err = stream.SetDeadline(time.Now().Add(ttfbTimeout)); err != nil {
			return
		}

		if err = callback(pid, stream); err != nil {
			//TODO(galaio): biz logic err, using it for peer scoring
			return
		}
	})
}
