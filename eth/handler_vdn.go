package eth

import (
	"io"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/vdn"
	"github.com/libp2p/go-libp2p/core/peer"
)

type VDNHandler struct {
	chain  *core.BlockChain
	server *vdn.Server
}

func NewVDNHandler(chain *core.BlockChain, server *vdn.Server) *VDNHandler {
	h := &VDNHandler{
		chain:  chain,
		server: server,
	}
	h.registerMsgHandler()
	h.registerSubscription()
	h.registerMinedBlock()
	h.registerVoting()
	h.registerDirectTxs()
	h.registerPeerConnection()
	go h.broadcastContactInfoLoop()
	return h
}

func (h *VDNHandler) registerMsgHandler() {
	h.server.SetMsgHandler(vdn.V1BlockTopic, h.handleBlockMsg)
	h.server.SetMsgHandler(vdn.V1HandshakeTopic, h.handleHandshakeMsg)
	h.server.SetMsgHandler(vdn.V1VoteTopic, h.handleVoteMsg)
	h.server.SetMsgHandler(vdn.V1TransactionsTopic, h.handleTxsMsg)
	h.server.SetMsgHandler(vdn.V1ReqBlockByRangeTopic, h.handleBlockByRangeMsg)
}

func (h *VDNHandler) registerSubscription() {
	h.server.SubscribeToTopic(vdn.V1ContactInfoTopic)
	h.server.SubscribeToTopic(vdn.V1BlockTopic)
	h.server.SubscribeToTopic(vdn.V1VoteTopic)
}

// registerMinedBlock, it will receive a mined block event, and send it to nextN validators, and gossip too
func (h *VDNHandler) registerMinedBlock() {
}

// registerVoting, it will receive a voting event, and send it to nextN validators, and gossip too
func (h *VDNHandler) registerVoting() {

}

// registerVoting, it will receive a vdn peer connect event, and send handshake msg for verification
func (h *VDNHandler) registerPeerConnection() {

}

// broadcastContactInfoLoop, it will periodically send its contact info and also the cached contact info to the network.
// Once it connects to most nodes, it will reduce the sending frequency.
func (h *VDNHandler) broadcastContactInfoLoop() {

}

// registerDirectTxs, it will receive a direct txs sending event, and send it to nextN validators
// TODO(galaio): impl it later
func (h *VDNHandler) registerDirectTxs() {
}

func (h *VDNHandler) handleBlockMsg(from peer.ID, rw io.ReadWriter) error {
	var msg vdn.BlockMsg
	if err := vdn.DecodeFromStream(&msg, rw); err != nil {
		return err
	}

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

// TODO(galaio): impl it later
func (h *VDNHandler) handleVoteMsg(from peer.ID, rw io.ReadWriter) error {
	return nil
}

// TODO(galaio): impl it later
func (h *VDNHandler) handleHandshakeMsg(from peer.ID, rw io.ReadWriter) error {
	return nil
}

// TODO(galaio): impl it later
func (h *VDNHandler) handleTxsMsg(from peer.ID, rw io.ReadWriter) error {
	return nil
}

// TODO(galaio): impl it later
func (h *VDNHandler) handleBlockByRangeMsg(from peer.ID, rw io.ReadWriter) error {
	return nil
}
