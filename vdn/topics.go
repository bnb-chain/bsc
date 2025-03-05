package vdn

const (
	BlockMessage = "block"
	VoteMessage  = "vote"

	// TopicPrefix v0's message format & validation is same as eth68/bsc1 in devp2p
	TopicPrefix = "/bsc/validator/v0/"
	BlockTopic  = TopicPrefix + BlockMessage
	VoteTopic   = TopicPrefix + VoteMessage
)
