package diff

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// softResponseLimit is the target maximum size of replies to data retrievals.
	softResponseLimit = 2 * 1024 * 1024

	// maxDiffLayerServe is the maximum number of diff layers to serve.
	maxDiffLayerServe = 128
)

var requestTracker = NewTracker(time.Minute)

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

// MakeProtocols constructs the P2P protocol definitions for `diff`.
func MakeProtocols(backend Backend, dnsdisc enode.Iterator) []p2p.Protocol {
	// Filter the discovery iterator for nodes advertising diff support.
	dnsdisc = enode.Filter(dnsdisc, func(n *enode.Node) bool {
		var diff enrEntry
		return n.Load(&diff) == nil
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

// Handle is the callback invoked to manage the life cycle of a `diff` peer.
// When this function terminates, the peer is disconnected.
func Handle(backend Backend, peer *Peer) error {
	for {
		if err := handleMessage(backend, peer); err != nil {
			peer.Log().Debug("Message handling failed in `diff`", "err", err)
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
	start := time.Now()
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
		}(start)
	}
	// Handle the message depending on its contents
	switch {
	case msg.Code == GetDiffLayerMsg:
		res := new(GetDiffLayersPacket)
		if err := msg.Decode(res); err != nil {
			return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
		}
		diffs := answerDiffLayersQuery(backend, res)

		p2p.Send(peer.rw, FullDiffLayerMsg, &FullDiffLayersPacket{
			RequestId:        res.RequestId,
			DiffLayersPacket: diffs,
		})
		return nil

	case msg.Code == DiffLayerMsg:
		// A batch of trie nodes arrived to one of our previous requests
		res := new(DiffLayersPacket)
		if err := msg.Decode(res); err != nil {
			return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
		}
		return backend.Handle(peer, res)
	case msg.Code == FullDiffLayerMsg:
		// A batch of trie nodes arrived to one of our previous requests
		res := new(FullDiffLayersPacket)
		if err := msg.Decode(res); err != nil {
			return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
		}
		if fulfilled := requestTracker.Fulfil(peer.id, peer.version, FullDiffLayerMsg, res.RequestId); fulfilled {
			return backend.Handle(peer, res)
		}
		return fmt.Errorf("%w: %v", errUnexpectedMsg, msg.Code)
	default:
		return fmt.Errorf("%w: %v", errInvalidMsgCode, msg.Code)
	}
}

func answerDiffLayersQuery(backend Backend, query *GetDiffLayersPacket) []rlp.RawValue {
	// Gather blocks until the fetch or network limits is reached
	var (
		bytes      int
		diffLayers []rlp.RawValue
	)
	// Need avoid transfer huge package
	for lookups, hash := range query.BlockHashes {
		if bytes >= softResponseLimit || len(diffLayers) >= maxDiffLayerServe ||
			lookups >= 2*maxDiffLayerServe {
			break
		}
		if data := backend.Chain().GetDiffLayerRLP(hash); len(data) != 0 {
			diffLayers = append(diffLayers, data)
			bytes += len(data)
		}
	}
	return diffLayers
}

// NodeInfo represents a short summary of the `diff` sub-protocol metadata
// known about the host peer.
type NodeInfo struct{}

// nodeInfo retrieves some `diff` protocol metadata about the running host node.
func nodeInfo(_ *core.BlockChain) *NodeInfo {
	return &NodeInfo{}
}
