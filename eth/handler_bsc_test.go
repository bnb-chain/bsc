package eth

import (
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/bsc"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

type testBscHandler struct {
	voteBroadcasts event.Feed
}

func (h *testBscHandler) Chain() *core.BlockChain { panic("no backing chain") }
func (h *testBscHandler) RunPeer(peer *bsc.Peer, handler bsc.Handler) error {
	panic("not used in tests")
}
func (h *testBscHandler) PeerInfo(enode.ID) interface{} { panic("not used in tests") }
func (h *testBscHandler) Handle(peer *bsc.Peer, packet bsc.Packet) error {
	switch packet := packet.(type) {
	case *bsc.VotesPacket:
		h.voteBroadcasts.Send(packet.Votes)
		return nil

	default:
		panic(fmt.Sprintf("unexpected bsc packet type in tests: %T", packet))
	}
}

func TestSendVotes67(t *testing.T) { testSendVotes(t, eth.ETH67) }

func testSendVotes(t *testing.T, protocol uint) {
	t.Parallel()

	// Create a message handler and fill the pool with big votes
	handler := newTestHandler()
	defer handler.close()

	insert := make([]*types.VoteEnvelope, 100)
	for index := range insert {
		vote := types.VoteEnvelope{
			VoteAddress: types.BLSPublicKey{},
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: uint64(0),
				SourceHash:   common.BytesToHash(common.Hex2Bytes(string(rune(0)))),
				TargetNumber: uint64(index),
				TargetHash:   common.BytesToHash(common.Hex2Bytes(string(rune(index)))),
			},
		}
		insert[index] = &vote
		go handler.votepool.PutVote(&vote)
	}
	time.Sleep(250 * time.Millisecond) // Wait until vote events get out of the system (can't use events, vote broadcaster races with peer join)

	protos := []p2p.Protocol{
		{
			Name:    "eth",
			Version: eth.ETH66,
		},
		{
			Name:    "eth",
			Version: eth.ETH67,
		},
		{
			Name:    "bsc",
			Version: bsc.Bsc1,
		},
	}
	caps := []p2p.Cap{
		{
			Name:    "eth",
			Version: eth.ETH66,
		},
		{
			Name:    "eth",
			Version: eth.ETH67,
		},
		{
			Name:    "bsc",
			Version: bsc.Bsc1,
		},
	}

	// Create a source handler to send messages through and a sink peer to receive them
	p2pEthSrc, p2pEthSink := p2p.MsgPipe()
	defer p2pEthSrc.Close()
	defer p2pEthSink.Close()

	localEth := eth.NewPeer(protocol, p2p.NewPeerWithProtocols(enode.ID{1}, protos, "", caps), p2pEthSrc, nil)
	remoteEth := eth.NewPeer(protocol, p2p.NewPeerWithProtocols(enode.ID{2}, protos, "", caps), p2pEthSink, nil)
	defer localEth.Close()
	defer remoteEth.Close()

	p2pBscSrc, p2pBscSink := p2p.MsgPipe()
	defer p2pBscSrc.Close()
	defer p2pBscSink.Close()

	localBsc := bsc.NewPeer(bsc.Bsc1, p2p.NewPeerWithProtocols(enode.ID{1}, protos, "", caps), p2pBscSrc)
	remoteBsc := bsc.NewPeer(bsc.Bsc1, p2p.NewPeerWithProtocols(enode.ID{3}, protos, "", caps), p2pBscSink)
	defer localBsc.Close()
	defer remoteBsc.Close()

	go func(p *bsc.Peer) {
		(*bscHandler)(handler.handler).RunPeer(p, func(peer *bsc.Peer) error {
			return bsc.Handle((*bscHandler)(handler.handler), peer)
		})
	}(localBsc)

	time.Sleep(200 * time.Millisecond)
	remoteBsc.Handshake()

	time.Sleep(200 * time.Millisecond)
	go func(p *eth.Peer) {
		handler.handler.runEthPeer(p, func(peer *eth.Peer) error {
			return eth.Handle((*ethHandler)(handler.handler), peer)
		})
	}(localEth)

	// Run the handshake locally to avoid spinning up a source handler
	var (
		genesis = handler.chain.Genesis()
		head    = handler.chain.CurrentBlock()
		td      = handler.chain.GetTd(head.Hash(), head.Number.Uint64())
	)
	time.Sleep(200 * time.Millisecond)
	if err := remoteEth.Handshake(1, td, head.Hash(), genesis.Hash(), forkid.NewIDWithChain(handler.chain), forkid.NewFilter(handler.chain), nil); err != nil {
		t.Fatalf("failed to run protocol handshake: %d", err)
	}
	// After the handshake completes, the source handler should stream the sink
	// the votes, subscribe to all inbound network events
	backend := new(testBscHandler)
	bcasts := make(chan []*types.VoteEnvelope)
	bcastSub := backend.voteBroadcasts.Subscribe(bcasts)
	defer bcastSub.Unsubscribe()

	go bsc.Handle(backend, remoteBsc)

	// Make sure we get all the votes on the correct channels
	seen := make(map[common.Hash]struct{})
	for len(seen) < len(insert) {
		votes := <-bcasts
		for _, vote := range votes {
			if _, ok := seen[vote.Hash()]; ok {
				t.Errorf("duplicate vote broadcast: %x", vote.Hash())
			}
			seen[vote.Hash()] = struct{}{}
		}
	}
	for _, vote := range insert {
		if _, ok := seen[vote.Hash()]; !ok {
			t.Errorf("missing vote: %x", vote.Hash())
		}
	}
}

