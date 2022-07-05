package trust

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
)

// Handler is a callback to invoke from an outside runner after the boilerplate
// exchanges have passed.
type Handler func(peer *Peer) error

type Backend interface {
	// Chain retrieves the blockchain object to serve data.
	Chain() *core.BlockChain

	// RunPeer is invoked when a peer joins on the `eth` protocol. The handler
	// should do any peer maintenance work, handshakes and validations. If all
	// is passed, control should be given back to the `handler` to process the
	// inbound messages going forward.
	RunPeer(peer *Peer, handler Handler) error

	PeerInfo(id enode.ID) interface{}

	Handle(peer *Peer, packet Packet) error
}

// MakeProtocols constructs the P2P protocol definitions for `trust`.
func MakeProtocols(backend Backend, dnsdisc enode.Iterator) []p2p.Protocol {
	// Filter the discovery iterator for nodes advertising trust support.
	dnsdisc = enode.Filter(dnsdisc, func(n *enode.Node) bool {
		var trust enrEntry
		return n.Load(&trust) == nil
	})

	protocols := make([]p2p.Protocol, len(ProtocolVersions))
	for i, version := range ProtocolVersions {
		version := version // Closure

		protocols[i] = p2p.Protocol{
			Name:    ProtocolName,
			Version: version,
			Length:  protocolLengths[version],
			Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
				return backend.RunPeer(NewPeer(version, p, rw), func(peer *Peer) error {
					defer peer.Close()
					return Handle(backend, peer)
				})
			},
			NodeInfo: func() interface{} {
				return nodeInfo(backend.Chain())
			},
			PeerInfo: func(id enode.ID) interface{} {
				return backend.PeerInfo(id)
			},
			Attributes:     []enr.Entry{&enrEntry{}},
			DialCandidates: dnsdisc,
		}
	}
	return protocols
}

// Handle is the callback invoked to manage the life cycle of a `trust` peer.
// When this function terminates, the peer is disconnected.
func Handle(backend Backend, peer *Peer) error {
	for {
		if err := handleMessage(backend, peer); err != nil {
			peer.Log().Debug("Message handling failed in `trust`", "err", err)
			return err
		}
	}
}

// handleMessage is invoked whenever an inbound message is received from a
// remote peer on the `diff` protocol. The remote connection is torn down upon
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

	// Track the amount of time it takes to serve the request and run the handler
	if metrics.Enabled {
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
	// Handle the message depending on its contents
	switch {
	case msg.Code == RequestRootMsg:
		return handleRootRequest(backend, msg, peer)

	case msg.Code == RespondRootMsg:
		return handleRootResponse(backend, msg, peer)

	default:
		return fmt.Errorf("%w: %v", errInvalidMsgCode, msg.Code)
	}
}

type Decoder interface {
	Decode(val interface{}) error
	Time() time.Time
}

func handleRootRequest(backend Backend, msg Decoder, peer *Peer) error {
	req := new(RootRequestPacket)
	if err := msg.Decode(req); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}

	res := backend.Chain().GetVerifyResult(req.BlockNumber, req.BlockHash, req.DiffHash)
	return p2p.Send(peer.rw, RespondRootMsg, RootResponsePacket{
		RequestId:   req.RequestId,
		Status:      res.Status,
		BlockNumber: req.BlockNumber,
		BlockHash:   req.BlockHash,
		Root:        res.Root,
		Extra:       defaultExtra,
	})
}

func handleRootResponse(backend Backend, msg Decoder, peer *Peer) error {
	res := new(RootResponsePacket)
	if err := msg.Decode(res); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}

	requestTracker.Fulfil(peer.id, peer.version, RespondRootMsg, res.RequestId)
	return backend.Handle(peer, res)
}

// NodeInfo represents a short summary of the `trust` sub-protocol metadata
// known about the host peer.
type NodeInfo struct{}

// nodeInfo retrieves some `trust` protocol metadata about the running host node.
func nodeInfo(chain *core.BlockChain) *NodeInfo {
	return &NodeInfo{}
}
