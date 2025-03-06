package eth

import (
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/monitor"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum/go-ethereum/core/vote"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/vdn"
	"github.com/libp2p/go-libp2p/core/peer"
)

type VDNHandlerConfig struct {
	chain       *core.BlockChain
	server      *vdn.Server
	votePool    *vote.VotePool
	eventMux    *event.TypeMux
	checkSynced func() bool
}

func (c *VDNHandlerConfig) sanityCheck() error {
	if c.chain == nil {
		return fmt.Errorf("chain is nil")
	}
	if c.server == nil {
		return fmt.Errorf("server is nil")
	}
	if c.eventMux == nil {
		return fmt.Errorf("eventMux is nil")
	}
	return nil
}

type VDNHandler struct {
	cfg VDNHandlerConfig

	// block related
	minedBlockSub *event.TypeMuxSubscription

	// vote related
	votesSub             event.Subscription
	voteCh               chan core.NewVoteEvent
	maliciousVoteMonitor *monitor.MaliciousVoteMonitor
	voteMonitorCh        chan *types.VoteEnvelope

	stopCh chan struct{}
}

func NewVDNHandler(cfg VDNHandlerConfig) (*VDNHandler, error) {
	if err := cfg.sanityCheck(); err != nil {
		return nil, err
	}
	h := &VDNHandler{
		cfg: cfg,
	}

	// set vdn business logic handlers
	h.setRPCMsgHandler()
	h.setGossipMsgHandler()
	return h, nil
}

func (h *VDNHandler) Start() error {
	// subscribe chain & network events
	h.minedBlockSub = h.cfg.eventMux.Subscribe(core.NewMinedBlockEvent{})
	go h.broadcastMinedBlockLoop()
	if h.cfg.votePool != nil {
		h.voteCh = make(chan core.NewVoteEvent, voteChanSize)
		h.votesSub = h.cfg.votePool.SubscribeNewVoteEvent(h.voteCh)
		go h.broadcastVoteLoop()
		if h.maliciousVoteMonitor != nil {
			h.voteMonitorCh = make(chan *types.VoteEnvelope, voteChanSize)
			go h.monitorMaliciousVoteLoop()
		}
	}
	h.subDirectTxsEvent()
	h.subPeerConnectionEvent() // this is for handshake

	// start background task
	go h.broadcastContactInfoLoop()
	return nil
}

func (h *VDNHandler) Stop() error {
	if h.minedBlockSub != nil {
		h.minedBlockSub.Unsubscribe()
	}
	if h.votesSub != nil {
		h.votesSub.Unsubscribe()
	}

	close(h.stopCh)
	log.Info("VDN handler stopped")
	return nil
}

func (h *VDNHandler) setRPCMsgHandler() {
	h.cfg.server.SetMsgHandler(vdn.V1BlockTopic, h.handleRPCBlockMsg)
	h.cfg.server.SetMsgHandler(vdn.V1VoteTopic, h.handleRPCVoteMsg)
	h.cfg.server.SetMsgHandler(vdn.V1HandshakeTopic, h.handleRPCHandshakeMsg)
	h.cfg.server.SetMsgHandler(vdn.V1TransactionsTopic, h.handleRPCTxsMsg)
	h.cfg.server.SetMsgHandler(vdn.V1ReqBlockByRangeTopic, h.handleRPCBlockByRangeMsg)
}

func (h *VDNHandler) setGossipMsgHandler() {
	h.cfg.server.Subscribe(vdn.V1BlockTopic, h.handleGossipBlockMsg)
	h.cfg.server.Subscribe(vdn.V1VoteTopic, h.handleGossipVoteMsg)
	h.cfg.server.Subscribe(vdn.V1ContactInfoTopic, h.handleGossipContactInfoMsg)
}

