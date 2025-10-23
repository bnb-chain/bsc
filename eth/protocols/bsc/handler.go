package bsc

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
)

const MaxRequestRangeBlocksCount = 64

// Handler is a callback to invoke from an outside runner after the boilerplate
// exchanges have passed.
type Handler func(peer *Peer) error

type Backend interface {
	// Chain retrieves the blockchain object to serve data.
	Chain() *core.BlockChain

	// RunPeer is invoked when a peer joins on the `bsc` protocol. The handler
	// should do any peer maintenance work, handshakes and validations. If all
	// is passed, control should be given back to the `handler` to process the
	// inbound messages going forward.
	RunPeer(peer *Peer, handler Handler) error

	// PeerInfo retrieves all known `bsc` information about a peer.
	PeerInfo(id enode.ID) interface{}

	// Handle is a callback to be invoked when a data packet is received from
	// the remote peer. Only packets not consumed by the protocol handler will
	// be forwarded to the backend.
	Handle(peer *Peer, packet Packet) error
}

// MakeProtocols constructs the P2P protocol definitions for `bsc`.
func MakeProtocols(backend Backend) []p2p.Protocol {
	protocols := make([]p2p.Protocol, len(ProtocolVersions))
	for i, version := range ProtocolVersions {
		protocols[i] = p2p.Protocol{
			Name:    ProtocolName,
			Version: version,
			Length:  protocolLengths[version],
			Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
				peer := NewPeer(version, p, rw)
				defer peer.Close()

				return backend.RunPeer(peer, func(peer *Peer) error {
					return Handle(backend, peer)
				})
			},
			NodeInfo: func() interface{} {
				return nodeInfo(backend.Chain())
			},
			PeerInfo: func(id enode.ID) interface{} {
				return backend.PeerInfo(id)
			},
			Attributes: []enr.Entry{&enrEntry{}},
		}
	}
	return protocols
}

// Handle is the callback invoked to manage the life cycle of a `bsc` peer.
// When this function terminates, the peer is disconnected.
func Handle(backend Backend, peer *Peer) error {
	for {
		if err := handleMessage(backend, peer); err != nil {
			peer.Log().Debug("Message handling failed in `bsc`", "err", err)
			return err
		}
	}
}

type msgHandler func(backend Backend, msg Decoder, peer *Peer) error
type Decoder interface {
	Decode(val interface{}) error
}

var bsc1 = map[uint64]msgHandler{
	VotesMsg: handleVotes,
}

var bsc2 = map[uint64]msgHandler{
	VotesMsg:            handleVotes,
	GetBlocksByRangeMsg: handleGetBlocksByRange,
	BlocksByRangeMsg:    handleBlocksByRange,
}

// handleMessage is invoked whenever an inbound message is received from a
// remote peer on the `bsc` protocol. The remote connection is torn down upon
// returning any error.
func handleMessage(backend Backend, peer *Peer) error {
	// Read the next message from the remote peer, and ensure it's fully consumed
	msg, err := peer.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Size > maxMessageSize {
		return fmt.Errorf("%w: %v > %v", errMsgTooLarge, msg.Size, maxMessageSize)
	}
	defer msg.Discard()

	var handlers = bsc1
	if peer.Version() >= Bsc2 {
		handlers = bsc2
	}

	// Track the amount of time it takes to serve the request and run the handler
	if metrics.Enabled() {
		h := fmt.Sprintf("%s/%s/%d/%#02x", p2p.HandleHistName, ProtocolName, peer.Version(), msg.Code)
		defer func(start time.Time) {
			sampler := func() metrics.Sample {
				return metrics.ResettingSample(
					metrics.NewExpDecaySample(1028, 0.015),
				)
			}
			metrics.GetOrRegisterHistogramLazy(h, nil, sampler).Update(time.Since(start).Microseconds())
		}(time.Now())
	}
	if handler := handlers[msg.Code]; handler != nil {
		return handler(backend, msg, peer)
	}
	return fmt.Errorf("%w: %v", errInvalidMsgCode, msg.Code)
}

func handleVotes(backend Backend, msg Decoder, peer *Peer) error {
	ann := new(VotesPacket)
	if err := msg.Decode(ann); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	// Schedule all the unknown hashes for retrieval
	peer.markVotes(ann.Votes)
	return backend.Handle(peer, ann)
}

func handleGetBlocksByRange(backend Backend, msg Decoder, peer *Peer) error {
	req := new(GetBlocksByRangePacket)
	if err := msg.Decode(req); err != nil {
		return fmt.Errorf("msg %v, decode err: %v", GetBlocksByRangeMsg, err)
	}

	log.Debug("receive GetBlocksByRange request", "from", peer.id, "req", req)
	// Validate request parameters
	if req.Count == 0 || req.Count > MaxRequestRangeBlocksCount { // Limit maximum request count
		return fmt.Errorf("msg %v, invalid count: %v", GetBlocksByRangeMsg, req.Count)
	}

	// Get requested blocks
	blocks := make([]*BlockData, 0, req.Count)
	var block *types.Block
	// Prioritize blockHash query, get block & sidecars from db
	if req.StartBlockHash != (common.Hash{}) {
		block = backend.Chain().GetBlockByHash(req.StartBlockHash)
	} else {
		block = backend.Chain().GetBlockByNumber(req.StartBlockHeight)
	}
	if block == nil {
		return fmt.Errorf("msg %v, cannot get start block: %v, %v", GetBlocksByRangeMsg, req.StartBlockHeight, req.StartBlockHash)
	}
	blocks = append(blocks, NewBlockData(block))
	for i := uint64(1); i < req.Count; i++ {
		block = backend.Chain().GetBlockByHash(block.ParentHash())
		if block == nil {
			break
		}
		blocks = append(blocks, NewBlockData(block))
	}

	log.Debug("reply GetBlocksByRange msg", "from", peer.id, "req", req.Count, "blocks", len(blocks))
	return p2p.Send(peer.rw, BlocksByRangeMsg, &BlocksByRangePacket{
		RequestId: req.RequestId,
		Blocks:    blocks,
	})
}

func handleBlocksByRange(backend Backend, msg Decoder, peer *Peer) error {
	res := new(BlocksByRangePacket)
	if err := msg.Decode(res); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}

	err := peer.dispatcher.DispatchResponse(&Response{
		requestID: res.RequestId,
		data:      res,
		code:      BlocksByRangeMsg,
	})
	log.Debug("receive BlocksByRange response", "from", peer.id, "requestId", res.RequestId, "blocks", len(res.Blocks), "err", err)
	return nil
}

// NodeInfo represents a short summary of the `bsc` sub-protocol metadata
// known about the host peer.
type NodeInfo struct{}

// nodeInfo retrieves some `bsc` protocol metadata about the running host node.
func nodeInfo(_ *core.BlockChain) *NodeInfo {
	return &NodeInfo{}
}
