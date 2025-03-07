package vdn

import (
	"bytes"
	"context"
	"time"

	"github.com/ethereum/go-ethereum/log"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
)

const (
	// Most of the parameters ref ETH 2.0 p2p spec
	gossipSubD   = 8  // topic stable mesh target count
	gossipSubDlo = 6  // topic stable mesh low watermark
	gossipSubDhi = 12 // topic stable mesh high watermark

	// gossip parameters
	gossipSubMcacheLen    = 6 // number of windows to retain full messages in cache for `IWANT` responses
	gossipSubMcacheGossip = 3 // number of windows to gossip about

	// heartbeat interval
	gossipSubHeartbeatInterval = 700 * time.Millisecond // frequency of heartbeat, milliseconds
)

// JoinTopic will join PubSub topic, if not already joined.
func (s *Server) JoinTopic(topic string, opts ...pubsub.TopicOpt) (*pubsub.Topic, error) {
	s.joinedTopicsLock.Lock()
	defer s.joinedTopicsLock.Unlock()

	if _, ok := s.joinedTopics[topic]; !ok {
		topicHandle, err := s.pubsub.Join(topic, opts...)
		if err != nil {
			return nil, err
		}
		s.joinedTopics[topic] = topicHandle
	}

	return s.joinedTopics[topic], nil
}

// LeaveTopic closes topic and removes corresponding handler from list of joined topics.
// This method will return error if there are outstanding event handlers or subscriptions.
func (s *Server) LeaveTopic(topic string) error {
	s.joinedTopicsLock.Lock()
	defer s.joinedTopicsLock.Unlock()

	if t, ok := s.joinedTopics[topic]; ok {
		if err := t.Close(); err != nil {
			return err
		}
		delete(s.joinedTopics, topic)
	}
	return nil
}