// broadcastMinedBlockLoop, it will receive a mined block event, and send it to nextN validators, and gossip too
func (h *VDNHandler) broadcastMinedBlockLoop() {
	for {
		select {
		case obj := <-h.minedBlockSub.Chan():
			if obj == nil {
				continue
			}
			if ev, ok := obj.Data.(core.NewMinedBlockEvent); ok {
				h.sendNewBlock(ev.Block)
			}
		case <-h.stopCh:
			return
		}
	}
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
	if !h.cfg.checkSynced() {
		return nil
	}
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
	if !h.cfg.checkSynced() {
		return nil
	}
	var vote types.VoteEnvelope
	// TODO(galaio): reply response
	if err := vdn.DecodeFromStream(&vote, rw); err != nil {
		return err
	}

	h.cfg.votePool.PutVote(&vote)
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
	if !h.cfg.checkSynced() {
		return nil
	}
	var msg vdn.BlockMsg
	if err := vdn.DecodeFromStream(&msg, reader); err != nil {
		return err
	}
	return h.handleNewBlock(msg)
}

func (h *VDNHandler) handleGossipVoteMsg(from peer.ID, reader io.Reader) error {
	if !h.cfg.checkSynced() {
		return nil
	}
	var vote types.VoteEnvelope
	// TODO(galaio): reply response
	if err := vdn.DecodeFromStream(&vote, reader); err != nil {
		return err
	}

	// TODO(galaio): add validation here to score peer?
	h.cfg.votePool.PutVote(&vote)
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
	block := msg.Block
	block.ReceivedAt = time.Now()
	if msg.Sidecars != nil {
		block = block.WithSidecars(msg.Sidecars)
	}

	if block.Header().EmptyWithdrawalsHash() {
		block = block.WithWithdrawals(make([]*types.Withdrawal, 0))
	}

	chain := h.cfg.chain
	if err := chain.Engine().VerifyHeader(chain, block.Header()); err != nil {
		return err
	}
	if _, err := chain.InsertChain(types.Blocks{block}); err != nil {
		return err
	}
	return nil
}

func (h *VDNHandler) broadcastVoteLoop() {
	for {
		select {
		case event := <-h.voteCh:
			// The timeliness of votes is very important,
			// so one vote will be sent instantly without waiting for other votes for batch sending by design.
			h.sendNewVote(event.Vote)
			if h.voteMonitorCh != nil {
				h.voteMonitorCh <- event.Vote
			}
		case <-h.votesSub.Err():
			return
		case <-h.stopCh:
			return
		}
	}
}

func (h *VDNHandler) monitorMaliciousVoteLoop() {
	for {
		select {
		case vote := <-h.voteMonitorCh:
			pendingBlockNumber := h.cfg.chain.CurrentHeader().Number.Uint64() + 1
			h.maliciousVoteMonitor.ConflictDetect(vote, pendingBlockNumber)
		case <-h.stopCh:
			return
		}
	}
}

func (h *VDNHandler) sendNewBlock(block *types.Block) {
	// Calculate the TD of the block (it's not imported yet, so block.Td is not valid)
	var td *big.Int
	if parent := h.cfg.chain.GetBlock(block.ParentHash(), block.NumberU64()-1); parent != nil {
		td = new(big.Int).Add(block.Difficulty(), h.cfg.chain.GetTd(block.ParentHash(), block.NumberU64()-1))
	} else {
		log.Error("Propagating dangling block", "number", block.Number(), "hash", block.Hash())
		return
	}
	err := h.cfg.server.Publish(&vdn.BlockMsg{
		Block:      block,
		TD:         td,
		Sidecars:   block.Sidecars(),
		CreateTime: time.Now().UnixMilli(),
	}, vdn.V1BlockTopic)
	if err != nil {
		log.Warn("Failed to publish new block", "block", block.Number(), "hash", block.Hash(), "err", err)
	}
	// TODO(galaio): add direct block sending logic here
}

func (h *VDNHandler) sendNewVote(vote *types.VoteEnvelope) {
	err := h.cfg.server.Publish(&vdn.VoteMsg{
		Vote:       vote,
		CreateTime: time.Now().UnixMilli(),
	}, vdn.V1VoteTopic)
	if err != nil {
		log.Warn("Failed to publish new vote", "source", vote.Data.SourceNumber, "target", vote.Data.TargetNumber, "err", err)
	}
	// TODO(galaio): add direct vote sending logic here
}
