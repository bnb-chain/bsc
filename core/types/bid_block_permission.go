package types

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type BidBlockPermissionStatus struct {
	Allowed   bool
	Reason    string
	BlockHash common.Hash
	RevokedAt time.Time
	ResetAt   time.Time
}