// PublishToTopic joins (if necessary) and publishes a message to a PubSub topic.
func (s *Server) PublishToTopic(ctx context.Context, topic string, data []byte, opts ...pubsub.PubOpt) error {
	topicHandle, err := s.JoinTopic(topic)
	if err != nil {
		return err
	}

	// Wait for at least 1 peer to be available to receive the published message.
	for {
		if len(topicHandle.ListPeers()) > 0 || s.cfg.MinimumSyncPeers == 0 {
			return topicHandle.Publish(ctx, data, opts...)
		}
		select {
		case <-ctx.Done():
			return errors.Wrapf(ctx.Err(), "unable to find requisite number of peers for topic %s, 0 peers found to publish to", topic)
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// SubscribeToTopic joins (if necessary) and subscribes to PubSub topic.
func (s *Server) SubscribeToTopic(topic string, opts ...pubsub.SubOpt) (*pubsub.Subscription, error) {
	topicHandle, err := s.JoinTopic(topic)
	if err != nil {
		return nil, err
	}
	scoringParams, err := s.topicScoreParams(topic)
	if err != nil {
		return nil, err
	}

	if scoringParams != nil {
		if err := topicHandle.SetScoreParams(scoringParams); err != nil {
			return nil, err
		}
	}
	return topicHandle.Subscribe(opts...)
}

func (s *Server) Subscribe(topic string, callback HandleSubscribeFn) error {
	sub, err := s.SubscribeToTopic(topic)
	if err != nil {
		return err
	}

	go s.gossipSubLoop(sub, callback)
	return nil
}

func (s *Server) Publish(msg interface{}, topic string) error {
	data, err := EncodeToBytes(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), ttfbTimeout)
	defer cancel()
	return s.PublishToTopic(ctx, topic, data)
}

func (s *Server) gossipSubLoop(sub *pubsub.Subscription, callback HandleSubscribeFn) {
	for {
		msg, err := sub.Next(s.ctx)
		if err != nil {
			// This should only happen when the context is cancelled or subscription is cancelled.
			if !errors.Is(err, pubsub.ErrSubscriptionCancelled) { // Only log a warning on unexpected errors.
				log.Debug("subscription next failed", "topic", sub.Topic(), "err", err)
			}
			// Cancel subscription in the event of an error, as we are
			// now exiting topic event loop.
			sub.Cancel()
			return
		}

		if msg.ReceivedFrom == s.peerID {
			continue
		}

		buf := bytes.NewBuffer(msg.Data)
		// TODO(galaio): check msg.ValidatorData, if add validator on libp2p
		if err = callback(msg.ReceivedFrom, buf); err != nil {
			//TODO(galaio): biz logic err, using it for peer scoring
			log.Debug("handle gossip msg err", "topic", sub.Topic(), "err", err)
		}
	}
}

// pubsubOptionss creates a list of options to configure our router with.
func (s *Server) pubsubOptions() []pubsub.Option {
	psOpts := []pubsub.Option{
		// enable msg signing & verification
		// Notice: Eth 2.0 uses no author, no sign & verify.
		pubsub.WithMessageAuthor(""),
		pubsub.WithMessageSignaturePolicy(pubsub.StrictSign),
		pubsub.WithPeerOutboundQueueSize(s.cfg.QueueSize),
		pubsub.WithMaxMessageSize(GossipMaxSize),
		pubsub.WithValidateQueueSize(s.cfg.QueueSize),
		pubsub.WithPeerScore(peerScoringParams()),
		pubsub.WithPeerScoreInspect(s.peerInspector, time.Minute),
		pubsub.WithGossipSubParams(pubsubGossipParam()),
		// TODO(galaio): optimize later
		//pubsub.WithSubscriptionFilter(s),
		//pubsub.WithRawTracer(gossipTracer{host: s.host}),
	}

	if len(s.cfg.StaticPeers) > 0 {
		directPeersAddrInfos, err := ParsePeersAddr(s.cfg.StaticPeers)
		if err != nil {
			log.Error("Could not add direct peer option", err)
			return psOpts
		}
		psOpts = append(psOpts, pubsub.WithDirectPeers(directPeersAddrInfos))
	}

	return psOpts
}

// creates a custom gossipsub parameter set.
func pubsubGossipParam() pubsub.GossipSubParams {
	gParams := pubsub.DefaultGossipSubParams()
	gParams.Dlo = gossipSubDlo
	gParams.D = gossipSubD
	gParams.Dhi = gossipSubDhi
	gParams.HeartbeatInterval = gossipSubHeartbeatInterval
	gParams.HistoryLength = gossipSubMcacheLen
	gParams.HistoryGossip = gossipSubMcacheGossip
	return gParams
}

// TODO(galaio): handle score inspect
func (s *Server) peerInspector(peerMap map[peer.ID]*pubsub.PeerScoreSnapshot) {
	// Iterate through all the connected peers and through any of their
	// relevant topics.
	//for pid, snap := range peerMap {
	//	s.peers.Scorers().GossipScorer().SetGossipData(pid, snap.Score,
	//		snap.BehaviourPenalty, convertTopicScores(snap.Topics))
	//}
}

// TODO(galaio): handle score inspect
// convert from libp2p's internal schema to a compatible prysm protobuf format.
//func convertTopicScores(topicMap map[string]*pubsub.TopicScoreSnapshot) map[string]*pbrpc.TopicScoreSnapshot {
//	newMap := make(map[string]*pbrpc.TopicScoreSnapshot, len(topicMap))
//	for t, s := range topicMap {
//		newMap[t] = &pbrpc.TopicScoreSnapshot{
//			TimeInMesh:               uint64(s.TimeInMesh.Milliseconds()),
//			FirstMessageDeliveries:   float32(s.FirstMessageDeliveries),
//			MeshMessageDeliveries:    float32(s.MeshMessageDeliveries),
//			InvalidMessageDeliveries: float32(s.InvalidMessageDeliveries),
//		}
//	}
//	return newMap
//}
