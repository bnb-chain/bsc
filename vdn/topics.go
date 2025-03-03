package vdn

const (
	HandshakeMessage       = "handshake"
	ContactInfoMessage     = "contact_info"
	BlockMessage           = "block"
	ReqBlockByRangeMessage = "req/block_by_range"
	VoteMessage            = "vote"
	TransactionsMessage    = "transactions"

	// V1TopicPrefix message format & validation is same as eth68/bsc1 in devp2p
	V1TopicPrefix          = "/bsc/vdn/v1/"
	V1HandshakeTopic       = V1TopicPrefix + HandshakeMessage
	V1ContactInfoTopic     = V1TopicPrefix + ContactInfoMessage
	V1BlockTopic           = V1TopicPrefix + BlockMessage
	V1ReqBlockByRangeTopic = V1TopicPrefix + ReqBlockByRangeMessage
	V1VoteTopic            = V1TopicPrefix + VoteMessage
	V1TransactionsTopic    = V1TopicPrefix + TransactionsMessage
)