func TestRecvVotes67(t *testing.T) { testRecvVotes(t, eth.ETH67) }

func testRecvVotes(t *testing.T, protocol uint) {
	t.Parallel()

	// Create a message handler and fill the pool with big votes
	handler := newTestHandler()
	defer handler.close()

	protos := []p2p.Protocol{
		{
			Name:    "eth",
			Version: eth.ETH66,
		},
		{
			Name:    "eth",
			Version: eth.ETH67,
		},
		{
			Name:    "bsc",
			Version: bsc.Bsc1,
		},
	}
	caps := []p2p.Cap{
		{
			Name:    "eth",
			Version: eth.ETH66,
		},
		{
			Name:    "eth",
			Version: eth.ETH67,
		},
		{
			Name:    "bsc",
			Version: bsc.Bsc1,
		},
	}

	// Create a source handler to send messages through and a sink peer to receive them
	p2pEthSrc, p2pEthSink := p2p.MsgPipe()
	defer p2pEthSrc.Close()
	defer p2pEthSink.Close()

	localEth := eth.NewPeer(protocol, p2p.NewPeerWithProtocols(enode.ID{1}, protos, "", caps), p2pEthSrc, nil)
	remoteEth := eth.NewPeer(protocol, p2p.NewPeerWithProtocols(enode.ID{2}, protos, "", caps), p2pEthSink, nil)
	defer localEth.Close()
	defer remoteEth.Close()

	p2pBscSrc, p2pBscSink := p2p.MsgPipe()
	defer p2pBscSrc.Close()
	defer p2pBscSink.Close()

	localBsc := bsc.NewPeer(bsc.Bsc1, p2p.NewPeerWithProtocols(enode.ID{1}, protos, "", caps), p2pBscSrc)
	remoteBsc := bsc.NewPeer(bsc.Bsc1, p2p.NewPeerWithProtocols(enode.ID{3}, protos, "", caps), p2pBscSink)
	defer localBsc.Close()
	defer remoteBsc.Close()

	go func(p *bsc.Peer) {
		(*bscHandler)(handler.handler).RunPeer(p, func(peer *bsc.Peer) error {
			return bsc.Handle((*bscHandler)(handler.handler), peer)
		})
	}(localBsc)

	time.Sleep(200 * time.Millisecond)
	remoteBsc.Handshake()

	time.Sleep(200 * time.Millisecond)
	go func(p *eth.Peer) {
		handler.handler.runEthPeer(p, func(peer *eth.Peer) error {
			return eth.Handle((*ethHandler)(handler.handler), peer)
		})
	}(localEth)

	// Run the handshake locally to avoid spinning up a source handler
	var (
		genesis = handler.chain.Genesis()
		head    = handler.chain.CurrentBlock()
		td      = handler.chain.GetTd(head.Hash(), head.Number.Uint64())
	)
	time.Sleep(200 * time.Millisecond)
	if err := remoteEth.Handshake(1, td, head.Hash(), genesis.Hash(), forkid.NewIDWithChain(handler.chain), forkid.NewFilter(handler.chain), nil); err != nil {
		t.Fatalf("failed to run protocol handshake: %d", err)
	}

	votesCh := make(chan core.NewVoteEvent)
	sub := handler.votepool.SubscribeNewVoteEvent(votesCh)
	defer sub.Unsubscribe()
	// Send the vote to the sink and verify that it's added to the vote pool
	vote := types.VoteEnvelope{
		VoteAddress: types.BLSPublicKey{},
		Signature:   types.BLSSignature{},
		Data: &types.VoteData{
			SourceNumber: uint64(0),
			SourceHash:   common.BytesToHash(common.Hex2Bytes(string(rune(0)))),
			TargetNumber: uint64(1),
			TargetHash:   common.BytesToHash(common.Hex2Bytes(string(rune(1)))),
		},
	}

	remoteBsc.AsyncSendVotes([]*types.VoteEnvelope{&vote})
	time.Sleep(100 * time.Millisecond)
	select {
	case event := <-votesCh:
		if event.Vote.Hash() != vote.Hash() {
			t.Errorf("added wrong vote hash: got %v, want %v", event.Vote.Hash(), vote.Hash())
		}
	case <-time.After(2 * time.Second):
		t.Errorf("no NewVotesEvent received within 2 seconds")
	}
}
