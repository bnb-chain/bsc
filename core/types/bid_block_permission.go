package types

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type BidBlockPermissionStatus struct {
	Allowed   bool
	Reason    string
	BlockHash common.Hash
	BlockNum  uint64
	RevokedAt time.Time
	ResetAt   time.Time
}
