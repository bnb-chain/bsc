package vdn

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type TopicPubSub interface {
	JoinTopic(topic string, opts ...pubsub.TopicOpt) (*pubsub.Topic, error)
	LeaveTopic(topic string) error
	PublishToTopic(ctx context.Context, topic string, data []byte, opts ...pubsub.PubOpt) error
	SubscribeToTopic(topic string, opts ...pubsub.SubOpt) (*pubsub.Subscription, error)
}

// MsgSender abstracts the sending functionality from libp2p.
type MsgSender interface {
	Send(context.Context, interface{}, string, peer.ID) (network.Stream, error)
}

// StreamHandler configures p2p to handle streams of a certain topic ID.
type StreamHandler interface {
	SetStreamHandler(topic string, handler network.StreamHandler)
}
