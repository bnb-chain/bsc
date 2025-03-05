package vdn

import (
	"context"
	"io"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

type HandleMsgFn func(from peer.ID, rw io.ReadWriter) error
type HandleRespFn func(reader io.Reader) error

type TopicPubSub interface {
	JoinTopic(topic string, opts ...pubsub.TopicOpt) (*pubsub.Topic, error)
	LeaveTopic(topic string) error
	PublishToTopic(ctx context.Context, topic string, data []byte, opts ...pubsub.PubOpt) error
	SubscribeToTopic(topic string, opts ...pubsub.SubOpt) (*pubsub.Subscription, error)
}

// MsgSender abstracts the sending functionality from libp2p.
type MsgSender interface {
	Send(msg interface{}, topic string, peerID peer.ID, callback HandleRespFn) error
}

// MsgReceiver configures p2p to handle streams of a certain topic ID.
type MsgReceiver interface {
	SetMsgHandler(topic string, callback HandleMsgFn)
}
