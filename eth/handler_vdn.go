package eth

import (
	"io"

	"github.com/ethereum/go-ethereum/core/vote"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/vdn"
	"github.com/libp2p/go-libp2p/core/peer"
)

type VDNHandler struct {
	chain    *core.BlockChain
	server   *vdn.Server
	votePool *vote.VotePool
}

func NewVDNHandler(chain *core.BlockChain, server *vdn.Server, votePool *vote.VotePool) *VDNHandler {
	h := &VDNHandler{
		chain:    chain,
		server:   server,
		votePool: votePool,
	}

	// set vdn business logic handlers
	h.setRPCMsgHandler()
	h.setGossipMsgHandler()

	// subscribe chain & network events
	h.subMinedBlockEvent()
	h.subVotingEvent()
	h.subDirectTxsEvent()
	h.subPeerConnectionEvent()

	// start background task
	go h.broadcastContactInfoLoop()
	return h
}

func (h *VDNHandler) setRPCMsgHandler() {
	h.server.SetMsgHandler(vdn.V1BlockTopic, h.handleRPCBlockMsg)
	h.server.SetMsgHandler(vdn.V1VoteTopic, h.handleRPCVoteMsg)
	h.server.SetMsgHandler(vdn.V1HandshakeTopic, h.handleRPCHandshakeMsg)
	h.server.SetMsgHandler(vdn.V1TransactionsTopic, h.handleRPCTxsMsg)
	h.server.SetMsgHandler(vdn.V1ReqBlockByRangeTopic, h.handleRPCBlockByRangeMsg)
}

func (h *VDNHandler) setGossipMsgHandler() {
	h.server.Subscribe(vdn.V1BlockTopic, h.handleGossipBlockMsg)
	h.server.Subscribe(vdn.V1VoteTopic, h.handleGossipVoteMsg)
	h.server.Subscribe(vdn.V1ContactInfoTopic, h.handleGossipContactInfoMsg)
}

// subMinedBlockEvent, it will receive a mined block event, and send it to nextN validators, and gossip too
func (h *VDNHandler) subMinedBlockEvent() {
}

// subVotingEvent, it will receive a voting event, and send it to nextN validators, and gossip too
func (h *VDNHandler) subVotingEvent() {

}

// subPeerConnectionEvent, it will receive a vdn peer connect event, and send handshake msg for verification
// TODO(galaio): impl it later
func (h *VDNHandler) subPeerConnectionEvent() {

}

// broadcastContactInfoLoop, it will periodically send its contact info and also the cached contact info to the network.
// Once it connects to most nodes, it will reduce the sending frequency.
// TODO(galaio): impl it later
func (h *VDNHandler) broadcastContactInfoLoop() {

}

// subDirectTxsEvent, it will receive a direct txs sending event, and send it to nextN validators
// TODO(galaio): impl it later
func (h *VDNHandler) subDirectTxsEvent() {
}

func (h *VDNHandler) handleRPCBlockMsg(from peer.ID, rw io.ReadWriter) error {
	var msg vdn.BlockMsg
	if err := vdn.DecodeFromStream(&msg, rw); err != nil {
		return err
	}

	if err := h.handleNewBlock(msg); err != nil {
		// TODO(galaio): reply err response
		return err
	}

	// TODO(galaio): reply ok response
	return nil
}

// TODO(galaio): impl it later
func (h *VDNHandler) handleRPCVoteMsg(from peer.ID, rw io.ReadWriter) error {
	var vote types.VoteEnvelope
	// TODO(galaio): reply response
	if err := vdn.DecodeFromStream(&vote, rw); err != nil {
		return err
	}

	h.votePool.PutVote(&vote)
	return nil
}

// TODO(galaio): impl it later
func (h *VDNHandler) handleRPCHandshakeMsg(from peer.ID, rw io.ReadWriter) error {
	return nil
}

// TODO(galaio): impl it later
func (h *VDNHandler) handleRPCTxsMsg(from peer.ID, rw io.ReadWriter) error {
	return nil
}

// TODO(galaio): impl it later
func (h *VDNHandler) handleRPCBlockByRangeMsg(from peer.ID, rw io.ReadWriter) error {
	return nil
}

func (h *VDNHandler) handleGossipBlockMsg(from peer.ID, reader io.Reader) error {
	var msg vdn.BlockMsg
	if err := vdn.DecodeFromStream(&msg, reader); err != nil {
		return err
	}
	return h.handleNewBlock(msg)
}

func (h *VDNHandler) handleGossipVoteMsg(from peer.ID, reader io.Reader) error {
	var vote types.VoteEnvelope
	// TODO(galaio): reply response
	if err := vdn.DecodeFromStream(&vote, reader); err != nil {
		return err
	}

	h.votePool.PutVote(&vote)
	return nil
}

// TODO(galaio): impl it later
func (h *VDNHandler) handleGossipContactInfoMsg(from peer.ID, reader io.Reader) error {
	return nil
}

func (h *VDNHandler) handleNewBlock(msg vdn.BlockMsg) error {
	if err := msg.SanityCheck(); err != nil {
		return err
	}

	// TODO(galaio): if we get a future block, we need to fetch missing block immediately
	msg.Block.WithSidecars(msg.Sidecars)
	if _, err := h.chain.InsertChain(types.Blocks{msg.Block}); err != nil {
		return err
	}
	return nil
}
