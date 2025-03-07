package vdn

const (
	HandshakeMsgSuffix       = "handshake"
	ContactInfoMsgSuffix     = "contact_info"
	BlockMsgSuffix           = "block"
	ReqBlockByRangeMsgSuffix = "req/block_by_range"
	VoteMsgSuffix            = "vote"
	TransactionsMsgSuffix    = "transactions"

	// V1TopicPrefix message format & validation is same as eth68/bsc1 in devp2p
	V1TopicPrefix          = "/bsc/vdn/v1/"
	V1HandshakeTopic       = V1TopicPrefix + HandshakeMsgSuffix
	V1ContactInfoTopic     = V1TopicPrefix + ContactInfoMsgSuffix
	V1BlockTopic           = V1TopicPrefix + BlockMsgSuffix
	V1ReqBlockByRangeTopic = V1TopicPrefix + ReqBlockByRangeMsgSuffix
	V1VoteTopic            = V1TopicPrefix + VoteMsgSuffix
	V1TransactionsTopic    = V1TopicPrefix + TransactionsMsgSuffix
)
